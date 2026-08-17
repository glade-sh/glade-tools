package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestG3ApexPagesZeroArgumentFixtureMatchesCandidateRejection(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "current-base-apexpages-set-data-category-zero-negative-api67.json")
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Expected.Error == nil || fixture.Expected.Error.Type != "Error" || fixture.Expected.Error.Message != "expects group and category Strings" {
		t.Fatalf("zero-argument rejection = %#v", fixture.Expected.Error)
	}
	wantSurfaceID := "apex:ApexPages.KnowledgeArticleVersionStandardController.setDataCategory()"
	if fixture.Command.Kind != "exec" || len(fixture.Evidence) != 1 || fixture.Evidence[0].SurfaceID != wantSurfaceID || fixture.Evidence[0].Kind != "negative" {
		t.Fatalf("zero-argument command/evidence = %#v / %#v", fixture.Command, fixture.Evidence)
	}
	wantReason := "The current Salesforce signature requires group and category arguments. Glade currently accepts the zero-argument shape during analysis but rejects it in VM dispatch; this local runtime-negative fixture makes no Salesforce parity claim."
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Mode                      string `json:"mode"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Mode != "local-runtime" || raw.SalesforceEligible == nil || *raw.SalesforceEligible || raw.SalesforceExclusionClass != "policy-local-only" || raw.SalesforceExclusionReason != wantReason {
		t.Fatalf("zero-argument proof boundary = %#v", raw)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("zero-argument fixture result = %#v, err = %v", result, err)
	}
}
