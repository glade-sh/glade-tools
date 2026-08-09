package corpusassurance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAssuranceOutcomesRequiresOneDefensibleOutcomePerSurface(t *testing.T) {
	rows := []AssuranceSurfaceRow{
		{SurfaceID: "apex:Compile.only", CompileReady: true},
		{SurfaceID: "apex:Test.ready", CompileReady: true, TestReady: true},
		{SurfaceID: "apex:Runtime.ready", CompileReady: true, TestReady: true, RuntimeParityReady: true},
		{SurfaceID: "apex:Hosted.only", NonParity: true, ExclusionClass: "hosted", ExclusionReason: "requires org identity"},
	}
	if err := ValidateAssuranceOutcomes(rows); err != nil {
		t.Fatalf("ValidateAssuranceOutcomes: %v", err)
	}
	rows[1].NonParity = true
	if err := ValidateAssuranceOutcomes(rows); err == nil {
		t.Fatal("accepted a surface with parity and non-parity outcomes")
	}
}

func TestWriteAssuranceHTMLIsSelfContainedAndCreateOnly(t *testing.T) {
	root := t.TempDir()
	reportPath := filepath.Join(root, "ASSURANCE.json")
	outputPath := filepath.Join(root, "ASSURANCE.html")
	data := []byte(`{"schemaVersion":1,"rows":[{"surfaceId":"apex:Example.run","repositoryIds":["private-corpus-001"]}]}`)
	if err := os.WriteFile(reportPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAssuranceHTML(reportPath, outputPath); err != nil {
		t.Fatalf("WriteAssuranceHTML: %v", err)
	}
	html, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"private-corpus-001", "apex:Example.run", "id=\"assurance-data\"", "filter"} {
		if !strings.Contains(string(html), text) {
			t.Fatalf("HTML misses %q", text)
		}
	}
	if err := WriteAssuranceHTML(reportPath, outputPath); err == nil {
		t.Fatal("WriteAssuranceHTML overwrote output")
	}
}
