package compat

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type LwcCorpusScanOptions struct {
	Root         string
	Out          string
	IncludeRepos []string
}

type LwcCorpusReport struct {
	Command         string                       `json:"command"`
	Root            string                       `json:"root"`
	IncludeRepos    []string                     `json:"includeRepos,omitempty"`
	Counts          LwcCorpusCounts              `json:"counts"`
	Targets         []LwcCorpusCountRow          `json:"targets,omitempty"`
	Imports         []LwcCorpusCountRow          `json:"imports,omitempty"`
	LightningTags   []LwcCorpusCountRow          `json:"lightningTags,omitempty"`
	PropertyTypes   []LwcCorpusCountRow          `json:"propertyTypes,omitempty"`
	UnsupportedTags []LwcCorpusCountRow          `json:"unsupportedTags,omitempty"`
	Examples        []LwcCorpusCountRow          `json:"examples,omitempty"`
	Repositories    []LwcCorpusRepositorySummary `json:"repositories"`
	Packages        []LwcCorpusPackageSummary    `json:"packages"`
}

type LwcCorpusCounts struct {
	Meta            int `json:"meta"`
	JS              int `json:"js"`
	HTML            int `json:"html"`
	Bundles         int `json:"bundles"`
	Targets         int `json:"targets"`
	Imports         int `json:"imports"`
	LightningTags   int `json:"lightningTags"`
	PropertyTypes   int `json:"propertyTypes"`
	UnsupportedTags int `json:"unsupportedTags"`
	Examples        int `json:"examples"`
}

type LwcCorpusCountRow struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type LwcCorpusRepositorySummary struct {
	Name   string          `json:"name"`
	Path   string          `json:"path"`
	Counts LwcCorpusCounts `json:"counts"`
}

type LwcCorpusPackageSummary struct {
	Repository string          `json:"repository"`
	Name       string          `json:"name,omitempty"`
	Path       string          `json:"path"`
	Default    bool            `json:"default,omitempty"`
	Counts     LwcCorpusCounts `json:"counts"`
}

type lwcCorpusRepo struct {
	name     string
	path     string
	packages []lwcCorpusPackage
	counts   LwcCorpusCounts
	bundles  map[string]bool
}

type lwcCorpusPackage struct {
	name           string
	path           string
	defaultPackage bool
	counts         LwcCorpusCounts
	bundles        map[string]bool
}

type lwcCorpusCounter struct {
	counts          LwcCorpusCounts
	bundles         map[string]bool
	targets         map[string]int
	imports         map[string]int
	lightningTags   map[string]int
	propertyTypes   map[string]int
	unsupportedTags map[string]int
	examples        map[string]int
}

type lwcCorpusProjectFile struct {
	PackageDirectories []struct {
		Path    string `json:"path"`
		Package string `json:"package"`
		Default bool   `json:"default"`
	} `json:"packageDirectories"`
}

var (
	lwcImportRE = regexp.MustCompile(`(?m)\bimport(?:\s+[^'";]+?\s+from\s+|\s*)['"]([^'"]+)['"]`)
	lwcTagRE    = regexp.MustCompile(`(?is)<\s*([a-zA-Z][a-zA-Z0-9_-]*)\b`)
)

