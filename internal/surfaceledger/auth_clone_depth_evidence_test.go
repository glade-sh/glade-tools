package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const authCloneDepthFixture = "core-runtime-auth-clone-depth"

var authCloneDepthIDs = []string{
	"apex:Auth.JWS.clone()",
	"apex:Auth.JWTBearerTokenExchange.clone()",
	"apex:Auth.VerificationResult.clone()",
}

func TestAuthCloneDepthHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", authCloneDepthFixture+".json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Name != authCloneDepthFixture || fixture.Command.Kind != "exec" || len(fixture.Source) != 1 || len(fixture.Command.Args) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
		t.Fatalf("fixture envelope = %#v", fixture)
	}
	if len(fixture.Evidence) != len(authCloneDepthIDs) {
		t.Fatalf("raw evidence rows = %d, want %d", len(fixture.Evidence), len(authCloneDepthIDs))
	}
	want := make(map[string]bool, len(authCloneDepthIDs))
	for _, id := range authCloneDepthIDs {
		want[id] = true
	}
	for _, item := range fixture.Evidence {
		if !want[item.SurfaceID] || item.Kind != "exec" {
			t.Fatalf("unexpected evidence row = %#v", item)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	owned := make([]string, 0, len(paths))
	counts := make(map[string]int, len(want))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatal(err)
		}
		if header.EvidenceOnly {
			continue
		}
		owned = append(owned, path)
		for _, item := range header.Evidence {
			if want[item.SurfaceID] {
				counts[item.SurfaceID]++
			}
		}
	}
	for _, id := range authCloneDepthIDs {
		if counts[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one", id, counts[id])
		}
	}

	evidence, err := BuildEvidenceSnapshot(owned)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range authCloneDepthIDs {
		rows := rowsBySurfaceID(evidence, id)
		if len(rows) != 1 || rows[0].Evidence != EvidenceFixture || rows[0].GladeBehavior != BehaviorSupported || len(rows[0].Sources) != 1 || rows[0].Sources[0] != "fixture:"+authCloneDepthFixture {
			t.Fatalf("%s evidence row = %#v", id, rows)
		}
	}

	source := fixture.Source[0].Content
	for _, witness := range []string{
		"Auth.JWS clonedJws = (Auth.JWS)jws.clone();",
		"Auth.JWTBearerTokenExchange clonedExchange = (Auth.JWTBearerTokenExchange)exchange.clone();",
		"Auth.VerificationResult clonedVerification = (Auth.VerificationResult)verification.clone();",
		"System.assert(clonedJws != jws);",
		"System.assert(clonedExchange != exchange);",
		"System.assert(clonedVerification != verification);",
		"System.assertEquals('ok', clonedVerification.message);",
	} {
		if !strings.Contains(source, witness) {
			t.Fatalf("source missing direct assertion %q", witness)
		}
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Mode                      string `json:"mode"`
		SalesforceEligible        *bool  `json:"salesforceEligible"`
		SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
		SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Mode != "local-runtime" || policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "zero Salesforce parity") {
		t.Fatalf("fixture policy = %#v", policy)
	}
	if result, err := compat.Run(fixture); err != nil || !result.OK {
		t.Fatalf("run %s = %#v, %v", fixture.Name, result, err)
	}
}
