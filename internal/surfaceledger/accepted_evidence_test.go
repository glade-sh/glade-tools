package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testFile struct {
	name    string
	content string
}

func TestAcceptedEvidence_Integration(t *testing.T) {
	dir := t.TempDir()
	writeTestJSON(t, filepath.Join(dir, "cb109-surface-scenario-map.json"), cb109TestJSON)
	writeTestJSON(t, filepath.Join(dir, "cb110-surface-scenario-map.json"), cb110TestJSON)
	writeTestJSON(t, filepath.Join(dir, "cb112-surface-scenario-map.json"), cb112TestJSON)
	writeTestJSON(t, filepath.Join(dir, "cb114-row-map.json"), cb114TestJSON)
	writeTestJSON(t, filepath.Join(dir, "cb115-row-map.json"), cb115TestJSON)
	writeTestJSON(t, filepath.Join(dir, "support-profile.json"), spTestJSON)

	paths := []string{
		filepath.Join(dir, "cb109-surface-scenario-map.json"),
		filepath.Join(dir, "cb110-surface-scenario-map.json"),
		filepath.Join(dir, "cb112-surface-scenario-map.json"),
		filepath.Join(dir, "cb114-row-map.json"),
		filepath.Join(dir, "cb115-row-map.json"),
	}

	manifest, err := IngestAcceptedEvidence(paths, filepath.Join(dir, "support-profile.json"))
	if err != nil {
		t.Fatalf("IngestAcceptedEvidence: %v", err)
	}

	if manifest.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", manifest.SchemaVersion)
	}
	if manifest.ManifestSHA == "" {
		t.Error("ManifestSHA is empty")
	}
	if len(manifest.SourceFiles) != 6 {
		t.Errorf("SourceFiles = %d, want 6", len(manifest.SourceFiles))
	}
	if manifest.SupportTotal != 94352 {
		t.Errorf("SupportTotal = %d, want 94352", manifest.SupportTotal)
	}

	wantAccepted := 1 + 2 + 11 + 12 + 0
	if manifest.Accepted != wantAccepted {
		t.Errorf("Accepted = %d, want %d (1 cb109 + 2 cb110 + 11 cb112 + 12 cb114 + 0 cb115)",
			manifest.Accepted, wantAccepted)
	}
	wantRejected := 3 + 2 + 0 + 2 + 4
	if manifest.Rejected != wantRejected {
		t.Errorf("Rejected = %d, want %d", manifest.Rejected, wantRejected)
	}
	wantInput := 4 + 4 + 11 + 14 + 4
	if manifest.TotalInput != wantInput {
		t.Errorf("TotalInput = %d, want %d", manifest.TotalInput, wantInput)
	}

	acceptedIDs := make(map[string]bool)
	for _, row := range manifest.AcceptedRows {
		acceptedIDs[row.SurfaceID] = true
	}
	if !acceptedIDs["apex:Apex.Stack"] {
		t.Error("expected accepted: apex:Apex.Stack")
	}
	if !acceptedIDs["apex:Database.BatchableContext"] {
		t.Error("expected accepted: apex:Database.BatchableContext")
	}
	if !acceptedIDs["apex:Messaging.ActionError"] {
		t.Error("expected accepted: apex:Messaging.ActionError")
	}
	if !acceptedIDs["apex:Auth.JWT"] {
		t.Error("expected accepted: apex:Auth.JWT")
	}
	if acceptedIDs["apex-language:SystemNamespaceDefaultImport"] {
		t.Error("unexpected accepted: cb109 uncovered language row")
	}
	if acceptedIDs["apex:System.AccessLevel"] {
		t.Error("unexpected accepted: cb109 uncovered System.AccessLevel")
	}
	if acceptedIDs["apex:Database.BatchableContextImpl"] {
		t.Error("unexpected accepted: cb110 mock-contract hosted row")
	}
	if acceptedIDs["apex:Database.DeletedRecord.deleteddate"] {
		t.Error("unexpected accepted: cb110 uncovered property row")
	}
	if acceptedIDs["apex:Auth.JWT.toJSONString()"] {
		t.Error("unexpected accepted: cb114 uncovered method")
	}
	if acceptedIDs["apex:ApexPages.Action.Action(String)"] {
		t.Error("unexpected accepted: cb115 not-passed one-sided")
	}
}

