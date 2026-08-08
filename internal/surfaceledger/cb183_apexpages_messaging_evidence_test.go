package surfaceledger

import (
	"path/filepath"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var cb183ApexPagesMessagingSurfaceIDs = []string{
	"apex:ApexPages.Severity",
	"apex:ApexPages.Severity.ERROR",
	"apex:ApexPages.Severity.INFO",
	"apex:ApexPages.Severity.WARNING",
	"apex:ApexPages.Severity.CONFIRM",
	"apex:ApexPages.Severity.FATAL",
	"apex:ApexPages.Message",
	"apex:ApexPages.Message.Message(ApexPages.Severity,String)",
	"apex:ApexPages.Message.getSeverity()",
	"apex:ApexPages.Message.getSummary()",
	"apex:ApexPages.Message.Message(ApexPages.Severity,String,String)",
	"apex:ApexPages.Message.getDetail()",
	"apex:ApexPages.Message.Message(ApexPages.Severity,String,String,String)",
	"apex:ApexPages.Message.getComponentLabel()",
	"apex:Messaging.SingleEmailMessage",
	"apex:Messaging.SingleEmailMessage.SingleEmailMessage()",
	"apex:Messaging.SingleEmailMessage.setSubject(String)",
	"apex:Messaging.SingleEmailMessage.getSubject()",
	"apex:Messaging.SingleEmailMessage.setToAddresses(List<String>)",
	"apex:Messaging.SingleEmailMessage.getToAddresses()",
}

func TestCB183ApexPagesMessagingRowsHaveExactDualEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePath := filepath.Join(root, "docs", "fixtures", "current-base-cb183-apexpages-messaging-positive-api67.json")
	fixture, err := compat.LoadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := compat.Run(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("fixture result = %#v", result)
	}
	fixtureEvidence, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildOracleEvidenceSnapshot([]string{filepath.Join(root, "docs", "fixtures", "salesforce-cb183-apexpages-messaging-comparisons.json")})
	if err != nil {
		t.Fatal(err)
	}
	assertExactSurfaceSet(t, fixtureEvidence, cb183ApexPagesMessagingSurfaceIDs)
	assertExactSurfaceSet(t, oracle, cb183ApexPagesMessagingSurfaceIDs)

	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(fixtureEvidence, oracle...))
	byID := rowsByID(ledger.Rows)
	for _, id := range cb183ApexPagesMessagingSurfaceIDs {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing CB183 evidence row %s", id)
		}
		if row.Evidence != EvidenceFixtureAndOracle {
			t.Errorf("%s evidence = %s, want fixture-and-oracle", id, row.Evidence)
		}
		if row.GladeBehavior != BehaviorSupported {
			t.Errorf("%s behavior = %s, want supported", id, row.GladeBehavior)
		}
	}
}

func assertExactSurfaceSet(t *testing.T, rows []SurfaceLedgerRow, want []string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("evidence rows = %d, want %d", len(rows), len(want))
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if seen[row.SurfaceID] {
			t.Fatalf("duplicate evidence row %s", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
	}
	for _, id := range want {
		if !seen[id] {
			t.Errorf("missing expected evidence row %s", id)
		}
	}
}
