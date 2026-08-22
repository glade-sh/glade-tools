package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestScalarConstructorAndCookieListEvidenceIsExact(t *testing.T) {
	root := filepath.Join("..", "..")
	cases := map[string]string{
		"current-api67-negative-date-constructor.json":     "apex:System.Date.Date()",
		"current-api67-negative-datetime-constructor.json": "apex:System.Datetime.Datetime()",
		"current-api67-negative-decimal-constructor.json":  "apex:System.Decimal.Decimal()",
		"current-api67-negative-double-constructor.json":   "apex:System.Double.Double()",
	}
	owners := make(map[string]int, len(cases))
	for _, id := range cases {
		owners[id] = 0
	}
	positiveIDs := map[string]bool{
		"apex:System.Cookie.equals(Object)": true,
		"apex:System.List.List(Integer)":    true,
	}
	for id := range positiveIDs {
		owners[id] = 0
	}

	for filename, id := range cases {
		path := filepath.Join(root, "docs", "fixtures", filename)
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || len(fixture.Source) != 1 || fixture.Source[0].Content != fixture.Command.Args[0] || len(fixture.Evidence) != 1 || fixture.Evidence[0].SurfaceID != id || fixture.Evidence[0].Kind != "unsupported" {
			t.Fatalf("%s envelope = %#v", filename, fixture)
		}
		result, err := compat.Run(fixture)
		if err != nil || !result.OK || result.Error == nil || result.Error.Type != "Error" || !strings.Contains(result.Error.Message, "Type cannot be constructed") {
			t.Fatalf("%s result = %#v, error = %v", filename, result, err)
		}
		rows, err := BuildEvidenceSnapshot([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].SurfaceID != id || rows[0].Evidence != EvidenceFixture || rows[0].GladeBehavior != BehaviorUnsupported {
			t.Fatalf("%s evidence = %#v", filename, rows)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var metadata struct {
			Candidate struct {
				Commit string `json:"commit"`
				SHA    string `json:"sha256"`
			} `json:"candidate"`
			Profile struct {
				CandidateCommit string `json:"candidateCommit"`
				CandidateSHA    string `json:"candidateSha256"`
				SelectedRows    int    `json:"selectedRowCount"`
			} `json:"profile"`
			Eligible        *bool  `json:"salesforceEligible"`
			ExclusionClass  string `json:"salesforceExclusionClass"`
			ExclusionReason string `json:"salesforceExclusionReason"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.Candidate.Commit != "693bc1b8652907eee1c40c1c9f4604637f06a172" || metadata.Candidate.SHA != "235ef5f5fd6b35a9eec2ab81c129c2639c0282ff66573e8dbace80e991481bc3" || metadata.Profile.CandidateCommit != metadata.Candidate.Commit || metadata.Profile.CandidateSHA != metadata.Candidate.SHA || metadata.Profile.SelectedRows != 1 || metadata.Eligible == nil || *metadata.Eligible || metadata.ExclusionClass != "policy-local-only" || !strings.Contains(metadata.ExclusionReason, "no Salesforce runtime parity") {
			t.Fatalf("%s provenance = %#v", filename, metadata)
		}
	}

	positivePath := filepath.Join(root, "docs", "fixtures", "core-runtime-cookie-list-integer-api67.json")
	positive, err := compat.LoadFile(positivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(positive); err != nil {
		t.Fatal(err)
	}
	if positive.Command.Kind != "exec" || len(positive.Command.Args) != 1 || len(positive.Source) != 1 || positive.Source[0].Content != positive.Command.Args[0] || len(positive.Evidence) != len(positiveIDs) {
		t.Fatalf("positive fixture envelope = %#v", positive)
	}
	for _, row := range positive.Evidence {
		if !positiveIDs[row.SurfaceID] || row.Kind != "exec" {
			t.Fatalf("positive evidence = %#v", positive.Evidence)
		}
	}
	if result, err := compat.Run(positive); err != nil || !result.OK {
		t.Fatalf("positive fixture result = %#v, error = %v", result, err)
	}
	positiveRows, err := BuildEvidenceSnapshot([]string{positivePath})
	if err != nil {
		t.Fatal(err)
	}
	if len(positiveRows) != len(positiveIDs) {
		t.Fatalf("positive snapshot = %#v", positiveRows)
	}
	for _, row := range positiveRows {
		if !positiveIDs[row.SurfaceID] || row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("positive snapshot row = %#v", row)
		}
	}
	positiveData, err := os.ReadFile(positivePath)
	if err != nil {
		t.Fatal(err)
	}
	var positiveMetadata struct {
		Candidate struct {
			Commit string `json:"commit"`
			SHA    string `json:"sha256"`
		} `json:"candidate"`
		Profile struct {
			CandidateCommit string `json:"candidateCommit"`
			CandidateSHA    string `json:"candidateSha256"`
			SelectedRows    int    `json:"selectedRowCount"`
		} `json:"profile"`
		Eligible        *bool  `json:"salesforceEligible"`
		ExclusionClass  string `json:"salesforceExclusionClass"`
		ExclusionReason string `json:"salesforceExclusionReason"`
	}
	if err := json.Unmarshal(positiveData, &positiveMetadata); err != nil {
		t.Fatal(err)
	}
	if positiveMetadata.Candidate.Commit != "693bc1b8652907eee1c40c1c9f4604637f06a172" || positiveMetadata.Candidate.SHA != "235ef5f5fd6b35a9eec2ab81c129c2639c0282ff66573e8dbace80e991481bc3" || positiveMetadata.Profile.CandidateCommit != positiveMetadata.Candidate.Commit || positiveMetadata.Profile.CandidateSHA != positiveMetadata.Candidate.SHA || positiveMetadata.Profile.SelectedRows != len(positiveIDs) || positiveMetadata.Eligible == nil || *positiveMetadata.Eligible || positiveMetadata.ExclusionClass != "policy-local-only" || !strings.Contains(positiveMetadata.ExclusionReason, "no hosted Salesforce parity") {
		t.Fatalf("positive provenance = %#v", positiveMetadata)
	}

	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		if json.Unmarshal(data, &fixture) != nil || fixture.EvidenceOnly {
			continue
		}
		for _, row := range fixture.Evidence {
			if _, ok := owners[row.SurfaceID]; ok {
				owners[row.SurfaceID]++
			}
		}
	}
	for id, count := range owners {
		if count != 1 {
			t.Errorf("non-evidenceOnly owners for %s = %d, want 1", id, count)
		}
	}
}