func TestAcceptedEvidence_DeterministicSHA(t *testing.T) {
	dir := t.TempDir()
	writeTestJSON(t, filepath.Join(dir, "cb109-surface-scenario-map.json"), cb109TestJSON)
	writeTestJSON(t, filepath.Join(dir, "cb110-surface-scenario-map.json"), cb110TestJSON)
	writeTestJSON(t, filepath.Join(dir, "cb112-surface-scenario-map.json"), cb112TestJSON)
	writeTestJSON(t, filepath.Join(dir, "cb114-row-map.json"), cb114TestJSON)
	writeTestJSON(t, filepath.Join(dir, "cb115-row-map.json"), cb115TestJSON)

	paths := []string{
		filepath.Join(dir, "cb109-surface-scenario-map.json"),
		filepath.Join(dir, "cb110-surface-scenario-map.json"),
		filepath.Join(dir, "cb112-surface-scenario-map.json"),
		filepath.Join(dir, "cb114-row-map.json"),
		filepath.Join(dir, "cb115-row-map.json"),
	}

	m1, err := IngestAcceptedEvidence(paths, "")
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	m2, err := IngestAcceptedEvidence(paths, "")
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if m1.ManifestSHA != m2.ManifestSHA {
		t.Errorf("ManifestSHA not deterministic: %s != %s", m1.ManifestSHA, m2.ManifestSHA)
	}
	if m1.Accepted != m2.Accepted {
		t.Errorf("accepted count changed: %d != %d", m1.Accepted, m2.Accepted)
	}
	if m1.Rejected != m2.Rejected {
		t.Errorf("rejected count changed: %d != %d", m1.Rejected, m2.Rejected)
	}
}

func TestAcceptedEvidence_RejectionReasons(t *testing.T) {
	dir := t.TempDir()
	writeTestJSON(t, filepath.Join(dir, "cb109-surface-scenario-map.json"), cb109TestJSON)
	writeTestJSON(t, filepath.Join(dir, "cb110-surface-scenario-map.json"), cb110TestJSON)
	writeTestJSON(t, filepath.Join(dir, "cb114-row-map.json"), cb114TestJSON)
	writeTestJSON(t, filepath.Join(dir, "cb115-row-map.json"), cb115TestJSON)

	paths := []string{
		filepath.Join(dir, "cb109-surface-scenario-map.json"),
		filepath.Join(dir, "cb110-surface-scenario-map.json"),
		filepath.Join(dir, "cb114-row-map.json"),
		filepath.Join(dir, "cb115-row-map.json"),
	}

	manifest, err := IngestAcceptedEvidence(paths, "")
	if err != nil {
		t.Fatalf("IngestAcceptedEvidence: %v", err)
	}

	reasonByID := map[string]string{}
	for _, row := range manifest.RejectedRows {
		reasonByID[row.SurfaceID] = row.Reason
	}

	if reasonByID["apex-language:SystemNamespaceDefaultImport"] != "uncovered" {
		t.Errorf("cb109 uncovered reason = %q, want uncovered", reasonByID["apex-language:SystemNamespaceDefaultImport"])
	}
	if reasonByID["apex:System.AccessLevel"] != "uncovered" {
		t.Errorf("cb109 uncovered System.AccessLevel reason = %q, want uncovered", reasonByID["apex:System.AccessLevel"])
	}
	if !strings.Contains(reasonByID["apex:Database.BatchableContextImpl"], "hosted") {
		t.Errorf("cb110 hosted-boundary reason = %q, want hosted", reasonByID["apex:Database.BatchableContextImpl"])
	}
	if reasonByID["apex:Auth.JWT.toJSONString()"] != "uncovered" {
		t.Errorf("cb114 uncovered reason = %q", reasonByID["apex:Auth.JWT.toJSONString()"])
	}
	if reasonByID["apex:ApexPages.Action.Action(String)"] != "uncovered" {
		t.Errorf("cb115 uncovered reason = %q", reasonByID["apex:ApexPages.Action.Action(String)"])
	}
}

func TestAcceptedEvidence_EmptyMaps(t *testing.T) {
	dir := t.TempDir()
	writeTestJSON(t, filepath.Join(dir, "cb109-surface-scenario-map.json"), `{"packetId":"CB109","rows":[]}`)

	manifest, err := IngestAcceptedEvidence([]string{filepath.Join(dir, "cb109-surface-scenario-map.json")}, "")
	if err != nil {
		t.Fatalf("IngestAcceptedEvidence: %v", err)
	}
	if manifest.Accepted != 0 {
		t.Errorf("Accepted = %d, want 0", manifest.Accepted)
	}
	if manifest.Rejected != 0 {
		t.Errorf("Rejected = %d, want 0", manifest.Rejected)
	}
}

