package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const g3TestSetMockStringSurfaceID = "apex:System.Test.setMock(String,Object)"

func TestG3TestSetMockStringEvidenceIsExactAndExecutable(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "g3-test-setmock-string-local.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		Evidence                  []struct {
			SurfaceID string `json:"surfaceId"`
			Symbol    string `json:"symbol"`
			Kind      string `json:"kind"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.SalesforceEligible == nil || *raw.SalesforceEligible || raw.SalesforceExclusionClass != "policy-local-only" || raw.SalesforceExclusionReason != "Glade accepts the String-key Test.setMock extension locally; it is not a Salesforce parity claim." {
		t.Fatalf("local-only metadata = %#v", raw)
	}
	if len(raw.Evidence) != 1 || raw.Evidence[0].SurfaceID != g3TestSetMockStringSurfaceID || raw.Evidence[0].Symbol != "Test.setMock(String,Object)" || raw.Evidence[0].Kind != "exec" {
		t.Fatalf("raw evidence = %#v", raw.Evidence)
	}

	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != "g3-test-setmock-string-local" || fixture.Command.Kind != "test" || len(fixture.Command.Args) != 0 || len(fixture.Source) != 2 || fixture.Source[0].Path != "force-app/main/default/classes/G3StringKeyHttpMock.cls" || fixture.Source[1].Path != "force-app/main/default/classes/G3StringKeyHttpMockTest.cls" {
		t.Fatalf("fixture execution metadata = %#v", fixture)
	}

	evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, evidence, []string{g3TestSetMockStringSurfaceID})
	if evidence[0].Evidence != EvidenceFixture || evidence[0].GladeBehavior != BehaviorSupported {
		t.Fatalf("fixture evidence = %#v, want fixture/supported", evidence[0])
	}

	gladeRow, ok := rowsByID(BuildGladeSnapshot())[g3TestSetMockStringSurfaceID]
	if !ok || gladeRow.GladeShape != ShapeSignatureKnown || gladeRow.GladeBehavior != BehaviorSupported || gladeRow.Kind != KindMethod {
		t.Fatalf("Glade snapshot row = %#v, want signature-known/supported method", gladeRow)
	}

	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("fixture execution = %#v, error = %v", result, err)
	}
}
