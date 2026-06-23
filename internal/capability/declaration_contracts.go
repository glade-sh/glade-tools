package capability

import (
	"encoding/json"
	"io"
	"sort"
	"strings"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
)

const DeclarationContractsSchemaVersion = 1

type DeclarationContracts struct {
	SchemaVersion int                           `json:"schemaVersion"`
	Documents     []DeclarationDocumentContract `json:"documents"`
}

type DeclarationDocumentContract struct {
	SourcePath string                      `json:"sourcePath"`
	Kind       string                      `json:"kind"`
	Namespace  string                      `json:"namespace,omitempty"`
	Name       string                      `json:"name"`
	Members    []DeclarationMemberContract `json:"members,omitempty"`
}

type DeclarationMemberContract struct {
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Signature    string   `json:"signature,omitempty"`
	PropertyType string   `json:"propertyType,omitempty"`
	ReturnType   string   `json:"returnType,omitempty"`
	Parameters   []string `json:"parameters,omitempty"`
	Static       bool     `json:"static"`
}

func BuildDeclarationContracts(inv apexdocs.Inventory) DeclarationContracts {
	out := DeclarationContracts{SchemaVersion: DeclarationContractsSchemaVersion}
	for _, doc := range inv.Documents {
		if !isApexDeclarationDocument(doc) {
			continue
		}
		contract := DeclarationDocumentContract{
			SourcePath: doc.SourcePath,
			Kind:       doc.Kind,
			Namespace:  doc.Namespace,
			Name:       declarationDocumentName(doc),
		}
		for _, member := range doc.Members {
			if !isDeclarationMember(member) {
				continue
			}
			name := declarationMemberName(member)
			propertyType := member.PropertyType
			if propertyType == "" && member.Kind == "property" {
				propertyType = propertyTypeFromSignature(member.Signature, name)
			}
			contract.Members = append(contract.Members, DeclarationMemberContract{
				Kind:         member.Kind,
				Name:         name,
				Signature:    member.Signature,
				PropertyType: propertyType,
				ReturnType:   member.ReturnType,
				Parameters:   append([]string(nil), member.Parameters...),
				Static:       isStaticDeclarationMember(doc, member, name),
			})
		}
		sort.SliceStable(contract.Members, func(i, j int) bool {
			a, b := contract.Members[i], contract.Members[j]
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			if strings.Join(a.Parameters, ",") != strings.Join(b.Parameters, ",") {
				return strings.Join(a.Parameters, ",") < strings.Join(b.Parameters, ",")
			}
			return a.Signature < b.Signature
		})
		out.Documents = append(out.Documents, contract)
	}
	sort.SliceStable(out.Documents, func(i, j int) bool {
		return out.Documents[i].SourcePath < out.Documents[j].SourcePath
	})
	return out
}

func WriteDeclarationContractsJSON(w io.Writer, contracts DeclarationContracts) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(contracts)
}

func isApexDeclarationDocument(doc apexdocs.Document) bool {
	if doc.SourcePath == "" || doc.Name == "" {
		return false
	}
	if !strings.HasPrefix(doc.SourcePath, "apex/") && !strings.HasPrefix(doc.SourcePath, "apex_") {
		return false
	}
	switch doc.Kind {
	case "class", "interface", "enum", "input", "output":
		return true
	case "document":
		return doc.Namespace != "" && len(doc.Members) > 0
	default:
		return false
	}
}

func isDeclarationMember(member apexdocs.Member) bool {
	switch member.Kind {
	case "constructor", "method", "property":
		return member.Name != ""
	default:
		return false
	}
}

func declarationMemberName(member apexdocs.Member) string {
	return cleanDeclarationName(member.Name)
}

func declarationDocumentName(doc apexdocs.Document) string {
	name := cleanDeclarationName(doc.Name)
	if doc.Namespace != "" {
		name = strings.TrimPrefix(name, doc.Namespace+".")
	}
	return name
}

func cleanDeclarationName(name string) string {
	replacer := strings.NewReplacer(
		`\_`, "_",
		"\u200b", "",
		"\u200c", "",
		"\u200d", "",
		"\ufeff", "",
	)
	return replacer.Replace(name)
}

func propertyTypeFromSignature(signature, name string) string {
	signature = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.Split(signature, "{")[0]), ";"))
	fields := strings.Fields(signature)
	if len(fields) < 2 {
		return ""
	}
	if fields[len(fields)-1] != name {
		return ""
	}
	fields = dropDeclarationModifiers(fields[:len(fields)-1])
	if len(fields) == 0 {
		return ""
	}
	return apexdocs.NormalizeApexDocType(fields[len(fields)-1])
}

func dropDeclarationModifiers(fields []string) []string {
	for len(fields) > 0 {
		switch strings.ToLower(fields[0]) {
		case "public", "private", "protected", "global", "webservice", "static", "virtual", "override", "abstract", "testmethod":
			fields = fields[1:]
		default:
			return fields
		}
	}
	return fields
}

func isStaticDeclarationMember(doc apexdocs.Document, member apexdocs.Member, name string) bool {
	if signatureHasStatic(member.Signature) {
		return true
	}
	if member.Kind == "property" && constantLikePropertyName(name) {
		return true
	}
	if strings.Contains(strings.ToLower(member.Section), "static") {
		return true
	}
	return strings.Contains(strings.ToLower(doc.SourcePath), "_static_methods")
}

func signatureHasStatic(signature string) bool {
	for _, field := range strings.Fields(signature) {
		field = strings.Trim(field, "`(){};,")
		if strings.EqualFold(field, "static") {
			return true
		}
	}
	return false
}

func constantLikePropertyName(name string) bool {
	hasLetter := false
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return hasLetter
}