func writeTestJSON(t *testing.T, path, content string) {
	t.Helper()
	if !json.Valid([]byte(content)) {
		t.Fatalf("invalid test JSON for %s", path)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write test file %s: %v", path, err)
	}
}

const cb109TestJSON = `{
  "packetId": "CB109",
  "candidate": {"path": "/test/candidate", "sha256": "ab01"},
  "apiVersion": "67.0",
  "sourceBatch": "CB107-B13",
  "rows": [
    {
      "surfaceId": "apex-language:SystemNamespaceDefaultImport",
      "coverageKind": "uncovered",
      "scenarioIds": [],
      "evidencePaths": [],
      "reason": "uncovered",
      "sourceWitnesses": []
    },
    {
      "surfaceId": "apex:Apex.Stack",
      "coverageKind": "exact-runtime",
      "scenarioIds": ["cb109r-context-request-stack"],
      "evidencePaths": ["comparison.json"],
      "reason": "passed both sides",
      "sourceWitnesses": [
        {"side": "salesforce", "kind": "anchored-type", "token": "Stack", "sourcePath": "sf.cls", "sourceSha256": "sf01"},
        {"side": "local", "kind": "anchored-type", "token": "Stack", "sourcePath": "local.cls", "sourceSha256": "lc01"}
      ],
      "comparisonStatuses": [
        {"scenarioId": "cb109r-context-request-stack", "source": "broad-salesforce-local-test", "comparison": "accepted-execution"}
      ]
    },
    {
      "surfaceId": "apex:System.AccessLevel",
      "coverageKind": "uncovered",
      "scenarioIds": ["cb109-handler-accesslevel"],
      "evidencePaths": [],
      "reason": "uncovered",
      "sourceWitnesses": [],
      "comparisonStatuses": [
        {"scenarioId": "cb109-handler-accesslevel", "source": "handler", "comparison": "mismatch"}
      ]
    },
    {
      "surfaceId": "apex:System.AccessLevel.clone()",
      "coverageKind": "uncovered",
      "scenarioIds": ["cb109-handler-accesslevel"],
      "evidencePaths": [],
      "reason": "uncovered",
      "sourceWitnesses": [],
      "comparisonStatuses": [
        {"scenarioId": "cb109-handler-accesslevel", "source": "handler", "comparison": "mismatch"}
      ]
    }
  ]
}`

const cb110TestJSON = `{
  "packetId": "CB110",
  "correctionId": "CB110R",
  "sourceBatch": "CB107-B14",
  "candidateSha256": "ab02",
  "orgAlias": "glade-sf-sweep-2",
  "apiVersion": "67.0",
  "rows": [
    {
      "surfaceId": "apex:Database.BatchableContext",
      "coverageKind": "exact-runtime",
      "scenarioIds": ["CB110-ASYNC-LIFECYCLE"],
      "evidencePaths": ["raw-salesforce/async-test-final.json"],
      "reason": "broad test with bot  h sides",
      "sourceWitnesses": [
        {"side": "salesforce", "kind": "anchored-type", "token": "BatchableContext", "sourcePath": "sf.cls", "sourceSha256": "sf02"},
        {"side": "local", "kind": "anchored-type", "token": "BatchableContext", "sourcePath": "local.cls", "sourceSha256": "lc02"}
      ],
      "comparisonStatuses": [
        {"scenarioId": "CB110-ASYNC-LIFECYCLE", "source": "broad", "comparison": "pass-or-contract"}
      ]
    },
    {
      "surfaceId": "apex:Database.BatchableContext.getChildJobId()",
      "coverageKind": "exact-runtime",
      "scenarioIds": ["CB110-ASYNC-LIFECYCLE"],
      "evidencePaths": ["raw-salesforce/async-test-final.json"],
      "reason": "broad test with both sides",
      "sourceWitnesses": [],
      "comparisonStatuses": [
        {"scenarioId": "CB110-ASYNC-LIFECYCLE", "source": "broad", "comparison": "pass-or-contract"}
      ]
    },
    {
      "surfaceId": "apex:Database.BatchableContextImpl",
      "coverageKind": "mock-contract",
      "scenarioIds": ["CB110-ASYNC-PASSIVE"],
      "evidencePaths": [],
      "reason": "local moc k contract, not salesforce eq",
      "sourceWitnesses": [],
      "comparisonStatuses": [
        {"scenarioId": "CB110-ASYNC-PASSIVE", "source": "broad", "comparison": "mock-contract"}
      ],
      "evidenceBasis": {"hostedBoundary": "Local fixture contract is explicitly bounded from hosted Salesforce equivalence."}
    },
    {
      "surfaceId": "apex:Database.DeletedRecord.deleteddate",
      "coverageKind": "uncovered",
      "scenarioIds": ["CB110-DATABASE-CURSOR"],
      "evidencePaths": [],
      "reason": "no typed-receiver evidence",
      "sourceWitnesses": [],
      "comparisonStatuses": [
        {"scenarioId": "CB110-DATABASE-CURSOR", "source": "broad", "comparison": "see-comparison"}
      ]
    }
  ]
}`

