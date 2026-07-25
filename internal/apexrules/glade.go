package apexrules

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
)

var unsafeFilename = regexp.MustCompile(`[^A-Za-z0-9_]`)

// RunGlade checks every probe in a fresh SFDX project with the supplied Glade
// executable. The outcome is intentionally limited to compiler acceptance so
// it can be compared with a Salesforce compiler result without coupling to
// either product's diagnostic text.
func RunGlade(ctx context.Context, binary string, rules []Rule) (map[string]Outcome, error) {
	if binary == "" {
		return nil, fmt.Errorf("glade binary is required")
	}
	outcomes := make(map[string]Outcome, len(rules))
	for _, rule := range rules {
		project, err := os.MkdirTemp("", "glade-apex-rule-")
		if err != nil {
			return nil, fmt.Errorf("create project for %s: %w", rule.ID, err)
		}
		outcome, runErr := runGladeRule(ctx, binary, project, rule)
		removeErr := os.RemoveAll(project)
		if runErr != nil {
			return nil, runErr
		}
		if removeErr != nil {
			return nil, fmt.Errorf("remove project for %s: %w", rule.ID, removeErr)
		}
		outcomes[rule.ID] = outcome
	}
	return outcomes, nil
}

func runGladeRule(ctx context.Context, binary, project string, rule Rule) (Outcome, error) {
	if err := writeGladeRuleProject(project, rule); err != nil {
		return "", fmt.Errorf("write project for %s: %w", rule.ID, err)
	}
	cmd := exec.CommandContext(ctx, binary, "check", "--project", project, "--json", "--no-progress")
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return OutcomeReject, nil
		}
		return "", fmt.Errorf("run glade for %s: %w", rule.ID, err)
	}
	return OutcomeAccept, nil
}

func writeGladeRuleProject(project string, rule Rule) error {
	apiVersion := rule.APIVersion
	if apiVersion == 0 {
		apiVersion = 66
	}
	projectJSON := "{\n  \"packageDirectories\": [{\"path\": \"force-app\", \"default\": true}],\n  \"sourceApiVersion\": \"" + strconv.FormatFloat(apiVersion, 'f', -1, 64) + "\"\n}\n"
	if err := os.WriteFile(filepath.Join(project, "sfdx-project.json"), []byte(projectJSON), 0o600); err != nil {
		return err
	}
	for _, dep := range rule.Dependencies {
		if filepath.IsAbs(dep.Path) || dep.Path == "" || filepath.Clean(dep.Path) != dep.Path || filepath.Dir(dep.Path) == "." && filepath.Base(dep.Path) == ".." {
			return fmt.Errorf("unsafe dependency path %q", dep.Path)
		}
		path := filepath.Join(project, dep.Path)
		rel, err := filepath.Rel(project, path)
		if err != nil || rel == ".." || len(rel) > 3 && rel[:3] == ".."+string(filepath.Separator) {
			return fmt.Errorf("unsafe dependency path %q", dep.Path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(dep.Content), 0o600); err != nil {
			return err
		}
	}
	name := unsafeFilename.ReplaceAllString(rule.ID, "_")
	if name == "" {
		name = "Probe"
	}
	var directory, extension string
	switch rule.SourceKind {
	case "class":
		directory, extension = "classes", ".cls"
	case "trigger":
		directory, extension = "triggers", ".trigger"
	default:
		return fmt.Errorf("unsupported source kind %q", rule.SourceKind)
	}
	path := filepath.Join(project, "force-app", "main", "default", directory, name+extension)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(rule.Source), 0o600)
}