func ScanLwcCorpus(options LwcCorpusScanOptions) (LwcCorpusReport, error) {
	root := strings.TrimSpace(options.Root)
	if root == "" {
		return LwcCorpusReport{}, errors.New("--root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return LwcCorpusReport{}, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return LwcCorpusReport{}, err
	}
	if !info.IsDir() {
		return LwcCorpusReport{}, fmt.Errorf("--root must be a directory: %s", root)
	}
	report := LwcCorpusReport{
		Command:      "glade compat lwc corpus",
		Root:         root,
		IncludeRepos: normalizeLwcCorpusList(options.IncludeRepos),
		Repositories: []LwcCorpusRepositorySummary{},
		Packages:     []LwcCorpusPackageSummary{},
	}
	counter := newLwcCorpusCounter()
	repos, err := discoverLwcCorpusRepos(absRoot, report.IncludeRepos)
	if err != nil {
		return LwcCorpusReport{}, err
	}
	for i := range repos {
		if err := scanLwcCorpusRepo(&repos[i], counter); err != nil {
			return LwcCorpusReport{}, err
		}
		report.Repositories = append(report.Repositories, LwcCorpusRepositorySummary{
			Name:   repos[i].name,
			Path:   repos[i].path,
			Counts: repos[i].counts,
		})
		for _, pkg := range repos[i].packages {
			report.Packages = append(report.Packages, LwcCorpusPackageSummary{
				Repository: repos[i].name,
				Name:       pkg.name,
				Path:       pkg.path,
				Default:    pkg.defaultPackage,
				Counts:     pkg.counts,
			})
		}
	}
	sort.Slice(report.Repositories, func(i, j int) bool {
		return report.Repositories[i].Name < report.Repositories[j].Name
	})
	sort.Slice(report.Packages, func(i, j int) bool {
		if report.Packages[i].Repository != report.Packages[j].Repository {
			return report.Packages[i].Repository < report.Packages[j].Repository
		}
		return report.Packages[i].Path < report.Packages[j].Path
	})
	report.Counts = counter.counts
	report.Targets = lwcCorpusCountRows(counter.targets)
	report.Imports = lwcCorpusCountRows(counter.imports)
	report.LightningTags = lwcCorpusCountRows(counter.lightningTags)
	report.PropertyTypes = lwcCorpusCountRows(counter.propertyTypes)
	report.UnsupportedTags = lwcCorpusCountRows(counter.unsupportedTags)
	report.Examples = lwcCorpusCountRows(counter.examples)
	return report, nil
}

func WriteLwcCorpusJSON(w io.Writer, report LwcCorpusReport) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func WriteLwcCorpusReportJSON(path string, report LwcCorpusReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return WriteLwcCorpusJSON(f, report)
}

func WriteLwcCorpusText(w io.Writer, report LwcCorpusReport) {
	fmt.Fprintf(w, "lwc corpus: repos=%d packages=%d bundles=%d meta=%d js=%d html=%d\n", len(report.Repositories), len(report.Packages), report.Counts.Bundles, report.Counts.Meta, report.Counts.JS, report.Counts.HTML)
	for _, repo := range report.Repositories {
		fmt.Fprintf(w, "- %s: bundles=%d meta=%d js=%d html=%d\n", repo.Name, repo.Counts.Bundles, repo.Counts.Meta, repo.Counts.JS, repo.Counts.HTML)
	}
}

func discoverLwcCorpusRepos(root string, includeRepos []string) ([]lwcCorpusRepo, error) {
	if _, err := os.Stat(filepath.Join(root, "sfdx-project.json")); err == nil {
		repo, err := newLwcCorpusRepo(filepath.Base(root), root)
		if err != nil {
			return nil, err
		}
		if len(includeRepos) != 0 && !lwcCorpusContains(includeRepos, repo.name) {
			repos := make([]lwcCorpusRepo, 0, len(includeRepos))
			for _, name := range includeRepos {
				repos = append(repos, emptyLwcCorpusRepo(name, filepath.Join(filepath.Dir(root), name)))
			}
			return repos, nil
		}
		return []lwcCorpusRepo{repo}, nil
	}
	if len(includeRepos) != 0 {
		repos := make([]lwcCorpusRepo, 0, len(includeRepos))
		for _, name := range includeRepos {
			path := filepath.Join(root, name)
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				repo, err := newLwcCorpusRepo(name, path)
				if err != nil {
					return nil, err
				}
				repos = append(repos, repo)
				continue
			}
			repos = append(repos, emptyLwcCorpusRepo(name, path))
		}
		return repos, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	repos := []lwcCorpusRepo{}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		repo, err := newLwcCorpusRepo(entry.Name(), filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].name < repos[j].name })
	return repos, nil
}

func newLwcCorpusRepo(name, path string) (lwcCorpusRepo, error) {
	repo := emptyLwcCorpusRepo(name, path)
	packages, err := readLwcCorpusPackages(path)
	if err != nil {
		return lwcCorpusRepo{}, err
	}
	repo.packages = packages
	return repo, nil
}

