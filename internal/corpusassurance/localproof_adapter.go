package corpusassurance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/glade-sh/glade/tools/internal/compat"
)

// materializeLocalProofFixture adapts an accepted compat fixture to the public
// frozen-candidate CLI. The candidate never receives a non-existent --fixture
// flag; it receives an ordinary, temporary SFDX project instead.
func materializeLocalProofFixture(entry LocalProofFixture, candidatePath string) (localProofCommand, func(), error) {
	fixture, err := loadLocalProofFixture(entry)
	if err != nil {
		return localProofCommand{}, nil, err
	}
	root, err := os.MkdirTemp("", "glade-assurance-fixture-*")
	if err != nil {
		return localProofCommand{}, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	apexInputs, err := writeLocalProofProject(root, fixture)
	if err != nil {
		cleanup()
		return localProofCommand{}, nil, err
	}
	command, err := localProofCommandForFixture(entry, fixture, candidatePath, root)
	if err != nil {
		cleanup()
		return localProofCommand{}, nil, err
	}
	command.ApexInputs = apexInputs
	return command, cleanup, nil
}

func loadLocalProofFixture(entry LocalProofFixture) (compat.Fixture, error) {
	data, err := os.ReadFile(entry.Path)
	if err != nil {
		return compat.Fixture{}, fmt.Errorf("read fixture %q: %w", entry.ID, err)
	}
	if replayBytesSHA256(data) != entry.SHA256 {
		return compat.Fixture{}, fmt.Errorf("fixture binding mismatch for %q", entry.ID)
	}
	fixture, err := decodeLocalProofFixture(data)
	if err != nil {
		return compat.Fixture{}, fmt.Errorf("decode fixture %q: %w", entry.ID, err)
	}
	if err := compat.Validate(fixture); err != nil {
		return compat.Fixture{}, fmt.Errorf("validate fixture %q: %w", entry.ID, err)
	}
	if err := validateLocalProofFixtureIdentity(entry, fixture); err != nil {
		return compat.Fixture{}, err
	}
	if !localProofFixtureIsMaterializable(fixture) {
		return compat.Fixture{}, fmt.Errorf("fixture %q has state the local-proof adapter does not materialize", entry.ID)
	}
	return fixture, nil
}

func decodeLocalProofFixture(data []byte) (compat.Fixture, error) {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&fields); err != nil {
		return compat.Fixture{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return compat.Fixture{}, fmt.Errorf("multiple JSON values")
	}
	for key := range fields {
		if localProofFixtureExtensionFields[key] {
			delete(fields, key)
			continue
		}
		if !localProofFixtureFields[key] {
			return compat.Fixture{}, fmt.Errorf("json: unknown field %q", key)
		}
	}
	filtered, err := json.Marshal(fields)
	if err != nil {
		return compat.Fixture{}, err
	}
	decoder = json.NewDecoder(bytes.NewReader(filtered))
	decoder.DisallowUnknownFields()
	var fixture compat.Fixture
	if err := decoder.Decode(&fixture); err != nil {
		return compat.Fixture{}, err
	}
	return fixture, nil
}

var localProofFixtureFields = map[string]bool{
	"name": true, "evidence": true, "project": true, "source": true, "schema": true,
	"metadata": true, "seedData": true, "serverRequests": true, "command": true,
	"expected": true, "limits": true,
}

var localProofFixtureExtensionFields = map[string]bool{
	"apiVersion": true, "candidate": true, "mode": true, "notes": true, "profile": true,
	"salesforceEligible": true, "salesforceExclusionClass": true, "salesforceExclusionReason": true,
}

func localProofFixtureIsMaterializable(fixture compat.Fixture) bool {
	return len(fixture.Metadata.Labels) == 0 &&
		len(fixture.Metadata.ManagedLabelNamespaces) == 0 &&
		len(fixture.Metadata.Tabs) == 0 &&
		len(fixture.Metadata.DataCategoryGroups) == 0 &&
		len(fixture.Metadata.QuickActions) == 0 &&
		len(fixture.Metadata.FieldSets) == 0 &&
		len(fixture.Metadata.StaticResources) == 0 &&
		len(fixture.Metadata.ContentAssets) == 0 &&
		len(fixture.Metadata.Endpoints) == 0 &&
		len(fixture.Metadata.EmailTemplates) == 0 &&
		len(fixture.SeedData) == 0 && len(fixture.ServerRequests) == 0 &&
		fixture.Command.LimitMode == "" && fixture.Expected.Stdout == "" && fixture.Expected.Stderr == "" &&
		fixture.Expected.Error == nil && len(fixture.Expected.SideEffects) == 0 &&
		fixture.Limits.SOQLQueries == nil && fixture.Limits.SOQLRows == nil && fixture.Limits.DMLStatements == nil &&
		fixture.Limits.DMLRows == nil && fixture.Limits.CPUTimeMS == nil && fixture.Limits.HeapBytes == nil
}

func validateLocalProofFixtureIdentity(entry LocalProofFixture, fixture compat.Fixture) error {
	if entry.Path == "" || !filepath.IsAbs(entry.Path) || fixture.Name == "" || fixture.Name != entry.Name {
		return fmt.Errorf("fixture name or path mismatch for %q", entry.ID)
	}
	evidence := make(map[string]string, len(fixture.Evidence))
	for _, item := range fixture.Evidence {
		if item.SurfaceID == "" || item.Kind == "" || evidence[item.SurfaceID] != "" {
			return fmt.Errorf("fixture %q has invalid evidence", entry.ID)
		}
		evidence[item.SurfaceID] = item.Kind
	}
	for _, surfaceID := range entry.OwnedSurfaceIDs {
		if !localProofEvidenceKindMatches(entry.Disposition, fixture.Command.Kind, evidence[surfaceID]) {
			return fmt.Errorf("fixture %q lacks required evidence for %q", entry.ID, surfaceID)
		}
	}
	if err := validateLocalProofSurfaceWitnesses(entry, fixture); err != nil {
		return err
	}
	return nil
}

func validateLocalProofSurfaceWitnesses(entry LocalProofFixture, fixture compat.Fixture) error {
	var source strings.Builder
	if entry.Disposition == localRuntimeRequired && fixture.Command.Kind == "exec" {
		source.WriteString(strings.Join(fixture.Command.Args, "\n"))
	} else {
		for _, file := range append(append([]compat.SourceFile(nil), fixture.Source...), sourceFilesFromSchema(fixture.Schema)...) {
			source.WriteString(file.Content)
			source.WriteByte('\n')
		}
	}
	tokens := localProofCodeIdentifiers(source.String())
	for _, evidence := range fixture.Evidence {
		if !stringSet(entry.OwnedSurfaceIDs)[evidence.SurfaceID] {
			continue
		}
		parts := localProofWitnessIdentifiers(evidence.Symbol)
		for _, part := range parts {
			if !tokens[part] {
				return fmt.Errorf("fixture %q surface %q lacks a source witness for %q", entry.ID, evidence.SurfaceID, evidence.Symbol)
			}
		}
	}
	return nil
}

func localProofWitnessIdentifiers(symbol string) []string {
	symbol = strings.TrimPrefix(symbol, "apex:")
	if index := strings.IndexByte(symbol, '('); index >= 0 {
		symbol = symbol[:index]
	}
	parts := strings.FieldsFunc(symbol, func(r rune) bool {
		return r == '.' || r == ':' || r == '<' || r == '>' || r == ',' || unicode.IsSpace(r)
	})
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return parts
}

func localProofCodeIdentifiers(source string) map[string]bool {
	identifiers := make(map[string]bool)
	var token []rune
	flush := func() {
		if len(token) > 0 {
			identifiers[string(token)] = true
			token = token[:0]
		}
	}
	state := byte(0)
	escaped := false
	runes := []rune(source)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch state {
		case 1:
			if r == '\n' {
				state = 0
			}
		case 2:
			if r == '*' && i+1 < len(runes) && runes[i+1] == '/' {
				i++
				state = 0
			}
		case 3, 4:
			if escaped {
				escaped = false
			} else if r == '\\' {
				escaped = true
			} else if (state == 3 && r == '\'') || (state == 4 && r == '"') {
				state = 0
			}
		default:
			if r == '/' && i+1 < len(runes) && runes[i+1] == '/' {
				flush()
				i++
				state = 1
			} else if r == '/' && i+1 < len(runes) && runes[i+1] == '*' {
				flush()
				i++
				state = 2
			} else if r == '\'' {
				flush()
				state = 3
			} else if r == '"' {
				flush()
				state = 4
			} else if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				token = append(token, r)
			} else {
				flush()
			}
		}
	}
	flush()
	return identifiers
}