const cb112TestJSON = `{
  "packetId": "CB112",
  "sourceBatch": "CB107-B15",
  "candidate": {"path": "/test/candidate", "sha256": "ab03"},
  "selectedOrg": {"alias": "glade-sf-sweep-2", "orgId": "00DRL00000TTINR2A5", "apiVersion": "67.0"},
  "rows": [
    {
      "surfaceId": "apex:Messaging.ActionError",
      "coverageKind": "exact-runtime",
      "comparisonStatus": "covered",
      "localObserved": true,
      "salesforceObserved": true,
      "scenarioIds": ["CB112-ENUM-NOTIFICATION"],
      "evidencePaths": ["raw-local/exec-enums/stdout.json"],
      "reason": "direct named invocation",
      "sourceEvidence": [{"sourceSha256": "sh01"}, {"sourceSha256": "sh02"}]
    },
    {
      "surfaceId": "apex:Messaging.ActionError.ACCESS_DENIED",
      "coverageKind": "exact-runtime",
      "comparisonStatus": "covered",
      "localObserved": true,
      "salesforceObserved": true,
      "scenarioIds": ["CB112-ENUM-NOTIFICATION"],
      "evidencePaths": ["raw-local/exec-enums/stdout.json"],
      "reason": "direct named invocation",
      "sourceEvidence": [{"sourceSha256": "sh01"}, {"sourceSha256": "sh02"}]
    },
    {
      "surfaceId": "apex:Messaging.ActionError.ACTION_NOT_IMPLEMENTED",
      "coverageKind": "exact-runtime",
      "comparisonStatus": "covered",
      "localObserved": true,
      "salesforceObserved": true,
      "scenarioIds": ["CB112-ENUM-NOTIFICATION"],
      "evidencePaths": [],
      "reason": "direct named invocation",
      "sourceEvidence": [{"sourceSha256": "sh01"}, {"sourceSha256": "sh02"}]
    },
    {
      "surfaceId": "apex:Messaging.ActionError.INTERNAL_ERROR",
      "coverageKind": "exact-runtime",
      "comparisonStatus": "covered",
      "localObserved": true,
      "salesforceObserved": true,
      "scenarioIds": ["CB112-ENUM-NOTIFICATION"],
      "evidencePaths": [],
      "reason": "direct named invocation",
      "sourceEvidence": [{"sourceSha256": "sh01"}, {"sourceSha256": "sh02"}]
    },
    {
      "surfaceId": "apex:Messaging.ActionError.INVALID_ACTION_PARAMETERS",
      "coverageKind": "exact-runtime",
      "comparisonStatus": "covered",
      "localObserved": true,
      "salesforceObserved": true,
      "scenarioIds": ["CB112-ENUM-NOTIFICATION"],
      "evidencePaths": [],
      "reason": "direct named invocation",
      "sourceEvidence": [{"sourceSha256": "sh01"}, {"sourceSha256": "sh02"}]
    },
    {
      "surfaceId": "apex:Messaging.ActionError.INVALID_STATE",
      "coverageKind": "exact-runtime",
      "comparisonStatus": "covered",
      "localObserved": true,
      "salesforceObserved": true,
      "scenarioIds": ["CB112-ENUM-NOTIFICATION"],
      "evidencePaths": [],
      "reason": "direct named invocation",
      "sourceEvidence": [{"sourceSha256": "sh01"}, {"sourceSha256": "sh02"}]
    },
    {
      "surfaceId": "apex:Messaging.ActionError.equals(Object)",
      "coverageKind": "exact-runtime",
      "comparisonStatus": "covered",
      "localObserved": true,
      "salesforceObserved": true,
      "scenarioIds": ["CB112-ENUM-NOTIFICATION"],
      "evidencePaths": [],
      "reason": "direct named invocation",
      "sourceEvidence": [{"sourceSha256": "sh01"}, {"sourceSha256": "sh02"}]
    },
    {
      "surfaceId": "apex:Messaging.ActionError.hashCode()",
      "coverageKind": "exact-runtime",
      "comparisonStatus": "covered",
      "localObserved": true,
      "salesforceObserved": true,
      "scenarioIds": ["CB112-ENUM-NOTIFICATION"],
      "evidencePaths": [],
      "reason": "direct named invocation",
      "sourceEvidence": [{"sourceSha256": "sh01"}, {"sourceSha256": "sh02"}]
    },
    {
      "surfaceId": "apex:Messaging.ActionError.ordinal()",
      "coverageKind": "exact-runtime",
      "comparisonStatus": "covered",
      "localObserved": true,
      "salesforceObserved": true,
      "scenarioIds": ["CB112-ENUM-NOTIFICATION"],
      "evidencePaths": [],
      "reason": "direct named invocation",
      "sourceEvidence": [{"sourceSha256": "sh01"}, {"sourceSha256": "sh02"}]
    },
    {
      "surfaceId": "apex:Messaging.ActionError.valueOf(String)",
      "coverageKind": "exact-runtime",
      "comparisonStatus": "covered",
      "localObserved": true,
      "salesforceObserved": true,
      "scenarioIds": ["CB112-ENUM-NOTIFICATION"],
      "evidencePaths": [],
      "reason": "direct named invocation",
      "sourceEvidence": [{"sourceSha256": "sh01"}, {"sourceSha256": "sh02"}]
    },
    {
      "surfaceId": "apex:Messaging.ActionError.values()",
      "coverageKind": "exact-runtime",
      "comparisonStatus": "covered",
      "localObserved": true,
      "salesforceObserved": true,
      "scenarioIds": ["CB112-ENUM-NOTIFICATION"],
      "evidencePaths": [],
      "reason": "direct named invocation",
      "sourceEvidence": [{"sourceSha256": "sh01"}, {"sourceSha256": "sh02"}]
    }
  ]
}`

