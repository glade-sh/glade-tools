package surfaceledger

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CorpusUsage holds deterministic aggregated usage across labeled source roots.
type CorpusUsage struct {
	PublicRootSHA256     string             `json:"publicRootSha256,omitempty"`
	PublicFailRootSHA256 string             `json:"publicFailRootSha256,omitempty"`
	PrivateRootSHA256    string             `json:"privateRootSha256,omitempty"`
	Usage                []CorpusUsageEntry `json:"usage"`
}

// CorpusUsageEntry records counts for one namespace/type/member key.
type CorpusUsageEntry struct {
	UsageKey         string `json:"usageKey"`
	Namespace        string `json:"namespace"`
	TypeName         string `json:"typeName,omitempty"`
	MemberName       string `json:"memberName,omitempty"`
	PubProdRefs      int    `json:"pubProdRefs"`
	PubTestRefs      int    `json:"pubTestRefs"`
	PubFailRefs      int    `json:"pubFailRefs"`
	PrivProdRefs     int    `json:"privProdRefs"`
	PrivTestRefs     int    `json:"privTestRefs"`
	PubProdFiles     int    `json:"pubProdFiles"`
	PubTestFiles     int    `json:"pubTestFiles"`
	PubFailFiles     int    `json:"pubFailFiles"`
	PrivProdFiles    int    `json:"privProdFiles"`
	PrivTestFiles    int    `json:"privTestFiles"`
	PubProdProjects  int    `json:"pubProdProjects"`
	PubTestProjects  int    `json:"pubTestProjects"`
	PubFailProjects  int    `json:"pubFailProjects"`
	PrivProdProjects int    `json:"privProdProjects"`
	PrivTestProjects int    `json:"privTestProjects"`
}

type corpusRootLabel int

const (
	labelPublicSuccess corpusRootLabel = iota
	labelPublicFail
	labelPrivateSuccess
)

type corpusScanner struct {
	namespaces map[string]string // lowercase → canonical namespace from ledger
}

// BuildCorpusUsage scans Apex source under labeled roots and returns
// aggregate usage counts for every namespace/type/member derived from the ledger.
func BuildCorpusUsage(ledgerRows []SurfaceLedgerRow, publicRoot, publicFailRoot, privateRoot string) (CorpusUsage, error) {
	// Derive candidate namespaces from Apex rows.
	// Map lowercase → canonical for case-insensitive matching.
	namespaces := make(map[string]string)
	for _, row := range ledgerRows {
		if row.Product == ProductApex && row.Namespace != "" {
			namespaces[strings.ToLower(row.Namespace)] = row.Namespace
		}
	}
	if len(namespaces) == 0 {
		return CorpusUsage{}, fmt.Errorf("no Apex namespaces in ledger")
	}

	scanner := &corpusScanner{namespaces: namespaces}

	roots := []struct {
		path  string
		label corpusRootLabel
	}{
		{publicRoot, labelPublicSuccess},
		{publicFailRoot, labelPublicFail},
		{privateRoot, labelPrivateSuccess},
	}

	// Validate every non-empty root before scanning.
	for _, r := range roots {
		if r.path == "" {
			continue
		}
		info, err := os.Stat(r.path)
		if err != nil {
			if os.IsNotExist(err) {
				return CorpusUsage{}, fmt.Errorf("public root does not exist: %s", r.path)
			}
			return CorpusUsage{}, fmt.Errorf("cannot access public root %s: %w", r.path, err)
		}
		if !info.IsDir() {
			return CorpusUsage{}, fmt.Errorf("public root is not a directory: %s", r.path)
		}
		files, err := scanApexFiles(r.path)
		if err != nil {
			return CorpusUsage{}, fmt.Errorf("scan root %s: %w", r.path, err)
		}
		if len(files) == 0 {
			return CorpusUsage{}, fmt.Errorf("no eligible Apex projects found under root %s", r.path)
		}
	}

	// Single scan pass with accurate file and project deduplication.
	acc := make(map[string]*CorpusUsageEntry)
	for _, r := range roots {
		if r.path == "" {
			continue
		}
		files, err := scanApexFiles(r.path)
		if err != nil {
			return CorpusUsage{}, fmt.Errorf("scan root %s: %w", r.path, err)
		}
		if len(files) == 0 {
			continue
		}
		projectFiles := make(map[string][]apexFile)
		for _, f := range files {
			projectFiles[f.project] = append(projectFiles[f.project], f)
		}

		for _, pFiles := range projectFiles {
			localTypes := make(map[string]bool)
			for _, f := range pFiles {
				if strings.HasSuffix(strings.ToLower(f.name), ".cls") {
					typeName := f.name[:len(f.name)-4]
					localTypes[strings.ToLower(typeName)] = true
				}
			}

			// Track project usage per file category so a project that has
			// both production and test files can contribute to both counts.
			projUsedByCat := make(map[string]map[string]bool)

			for _, f := range pFiles {
				cat := categoryForFile(f)
				if cat == "" {
					continue
				}

				content, err := os.ReadFile(f.absPath)
				if err != nil {
					continue
				}
				stripped := stripCommentsAndStrings(string(content))
				refs := scanner.findRefs(stripped, localTypes)

				fileUsed := make(map[string]bool)
				for _, ref := range refs {
					entry := ensureEntry(acc, ref)
					addRef(entry, r.label, cat, 1)
					fileUsed[ref.usageKey] = true
					if projUsedByCat[cat] == nil {
						projUsedByCat[cat] = make(map[string]bool)
					}
					projUsedByCat[cat][ref.usageKey] = true
				}
				for key := range fileUsed {
					entry := acc[key]
					addFile(entry, r.label, cat, 1)
				}
			}
			// Public-fail project counts are collapsed across categories;
			// deduplicate so the same usage key in the same project
			// contributes only once regardless of prod/test file mix.
			failProjDedup := make(map[string]bool)
			for cat, used := range projUsedByCat {
				for key := range used {
					entry := acc[key]
					if r.label == labelPublicFail {
						if failProjDedup[key] {
							continue
						}
						failProjDedup[key] = true
					}
					addProject(entry, r.label, cat, 1)
				}
			}
		}
	}

	// Build sorted slice.
	cu := CorpusUsage{}
	cu.PublicRootSHA256 = rootDigest(publicRoot)
	cu.PublicFailRootSHA256 = rootDigest(publicFailRoot)
	cu.PrivateRootSHA256 = rootDigest(privateRoot)

	for _, entry := range acc {
		cu.Usage = append(cu.Usage, *entry)
	}
	sort.Slice(cu.Usage, func(i, j int) bool {
		return cu.Usage[i].UsageKey < cu.Usage[j].UsageKey
	})

	return cu, nil
}

