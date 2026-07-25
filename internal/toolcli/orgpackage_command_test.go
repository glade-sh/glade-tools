package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOrgPackageCaptureParsesFlagsAndWritesArtifact(t *testing.T) {
	restore := stubOrgPackageCapture(t, func(ctx context.Context, opts orgPackageCaptureOptions) (orgPackageCaptureResult, error) {
		if opts.TargetOrg != "packaging" || opts.Namespace != "pkg" || opts.Output == "" {
			t.Fatalf("capture opts = %#v", opts)
		}
		if opts.APIVersion != "65.0" || opts.SFBin != "/usr/local/bin/sf" || !opts.JSON {
			t.Fatalf("capture opts = %#v", opts)
		}
		return orgPackageCaptureResult{
			Namespace:     opts.Namespace,
			PackageName:   "Billing Core",
			Version:       "1.2.3.4",
			APIVersion:    opts.APIVersion,
			Output:        opts.Output,
			ApexTypes:     2,
			Objects:       3,
			Labels:        1,
			ArtifactJSON:  []byte(`{"namespace":"pkg"}`),
			ConfigSnippet: orgPackageConfigSnippet(opts.Namespace, opts.Output, "1.2.3.4"),
		}, nil
	})
	defer restore()

	outPath := filepath.Join(t.TempDir(), "pkg.glade-package.json")
	var stdout, stderr bytes.Buffer
	code := RunOrgPackage(context.Background(), []string{
		"capture",
		"--target-org", "packaging",
		"--namespace", "pkg",
		"--output", outPath,
		"--api-version", "65.0",
		"--sf-bin", "/usr/local/bin/sf",
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"namespace\":\"pkg\"}\n" {
		t.Fatalf("artifact = %q", string(data))
	}
	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if report["namespace"] != "pkg" || report["output"] != outPath {
		t.Fatalf("report = %#v", report)
	}
}

func TestOrgPackageCaptureConfigSnippet(t *testing.T) {
	restore := stubOrgPackageCapture(t, func(ctx context.Context, opts orgPackageCaptureOptions) (orgPackageCaptureResult, error) {
		return orgPackageCaptureResult{
			Namespace:     opts.Namespace,
			Output:        opts.Output,
			ArtifactJSON:  []byte(`{"namespace":"pkg"}`),
			ConfigSnippet: orgPackageConfigSnippet(opts.Namespace, opts.Output, ""),
		}, nil
	})
	defer restore()

	outPath := filepath.Join(t.TempDir(), "pkg.glade-package.json")
	var stdout, stderr bytes.Buffer
	code := RunOrgPackage(context.Background(), []string{
		"orgpackage", "capture",
		"--target-org", "packaging",
		"--namespace", "pkg",
		"--output", outPath,
		"--config-snippet",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	for _, want := range []string{"managedPackageDependencies", "pkg:artifact:" + outPath} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("snippet missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestOrgPackageCaptureWriteFailureDoesNotClaimSuccess(t *testing.T) {
	restore := stubOrgPackageCapture(t, func(ctx context.Context, opts orgPackageCaptureOptions) (orgPackageCaptureResult, error) {
		return orgPackageCaptureResult{Namespace: opts.Namespace, Output: opts.Output, ArtifactJSON: []byte(`{}`)}, nil
	})
	defer restore()

	var stdout, stderr bytes.Buffer
	code := RunOrgPackage(context.Background(), []string{
		"capture",
		"--target-org", "packaging",
		"--namespace", "pkg",
		"--output", t.TempDir(),
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected write failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "captured") {
		t.Fatalf("stdout claimed success after write failure: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "is a directory") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestOrgPackageCaptureReportsCaptureFailure(t *testing.T) {
	restore := stubOrgPackageCapture(t, func(context.Context, orgPackageCaptureOptions) (orgPackageCaptureResult, error) {
		return orgPackageCaptureResult{}, errors.New("sf api request rest failed")
	})
	defer restore()

	var stdout, stderr bytes.Buffer
	code := RunOrgPackage(context.Background(), []string{
		"capture",
		"--target-org", "packaging",
		"--namespace", "pkg",
		"--output", filepath.Join(t.TempDir(), "pkg.glade-package.json"),
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected missing implementation to fail stdout=%q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "sf api request rest failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestOrgPackageHelpAndManifest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunOrgPackage(context.Background(), []string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("help exit %d stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"orgpackage capture", "--target-org", "--config-snippet"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("help missing %q:\n%s", want, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = RunOrgPackage(context.Background(), []string{"manifest", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("manifest exit %d stderr=%q", code, stderr.String())
	}
	var manifest pluginManifest
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, stdout.String())
	}
	if manifest.Name != "orgpackage" {
		t.Fatalf("manifest name = %q", manifest.Name)
	}
	if manifest.Editor != nil {
		t.Fatalf("orgpackage manifest editor = %#v", manifest.Editor)
	}
	if got := runtimeCommandSummaryByPath(manifest.Commands); got["orgpackage capture"] == "" {
		t.Fatalf("manifest commands = %#v", got)
	}
}

func TestRunRoutesOrgPackageRootAndKeepsCompatRoot(t *testing.T) {
	restore := stubOrgPackageCapture(t, func(ctx context.Context, opts orgPackageCaptureOptions) (orgPackageCaptureResult, error) {
		return orgPackageCaptureResult{Namespace: opts.Namespace, Output: opts.Output, ArtifactJSON: []byte(`{}`)}, nil
	})
	defer restore()

	outPath := filepath.Join(t.TempDir(), "pkg.glade-package.json")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"orgpackage", "capture",
		"--target-org", "packaging",
		"--namespace", "pkg",
		"--output", outPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("orgpackage exit %d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"compat", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("compat help exit %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "glade-tools matrix --json") {
		t.Fatalf("compat help changed:\n%s", stdout.String())
	}
}

func TestPluginArchiveIndexIncludesOrgPackage(t *testing.T) {
	scriptData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "build-plugin-archives.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptData)
	for _, want := range []string{"build_archive orgpackage", "scripts/build-plugin-registry.py"} {
		if !strings.Contains(script, want) {
			t.Fatalf("archive script missing %q", want)
		}
	}

	builderData, err := os.ReadFile(filepath.Join("..", "..", "scripts", "build-plugin-registry.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(builderData), `"name": "@glade/orgpackage"`) {
		t.Fatal("registry builder is missing the canonical @glade/orgpackage coordinate")
	}
}

func stubOrgPackageCapture(t *testing.T, fn func(context.Context, orgPackageCaptureOptions) (orgPackageCaptureResult, error)) func() {
	t.Helper()
	previous := captureOrgPackage
	captureOrgPackage = fn
	return func() {
		captureOrgPackage = previous
	}
}
