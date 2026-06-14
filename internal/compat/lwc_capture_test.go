package compat

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunLwcCaptureSkipDeployWritesFixtureReport(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "capture.json")

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:    root,
		TargetOrg:  "oaer-probe-max",
		Hosts:      []string{"lightning-shell", "visualforce-lightning-out"},
		Out:        out,
		SkipDeploy: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if report.Command != "glade compat lwc capture" {
		t.Fatalf("command = %q", report.Command)
	}
	if report.TargetOrg != "oaer-probe-max" || report.Deployed {
		t.Fatalf("target/deploy = %#v", report)
	}
	if report.Mode != "fixture-manifest" {
		t.Fatalf("mode = %q", report.Mode)
	}
	if report.Counts.Targets != 9 || report.Counts.Prepared != 9 || report.Counts.Pass != 0 || report.Counts.Fail != 0 {
		t.Fatalf("counts = %#v", report.Counts)
	}
	if report.Artifacts.Report != out {
		t.Fatalf("artifacts = %#v", report.Artifacts)
	}

	gotNames := make([]string, 0, len(report.Cases))
	for _, c := range report.Cases {
		gotNames = append(gotNames, c.Name)
		if c.Status != "prepared" {
			t.Fatalf("%s status = %q", c.Name, c.Status)
		}
		if !strings.HasPrefix(c.TargetURL, "fixture://lwc/") {
			t.Fatalf("%s target URL = %q", c.Name, c.TargetURL)
		}
	}
	wantNames := []string{
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
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("case names = %#v", gotNames)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"deployed": false`) || !strings.Contains(string(data), `"visualforce-lightning-out"`) {
		t.Fatalf("capture json = %s", data)
	}
}

func TestRunLwcCaptureRequiresTargetOrg(t *testing.T) {
	_, err := RunLwcCapture(context.Background(), LwcCaptureOptions{SkipDeploy: true})
	if err == nil || !strings.Contains(err.Error(), "--target-org is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunLwcCaptureFiltersTargetsByIncludedHosts(t *testing.T) {
	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:    t.TempDir(),
		TargetOrg:  "oaer-probe-max",
		Targets:    []string{"visualforce-lightning-out"},
		Hosts:      []string{"lightning-shell"},
		SkipDeploy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.Targets != 0 || len(report.Cases) != 0 {
		t.Fatalf("lightning-shell host should filter Visualforce target: %#v", report)
	}

	report, err = RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:    t.TempDir(),
		TargetOrg:  "oaer-probe-max",
		Targets:    []string{"direct-component"},
		Hosts:      []string{"visualforce-lightning-out"},
		SkipDeploy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.Targets != 0 || len(report.Cases) != 0 {
		t.Fatalf("visualforce host should filter direct component target: %#v", report)
	}
}

func TestWriteLwcCaptureTextReportsCountsAndArtifact(t *testing.T) {
	var out bytes.Buffer
	WriteLwcCaptureText(&out, LwcCaptureReport{
		Counts: LwcCaptureCounts{Targets: 9, Prepared: 9, Pass: 0, Fail: 0},
		Artifacts: LwcCaptureArtifacts{
			Report: "/tmp/glade-lwc-capture.json",
		},
	})

	if got, want := out.String(), "prepared 9 LWC fixture-manifest targets: prepared=9 pass=0 fail=0 artifacts=/tmp/glade-lwc-capture.json\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}