func writeLocalProofProject(root string, fixture compat.Fixture) (int, error) {
	apexInputs, err := localProofApexInputCount(fixture)
	if err != nil {
		return 0, err
	}
	packages := fixture.Project.PackageDirectories
	if len(packages) == 0 {
		packages = []compat.PackageDirectory{{Path: "force-app", Default: true}}
	}
	project := struct {
		PackageDirectories []compat.PackageDirectory `json:"packageDirectories"`
		Namespace          string                    `json:"namespace,omitempty"`
		SourceAPIVersion   string                    `json:"sourceApiVersion,omitempty"`
	}{packages, fixture.Project.Namespace, fixture.Project.SourceAPIVersion}
	data, err := json.Marshal(project)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(root, "sfdx-project.json"), data, 0o600); err != nil {
		return 0, err
	}
	for _, file := range append(append([]compat.SourceFile(nil), fixture.Source...), sourceFilesFromSchema(fixture.Schema)...) {
		path, err := localProofProjectPath(root, file.Path)
		if err != nil {
			return 0, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return 0, err
		}
		if err := os.WriteFile(path, []byte(file.Content), 0o600); err != nil {
			return 0, err
		}
	}
	return apexInputs, nil
}

func localProofApexInputCount(fixture compat.Fixture) (int, error) {
	packages := fixture.Project.PackageDirectories
	if len(packages) == 0 {
		packages = []compat.PackageDirectory{{Path: "force-app", Default: true}}
	}
	roots, err := localProofPackageRoots(packages)
	if err != nil {
		return 0, err
	}
	seen := make(map[string]bool, len(fixture.Source)+len(fixture.Schema))
	count := 0
	for _, file := range append(append([]compat.SourceFile(nil), fixture.Source...), sourceFilesFromSchema(fixture.Schema)...) {
		clean := filepath.Clean(file.Path)
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || seen[clean] {
			return 0, fmt.Errorf("duplicate or invalid fixture source path %q", file.Path)
		}
		seen[clean] = true
		if localProofApexSource(clean) {
			if !localProofPathInPackages(clean, roots) {
				return 0, fmt.Errorf("Apex fixture source %q is outside packageDirectories", file.Path)
			}
			count++
		}
	}
	return count, nil
}