type apexFile struct {
	absPath string
	relPath string
	name    string
	project string
}

// isExcludedDir returns true for generated, cache, and VCS directories.
func isExcludedDir(name string) bool {
	switch name {
	case ".git", ".sfdx", ".sf", "node_modules":
		return true
	}
	return false
}

// scanApexFiles walks root and returns all .cls and .trigger files,
// skipping excluded directories and using the first path component
// beneath root as the project identity.
func scanApexFiles(root string) ([]apexFile, error) {
	var files []apexFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if isExcludedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".cls") && !strings.HasSuffix(lower, ".trigger") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		// First path component beneath root is the project identity.
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		project := parts[0]
		files = append(files, apexFile{
			absPath: path,
			relPath: rel,
			name:    name,
			project: project,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// categoryForFile returns the category for a single file.
func categoryForFile(f apexFile) string {
	// Determine test vs production.
	content, err := os.ReadFile(f.absPath)
	if err != nil {
		return ""
	}
	stripped := stripCommentsAndStrings(string(content))
	isTest := isTestFile(f.name, stripped)
	if isTest {
		return "test"
	}
	return "production"
}

func isTestFile(name string, strippedContent string) bool {
	// @isTest annotation.
	if strings.Contains(strippedContent, "@isTest") || strings.Contains(strippedContent, "@Istest") {
		return true
	}
	// Test filename pattern.
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, "test.cls") || strings.HasSuffix(lower, "_test.cls") {
		return true
	}
	return false
}

// stripCommentsAndStrings removes line comments, block comments, and Apex string literals.
func stripCommentsAndStrings(src string) string {
	var out strings.Builder
	out.Grow(len(src))
	i := 0
	n := len(src)
	for i < n {
		// String literal.
		if src[i] == '\'' {
			i++ // skip opening quote
			for i < n {
				if src[i] == '\\' {
					i += 2 // skip escaped char
					continue
				}
				if src[i] == '\'' {
					i++ // skip closing quote
					break
				}
				i++
			}
			// Replace string with a single space to avoid joining identifiers.
			out.WriteByte(' ')
			continue
		}
		// Line comment.
		if i+1 < n && src[i] == '/' && src[i+1] == '/' {
			for i < n && src[i] != '\n' {
				i++
			}
			out.WriteByte(' ')
			continue
		}
		// Block comment.
		if i+1 < n && src[i] == '/' && src[i+1] == '*' {
			i += 2
			for i+1 < n {
				if src[i] == '*' && src[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			out.WriteByte(' ')
			continue
		}
		out.WriteByte(src[i])
		i++
	}
	return out.String()
}

type refKey struct {
	usageKey   string
	namespace  string
	typeName   string
	memberName string
}

// findRefs finds all namespace references in stripped code.
// Namespace matching is case-insensitive; output uses canonical ledger spelling.
func (s *corpusScanner) findRefs(stripped string, localTypes map[string]bool) []refKey {
	// Split on non-identifier-non-dot characters.
	var fragments []string
	var current strings.Builder
	for i := 0; i < len(stripped); i++ {
		c := stripped[i]
		if isIdentChar(c) || c == '.' {
			current.WriteByte(c)
		} else {
			if current.Len() > 0 {
				fragments = append(fragments, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		fragments = append(fragments, current.String())
	}

	// Reference counts are per occurrence. File and project distinct counts
	// are deduplicated by the caller.
	var refs []refKey
	for _, frag := range fragments {
		parts := strings.Split(frag, ".")
		if len(parts) < 1 {
			continue
		}
		firstLower := strings.ToLower(parts[0])
		canonical, ok := s.namespaces[firstLower]
		if !ok {
			continue
		}
		// Shadow check: if this project has a local type with the same name as
		// the namespace (case-insensitive), skip counting.
		if localTypes[firstLower] {
			continue
		}

		// Namespace-level ref — use canonical spelling.
		refs = append(refs, refKey{usageKey: canonical, namespace: canonical})

		// Namespace.Type ref.
		if len(parts) >= 2 {
			nsTypeKey := canonical + "." + parts[1]
			refs = append(refs, refKey{usageKey: nsTypeKey, namespace: canonical, typeName: parts[1]})
		}

		// Namespace.Type.member ref.
		if len(parts) >= 3 {
			nsTypeMemKey := canonical + "." + parts[1] + "." + parts[2]
			refs = append(refs, refKey{usageKey: nsTypeMemKey, namespace: canonical, typeName: parts[1], memberName: parts[2]})
		}
	}
	return refs
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func ensureEntry(acc map[string]*CorpusUsageEntry, ref refKey) *CorpusUsageEntry {
	e, ok := acc[ref.usageKey]
	if !ok {
		e = &CorpusUsageEntry{
			UsageKey:  ref.usageKey,
			Namespace: ref.namespace,
			TypeName:  ref.typeName,
			MemberName: ref.memberName,
		}
		acc[ref.usageKey] = e
	}
	return e
}

func addRef(entry *CorpusUsageEntry, label corpusRootLabel, fileCat string, n int) {
	switch {
	case label == labelPublicSuccess && fileCat != "test":
		entry.PubProdRefs += n
	case label == labelPublicSuccess && fileCat == "test":
		entry.PubTestRefs += n
	case label == labelPublicFail && fileCat != "test":
		entry.PubFailRefs += n
	case label == labelPublicFail && fileCat == "test":
		entry.PubFailRefs += n
	case label == labelPrivateSuccess && fileCat != "test":
		entry.PrivProdRefs += n
	case label == labelPrivateSuccess && fileCat == "test":
		entry.PrivTestRefs += n
	}
}

func addFile(entry *CorpusUsageEntry, label corpusRootLabel, fileCat string, n int) {
	switch {
	case label == labelPublicSuccess && fileCat != "test":
		entry.PubProdFiles += n
	case label == labelPublicSuccess && fileCat == "test":
		entry.PubTestFiles += n
	case label == labelPublicFail && fileCat != "test":
		entry.PubFailFiles += n
	case label == labelPublicFail && fileCat == "test":
		entry.PubFailFiles += n
	case label == labelPrivateSuccess && fileCat != "test":
		entry.PrivProdFiles += n
	case label == labelPrivateSuccess && fileCat == "test":
		entry.PrivTestFiles += n
	}
}

func addProject(entry *CorpusUsageEntry, label corpusRootLabel, projCat string, n int) {
	switch {
	case label == labelPublicSuccess && projCat != "test":
		entry.PubProdProjects += n
	case label == labelPublicSuccess && projCat == "test":
		entry.PubTestProjects += n
	case label == labelPublicFail && projCat != "test":
		entry.PubFailProjects += n
	case label == labelPublicFail && projCat == "test":
		entry.PubFailProjects += n
	case label == labelPrivateSuccess && projCat != "test":
		entry.PrivProdProjects += n
	case label == labelPrivateSuccess && projCat == "test":
		entry.PrivTestProjects += n
	}
}

func rootDigest(root string) string {
	if root == "" {
		return ""
	}
	files, err := scanApexFiles(root)
	if err != nil || len(files) == 0 {
		return ""
	}
	// Sort by relative path for deterministic hash.
	sort.Slice(files, func(i, j int) bool {
		return files[i].relPath < files[j].relPath
	})
	h := sha256.New()
	for _, f := range files {
		content, err := os.ReadFile(f.absPath)
		if err != nil {
			continue
		}
		h.Write([]byte(f.relPath))
		h.Write([]byte{0})
		h.Write(content)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// UsageKeyForRow returns the stable usage key for a profile row.
func UsageKeyForRow(row SurfaceLedgerRow) string {
	return usageKeyForSurface(row.Namespace, row.TypeName, row.MemberName)
}