const cb114TestJSON = `{
  "schemaVersion": 1,
  "total": 14,
  "rows": [
    {
      "rowId": "apex:Auth.AuthToken.revokeAccess(String,String,String,String)",
      "sourceBatch": "CB107-B17",
      "coverageKind": "exact-runtime",
      "passed": true,
      "evidenceReason": "direct invocation passed",
      "scenarioIds": ["CB114R_REUSE_cb114-existing-integration-auth-token-local-mocks"]
    },
    {
      "rowId": "apex:Auth.CommunitiesUtil.isGuestUser()",
      "sourceBatch": "CB107-B17",
      "coverageKind": "exact-runtime",
      "passed": true,
      "evidenceReason": "direct invocation passed",
      "scenarioIds": ["CB114R_REUSE_cb114-existing-integration-auth-communities-local"]
    },
    {
      "rowId": "apex:Auth.JWT",
      "sourceBatch": "CB107-B17",
      "coverageKind": "exact-runtime",
      "passed": true,
      "evidenceReason": "direct constructor passed",
      "scenarioIds": ["CB114R_REUSE_cb114-auth-jwt-user-context"]
    },
    {
      "rowId": "apex:Auth.JWT.JWT()",
      "sourceBatch": "CB107-B17",
      "coverageKind": "exact-runtime",
      "passed": true,
      "evidenceReason": "direct constructor passed",
      "scenarioIds": ["CB114R_REUSE_cb114-auth-jwt-user-context"]
    },
    {
      "rowId": "apex:Auth.JWT.clone()",
      "sourceBatch": "CB107-B17",
      "coverageKind": "exact-runtime",
      "passed": true,
      "evidenceReason": "direct invocation passed",
      "scenarioIds": ["CB114C_AUTH_JWT_GETTERS_CLONE"]
    },
    {
      "rowId": "apex:Auth.JWT.getAdditionalClaims()",
      "sourceBatch": "CB107-B17",
      "coverageKind": "exact-runtime",
      "passed": true,
      "evidenceReason": "direct invocation passed",
      "scenarioIds": ["CB114C_AUTH_JWT_GETTERS_CLONE"]
    },
    {
      "rowId": "apex:Auth.JWT.getAud()",
      "sourceBatch": "CB107-B17",
      "coverageKind": "exact-runtime",
      "passed": true,
      "evidenceReason": "direct invocation passed",
      "scenarioIds": ["CB114C_AUTH_JWT_GETTERS_CLONE"]
    },
    {
      "rowId": "apex:Auth.JWT.getIss()",
      "sourceBatch": "CB107-B17",
      "coverageKind": "exact-runtime",
      "passed": true,
      "evidenceReason": "direct invocation passed",
      "scenarioIds": ["CB114C_AUTH_JWT_GETTERS_CLONE"]
    },
    {
      "rowId": "apex:Auth.JWT.getNbfClockSkew()",
      "sourceBatch": "CB107-B17",
      "coverageKind": "exact-runtime",
      "passed": true,
      "evidenceReason": "direct invocation passed",
      "scenarioIds": ["CB114C_AUTH_JWT_GETTERS_CLONE"]
    },
    {
      "rowId": "apex:Auth.JWT.getSub()",
      "sourceBatch": "CB107-B17",
      "coverageKind": "exact-runtime",
      "passed": true,
      "evidenceReason": "direct invocation passed",
      "scenarioIds": ["CB114C_AUTH_JWT_GETTERS_CLONE"]
    },
    {
      "rowId": "apex:Auth.JWT.getValidityLength()",
      "sourceBatch": "CB107-B17",
      "coverageKind": "exact-runtime",
      "passed": true,
      "evidenceReason": "direct invocation passed",
      "scenarioIds": ["CB114C_AUTH_JWT_GETTERS_CLONE"]
    },
    {
      "rowId": "apex:Auth.JWT.setAdditionalClaims(Map<String,Object>)",
      "sourceBatch": "CB107-B17",
      "coverageKind": "exact-runtime",
      "passed": true,
      "evidenceReason": "direct setter passed",
      "scenarioIds": ["CB114R_REUSE_cb114-auth-jwt-user-context"]
    },
    {
      "rowId": "apex:Auth.JWT.toJSONString()",
      "sourceBatch": "CB107-B17",
      "coverageKind": "uncovered",
      "passed": false,
      "evidenceReason": "no concrete evidence",
      "scenarioIds": []
    },
    {
      "rowId": "apex:Auth.SessionManagement.getCurrentSession()",
      "sourceBatch": "CB107-B17",
      "coverageKind": "shared-handler-runtime",
      "passed": false,
      "evidenceReason": "outputs differ",
      "scenarioIds": ["CB114R_existing-auth-session-current"]
    }
  ]
}`