func emptyLwcCorpusRepo(name, path string) lwcCorpusRepo {
	return lwcCorpusRepo{
		name:    name,
		path:    path,
		bundles: map[string]bool{},
	}
}

func readLwcCorpusPackages(repoPath string) ([]lwcCorpusPackage, error) {
	projectPath := filepath.Join(repoPath, "sfdx-project.json")
	data, err := os.ReadFile(projectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []lwcCorpusPackage{newLwcCorpusPackage("", ".")}, nil
		}
		return nil, err
	}
	var project lwcCorpusProjectFile
	if err := json.Unmarshal(data, &project); err != nil {
		return nil, fmt.Errorf("read %s: %w", projectPath, err)
	}
	if len(project.PackageDirectories) == 0 {
		return []lwcCorpusPackage{newLwcCorpusPackage("", ".")}, nil
	}
	packages := make([]lwcCorpusPackage, 0, len(project.PackageDirectories))
	for _, dir := range project.PackageDirectories {
		pkg := newLwcCorpusPackage(dir.Package, dir.Path)
		pkg.defaultPackage = dir.Default
		packages = append(packages, pkg)
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].path < packages[j].path })
	return packages, nil
}

func newLwcCorpusPackage(name, path string) lwcCorpusPackage {
	cleanPath := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if cleanPath == "" || cleanPath == "/" {
		cleanPath = "."
	}
	return lwcCorpusPackage{
		name:    name,
		path:    cleanPath,
		bundles: map[string]bool{},
	}
}

func scanLwcCorpusRepo(repo *lwcCorpusRepo, counter *lwcCorpusCounter) error {
	if info, err := os.Stat(repo.path); err != nil || !info.IsDir() {
		return nil
	}
	return filepath.WalkDir(repo.path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipLwcCorpusDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repo.path, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		kind := lwcCorpusFileKind(rel)
		if kind == "" {
			return nil
		}
		pkg := lwcCorpusPackageForRel(repo.packages, rel)
		lwcCorpusIncrementFile(kind, &repo.counts, repo.bundles, rel)
		if pkg != nil {
			lwcCorpusIncrementFile(kind, &pkg.counts, pkg.bundles, rel)
		}
		lwcCorpusIncrementFile(kind, &counter.counts, counter.bundles, repo.name+"/"+rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		switch kind {
		case "js":
			scanLwcCorpusJS(data, counter, lwcCorpusDetailCounts(&repo.counts, pkg)...)
		case "html":
			scanLwcCorpusHTML(data, counter, lwcCorpusDetailCounts(&repo.counts, pkg)...)
		case "meta":
			scanLwcCorpusMeta(data, counter, lwcCorpusDetailCounts(&repo.counts, pkg)...)
		}
		return nil
	})
}

func lwcCorpusDetailCounts(repoCounts *LwcCorpusCounts, pkg *lwcCorpusPackage) []*LwcCorpusCounts {
	counts := []*LwcCorpusCounts{repoCounts}
	if pkg != nil {
		counts = append(counts, &pkg.counts)
	}
	return counts
}

func shouldSkipLwcCorpusDir(name string) bool {
	switch name {
	case ".git", ".sfdx", ".sf", ".glade", "node_modules":
		return true
	default:
		return false
	}
}

func lwcCorpusFileKind(rel string) string {
	if !lwcCorpusPathHasSegment(rel, "lwc") {
		return ""
	}
	switch {
	case strings.HasSuffix(rel, ".js-meta.xml"):
		return "meta"
	case strings.HasSuffix(rel, ".js"):
		return "js"
	case strings.HasSuffix(rel, ".html"):
		return "html"
	default:
		return ""
	}
}

func lwcCorpusPathHasSegment(path, segment string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == segment {
			return true
		}
	}
	return false
}

func lwcCorpusIncrementFile(kind string, counts *LwcCorpusCounts, bundles map[string]bool, rel string) {
	switch kind {
	case "meta":
		counts.Meta++
	case "js":
		counts.JS++
	case "html":
		counts.HTML++
	}
	if bundles == nil {
		return
	}
	bundle := lwcCorpusBundlePath(rel)
	if bundle != "" && !bundles[bundle] {
		bundles[bundle] = true
		counts.Bundles++
	}
}

