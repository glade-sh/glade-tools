package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestPrivateAuthTypesHaveDirectCompileShapeEvidence(t *testing.T) {
	const owner = "private-corpus-auth-types-compile-shape"
	want := map[string]string{
		"apex:Auth.UserData":            "Auth.UserData data = new Auth.UserData(",
		"apex:Auth.VerificationMethod":  "Auth.VerificationMethod method = Auth.VerificationMethod.EMAIL;",
		"apex:Auth.AuthConfiguration":   "new Auth.AuthConfiguration(",
		"apex:Auth.VerificationResult":  "new Auth.VerificationResult(",
		"apex:Auth.SessionManagement":   "Auth.SessionManagement.getCurrentSession()",
		"apex:Auth.RegistrationHandler": "implements Auth.RegistrationHandler",
		"apex:Auth.AuthToken":           "Auth.AuthToken.revokeAccess(",
	}
	root := filepath.Join("..", "..", "docs", "fixtures")
	fixturePath := filepath.Join(root, owner+".json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "check" || len(fixture.Source) != 1 || len(fixture.Evidence) != len(want) {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	source := fixture.Source[0].Content
	for id, witness := range want {
		if !strings.Contains(source, witness) {
			t.Fatalf("%s source missing %q", id, witness)
		}
	}
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Eligible *bool  `json:"salesforceEligible"`
		Class    string `json:"salesforceExclusionClass"`
		Reason   string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Eligible == nil || *policy.Eligible || policy.Class != "policy-local-only" || !strings.Contains(policy.Reason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}
	result, err := compat.Run(fixture)
	if err != nil || !result.OK {
		t.Fatalf("fixture check = %#v, error = %v", result, err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	for id := range want {
		count := 0
		for _, row := range rows {
			if row.SurfaceID != id {
				continue
			}
			count++
			if len(row.Sources) != 1 || row.Sources[0] != "fixture:"+owner || row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
				t.Fatalf("%s evidence row = %#v", id, row)
			}
		}
		if count != 1 {
			t.Fatalf("%s fixture rows = %d, want exactly one", id, count)
		}
	}
}
