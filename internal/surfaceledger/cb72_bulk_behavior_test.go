package surfaceledger

import (
	"path/filepath"
	"strings"
	"testing"
)

var cb72FrozenBehaviorIDs = []string{
	"apex:System.Security.stripInaccessible(AccessType,List<Object>,Boolean,Id)",
	"apex:Process.SparkPlugApi.describePlugin(String)",
	"apex:Process.SparkPlugApi.describePlugins()",
	"apex:Process.SparkPlugApi.invokePluginWithJson(String,String)",
	"apex:System.Crypto.decryptWithManagedIV(String,Blob,Blob,Blob)",
	"apex:System.Crypto.encryptWithManagedIV(String,Blob,Blob,Blob)",
	"apex:System.TimeZone.getDisplayName()",
	"apex:System.TimeZone.getTimeZone(String)",
}

func TestCB72FrozenBehaviorRowsCloseWithCanonicalDualEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturePaths := []string{
		filepath.Join(root, "docs", "fixtures", "core-runtime-cb72-frozen-behavior-local-evidence.json"),
		filepath.Join(root, "docs", "fixtures", "core-datetime-local-model-cleanup.json"),
		filepath.Join(root, "docs", "fixtures", "core-timezone-named-zone-dst-table.json"),
		filepath.Join(root, "docs", "fixtures", "core-timezone-display-daylight-flag.json"),
	}
	evidence, err := BuildEvidenceSnapshot(fixturePaths)
	if err != nil {
		t.Fatal(err)
	}
	oraclePath := filepath.Join(root, "docs", "fixtures", "salesforce-cb72-frozen-behavior-comparisons.json")
	oracle, err := BuildOracleEvidenceSnapshot([]string{oraclePath})
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(evidence, oracle...))
	byID := rowsByID(ledger.Rows)

	wantDisposition := map[string]SupportDisposition{
		cb72FrozenBehaviorIDs[0]: DispositionLocalRuntimeRequired,
		cb72FrozenBehaviorIDs[1]: DispositionDeterministicMockRequired,
		cb72FrozenBehaviorIDs[2]: DispositionDeterministicMockRequired,
		cb72FrozenBehaviorIDs[3]: DispositionDeterministicMockRequired,
		cb72FrozenBehaviorIDs[4]: DispositionLocalRuntimeRequired,
		cb72FrozenBehaviorIDs[5]: DispositionLocalRuntimeRequired,
		cb72FrozenBehaviorIDs[6]: DispositionLocalRuntimeRequired,
		cb72FrozenBehaviorIDs[7]: DispositionLocalRuntimeRequired,
	}

	for _, id := range cb72FrozenBehaviorIDs {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("frozen row is missing from bounded ledger: %s", id)
		}
		if row.GladeShape != ShapeSignatureKnown {
			t.Errorf("%s Glade shape = %s, want signature-known", id, row.GladeShape)
		}
		if row.GladeBehavior != BehaviorSupported {
			t.Errorf("%s Glade behavior = %s, want supported", id, row.GladeBehavior)
		}
		if row.Evidence != EvidenceFixtureAndOracle {
			t.Errorf("%s evidence = %s, want fixture-and-oracle", id, row.Evidence)
		}
		if strings.Contains(strings.ToLower(row.Notes), "salesforce-hosted") {
			t.Errorf("%s notes overclaim hosted Salesforce state: %q", id, row.Notes)
		}
	}

	for _, id := range []string{
		"apex:System.Security.stripInaccessible(AccessType,List<Object>)",
		"apex:System.Security.stripInaccessible(AccessType,List<Object>,Boolean)",
		"apex:System.Crypto.decryptWithManagedIV(String,Blob,Blob)",
		"apex:System.Crypto.encryptWithManagedIV(String,Blob,Blob)",
		"apex:System.TimeZone.getDisplayName()",
		"apex:System.TimeZone.getTimeZone(String)",
	} {
		if _, ok := byID[id]; !ok {
			t.Errorf("canonical neighboring row was not preserved: %s", id)
		}
	}
	for _, id := range []string{
		"apex:System.TimeZone.getDisplayName",
		"apex:System.TimeZone.getTimeZone",
	} {
		if _, ok := byID[id]; ok {
			t.Errorf("noncanonical TimeZone alias remains: %s", id)
		}
	}

	policy, err := LoadSupportPolicy(filepath.Join(root, "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	policy = cb72BoundedPolicy(policy)
	var bounded []SurfaceLedgerRow
	for _, id := range append(append([]string{}, cb72FrozenBehaviorIDs...),
		"apex:System.Security.stripInaccessible(AccessType,List<Object>)",
		"apex:System.Security.stripInaccessible(AccessType,List<Object>,Boolean)",
		"apex:System.Crypto.decryptWithManagedIV(String,Blob,Blob)",
		"apex:System.Crypto.encryptWithManagedIV(String,Blob,Blob)",
	) {
		if row, ok := byID[id]; ok {
			bounded = append(bounded, row)
		}
	}
	profile := ComputeSupportProfile(bounded, policy, nil)
	frozenSet := make(map[string]struct{}, len(cb72FrozenBehaviorIDs))
	for _, id := range cb72FrozenBehaviorIDs {
		frozenSet[id] = struct{}{}
	}
	for _, row := range profile.NonDeferredGaps {
		if _, ok := frozenSet[row.SurfaceID]; ok {
			t.Errorf("bounded CB72 profile retains non-deferred gap %s", row.SurfaceID)
		}
	}
	profileByID := make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		profileByID[row.SurfaceID] = row
	}
	for _, id := range cb72FrozenBehaviorIDs {
		row, ok := profileByID[id]
		if !ok {
			t.Fatalf("bounded profile omitted frozen row: %s", id)
		}
		if row.GapClass != "" {
			t.Errorf("%s remains in bounded profile gap %q", id, row.GapClass)
		}
		if row.Disposition != wantDisposition[id] {
			t.Errorf("%s disposition = %s, want %s", id, row.Disposition, wantDisposition[id])
		}
	}
}

func cb72BoundedPolicy(policy SupportPolicy) SupportPolicy {
	var rules []SupportPolicyRule
	for _, rule := range policy.Rules {
		if rule.Namespace != "System" && rule.Namespace != "Process" {
			continue
		}
		if rule.Namespace == "System" {
			var exceptions []SupportPolicyMemberException
			for _, exception := range rule.MemberExceptions {
				if exception.TypeName == "Crypto" || exception.TypeName == "Security" || exception.TypeName == "TimeZone" {
					exceptions = append(exceptions, exception)
				}
			}
			rule.MemberExceptions = exceptions
		}
		rules = append(rules, rule)
	}
	return SupportPolicy{Rules: rules}
}
