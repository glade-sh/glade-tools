package surfaceledger

import (
	"testing"
)

// --- helpers ---

func surfaceIDs(entries []DeltaEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.SurfaceID
	}
	return ids
}

func stdClassification(id string) ReleaseClassification {
	return ReleaseClassification{
		SurfaceID:   id,
		Scope:       ScopeT0,
		Disposition: DispoExistingCase,
		CaseID:      "CASE-001",
	}
}

func stdClassifyAll(ids ...string) []ReleaseClassification {
	cls := make([]ReleaseClassification, len(ids))
	for i, id := range ids {
		cls[i] = stdClassification(id)
	}
	return cls
}

// --- test case 1: rows join through existing surfaceIDKey canonicalization ---

func TestReleaseDelta_CanonicalIDJoin(t *testing.T) {
	// surfaceIDKey lowercases the rest of apex: IDs — so "apex:System.String"
	// and "apex:system.string" resolve to the same canonical key.
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:System.String", Signature: "old-sig"},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:system.string", Signature: "old-sig"},
	}
	// No classification needed — the row is unchanged, proving only the join.
	added, removed, changed, unchanged, err := ComputeReleaseDelta(prev, current, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("added: got %d, want 0", len(added))
	}
	if len(removed) != 0 {
		t.Errorf("removed: got %d, want 0", len(removed))
	}
	if len(changed) != 0 {
		t.Errorf("changed: got %d, want 0", len(changed))
	}
	if len(unchanged) != 1 {
		t.Errorf("unchanged: got %d, want 1", len(unchanged))
	}
	if unchanged[0].SurfaceID != "apex:system.string" {
		t.Errorf("unchanged SurfaceID: got %q, want %q", unchanged[0].SurfaceID, "apex:system.string")
	}
}

// --- test case 2: new canonical ID is added ---

func TestReleaseDelta_NewIDAdded(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:New.Class", Signature: "v1"},
	}
	added, removed, changed, unchanged, err := ComputeReleaseDelta(prev, current, stdClassifyAll("apex:New.Class"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(added) != 1 {
		t.Fatalf("added: got %d, want 1", len(added))
	}
	if added[0].SurfaceID != "apex:New.Class" {
		t.Errorf("added SurfaceID: got %q, want %q", added[0].SurfaceID, "apex:New.Class")
	}
	if added[0].Kind != DeltaAdded {
		t.Errorf("added Kind: got %q, want %q", added[0].Kind, DeltaAdded)
	}
	if len(removed) != 0 || len(changed) != 0 || len(unchanged) != 0 {
		t.Errorf("other lists should be empty")
	}
}

// --- test case 3: missing canonical ID is removed ---

func TestReleaseDelta_MissingIDRemoved(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:Old.Class", Signature: "v1"},
	}
	current := []SurfaceLedgerRow{}
	added, removed, changed, unchanged, err := ComputeReleaseDelta(prev, current, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("removed: got %d, want 1", len(removed))
	}
	if removed[0].SurfaceID != "apex:Old.Class" {
		t.Errorf("removed SurfaceID: got %q, want %q", removed[0].SurfaceID, "apex:Old.Class")
	}
	if removed[0].Kind != DeltaRemoved {
		t.Errorf("removed Kind: got %q, want %q", removed[0].Kind, DeltaRemoved)
	}
	if len(added) != 0 || len(changed) != 0 || len(unchanged) != 0 {
		t.Errorf("other lists should be empty")
	}
}

// --- test case 4: same ID with changed contract-bearing row is changed ---

func TestReleaseDelta_SameIDChangedContract(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:Foo.Class", Signature: "old-sig", Product: "apex"},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:Foo.Class", Signature: "new-sig", Product: "apex"},
	}
	added, removed, changed, unchanged, err := ComputeReleaseDelta(prev, current, stdClassifyAll("apex:Foo.Class"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changed) != 1 {
		t.Fatalf("changed: got %d, want 1", len(changed))
	}
	if changed[0].SurfaceID != "apex:Foo.Class" {
		t.Errorf("changed SurfaceID: got %q, want %q", changed[0].SurfaceID, "apex:Foo.Class")
	}
	if changed[0].Kind != DeltaChanged {
		t.Errorf("changed Kind: got %q, want %q", changed[0].Kind, DeltaChanged)
	}
	if len(added) != 0 || len(removed) != 0 || len(unchanged) != 0 {
		t.Errorf("other lists should be empty")
	}
}

