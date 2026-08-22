package corpusassurance

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func TestSurfaceTerminalAuthorityBindsProvenanceAndSeparatesAccounting(t *testing.T) {
	root := t.TempDir()
	fixtureRoot := filepath.Join(root, "fixtures")
	if err := os.Mkdir(fixtureRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	terminalA := "apex:System.oldMember"
	terminalB := "apex:Site.contextMember"
	actionable := "apex:System.actionable"
	covered := "apex:System.covered"
	ledger := surfaceledger.SurfaceLedger{SchemaVersion: surfaceledger.SchemaVersion, Rows: []surfaceledger.SurfaceLedgerRow{
		{SurfaceID: terminalA, Product: "apex", Namespace: "System", TypeName: "System", MemberName: "oldMember", Kind: "method", Sources: []string{"fixture:negative"}},
		{SurfaceID: terminalB, Product: "apex", Namespace: "Site", TypeName: "Site", MemberName: "contextMember", Kind: "method", Sources: []string{"docs", "fixture:context"}},
		{SurfaceID: actionable, Product: "apex", Namespace: "System", TypeName: "System", MemberName: "actionable", Kind: "method"},
		{SurfaceID: covered, Product: "apex", Namespace: "System", TypeName: "System", MemberName: "covered", Kind: "method"},
	}}
	policy := surfaceledger.SupportPolicy{Rules: []surfaceledger.SupportPolicyRule{
		{Namespace: "System", Disposition: surfaceledger.DispositionLocalRuntimeRequired, Reason: "system runtime"},
		{Namespace: "Site", Disposition: surfaceledger.DispositionLocalRuntimeRequired, Reason: "site runtime"},
	}}
	ledgerPath, policyPath := filepath.Join(root, "ledger.json"), filepath.Join(root, "support-policy.json")
	if err := WriteNewJSON(ledgerPath, ledger); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(policyPath, policy); err != nil {
		t.Fatal(err)
	}
	ledgerSHA, _ := proofInputSHA256(ledgerPath)
	policySHA, _ := proofInputSHA256(policyPath)
	scope := SurfaceOracleScope{SchemaVersion: 1, Kind: "all-runtime", LedgerSHA256: ledgerSHA, PolicySHA256: policySHA, Total: 4, ByDisposition: map[string]int{localRuntimeRequired: 4}, Rows: []SurfaceOracleScopeRow{
		{SurfaceID: terminalB, Disposition: localRuntimeRequired},
		{SurfaceID: covered, Disposition: localRuntimeRequired},
		{SurfaceID: actionable, Disposition: localRuntimeRequired},
		{SurfaceID: terminalA, Disposition: localRuntimeRequired},
	}}
	sortSurfaceScopeRows(scope.Rows)
	scopePath := filepath.Join(root, "scope.json")
	if err := WriteNewJSON(scopePath, scope); err != nil {
		t.Fatal(err)
	}
	scopeSHA, _ := proofInputSHA256(scopePath)
	coverage := SurfaceLocalProofCoverage{SchemaVersion: 1, ScopeSHA256: scopeSHA, Total: 4, Covered: 1, MissingCount: 3, Missing: []SurfaceOracleScopeRow{
		{SurfaceID: terminalB, Disposition: localRuntimeRequired},
		{SurfaceID: actionable, Disposition: localRuntimeRequired},
		{SurfaceID: terminalA, Disposition: localRuntimeRequired},
	}, UnclassifiedFixtures: []string{}}
	sortSurfaceScopeRows(coverage.Missing)
	coveragePath := filepath.Join(root, "coverage.json")
	if err := WriteNewJSON(coveragePath, coverage); err != nil {
		t.Fatal(err)
	}
	fixture := `{"name":"negative","evidence":[{"surfaceId":"` + terminalA + `","symbol":"System.oldMember","kind":"exec"}],"command":{"kind":"exec","args":["System.oldMember();"]},"expected":{},"salesforceEligible":false,"salesforceExclusionClass":"policy-local-only","salesforceExclusionReason":"API 67 rejects the member"}`
	fixturePath := filepath.Join(fixtureRoot, "negative.json")
	if err := os.WriteFile(fixturePath, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	contextFixture := `{"name":"context","proofObligation":"compile-shape-required","evidence":[{"surfaceId":"` + terminalB + `","symbol":"Site.contextMember","kind":"shape"}]}`
	if err := os.WriteFile(filepath.Join(fixtureRoot, "context.json"), []byte(contextFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	classifications := ExclusionPolicy{SchemaVersion: 1, Rows: []ExclusionPolicyRow{
		{SurfaceID: terminalB, Class: "hosted-context-boundary", Reason: "requires a hosted Site lifecycle"},
		{SurfaceID: terminalA, Class: "version-current-api-exclusion", Reason: "rejected by the current API"},
	}}
	classificationPath := filepath.Join(root, "classifications.json")
	if err := WriteNewJSON(classificationPath, classifications); err != nil {
		t.Fatal(err)
	}

	request := SurfaceTerminalAuthorityRequest{ScopePath: scopePath, CoveragePath: coveragePath, LedgerPath: ledgerPath, SupportPolicyPath: policyPath, FixtureRoot: fixtureRoot, ClassificationPath: classificationPath, OutputPath: filepath.Join(root, "authority.json")}
	authority, err := CreateSurfaceTerminalAuthority(request)
	if err != nil {
		t.Fatal(err)
	}
	if authority.SchemaVersion != 1 || authority.ScopeSHA256 != scopeSHA || authority.Count != 2 || authority.LocalRuntimeCredit != 0 || authority.SalesforceParityCredit != 0 || authority.ByClass["version-current-api-exclusion"] != 1 || authority.ByClass["hosted-context-boundary"] != 1 || !sha256Pattern.MatchString(authority.RowsSHA256) {
		t.Fatalf("authority summary = %+v", authority)
	}
	if got := []string{authority.Rows[0].SurfaceID, authority.Rows[1].SurfaceID}; !reflect.DeepEqual(got, []string{terminalB, terminalA}) {
		t.Fatalf("sorted authority rows = %v", got)
	}
	var negativeRow SurfaceTerminalAuthorityRow
	for _, row := range authority.Rows {
		if row.SurfaceID == terminalA {
			negativeRow = row
		}
	}
	if negativeRow.Policy.Disposition != localRuntimeRequired || negativeRow.Policy.Reason != "system runtime" || negativeRow.Ledger.SHA256 == "" || !reflect.DeepEqual(negativeRow.Ledger.Sources, []string{"fixture:negative"}) || len(negativeRow.Fixtures) != 1 || negativeRow.Fixtures[0].SalesforceEligible == nil || *negativeRow.Fixtures[0].SalesforceEligible || negativeRow.Fixtures[0].SalesforceExclusionReason != "API 67 rejects the member" {
		t.Fatalf("negative provenance = %+v", negativeRow)
	}
	if len(authority.Rows[0].Fixtures) != 1 || authority.Rows[0].Fixtures[0].ID != "context" || authority.Rows[0].Fixtures[0].SalesforceEligible != nil {
		t.Fatalf("context provenance = %+v", authority.Rows[0].Fixtures)
	}
	stat, err := os.Stat(request.OutputPath)
	if err != nil || stat.Mode().Perm() != 0o600 {
		t.Fatalf("authority mode = %v, err = %v", stat.Mode().Perm(), err)
	}
	second := request
	second.OutputPath = filepath.Join(root, "authority-2.json")
	if _, err := CreateSurfaceTerminalAuthority(second); err != nil {
		t.Fatal(err)
	}
	firstBytes, _ := os.ReadFile(request.OutputPath)
	secondBytes, _ := os.ReadFile(second.OutputPath)
	if !reflect.DeepEqual(firstBytes, secondBytes) {
		t.Fatal("authority output is not deterministic")
	}

	authoritySHA, _ := proofInputSHA256(request.OutputPath)
	accounting, err := ApplySurfaceTerminalAuthority(coverage, authority, authoritySHA, authority.FixtureSetSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if accounting.DirectLocalProof != 1 || accounting.TerminalAccounted != 2 || accounting.Accounted != 3 || accounting.Remaining != 1 || accounting.LocalRuntimeCredit != 0 || accounting.SalesforceParityCredit != 0 || len(accounting.ActionableMissing) != 1 || accounting.ActionableMissing[0].SurfaceID != actionable {
		t.Fatalf("terminal accounting = %+v", accounting)
	}

	forged := authority
	forged.Rows = append([]SurfaceTerminalAuthorityRow(nil), authority.Rows...)
	forged.Rows[0].SurfaceID = covered
	forged.RowsSHA256 = surfaceTerminalRowsSHA256(forged.Rows)
	if _, err := ApplySurfaceTerminalAuthority(coverage, forged, strings.Repeat("a", 64), authority.FixtureSetSHA256); err == nil || !strings.Contains(err.Error(), "not currently missing") {
		t.Fatalf("covered terminal row error = %v", err)
	}
	if _, err := ApplySurfaceTerminalAuthority(coverage, authority, authoritySHA, strings.Repeat("f", 64)); err == nil || !strings.Contains(err.Error(), "fixture set") {
		t.Fatalf("fixture drift error = %v", err)
	}
	unrelated := `{"name":"unrelated","evidence":[{"surfaceId":"apex:System.unrelated","symbol":"System.unrelated","kind":"exec"}]}`
	if err := os.WriteFile(filepath.Join(fixtureRoot, "unrelated.json"), []byte(unrelated), 0o600); err != nil {
		t.Fatal(err)
	}
	fixtureSetSHA, err := terminalFixtureSetSHA256(fixtureRoot, authority.Rows)
	if err != nil {
		t.Fatal(err)
	}
	if fixtureSetSHA != authority.FixtureSetSHA256 {
		t.Fatal("unrelated fixture changed the terminal fixture seal")
	}
}

func sortSurfaceScopeRows(rows []SurfaceOracleScopeRow) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].SurfaceID < rows[j].SurfaceID })
}
