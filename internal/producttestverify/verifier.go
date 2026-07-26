package producttestverify

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/glade-sh/glade/tools/internal/apexrules"
)

type Finding struct {
	RuleID      string
	ProductTest string
	Message     string
}

type productTestRef struct {
	path     string
	testName string
	subcase  string
}

func Verify(catalog apexrules.Catalog, gladeRoot string) []Finding {
	var findings []Finding
	if !apexrules.IsFullGitSHA(catalog.GladeCommit) {
		findings = append(findings, Finding{
			RuleID:  "catalog",
			Message: "catalog gladeCommit must be a full 40-character Git SHA",
		})
	}
	refs := make(map[string]productTestRef, len(catalog.Rules))
	rulesByID := make(map[string]apexrules.Rule, len(catalog.Rules))
	baseRefs := map[string]map[string]bool{}
	productTestUsers := map[string][]apexrules.Rule{}

	for _, rule := range catalog.Rules {
		rulesByID[rule.ID] = rule
		if strings.TrimSpace(rule.ProductTest) == "" {
			if rule.Status == apexrules.StatusSupported {
				findings = append(findings, Finding{RuleID: rule.ID, Message: "supported rule has no product test"})
			}
			continue
		}
		ref, err := parseProductTestRef(rule.ProductTest)
		if err != nil {
			findings = append(findings, Finding{RuleID: rule.ID, ProductTest: rule.ProductTest, Message: err.Error()})
			continue
		}
		refs[rule.ID] = ref
		base := ref.path + ":" + ref.testName
		if baseRefs[base] == nil {
			baseRefs[base] = map[string]bool{}
		}
		baseRefs[base][rule.ProductTest] = true
		productTestUsers[rule.ProductTest] = append(productTestUsers[rule.ProductTest], rule)
	}

	for productTest, users := range productTestUsers {
		if len(users) < 2 {
			if len(users) == 1 && users[0].ProductTestAliasOf != "" {
				findings = append(findings, invalidAliasFinding(users[0], productTest, rulesByID))
			}
			continue
		}
		var canonical []apexrules.Rule
		for _, rule := range users {
			if rule.ProductTestAliasOf == "" {
				canonical = append(canonical, rule)
			}
		}
		if len(canonical) != 1 {
			for _, rule := range canonical {
				findings = append(findings, Finding{
					RuleID:      rule.ID,
					ProductTest: productTest,
					Message:     "duplicate product test requires productTestAliasOf",
				})
			}
		}
		for _, rule := range users {
			if rule.ProductTestAliasOf == "" {
				continue
			}
			target, ok := rulesByID[rule.ProductTestAliasOf]
			if !ok || target.ProductTest != productTest || target.ProductTestAliasOf != "" {
				findings = append(findings, invalidAliasFinding(rule, productTest, rulesByID))
			}
		}
	}

	files := map[string]*ast.File{}
	for _, rule := range catalog.Rules {
		ref, ok := refs[rule.ID]
		if !ok {
			continue
		}
		base := ref.path + ":" + ref.testName
		if len(baseRefs[base]) > 1 && ref.subcase == "" {
			findings = append(findings, Finding{
				RuleID:      rule.ID,
				ProductTest: rule.ProductTest,
				Message:     "shared product test requires an explicit /subcase",
			})
		}
		fullPath := filepath.Join(gladeRoot, filepath.FromSlash(ref.path))
		file := files[fullPath]
		if file == nil {
			parsed, err := parser.ParseFile(token.NewFileSet(), fullPath, nil, 0)
			if err != nil {
				message := fmt.Sprintf("parse product test file: %v", err)
				if os.IsNotExist(err) {
					message = "product test file does not exist"
				}
				findings = append(findings, Finding{RuleID: rule.ID, ProductTest: rule.ProductTest, Message: message})
				continue
			}
			file = parsed
			files[fullPath] = file
		}
		fn := findTestFunction(file, ref.testName)
		if fn == nil {
			findings = append(findings, Finding{
				RuleID:      rule.ID,
				ProductTest: rule.ProductTest,
				Message:     fmt.Sprintf("test %q was not found", ref.testName),
			})
			continue
		}
		if ref.subcase != "" && !functionContainsSubcase(fn, ref.subcase) {
			findings = append(findings, Finding{
				RuleID:      rule.ID,
				ProductTest: rule.ProductTest,
				Message:     fmt.Sprintf("subcase %q was not found in %s", ref.subcase, ref.testName),
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].RuleID != findings[j].RuleID {
			return findings[i].RuleID < findings[j].RuleID
		}
		if findings[i].ProductTest != findings[j].ProductTest {
			return findings[i].ProductTest < findings[j].ProductTest
		}
		return findings[i].Message < findings[j].Message
	})
	return findings
}

func invalidAliasFinding(rule apexrules.Rule, productTest string, rulesByID map[string]apexrules.Rule) Finding {
	message := "productTestAliasOf must name a canonical rule with the same product test"
	if _, ok := rulesByID[rule.ProductTestAliasOf]; !ok {
		message = "productTestAliasOf target does not exist"
	}
	return Finding{RuleID: rule.ID, ProductTest: productTest, Message: message}
}

func FormatFindings(findings []Finding) string {
	var out strings.Builder
	for _, finding := range findings {
		fmt.Fprintf(&out, "%s: %s", finding.RuleID, finding.Message)
		if finding.ProductTest != "" {
			fmt.Fprintf(&out, " (%s)", finding.ProductTest)
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func parseProductTestRef(value string) (productTestRef, error) {
	value = strings.TrimSpace(value)
	index := strings.LastIndex(value, ":")
	if index <= 0 || index == len(value)-1 {
		return productTestRef{}, fmt.Errorf("productTest must use path:TestName[/subcase]")
	}
	path := value[:index]
	testAndSubcase := value[index+1:]
	testName, subcase, _ := strings.Cut(testAndSubcase, "/")
	if filepath.IsAbs(path) || filepath.Clean(path) != path || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return productTestRef{}, fmt.Errorf("unsafe product test path")
	}
	if filepath.Ext(path) != ".go" || !strings.HasSuffix(path, "_test.go") {
		return productTestRef{}, fmt.Errorf("product test path must name a Go _test.go file")
	}
	if !strings.HasPrefix(testName, "Test") {
		return productTestRef{}, fmt.Errorf("product test function must start with Test")
	}
	if subcase != "" && strings.TrimSpace(subcase) != subcase {
		return productTestRef{}, fmt.Errorf("product test subcase has surrounding whitespace")
	}
	return productTestRef{path: path, testName: testName, subcase: subcase}, nil
}

func findTestFunction(file *ast.File, name string) *ast.FuncDecl {
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

func functionContainsSubcase(fn *ast.FuncDecl, subcase string) bool {
	return nodeContainsSubcase(fn.Body, subcase)
}

func nodeContainsSubcase(root ast.Node, subcase string) bool {
	found := false
	ast.Inspect(root, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}
		if value == subcase {
			found = true
			return false
		}
		if !strings.ContainsAny(subcase, " \t\r\n") {
			for _, field := range strings.FieldsFunc(value, func(char rune) bool {
				return !isSubcaseTokenChar(char)
			}) {
				if field == subcase {
					found = true
					return false
				}
			}
		}
		return true
	})
	return found
}

func isSubcaseTokenChar(char rune) bool {
	return char >= 'a' && char <= 'z' ||
		char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9' ||
		char == '_' || char == '-' || char == '.'
}