// --- test case 5: same contract-bearing row is unchanged ---

func TestReleaseDelta_SameRowUnchanged(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:Bar.Class", Signature: "sig", Product: "apex", Kind: KindType},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:Bar.Class", Signature: "sig", Product: "apex", Kind: KindType},
	}
	added, removed, changed, unchanged, err := ComputeReleaseDelta(prev, current, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(unchanged) != 1 {
		t.Fatalf("unchanged: got %d, want 1", len(unchanged))
	}
	if unchanged[0].SurfaceID != "apex:Bar.Class" {
		t.Errorf("unchanged SurfaceID: got %q, want %q", unchanged[0].SurfaceID, "apex:Bar.Class")
	}
	if unchanged[0].Kind != DeltaUnchanged {
		t.Errorf("unchanged Kind: got %q, want %q", unchanged[0].Kind, DeltaUnchanged)
	}
	if len(added) != 0 || len(removed) != 0 || len(changed) != 0 {
		t.Errorf("other lists should be empty")
	}
}

// --- test case 6: all four lists have deterministic SurfaceID ordering ---

func TestReleaseDelta_DeterministicOrdering(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:Z.Class", Signature: "removed"},
		{SurfaceID: "apex:B.Class", Signature: "unchanged"},
		{SurfaceID: "apex:M.Class", Signature: "old"},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:A.Class", Signature: "new"},
		{SurfaceID: "apex:B.Class", Signature: "unchanged"},
		{SurfaceID: "apex:M.Class", Signature: "new"},
	}
	cls := stdClassifyAll("apex:A.Class", "apex:M.Class")
	added, removed, changed, unchanged, err := ComputeReleaseDelta(prev, current, cls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Added sorted: A
	if ids := surfaceIDs(added); !equalStrings(ids, []string{"apex:A.Class"}) {
		t.Errorf("added order: got %v", ids)
	}
	// Removed sorted: Z
	if ids := surfaceIDs(removed); !equalStrings(ids, []string{"apex:Z.Class"}) {
		t.Errorf("removed order: got %v", ids)
	}
	// Changed sorted: M
	if ids := surfaceIDs(changed); !equalStrings(ids, []string{"apex:M.Class"}) {
		t.Errorf("changed order: got %v", ids)
	}
	// Unchanged sorted: B
	if ids := surfaceIDs(unchanged); !equalStrings(ids, []string{"apex:B.Class"}) {
		t.Errorf("unchanged order: got %v", ids)
	}
}

// --- test case 7: duplicate canonical IDs in either input fail ---

func TestReleaseDelta_DuplicatePrevFails(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:Dup.Class", Signature: "a"},
		{SurfaceID: "apex:Dup.Class", Signature: "b"},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:Dup.Class", Signature: "a"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, stdClassifyAll("apex:Dup.Class"))
	if err == nil {
		t.Fatal("expected error for duplicate in prev, got nil")
	}
}

func TestReleaseDelta_DuplicateCurrentFails(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:Dup.Class", Signature: "a"},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:Dup.Class", Signature: "a"},
		{SurfaceID: "apex:Dup.Class", Signature: "b"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, stdClassifyAll("apex:Dup.Class"))
	if err == nil {
		t.Fatal("expected error for duplicate in current, got nil")
	}
}

// --- test case 8: every added or changed row requires explicit classification ---

func TestReleaseDelta_AddedMissingClassificationFails(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:New.Class", Signature: "v1"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, nil)
	if err == nil {
		t.Fatal("expected error for missing classification on added row, got nil")
	}
}

func TestReleaseDelta_ChangedMissingClassificationFails(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:Foo.Class", Signature: "old"},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:Foo.Class", Signature: "new"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, nil)
	if err == nil {
		t.Fatal("expected error for missing classification on changed row, got nil")
	}
}

// --- test case 9: classification uses canonical SurfaceID ---

func TestReleaseDelta_ClassificationUsesCanonicalID(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:New.Class", Signature: "v1"},
	}
	// Classify by canonical (lowercased) form — surfaceIDKey normalizes both.
	cls := stdClassifyAll("apex:new.class")
	added, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(added) != 1 {
		t.Fatalf("added: got %d, want 1", len(added))
	}
	if added[0].SurfaceID != "apex:New.Class" {
		t.Errorf("added SurfaceID: got %q, want %q", added[0].SurfaceID, "apex:New.Class")
	}
}

