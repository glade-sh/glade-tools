package compat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type LwcCaptureOptions struct {
	Project    string
	TargetOrg  string
	Targets    []string
	Hosts      []string
	Out        string
	SkipDeploy bool
}

type LwcCaptureReport struct {
	Command   string              `json:"command"`
	TargetOrg string              `json:"targetOrg"`
	Project   string              `json:"project,omitempty"`
	Mode      string              `json:"mode"`
	Deployed  bool                `json:"deployed"`
	Hosts     []string            `json:"hosts,omitempty"`
	Cases     []LwcCaptureCase    `json:"cases"`
	Counts    LwcCaptureCounts    `json:"counts"`
	Artifacts LwcCaptureArtifacts `json:"artifacts"`
}

type LwcCaptureCase struct {
	Name      string `json:"name"`
	Host      string `json:"host,omitempty"`
	Status    string `json:"status"`
	TargetURL string `json:"targetUrl"`
}

type LwcCaptureCounts struct {
	Targets  int `json:"targets"`
	Prepared int `json:"prepared"`
	Pass     int `json:"pass"`
	Fail     int `json:"fail"`
}

type LwcCaptureArtifacts struct {
	Report string `json:"report,omitempty"`
}

var defaultLwcCaptureCases = []string{
	"direct-component",
	"record-page",
	"app-page",
	"home-page",
	"custom-tab",
	"visualforce-lightning-out",
	"apex-wire",
	"imperative-apex",
	"navigation",
}

func RunLwcCapture(ctx context.Context, options LwcCaptureOptions) (LwcCaptureReport, error) {
	if err := ctx.Err(); err != nil {
		return LwcCaptureReport{}, err
	}
	options.TargetOrg = strings.TrimSpace(options.TargetOrg)
	if options.TargetOrg == "" {
		return LwcCaptureReport{}, errors.New("--target-org is required")
	}
	if options.Project == "" {
		options.Project = "."
	}
	absProject, err := filepath.Abs(options.Project)
	if err != nil {
		return LwcCaptureReport{}, err
	}
	targets, err := normalizeLwcCaptureTargets(options.Targets)
	if err != nil {
		return LwcCaptureReport{}, err
	}
	hosts := normalizeLwcCaptureList(options.Hosts)
	report := LwcCaptureReport{
		Command:   "glade compat lwc capture",
		TargetOrg: options.TargetOrg,
		Project:   absProject,
		Mode:      "fixture-manifest",
		Hosts:     hosts,
		Artifacts: LwcCaptureArtifacts{Report: options.Out},
	}
	if !options.SkipDeploy {
		if err := runLwcCaptureDeploy(ctx, absProject, options.TargetOrg); err != nil {
			report.Counts.Fail = len(targets)
			return report, err
		}
		report.Deployed = true
	}
	report.Cases = buildLwcCaptureCases(targets, hosts)
	report.Counts.Targets = len(report.Cases)
	for _, c := range report.Cases {
		if c.Status == "prepared" {
			report.Counts.Prepared++
		} else if c.Status == "pass" {
			report.Counts.Pass++
		} else {
			report.Counts.Fail++
		}
	}
	if options.Out != "" {
		if err := WriteLwcCaptureJSON(options.Out, report); err != nil {
			return report, err
		}
	}
	return report, nil
}

func WriteLwcCaptureJSON(path string, report LwcCaptureReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func WriteLwcCaptureText(w io.Writer, report LwcCaptureReport) {
	mode := strings.TrimSpace(report.Mode)
	if mode == "" {
		mode = "fixture-manifest"
	}
	fmt.Fprintf(w, "prepared %d LWC %s targets: prepared=%d pass=%d fail=%d artifacts=%s\n", report.Counts.Targets, mode, report.Counts.Prepared, report.Counts.Pass, report.Counts.Fail, report.Artifacts.Report)
}

func normalizeLwcCaptureTargets(values []string) ([]string, error) {
	targets := normalizeLwcCaptureList(values)
	if len(targets) == 0 {
		return append([]string(nil), defaultLwcCaptureCases...), nil
	}
	known := make(map[string]bool, len(defaultLwcCaptureCases))
	for _, name := range defaultLwcCaptureCases {
		known[name] = true
	}
	for _, target := range targets {
		if !known[target] {
			return nil, fmt.Errorf("unknown LWC capture target %q", target)
		}
	}
	return targets, nil
}

func normalizeLwcCaptureList(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			result = append(result, part)
		}
	}
	return result
}

func buildLwcCaptureCases(targets, hosts []string) []LwcCaptureCase {
	result := make([]LwcCaptureCase, 0, len(targets))
	for _, target := range targets {
		host := lwcCaptureHostForTarget(target, hosts)
		if host == "" {
			continue
		}
		result = append(result, LwcCaptureCase{
			Name:      target,
			Host:      host,
			Status:    "prepared",
			TargetURL: "fixture://lwc/" + target + lwcCaptureHostQuery(host),
		})
	}
	return result
}

func lwcCaptureHostForTarget(target string, hosts []string) string {
	naturalHost := "lightning-shell"
	if target == "visualforce-lightning-out" {
		naturalHost = "visualforce-lightning-out"
	}
	if len(hosts) == 0 || containsLwcCaptureHost(hosts, naturalHost) {
		return naturalHost
	}
	return ""
}

func containsLwcCaptureHost(hosts []string, want string) bool {
	for _, host := range hosts {
		if host == want {
			return true
		}
	}
	return false
}

func lwcCaptureHostQuery(host string) string {
	if host == "" {
		return ""
	}
	return "?host=" + host
}

func runLwcCaptureDeploy(ctx context.Context, project, targetOrg string) error {
	cmd := exec.CommandContext(ctx, "sf", "project", "deploy", "start", "--target-org", targetOrg, "--source-dir", ".", "--ignore-conflicts", "--json")
	cmd.Dir = project
	cmd.Env = append(os.Environ(), "PWD="+project)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sf project deploy start failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var status struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal(output, &status); err == nil && status.Status != 0 {
		return fmt.Errorf("sf project deploy start returned status %d: %s", status.Status, strings.TrimSpace(string(output)))
	}
	return nil
}

func LwcCaptureCaseNames() []string {
	names := append([]string(nil), defaultLwcCaptureCases...)
	sort.Strings(names)
	return names
}
