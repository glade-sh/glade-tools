package toolcli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunApexRulesValidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, []byte(`{"rules":[{"id":"APEX-001","area":"identifiers","docsPath":"reference","docsLines":"1","sourceKind":"class","source":"public class Probe {}","oracle":"reject","owner":"parser","status":"supported","productTest":"internal/apexast/parser_test.go:TestReserved"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"apex-rules", "validate", "--catalog", path}, &stdout, &stderr); code != 0 || stdout.String() != "ok\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