// --- test case 10: unknown scope or disposition fails ---

func TestReleaseDelta_UnknownScopeFails(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:X.Class", Signature: "v1"},
	}
	cls := []ReleaseClassification{
		{SurfaceID: "apex:X.Class", Scope: "bogus-scope", Disposition: DispoExistingCase, CaseID: "CASE-001"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err == nil {
		t.Fatal("expected error for unknown scope, got nil")
	}
}

func TestReleaseDelta_UnknownDispositionFails(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:X.Class", Signature: "v1"},
	}
	cls := []ReleaseClassification{
		{SurfaceID: "apex:X.Class", Scope: ScopeT0, Disposition: "bogus-dispo", CaseID: "CASE-001"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err == nil {
		t.Fatal("expected error for unknown disposition, got nil")
	}
}

// --- test case 11: T0 and T1 rows require existing-case, new-case, deterministic-mock, or explicit-unsupported + reason/ref ---

func TestReleaseDelta_T0ExistingCaseRequiresCaseID(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:T.Class", Signature: "v1"},
	}
	cls := []ReleaseClassification{
		{SurfaceID: "apex:T.Class", Scope: ScopeT0, Disposition: DispoExistingCase, CaseID: ""},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err == nil {
		t.Fatal("expected error for existing-case with empty case ID, got nil")
	}
}

func TestReleaseDelta_T0NewCaseRequiresCaseID(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:T.Class", Signature: "v1"},
	}
	cls := []ReleaseClassification{
		{SurfaceID: "apex:T.Class", Scope: ScopeT0, Disposition: DispoNewCase, CaseID: ""},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err == nil {
		t.Fatal("expected error for new-case with empty case ID, got nil")
	}
}

func TestReleaseDelta_T0DeterministicMockRequiresReason(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:T.Class", Signature: "v1"},
	}
	cls := []ReleaseClassification{
		{SurfaceID: "apex:T.Class", Scope: ScopeT0, Disposition: DispoDeterministicMock, ReasonRef: ""},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err == nil {
		t.Fatal("expected error for deterministic-mock with empty reason, got nil")
	}
}

func TestReleaseDelta_T0ExplicitUnsupportedRequiresReason(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:T.Class", Signature: "v1"},
	}
	cls := []ReleaseClassification{
		{SurfaceID: "apex:T.Class", Scope: ScopeT0, Disposition: DispoExplicitUnsupported, ReasonRef: ""},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err == nil {
		t.Fatal("expected error for explicit-unsupported with empty reason, got nil")
	}
}

func TestReleaseDelta_T1ValidClassifications(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:T.Class", Signature: "v1"},
	}
	// T1 with new-case and case ID should succeed
	cls := []ReleaseClassification{
		{SurfaceID: "apex:T.Class", Scope: ScopeT1, Disposition: DispoNewCase, CaseID: "CASE-002"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err != nil {
		t.Fatalf("unexpected error for valid T1 classification: %v", err)
	}
}

// --- test case 12: T2 and outside-claim rows still require explicit decision and reason ---

func TestReleaseDelta_T2RequiresDecision(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:T2.Class", Signature: "v1"},
	}
	// T2 with deterministic-mock and reason should succeed
	cls := []ReleaseClassification{
		{SurfaceID: "apex:T2.Class", Scope: ScopeT2, Disposition: DispoDeterministicMock, ReasonRef: "reason-42"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err != nil {
		t.Fatalf("unexpected error for valid T2 classification: %v", err)
	}
}

func TestReleaseDelta_OutsideClaimRequiresDecision(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:OC.Class", Signature: "v1"},
	}
	// outside-claim with explicit-unsupported and reason should succeed
	cls := []ReleaseClassification{
		{SurfaceID: "apex:OC.Class", Scope: ScopeOutsideClaim, Disposition: DispoExplicitUnsupported, ReasonRef: "not-ours"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err != nil {
		t.Fatalf("unexpected error for valid outside-claim classification: %v", err)
	}
}

func TestReleaseDelta_T2EmptyReasonFails(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:T2.Class", Signature: "v1"},
	}
	cls := []ReleaseClassification{
		{SurfaceID: "apex:T2.Class", Scope: ScopeT2, Disposition: DispoExplicitUnsupported, ReasonRef: ""},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err == nil {
		t.Fatal("expected error for T2 without reason, got nil")
	}
}

// --- test case 13: removed and unchanged rows do not require release classification ---

func TestReleaseDelta_RemovedRowNoClassificationNeeded(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:Old.Class", Signature: "v1"},
	}
	current := []SurfaceLedgerRow{}
	added, removed, changed, unchanged, err := ComputeReleaseDelta(prev, current, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 1 {
		t.Errorf("removed: got %d, want 1", len(removed))
	}
	if len(added) != 0 || len(changed) != 0 || len(unchanged) != 0 {
		t.Errorf("other lists should be empty")
	}
}

func TestReleaseDelta_UnchangedRowNoClassificationNeeded(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:Stable.Class", Signature: "sig", Product: "apex", Kind: KindType},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:Stable.Class", Signature: "sig", Product: "apex", Kind: KindType},
	}
	added, removed, changed, unchanged, err := ComputeReleaseDelta(prev, current, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(unchanged) != 1 {
		t.Errorf("unchanged: got %d, want 1", len(unchanged))
	}
	if len(added) != 0 || len(removed) != 0 || len(changed) != 0 {
		t.Errorf("other lists should be empty")
	}
}

// --- test case 14: classifications that don't correspond to added/changed row fail as stale ---

func TestReleaseDelta_StaleClassificationFails(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:Only.Class", Signature: "v1", Product: "apex", Kind: KindType},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:Only.Class", Signature: "v1", Product: "apex", Kind: KindType},
	}
	// Classify an unchanged row — stale
	cls := stdClassifyAll("apex:Only.Class")
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err == nil {
		t.Fatal("expected error for stale classification on unchanged row, got nil")
	}
}

func TestReleaseDelta_ClassificationForNonExistentIDFails(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:A.Class", Signature: "v1", Product: "apex", Kind: KindType},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:A.Class", Signature: "v1", Product: "apex", Kind: KindType},
	}
	cls := stdClassifyAll("apex:Bogus.Class")
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err == nil {
		t.Fatal("expected error for classification of non-existent ID, got nil")
	}
}

