package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const (
	databaseQueryLocatorFixture          = "core-runtime-database-query-locator-api67.json"
	databaseQueryLocatorLocalOnlyFixture = "core-runtime-database-query-locator-local-only-api67.json"
)

var databaseQueryLocatorHostableIDs = []string{
	"apex:Database.QueryLocator.getQuery()",
	"apex:Database.QueryLocator.iterator()",
	"apex:Database.QueryLocatorIterator.hasNext()",
	"apex:Database.QueryLocatorIterator.next()",
}

var databaseQueryLocatorLocalOnlyIDs = []string{
	"apex:System.Database.getQueryLocator(List,Object)",
	"apex:Database.QueryLocator.QueryLocator()",
	"apex:Database.QueryLocator.equals(Object)",
	"apex:Database.QueryLocator.hashCode()",
	"apex:Database.QueryLocator.querymore(Integer)",
	"apex:Database.QueryLocator.toString()",
	"apex:Database.QueryLocatorChunkIterator",
	"apex:Database.QueryLocatorChunkIterator.QueryLocatorChunkIterator()",
	"apex:Database.QueryLocatorIterator.QueryLocatorIterator()",
	"apex:Database.QueryLocatorIterator.clone()",
}

type databaseQueryLocatorMetadata struct {
	APIVersion                string `json:"apiVersion"`
	Mode                      string `json:"mode"`
	Notes                     string `json:"notes"`
	EvidenceOnly              bool   `json:"evidenceOnly"`
	SalesforceEligible        *bool  `json:"salesforceEligible"`
	SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
	SalesforceExclusionReason string `json:"salesforceExclusionReason"`
	Salesforce                any    `json:"salesforce"`
	Comparisons               any    `json:"comparisons"`
	Profile                   struct {
		CandidateCommit string `json:"candidateCommit"`
		CandidateSHA256 string `json:"candidateSha256"`
		SelectedRows    int    `json:"selectedRowCount"`
	} `json:"profile"`
}

func TestDatabaseQueryLocatorHasExactExecutableLocalEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixtures := []struct {
		name     string
		ids      []string
		eligible bool
		witness  []string
	}{
		{
			name:     databaseQueryLocatorFixture,
			ids:      databaseQueryLocatorHostableIDs,
			eligible: true,
			witness: []string{
				"Database.getQueryLocator(queryText)",
				"locator.getQuery()",
				"locator.iterator()",
				"rowIterator.hasNext()",
				"rowIterator.next()",
			},
		},
		{
			name: databaseQueryLocatorLocalOnlyFixture,
			ids:  databaseQueryLocatorLocalOnlyIDs,
			witness: []string{
				"Object accessMode = AccessLevel.USER_MODE",
				"Database.QueryLocator broadAccessLocator = Database.getQueryLocator([SELECT Id, Name FROM Account ORDER BY Name], accessMode)",
				"new Database.QueryLocator()",
				"!firstLocator.equals(secondLocator)",
				"firstLocator.hashCode()",
				"firstLocator.querymore(1)",
				"firstLocator.toString()",
				"new Database.QueryLocatorChunkIterator()",
				"new Database.QueryLocatorIterator()",
				"original.clone()",
				"original !== cloned",
				"original.next()",
				"cloned.next()",
			},
		},
	}

	allIDs := append(append([]string{}, databaseQueryLocatorHostableIDs...), databaseQueryLocatorLocalOnlyIDs...)
	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			fixturePath := filepath.Join(root, "docs", "fixtures", tc.name)
			fixture, err := compat.LoadFile(fixturePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := compat.Validate(fixture); err != nil {
				t.Fatal(err)
			}
			if fixture.Name != strings.TrimSuffix(tc.name, ".json") || fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] {
				t.Fatalf("fixture execution envelope = %#v", fixture)
			}

			data, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatal(err)
			}
			var metadata databaseQueryLocatorMetadata
			if err := json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata.APIVersion != "67.0" || metadata.Mode != "local-runtime" || metadata.EvidenceOnly || metadata.SalesforceEligible == nil || *metadata.SalesforceEligible != tc.eligible || metadata.Profile.CandidateCommit != "3409c4c85827b19712e9df83fc8905aa02bd1dc8" || metadata.Profile.CandidateSHA256 != "960ac9f26fa92aae6054cbe0e59f9c4ab1f84397df67bd8a89528068d02a1fce" || metadata.Profile.SelectedRows != len(tc.ids) {
				t.Fatalf("fixture provenance = %#v", metadata)
			}
			if metadata.Salesforce != nil || metadata.Comparisons != nil || !strings.Contains(metadata.Notes, "no hosted Salesforce execution or parity claim") {
				t.Fatalf("fixture makes an unsupported Salesforce parity claim: %#v", metadata)
			}
			if tc.eligible {
				if metadata.SalesforceExclusionClass != "" || metadata.SalesforceExclusionReason != "" || strings.Contains(fixture.Source[0].Content, "new Database.QueryLocator") {
					t.Fatalf("hostable fixture policy/source = %#v / %q", metadata, fixture.Source[0].Content)
				}
			} else if metadata.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(metadata.SalesforceExclusionReason, "zero Salesforce parity") {
				t.Fatalf("local-only fixture policy = %#v", metadata)
			}

			evidence, err := BuildEvidenceSnapshot([]string{fixturePath})
			if err != nil {
				t.Fatal(err)
			}
			assertExactSurfaceSet(t, evidence, tc.ids)
			for _, row := range evidence {
				if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
					t.Fatalf("%s evidence/behavior = %s/%s, want fixture/supported", row.SurfaceID, row.Evidence, row.GladeBehavior)
				}
			}
			for _, witness := range tc.witness {
				if !strings.Contains(fixture.Source[0].Content, witness) {
					t.Fatalf("source missing executable witness %q", witness)
				}
			}
			if result, err := compat.Run(fixture); err != nil || !result.OK {
				t.Fatalf("fixture execution = %#v, error = %v", result, err)
			}
		})
	}

	want := mapFromIDs(allIDs)
	owners := make(map[string]int, len(want))
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		var header struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		readJSON(t, path, &header)
		if header.EvidenceOnly {
			continue
		}
		for _, row := range header.Evidence {
			if want[row.SurfaceID] {
				owners[row.SurfaceID]++
			}
		}
	}
	for _, id := range allIDs {
		if owners[id] != 1 {
			t.Fatalf("fixture ownership for %s = %d, want exactly one non-evidenceOnly owner", id, owners[id])
		}
	}
}
