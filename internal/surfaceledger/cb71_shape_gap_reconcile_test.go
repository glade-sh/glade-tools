package surfaceledger

import (
	"path/filepath"
	"testing"
)

// cb75FrozenMissingShapeIDs is the exact rows[] SurfaceID set from the CB71
// classification, in the frozen profile order. The pre-edit queue/classifier
// reconciliation proved this list is 51 unique IDs with no set difference.
var cb75FrozenMissingShapeIDs = []string{
	"apex:Canvas.Test_constants",
	"apex:Messaging.InboundEmail.AuthenticationResult.InboundEmail.AuthenticationResult()",
	"apex:Messaging.InboundEmail.AuthenticationResultField.InboundEmail.AuthenticationResultField()",
	"apex:PushUpgradeCustomizationRepository.create(String,String,Boolean,Integer)",
	"apex:PushUpgradeCustomizationRepository.getCustomizationSummaryById(String)",
	"apex:PushUpgradeCustomizationRepository.getCustomizationSummaryByIndex(String,String)",
	"apex:PushUpgradeCustomizationRepository.getExpirationDaysForId(String)",
	"apex:PushUpgradeCustomizationRepository.getExpirationDaysForIndex(String,String)",
	"apex:PushUpgradeCustomizationRepository.getPushUpgradeBlockInitiatedDateForId(String)",
	"apex:PushUpgradeCustomizationRepository.getPushUpgradeBlockInitiatedDateForIndex(String,String)",
	"apex:PushUpgradeCustomizationRepository.isBlockingCapabilityExpiredForId(String)",
	"apex:PushUpgradeCustomizationRepository.isBlockingCapabilityExpiredForIndex(String,String)",
	"apex:PushUpgradeCustomizationRepository.listAllCustomizationSummaries()",
	"apex:PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForId(String,Boolean,Integer)",
	"apex:PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForIndex(String,String,Boolean,Integer)",
	"apex:PushUpgradeCustomizationRepository.setExpirationDaysForId(String,Integer)",
	"apex:PushUpgradeCustomizationRepository.setExpirationDaysForIndex(String,String,Integer)",
	"apex:RestResource",
	"apex:System.Database.lock",
	"apex:System.Database.unlock",
	"apex:System.Exception.Exception()",
	"apex:System.Exception.Exception(Exception)",
	"apex:System.Exception.Exception(String)",
	"apex:System.Exception.Exception(String,Exception)",
	"apex:System.InvalidParameterValueException.InvalidParameterValueException()",
	"apex:System.InvalidParameterValueException.InvalidParameterValueException(Exception)",
	"apex:System.InvalidParameterValueException.InvalidParameterValueException(String)",
	"apex:System.InvalidParameterValueException.InvalidParameterValueException(String,String)",
	"apex:System.Iterator.remove",
	"apex:System.Matcher.appendReplacement",
	"apex:System.Matcher.appendTail",
	"apex:System.Messaging.SingleEmailMessage",
	"apex:System.NoAccessException.NoAccessException(Exception)",
	"apex:System.NoAccessException.NoAccessException(String)",
	"apex:System.NoAccessException.NoAccessException(String,Exception)",
	"apex:System.NoDataFoundException.NoDataFoundException(Exception)",
	"apex:System.NoDataFoundException.NoDataFoundException(String)",
	"apex:System.NoDataFoundException.NoDataFoundException(String,Exception)",
	"apex:System.NullPointerException.NullPointerException(Exception)",
	"apex:System.NullPointerException.NullPointerException(String)",
	"apex:System.NullPointerException.NullPointerException(String,Exception)",
	"apex:System.PushUpgradeCustomizationRepository.create(String,String,Boolean)",
	"apex:System.PushUpgradeCustomizationRepository.deleteById(String)",
	"apex:System.PushUpgradeCustomizationRepository.deleteByIndex(String,String)",
	"apex:System.PushUpgradeCustomizationRepository.getCustomUpgradeAllowedForId(String)",
	"apex:System.PushUpgradeCustomizationRepository.getCustomUpgradeAllowedForIndex(String,String)",
	"apex:System.PushUpgradeCustomizationRepository.getCustomUpgradeTypeForId(String)",
	"apex:System.PushUpgradeCustomizationRepository.getCustomUpgradeTypeForIndex(String,String)",
	"apex:System.PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForId(String,Boolean)",
	"apex:System.PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForIndex(String,String,Boolean)",
	"apex:System.Type.newInstance",
}

