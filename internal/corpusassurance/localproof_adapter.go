package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if err := writeLocalProofProject(root, fixture); err != nil {
		cleanup()
		return localProofCommand{}, nil, err
	}
	command, err := localProofCommandForFixture(entry, fixture, candidatePath, root)
	if err != nil {
		cleanup()
		return localProofCommand{}, nil, err
	}
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
	fixture, err := compat.LoadData(data)
	if err != nil {
		return compat.Fixture{}, fmt.Errorf("decode fixture %q: %w", entry.ID, err)
	}
	if err := compat.Validate(fixture); err != nil {
		return compat.Fixture{}, fmt.Errorf("validate fixture %q: %w", entry.ID, err)
	}
	if err := validateLocalProofFixtureIdentity(entry, fixture); err != nil {
		return compat.Fixture{}, err
	}
	return fixture, nil
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
		if evidence[surfaceID] != localProofEvidenceKind(entry.Disposition) {
			return fmt.Errorf("fixture %q lacks required evidence for %q", entry.ID, surfaceID)
		}
	}
	return nil
}

func writeLocalProofProject(root string, fixture compat.Fixture) error {
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
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "sfdx-project.json"), data, 0o600); err != nil {
		return err
	}
	for _, file := range append(append([]compat.SourceFile(nil), fixture.Source...), sourceFilesFromSchema(fixture.Schema)...) {
		path, err := localProofProjectPath(root, file.Path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(file.Content), 0o600); err != nil {
			return err
		}
	}
	return nil
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

func validatesCandidateJSON(data []byte, operation string) bool {
	var result struct {
		Status   string          `json:"status"`
		ExitCode int             `json:"exitCode"`
		Tests    json.RawMessage `json:"tests"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return false
	}
	if result.Status != "passed" || result.ExitCode != 0 {
		return false
	}
	if len(result.Tests) == 0 {
		return operation != "test"
	}
	var tests struct {
		Total  int `json:"total"`
		Failed int `json:"failed"`
		Errors int `json:"errors"`
	}
	return json.Unmarshal(result.Tests, &tests) == nil && tests.Total > 0 && tests.Failed == 0 && tests.Errors == 0
}