func localProofPackageRoots(packages []compat.PackageDirectory) ([]string, error) {
	roots := make([]string, 0, len(packages))
	seen := make(map[string]bool, len(packages))
	for _, directory := range packages {
		clean := filepath.Clean(directory.Path)
		if directory.Path == "" || clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || seen[clean] {
			return nil, fmt.Errorf("invalid or duplicate package directory %q", directory.Path)
		}
		seen[clean] = true
		roots = append(roots, clean)
	}
	return roots, nil
}

func localProofApexSource(path string) bool {
	return strings.HasSuffix(path, ".cls") || strings.HasSuffix(path, ".trigger")
}

func localProofPathInPackages(path string, packages []string) bool {
	for _, root := range packages {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func sourceFilesFromSchema(schema []compat.SchemaFile) []compat.SourceFile {
	files := make([]compat.SourceFile, 0, len(schema))
	for _, file := range schema {
		files = append(files, compat.SourceFile{Path: file.Path, Content: file.Content})
	}
	return files
}

func localProofProjectPath(root, relative string) (string, error) {
	clean := filepath.Clean(relative)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("fixture path %q must stay inside project root", relative)
	}
	return filepath.Join(root, clean), nil
}

func localProofCommandForFixture(entry LocalProofFixture, fixture compat.Fixture, candidatePath, root string) (localProofCommand, error) {
	operation := ""
	switch entry.Disposition {
	case localRuntimeRequired:
		operation = "exec"
		if fixture.Command.Kind == "test" {
			operation = "test"
		}
	case deterministicMockRequired:
		operation = "test"
	case compileShapeRequired:
		operation = "check"
	default:
		return localProofCommand{}, fmt.Errorf("invalid fixture disposition %q", entry.Disposition)
	}
	if fixture.Command.Kind != operation {
		return localProofCommand{}, fmt.Errorf("fixture %q command kind %q does not satisfy %s proof", entry.ID, fixture.Command.Kind, entry.Disposition)
	}
	args := []string{operation, "--project", root, "--json"}
	if operation != "exec" {
		args = append(args, "--no-progress")
	}
	if operation == "exec" {
		if len(fixture.Command.Args) != 1 || fixture.Command.Args[0] == "" {
			return localProofCommand{}, fmt.Errorf("runtime fixture %q must have one anonymous Apex program", entry.ID)
		}
		args = append(args, fixture.Command.Args[0])
	}
	return localProofCommand{Path: candidatePath, Args: args, Dir: root}, nil
}

func validatesCandidateJSON(data []byte, operation string, apexInputs int) bool {
	var result struct {
		Status   string          `json:"status"`
		ExitCode int             `json:"exitCode"`
		Summary  json.RawMessage `json:"summary"`
		Tests    json.RawMessage `json:"tests"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return false
	}
	if result.Status != "passed" || result.ExitCode != 0 {
		return false
	}
	if operation == "exec" {
		return true
	}
	if len(result.Summary) == 0 {
		return false
	}
	if operation == "check" {
		var summary struct {
			Types    int `json:"types"`
			Triggers int `json:"triggers"`
		}
		return apexInputs > 0 && json.Unmarshal(result.Summary, &summary) == nil && summary.Types+summary.Triggers >= apexInputs
	}
	var summary struct {
		Total         int `json:"total"`
		Passed        int `json:"passed"`
		Failed        int `json:"failed"`
		Errors        int `json:"errors"`
		CompileErrors int `json:"compileErrors"`
		RuntimeErrors int `json:"runtimeErrors"`
	}
	return json.Unmarshal(result.Summary, &summary) == nil && summary.Total > 0 && summary.Passed == summary.Total && summary.Failed == 0 && summary.Errors == 0 && summary.CompileErrors == 0 && summary.RuntimeErrors == 0 && len(result.Tests) > 0
}