const cb115TestJSON = `{
  "schemaVersion": 1,
  "packet": "CB115",
  "total": 4,
  "rows": [
    {
      "surfaceId": "apex-language:NamespaceClassVariablePrecedence",
      "sourceBatch": "CB107-B22",
      "coverageKind": "uncovered",
      "scenarioIds": [],
      "evidencePaths": [],
      "reason": "no grouped scenario",
      "directWitnessAudit": {"passed": false}
    },
    {
      "surfaceId": "apex-language:TypeResolutionSystemNamespace",
      "sourceBatch": "CB107-B22",
      "coverageKind": "uncovered",
      "scenarioIds": [],
      "evidencePaths": [],
      "reason": "no grouped scenario",
      "directWitnessAudit": {"passed": false}
    },
    {
      "surfaceId": "apex:ApexPages.Action.Action(String)",
      "sourceBatch": "CB107-B21",
      "coverageKind": "uncovered",
      "scenarioIds": ["cb115-apexpages-action-component"],
      "evidencePaths": ["raw-glade/cb115-apexpages-action-component/test.exit"],
      "reason": "execution-failure",
      "comparison": [{"scenarioId": "cb115-apexpages-action-component", "status": "execution-failure", "localPassed": true, "salesforcePassed": false}],
      "directWitnessAudit": {"passed": false}
    },
    {
      "surfaceId": "apex:ApexPages.Action.clone()",
      "sourceBatch": "CB107-B21",
      "coverageKind": "uncovered",
      "scenarioIds": ["cb115-apexpages-action-component"],
      "evidencePaths": ["raw-glade/cb115-apexpages-action-component/test.exit"],
      "reason": "execution-failure",
      "comparison": [{"scenarioId": "cb115-apexpages-action-component", "status": "execution-failure", "localPassed": true, "salesforcePassed": false}],
      "directWitnessAudit": {"passed": false}
    }
  ]
}`

const spTestJSON = `{"total": 94352}`