func lwcCorpusBundlePath(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "lwc" && i+1 < len(parts) {
			return strings.Join(parts[:i+2], "/")
		}
	}
	return ""
}

func lwcCorpusPackageForRel(packages []lwcCorpusPackage, rel string) *lwcCorpusPackage {
	var best *lwcCorpusPackage
	for i := range packages {
		pkgPath := packages[i].path
		if pkgPath == "." || rel == pkgPath || strings.HasPrefix(rel, pkgPath+"/") {
			if best == nil || len(pkgPath) > len(best.path) {
				best = &packages[i]
			}
		}
	}
	return best
}

func scanLwcCorpusJS(data []byte, counter *lwcCorpusCounter, detailCounts ...*LwcCorpusCounts) {
	for _, match := range lwcImportRE.FindAllSubmatch(data, -1) {
		name := strings.TrimSpace(string(match[1]))
		if name == "" {
			continue
		}
		counter.imports[name]++
		counter.counts.Imports++
		for _, counts := range detailCounts {
			counts.Imports++
		}
	}
}

func scanLwcCorpusHTML(data []byte, counter *lwcCorpusCounter, detailCounts ...*LwcCorpusCounts) {
	for _, match := range lwcTagRE.FindAllSubmatch(data, -1) {
		tag := strings.ToLower(strings.TrimSpace(string(match[1])))
		if tag == "" || strings.HasPrefix(tag, "!") {
			continue
		}
		if strings.HasPrefix(tag, "lightning-") {
			counter.lightningTags[tag]++
			counter.counts.LightningTags++
			for _, counts := range detailCounts {
				counts.LightningTags++
			}
			continue
		}
		if isUnsupportedLwcCorpusTag(tag) {
			counter.unsupportedTags[tag]++
			counter.counts.UnsupportedTags++
			for _, counts := range detailCounts {
				counts.UnsupportedTags++
			}
		}
	}
}

func isUnsupportedLwcCorpusTag(tag string) bool {
	if tag == "template" || tag == "slot" {
		return false
	}
	if strings.HasPrefix(tag, "c-") {
		return false
	}
	if strings.Contains(tag, "-") {
		return true
	}
	return false
}

func scanLwcCorpusMeta(data []byte, counter *lwcCorpusCounter, detailCounts ...*LwcCorpusCounts) {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "target":
			value := readLwcCorpusElementText(dec)
			if value != "" {
				counter.targets[value]++
				counter.counts.Targets++
				for _, counts := range detailCounts {
					counts.Targets++
				}
			}
		case "property":
			for _, attr := range start.Attr {
				if attr.Name.Local == "type" && strings.TrimSpace(attr.Value) != "" {
					counter.propertyTypes[strings.TrimSpace(attr.Value)]++
					counter.counts.PropertyTypes++
					for _, counts := range detailCounts {
						counts.PropertyTypes++
					}
				}
			}
		case "example":
			value := readLwcCorpusElementText(dec)
			if value != "" {
				counter.examples[value]++
				counter.counts.Examples++
				for _, counts := range detailCounts {
					counts.Examples++
				}
			}
		}
	}
}

func readLwcCorpusElementText(dec *xml.Decoder) string {
	var b strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return strings.TrimSpace(b.String())
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write([]byte(t))
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return strings.TrimSpace(b.String())
}

func newLwcCorpusCounter() *lwcCorpusCounter {
	return &lwcCorpusCounter{
		bundles:         map[string]bool{},
		targets:         map[string]int{},
		imports:         map[string]int{},
		lightningTags:   map[string]int{},
		propertyTypes:   map[string]int{},
		unsupportedTags: map[string]int{},
		examples:        map[string]int{},
	}
}

func normalizeLwcCorpusList(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func lwcCorpusContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func lwcCorpusCountRows(counts map[string]int) []LwcCorpusCountRow {
	rows := make([]LwcCorpusCountRow, 0, len(counts))
	for name, count := range counts {
		rows = append(rows, LwcCorpusCountRow{Name: name, Count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}