var cb75CanonicalNeighborIDs = []string{
	"apex:Canvas.Test",
	"apex:Canvas.Test.KEY_CANVAS_URL",
	"apex:Messaging.InboundEmail.AuthenticationResult.AuthenticationResult()",
	"apex:Messaging.InboundEmail.AuthenticationResultField.AuthenticationResultField()",
	"apex:Messaging.SingleEmailMessage",
	"apex:Messaging.SingleEmailMessage.SingleEmailMessage()",
	"apex:System.Type.newInstance()",
	"apex:PushUpgradeCustomizationRepository.create(String,String,Boolean)",
	"apex:PushUpgradeCustomizationRepository.deleteById(String)",
	"apex:PushUpgradeCustomizationRepository.deleteByIndex(String,String)",
	"apex:PushUpgradeCustomizationRepository.getCustomUpgradeAllowedForId(String)",
	"apex:PushUpgradeCustomizationRepository.getCustomUpgradeAllowedForIndex(String,String)",
	"apex:PushUpgradeCustomizationRepository.getCustomUpgradeTypeForId(String)",
	"apex:PushUpgradeCustomizationRepository.getCustomUpgradeTypeForIndex(String,String)",
	"apex:PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForId(String,Boolean)",
	"apex:PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForIndex(String,String,Boolean)",
	"apex:System.NoAccessException.NoAccessException()",
	"apex:System.NoDataFoundException.NoDataFoundException()",
	"apex:System.NullPointerException.NullPointerException()",
}

func TestCB71ShapeGapRowsAreFilteredExactlyOnceAndCanonicalNeighborsRemain(t *testing.T) {
	if len(cb75FrozenMissingShapeIDs) != 51 {
		t.Fatalf("classification row count = %d, want 51", len(cb75FrozenMissingShapeIDs))
	}
	seen := make(map[string]struct{}, len(cb75FrozenMissingShapeIDs))
	for _, id := range cb75FrozenMissingShapeIDs {
		if _, ok := seen[id]; ok {
			t.Fatalf("classification repeats SurfaceID %q", id)
		}
		seen[id] = struct{}{}
	}

	root := filepath.Join("..", "..")
	evidence, err := BuildEvidenceSnapshot([]string{
		filepath.Join(root, "docs", "fixtures", "core-runtime-messaging-inbound-email-dto-evidence.json"),
		filepath.Join(root, "docs", "fixtures", "core-runtime-push-upgrade-customization-repository-unsupported.json"),
		filepath.Join(root, "docs", "fixtures", "examples-apex-rest.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// These rows model the frozen docs/org obligations. Merge must discard only
	// the exact stale shape identities while retaining the canonical product and
	// fixture rows around them.
	docsRows, orgRows := cb75FrozenSourceRows()
	gladeRows := BuildGladeSnapshot()
	snapshotByID := rowsByID(gladeRows)
	ledger := Merge(docsRows, orgRows, gladeRows, evidence)
	byID := rowsByID(ledger.Rows)

	policy, err := LoadSupportPolicy(filepath.Join(root, "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	boundedIDs := make(map[string]struct{}, len(cb75FrozenMissingShapeIDs)+len(cb75CanonicalNeighborIDs))
	for _, id := range append(append([]string{}, cb75FrozenMissingShapeIDs...), cb75CanonicalNeighborIDs...) {
		boundedIDs[id] = struct{}{}
	}
	boundedRows := make([]SurfaceLedgerRow, 0, len(boundedIDs))
	for _, row := range ledger.Rows {
		if _, ok := boundedIDs[row.SurfaceID]; ok {
			boundedRows = append(boundedRows, row)
		}
	}
	profile := ComputeSupportProfile(boundedRows, policy, nil)
	t.Logf("bounded profile rows=%d non-deferred gaps=%d frozen shape gaps=0", len(profile.Rows), len(profile.NonDeferredGaps))

	for _, id := range cb75FrozenMissingShapeIDs {
		t.Run(id, func(t *testing.T) {
			if !isNonCanonicalGeneratedSurfaceID(id) {
				t.Fatalf("frozen stale identity is not registered in the exact filter: %s", id)
			}
			if _, ok := snapshotByID[id]; ok {
				t.Fatalf("fresh Glade snapshot retains frozen shape row: %s", id)
			}
			if _, ok := byID[id]; ok {
				t.Fatalf("fresh merged ledger retains frozen shape row: %s", id)
			}
			for _, gap := range profile.NonDeferredGaps {
				if gap.SurfaceID == id {
					t.Fatalf("bounded profile retains frozen non-deferred gap: %s", id)
				}
			}
		})
	}

	for _, id := range cb75CanonicalNeighborIDs {
		if _, ok := byID[id]; !ok {
			t.Errorf("canonical neighbor was removed from the fresh merged ledger: %s", id)
		}
	}

	if got := countRowsByID(ledger.Rows, cb75CanonicalNeighborIDs); got != len(cb75CanonicalNeighborIDs) {
		t.Fatalf("canonical neighbor rows = %d, want %d", got, len(cb75CanonicalNeighborIDs))
	}
}

func cb75FrozenSourceRows() ([]SurfaceLedgerRow, []SurfaceLedgerRow) {
	docs := make([]SurfaceLedgerRow, 0, len(cb75FrozenMissingShapeIDs))
	org := make([]SurfaceLedgerRow, 0, len(cb75FrozenMissingShapeIDs))
	for _, id := range cb75FrozenMissingShapeIDs {
		row := SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime}
		fillFromApexID(&row)
		docs = append(docs, RowFromDocs(row))
		org = append(org, RowFromOrg(row))
	}
	return docs, org
}

func countRowsByID(rows []SurfaceLedgerRow, ids []string) int {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	count := 0
	for _, row := range rows {
		if _, ok := wanted[row.SurfaceID]; ok {
			count++
		}
	}
	return count
}