// --- Codex gate: empty SurfaceID must fail, not skip ---

func TestReleaseDelta_EmptySurfaceIDInPrevFails(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "", Signature: "v1"},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:X.Class", Signature: "v1"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, stdClassifyAll("apex:X.Class"))
	if err == nil {
		t.Fatal("expected error for empty SurfaceID in prev, got nil")
	}
}

func TestReleaseDelta_WhitespaceOnlySurfaceIDInPrevFails(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "   ", Signature: "v1"},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:X.Class", Signature: "v1"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, stdClassifyAll("apex:X.Class"))
	if err == nil {
		t.Fatal("expected error for whitespace-only SurfaceID in prev, got nil")
	}
}

func TestReleaseDelta_EmptySurfaceIDInCurrentFails(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:X.Class", Signature: "v1"},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "", Signature: "v1"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, nil)
	if err == nil {
		t.Fatal("expected error for empty SurfaceID in current, got nil")
	}
}

func TestReleaseDelta_WhitespaceOnlySurfaceIDInCurrentFails(t *testing.T) {
	prev := []SurfaceLedgerRow{
		{SurfaceID: "apex:X.Class", Signature: "v1"},
	}
	current := []SurfaceLedgerRow{
		{SurfaceID: "\t ", Signature: "v1"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, nil)
	if err == nil {
		t.Fatal("expected error for whitespace-only SurfaceID in current, got nil")
	}
}

func TestReleaseDelta_EmptySurfaceIDInClassificationFails(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:X.Class", Signature: "v1"},
	}
	cls := []ReleaseClassification{
		{SurfaceID: "", Scope: ScopeT0, Disposition: DispoExistingCase, CaseID: "CASE-001"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err == nil {
		t.Fatal("expected error for empty SurfaceID in classification, got nil")
	}
}

func TestReleaseDelta_WhitespaceOnlySurfaceIDInClassificationFails(t *testing.T) {
	prev := []SurfaceLedgerRow{}
	current := []SurfaceLedgerRow{
		{SurfaceID: "apex:X.Class", Signature: "v1"},
	}
	cls := []ReleaseClassification{
		{SurfaceID: "  ", Scope: ScopeT0, Disposition: DispoExistingCase, CaseID: "CASE-001"},
	}
	_, _, _, _, err := ComputeReleaseDelta(prev, current, cls)
	if err == nil {
		t.Fatal("expected error for whitespace-only SurfaceID in classification, got nil")
	}
}

// --- helpers ---

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
