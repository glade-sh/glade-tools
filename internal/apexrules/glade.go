package apexrules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	if err := writeGladeRuleSetup(project, rule); err != nil {
		return "", fmt.Errorf("write project setup for %s: %w", rule.ID, err)
	}
	if len(rule.Dependencies) > 0 || len(rule.ProjectFiles) > 0 {
		if _, err := runGladeCheck(ctx, binary, project, rule, ""); err != nil {
			return "", fmt.Errorf("run glade setup for %s: %w", rule.ID, err)
		}
	}
	probePath, err := gladeProbePath(rule)
	if err != nil {
		return "", err
	}
	if err := writeGladeRuleProbe(project, rule, probePath); err != nil {
		return "", fmt.Errorf("write project probe for %s: %w", rule.ID, err)
	}
	return runGladeCheck(ctx, binary, project, rule, probePath)
}

func runGladeCheck(ctx context.Context, binary, project string, rule Rule, probePath string) (Outcome, error) {
	cmd := exec.CommandContext(ctx, binary, "check", "--project", project, "--json", "--no-progress")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("run glade for %s: %w", rule.ID, ctx.Err())
	}
	var report struct {
		Status      string `json:"status"`
		ExitCode    int    `json:"exitCode"`
		Diagnostics []struct {
			Severity string `json:"severity"`
			File     string `json:"file"`
		} `json:"diagnostics"`
	}
	start, end := bytes.IndexByte(output, '{'), bytes.LastIndexByte(output, '}')
	if start < 0 || end < start || json.Unmarshal(output[start:end+1], &report) != nil {
		return "", fmt.Errorf("run glade for %s produced no valid JSON report", rule.ID)
	}
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return "", fmt.Errorf("run glade for %s: %w", rule.ID, err)
		}
		if exitErr.ExitCode() == 1 && report.ExitCode == 1 && report.Status == "failed" {
			if probePath == "" {
				return "", fmt.Errorf("run glade for %s rejected project setup", rule.ID)
			}
			for _, diag := range report.Diagnostics {
				if strings.EqualFold(diag.Severity, "error") &&
					filepath.Clean(filepath.FromSlash(diag.File)) == filepath.Clean(probePath) {
					return OutcomeReject, nil
				}
			}
			return "", fmt.Errorf("run glade for %s failed without an error diagnostic for probe source %s", rule.ID, filepath.ToSlash(probePath))
		}
		return "", fmt.Errorf("run glade for %s failed operationally with exit code %d", rule.ID, exitErr.ExitCode())
	}
	if report.ExitCode != 0 || report.Status != "passed" {
		return "", fmt.Errorf("run glade for %s returned an inconsistent success report", rule.ID)
	}
	return OutcomeAccept, nil
}

func writeGladeRuleProject(project string, rule Rule) error {
	if err := writeGladeRuleSetup(project, rule); err != nil {
		return err
	}
	probePath, err := gladeProbePath(rule)
	if err != nil {
		return err
	}
	return writeGladeRuleProbe(project, rule, probePath)
}

func writeGladeRuleSetup(project string, rule Rule) error {
	apiVersion := rule.APIVersion
	if apiVersion == 0 {
		apiVersion = 66
	}
	if apiVersion != math.Trunc(apiVersion) {
		return fmt.Errorf("unsupported source API version %.10g", apiVersion)
	}
	projectJSON := "{\n  \"packageDirectories\": [{\"path\": \"force-app\", \"default\": true}],\n  \"sourceApiVersion\": \"" + strconv.FormatFloat(apiVersion, 'f', 1, 64) + "\"\n}\n"
	if err := os.WriteFile(filepath.Join(project, "sfdx-project.json"), []byte(projectJSON), 0o600); err != nil {
		return err
	}
	for _, file := range append(append([]SourceFile(nil), rule.Dependencies...), rule.ProjectFiles...) {
		if filepath.IsAbs(file.Path) || file.Path == "" || filepath.Clean(file.Path) != file.Path || filepath.Dir(file.Path) == "." && filepath.Base(file.Path) == ".." {
			return fmt.Errorf("unsafe dependency path %q", file.Path)
		}
		path := filepath.Join(project, file.Path)
		rel, err := filepath.Rel(project, path)
		if err != nil || rel == ".." || len(rel) > 3 && rel[:3] == ".."+string(filepath.Separator) {
			return fmt.Errorf("unsafe dependency path %q", file.Path)
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

func writeGladeRuleProbe(project string, rule Rule, relativePath string) error {
	path := filepath.Join(project, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(rule.Source), 0o600)
}

func gladeProbePath(rule Rule) (string, error) {
	name := unsafeFilename.ReplaceAllString(rule.ID, "_")
	if name == "" {
		name = "Probe"
	}
	switch rule.SourceKind {
	case "class":
		return filepath.Join("force-app", "main", "default", "classes", name+".cls"), nil
	case "trigger":
		return filepath.Join("force-app", "main", "default", "triggers", name+".trigger"), nil
	default:
		return "", fmt.Errorf("unsupported source kind %q", rule.SourceKind)
	}
}
