package toolcli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOracleStdlibManifestValidationHappensBeforeSF(t *testing.T) {
	binDir := t.TempDir()
	called := filepath.Join(binDir, "sf-called")
	sfPath := filepath.Join(binDir, "sf")
	if err := os.WriteFile(sfPath, []byte("#!/bin/sh\ntouch "+called+"\nprintf '%s\\n' 'GLADE_STDLIB_ORACLE:[]'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manifest := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(manifest, []byte(`[{"id":"invalid","area":"System.Assert","api":"System.Assert.isTrue","mode":"anonymous","surfaceIds":["*"],"expression":"true"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"oracle-stdlib", "--target-org", "org", "--cases", manifest}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "canonical") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("sf was invoked, marker stat error = %v", err)
	}
}

func TestOracleStdlibManifestAndWorkDirReachAnonymousRunner(t *testing.T) {
	binDir := t.TempDir()
	sfPath := filepath.Join(binDir, "sf")
	if err := os.WriteFile(sfPath, []byte("#!/bin/sh\nprintf '%s\\n' 'GLADE_STDLIB_ORACLE:[{\"id\":\"assert-true\",\"area\":\"System.Assert\",\"api\":\"System.Assert.isTrue\",\"mode\":\"anonymous\",\"value\":\"true\",\"valueType\":\"Boolean\"}]'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manifest := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(manifest, []byte(`[{"id":"assert-true","area":"System.Assert","api":"System.Assert.isTrue","mode":"anonymous","surfaceIds":["apex:System.Assert.isTrue(Boolean)"],"expression":"true","valueType":"Boolean"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(t.TempDir(), "oracle-work")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"oracle-stdlib", "--target-org", "org", "--cases", manifest, "--work-dir", workDir, "--output", reportPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(workDir, "probe.apex")); err != nil {
		t.Fatalf("retained probe: %v", err)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"id": "assert-true"`) {
		t.Fatalf("report = %s", data)
	}
}
