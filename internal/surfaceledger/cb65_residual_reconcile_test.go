package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCB65EventBusAccessLevelSurfaceIDsAreCanonicalAndUnique(t *testing.T) {
	rows := BuildGladeSnapshot()
	counts := make(map[string]int)
	for _, row := range rows {
		if strings.Contains(row.SurfaceID, "EventBus.publishWith") {
			counts[row.SurfaceID]++
		}
	}

	want := map[string]struct {
		returnType string
		parameters []string
	}{
		"apex:System.EventBus.publishWithAccessLevel(SObject,AccessLevel)": {
			returnType: "Database.SaveResult",
			parameters: []string{"SObject", "AccessLevel"},
		},
		"apex:System.EventBus.publishWithAccessLevel(SObject,Object,AccessLevel)": {
			returnType: "Database.SaveResult",
			parameters: []string{"SObject", "Object", "AccessLevel"},
		},
		"apex:System.EventBus.publishWithAccessLevel(List<SObject>,AccessLevel)": {
			returnType: "List<Database.SaveResult>",
			parameters: []string{"List<SObject>", "AccessLevel"},
		},
		"apex:System.EventBus.publishWithAccessLevel(List<SObject>,Object,AccessLevel)": {
			returnType: "List<Database.SaveResult>",
			parameters: []string{"List<SObject>", "Object", "AccessLevel"},
		},
	}
	for id, expected := range want {
		if counts[id] != 1 {
			t.Errorf("canonical EventBus SurfaceID %q count = %d, want exactly once", id, counts[id])
			continue
		}
		for _, row := range rows {
			if row.SurfaceID != id {
				continue
			}
			if row.ReturnType != expected.returnType {
				t.Errorf("canonical EventBus SurfaceID %q return type = %q, want %q", id, row.ReturnType, expected.returnType)
			}
			if strings.Join(row.Parameters, ",") != strings.Join(expected.parameters, ",") {
				t.Errorf("canonical EventBus SurfaceID %q parameters = %v, want %v", id, row.Parameters, expected.parameters)
			}
		}
	}
	for id, count := range counts {
		if _, ok := want[id]; !ok {
			t.Errorf("noncanonical EventBus SurfaceID %q occurs %d time(s)", id, count)
		}
	}

	if got := strings.Join(eventBusAccessLevelParameters("List<Database.SaveResult>", []string{"List<SObject>", "AccessLevel"}), ","); got != "List<SObject>,AccessLevel" {
		t.Fatalf("list EventBus two-parameter mapping = %q, want List<SObject>,AccessLevel", got)
	}
}

func TestCB65EventBusCanonicalizerAllowlist(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "exact SObject two-parameter", input: []string{"SObject", "AccessLevel"}, want: []string{"SObject", "AccessLevel"}},
		{name: "exact SObject three-parameter", input: []string{"SObject", "Object", "AccessLevel"}, want: []string{"SObject", "Object", "AccessLevel"}},
		{name: "exact List SObject two-parameter", input: []string{"List<SObject>", "AccessLevel"}, want: []string{"List<SObject>", "AccessLevel"}},
		{name: "exact List SObject three-parameter", input: []string{"List<SObject>", "Object", "AccessLevel"}, want: []string{"List<SObject>", "Object", "AccessLevel"}},
		{name: "Tooling generic Object two-parameter", input: []string{"Object", "Object"}, want: []string{"SObject", "AccessLevel"}},
		{name: "Tooling generic Object three-parameter", input: []string{"Object", "Object", "Object"}, want: []string{"SObject", "Object", "AccessLevel"}},
		{name: "Tooling generic List Object two-parameter", input: []string{"List<Object>", "Object"}, want: []string{"List<SObject>", "AccessLevel"}},
		{name: "Tooling generic List Object three-parameter", input: []string{"List<Object>", "Object", "Object"}, want: []string{"List<SObject>", "Object", "AccessLevel"}},
		{name: "unexpected scalar", input: []string{" String ", " AccessLevel "}, want: []string{"String", "AccessLevel"}},
		{name: "unexpected list", input: []string{" List<Account> ", " AccessLevel "}, want: []string{"List<Account>", "AccessLevel"}},
		{name: "unexpected SObject scalar pairing", input: []string{"SObject", "Object"}, want: []string{"Object", "Object"}},
		{name: "unexpected SObject three-parameter pairing", input: []string{"SObject", "Object", "Object"}, want: []string{"Object", "Object", "Object"}},
		{name: "unexpected List SObject scalar pairing", input: []string{"List<SObject>", "Object"}, want: []string{"List<Object>", "Object"}},
		{name: "unexpected Object AccessLevel pairing", input: []string{"Object", "AccessLevel"}, want: []string{"Object", "AccessLevel"}},
		{name: "unexpected arity", input: []string{"SObject", "AccessLevel", "Boolean", "Extra"}, want: []string{"Object", "AccessLevel", "Boolean", "Extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalEventBusAccessLevelParameters(tt.input)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("canonicalEventBusAccessLevelParameters(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCB65GeneratedSnapshotUsesOnlyCanonicalProductRows(t *testing.T) {
	byID := rowsByID(BuildGladeSnapshot())

	for _, id := range []string{
		"apex:Messaging.SendEmailOptions",
		"apex:System.Iterator.remove",
		"apex:System.Matcher.appendReplacement",
		"apex:System.Matcher.appendTail",
		"apex:System.Type.newInstance",
		"apex:System.PushUpgradeCustomizationRepository",
		"apex:System.QuickAction.describeAvailableActions(String)",
		"apex:System.Pattern.compile(String,Integer)",
		"apex:System.TimeZone.getDisplayName(Boolean)",
		"apex:System.Pattern.CANON_EQ()",
		"apex:System.Pattern.CASE_INSENSITIVE()",
		"apex:System.Pattern.COMMENTS()",
		"apex:System.Pattern.DOTALL()",
		"apex:System.Pattern.LITERAL()",
		"apex:System.Pattern.MULTILINE()",
		"apex:System.Pattern.UNICODE_CASE()",
		"apex:System.Pattern.UNIX_LINES()",
	} {
		if _, ok := byID[id]; ok {
			t.Errorf("noncanonical generated row remains: %s", id)
		}
	}

	for _, id := range []string{
		"apex:System.EventBus.publishWithAccessLevel(SObject,AccessLevel)",
		"apex:System.EventBus.publishWithAccessLevel(SObject,Object,AccessLevel)",
		"apex:System.EventBus.publishWithAccessLevel(List<SObject>,AccessLevel)",
		"apex:System.EventBus.publishWithAccessLevel(List<SObject>,Object,AccessLevel)",
		"apex:System.TimeZone.getDisplayName()",
		"apex:Messaging.InboundEmail.AuthenticationResult.AuthenticationResult()",
		"apex:Messaging.InboundEmail.AuthenticationResultField.AuthenticationResultField()",
		"apex:Messaging.InboundEmail.BinaryAttachment.BinaryAttachment()",
		"apex:Messaging.InboundEmail.TextAttachment.TextAttachment()",
		"apex:Messaging.SingleEmailMessage",
		"apex:PushUpgradeCustomizationRepository",
	} {
		if _, ok := byID[id]; !ok {
			t.Errorf("canonical product row is missing: %s", id)
		}
	}

	for _, id := range []string{
		"apex:Messaging.InboundEmail.AuthenticationResult.InboundEmail.AuthenticationResult()",
		"apex:Messaging.InboundEmail.AuthenticationResultField.InboundEmail.AuthenticationResultField()",
		"apex:Messaging.InboundEmail.BinaryAttachment.InboundEmail.BinaryAttachment()",
		"apex:Messaging.InboundEmail.TextAttachment.InboundEmail.TextAttachment()",
	} {
		if _, ok := byID[id]; ok {
			t.Errorf("malformed inbound constructor row remains: %s", id)
		}
	}
	for _, id := range []string{
		"apex:Schema.Schema.describeSObjects(List<String>)",
		"apex:Schema.Schema.getGlobalDescribe()",
	} {
		if _, ok := byID[id]; !ok {
			t.Errorf("canonical Schema row is missing: %s", id)
		}
	}
	for _, id := range []string{
		"apex:System.Schema.describeSObjects(List<String>)",
		"apex:System.Schema.getGlobalDescribe()",
	} {
		if _, ok := byID[id]; ok {
			t.Errorf("qualified Schema alias remains as a duplicate row: %s", id)
		}
	}
	if _, ok := byID["apex:RestResource"]; ok {
		t.Fatalf("@RestResource was invented as a standard-library type")
	}
}

func TestCB65EventBusGenericToolingRowsMergeWithExactContracts(t *testing.T) {
	type eventBusContract struct {
		parameters        []string
		genericParameters []string
		returnType        string
	}
	contracts := []eventBusContract{
		{parameters: []string{"SObject", "AccessLevel"}, genericParameters: []string{"Object", "Object"}, returnType: "Database.SaveResult"},
		{parameters: []string{"SObject", "Object", "AccessLevel"}, genericParameters: []string{"Object", "Object", "Object"}, returnType: "Database.SaveResult"},
		{parameters: []string{"List<SObject>", "AccessLevel"}, genericParameters: []string{"List<Object>", "Object"}, returnType: "List<Database.SaveResult>"},
		{parameters: []string{"List<SObject>", "Object", "AccessLevel"}, genericParameters: []string{"List<Object>", "Object", "Object"}, returnType: "List<Database.SaveResult>"},
	}
	var docs, org, glade, evidence []SurfaceLedgerRow
	for _, contract := range contracts {
		exactID := ApexMemberID("System", "EventBus", "publishWithAccessLevel", contract.parameters)
		genericID := "apex:System.EventBus.publishWithAccessLevel(" + strings.Join(contract.genericParameters, ",") + ")"
		base := SurfaceLedgerRow{
			Product:    ProductApex,
			Area:       AreaRuntime,
			Namespace:  "System",
			TypeName:   "EventBus",
			MemberName: "publishWithAccessLevel",
			Kind:       KindMethod,
		}
		docs = append(docs, RowFromDocs(SurfaceLedgerRow{
			SurfaceID:  exactID,
			Product:    base.Product,
			Area:       base.Area,
			Namespace:  base.Namespace,
			TypeName:   base.TypeName,
			MemberName: base.MemberName,
			Kind:       base.Kind,
			ReturnType: contract.returnType,
			Parameters: contract.parameters,
		}))
		org = append(org, RowFromOrg(SurfaceLedgerRow{
			SurfaceID:  genericID,
			Product:    base.Product,
			Area:       base.Area,
			Namespace:  base.Namespace,
			TypeName:   base.TypeName,
			MemberName: base.MemberName,
			Kind:       base.Kind,
			ReturnType: contract.returnType,
			Parameters: contract.genericParameters,
		}))
		glade = append(glade, RowFromGladeShape(SurfaceLedgerRow{
			SurfaceID:     exactID,
			Product:       base.Product,
			Area:          base.Area,
			Namespace:     base.Namespace,
			TypeName:      base.TypeName,
			MemberName:    base.MemberName,
			Kind:          base.Kind,
			ReturnType:    contract.returnType,
			Parameters:    contract.parameters,
			GladeBehavior: BehaviorSupported,
		}))
		evidence = append(evidence, RowFromEvidence(SurfaceLedgerRow{
			SurfaceID:     exactID,
			Product:       base.Product,
			Area:          base.Area,
			Namespace:     base.Namespace,
			TypeName:      base.TypeName,
			MemberName:    base.MemberName,
			Kind:          base.Kind,
			Parameters:    contract.parameters,
			GladeBehavior: BehaviorSupported,
			Evidence:      EvidenceFixture,
		}))
	}

	ledger := Merge(docs, org, glade, evidence)
	want := map[string]eventBusContract{}
	for _, contract := range contracts {
		id := ApexMemberID("System", "EventBus", "publishWithAccessLevel", contract.parameters)
		want[id] = contract
	}
	counts := map[string]int{}
	for _, row := range ledger.Rows {
		if strings.Contains(row.SurfaceID, "EventBus.publishWithAccessLevel") {
			counts[row.SurfaceID]++
		}
	}
	if len(counts) != len(want) || len(ledger.Rows) < len(want) {
		t.Fatalf("EventBus merged IDs = %d (%v), want exactly %d canonical IDs", len(counts), counts, len(want))
	}
	for _, contract := range contracts {
		id := ApexMemberID("System", "EventBus", "publishWithAccessLevel", contract.parameters)
		if counts[id] != 1 {
			t.Fatalf("canonical merged EventBus ID %q count = %d, want exactly once", id, counts[id])
		}
		row := rowsByID(ledger.Rows)[id]
		if strings.Join(row.Parameters, ",") != strings.Join(contract.parameters, ",") ||
			strings.Join(row.DocsParameters, ",") != strings.Join(contract.parameters, ",") ||
			strings.Join(row.OrgParameters, ",") != strings.Join(contract.parameters, ",") ||
			strings.Join(row.GladeParameters, ",") != strings.Join(contract.parameters, ",") {
			t.Errorf("%s parameter fields are inconsistent: parameters=%v docs=%v org=%v glade=%v", id, row.Parameters, row.DocsParameters, row.OrgParameters, row.GladeParameters)
		}
		if row.ReturnType != contract.returnType || row.DocsReturnType != contract.returnType || row.OrgReturnType != contract.returnType || row.GladeReturnType != contract.returnType {
			t.Errorf("%s return types are inconsistent: return=%q docs=%q org=%q glade=%q", id, row.ReturnType, row.DocsReturnType, row.OrgReturnType, row.GladeReturnType)
		}
		if row.Bucket == BucketFailure || row.GapClass != "" {
			t.Errorf("%s classified as bucket=%q gap=%q, want no mismatch/failure bucket", id, row.Bucket, row.GapClass)
		}
	}
	for _, genericID := range []string{
		"apex:System.EventBus.publishWithAccessLevel(Object,Object)",
		"apex:System.EventBus.publishWithAccessLevel(Object,Object,Object)",
		"apex:System.EventBus.publishWithAccessLevel(List<Object>,Object)",
		"apex:System.EventBus.publishWithAccessLevel(List<Object>,Object,Object)",
	} {
		if counts[genericID] != 0 {
			t.Errorf("generic Tooling duplicate ID %q remains with count %d", genericID, counts[genericID])
		}
	}
}

func TestCB65EventBusFixtureExercisesAndMergesAllFourOverloads(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "docs", "fixtures", "core-runtime-eventbus-accesslevel.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Evidence []struct {
			SurfaceID string `json:"surfaceId"`
		} `json:"evidence"`
		Command struct {
			Args []string `json:"args"`
		} `json:"command"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{
		"apex:System.EventBus.publishWithAccessLevel(SObject,AccessLevel)",
		"apex:System.EventBus.publishWithAccessLevel(SObject,Object,AccessLevel)",
		"apex:System.EventBus.publishWithAccessLevel(List<SObject>,AccessLevel)",
		"apex:System.EventBus.publishWithAccessLevel(List<SObject>,Object,AccessLevel)",
	}
	counts := map[string]int{}
	for _, item := range fixture.Evidence {
		counts[item.SurfaceID]++
	}
	for _, id := range wantIDs {
		if counts[id] != 1 {
			t.Errorf("fixture evidence ID %q count = %d, want exactly once", id, counts[id])
		}
	}
	if len(fixture.Evidence) != len(wantIDs) {
		t.Fatalf("fixture evidence rows = %d, want %d", len(fixture.Evidence), len(wantIDs))
	}
	if len(fixture.Command.Args) != 1 {
		t.Fatalf("fixture command args = %v, want one executable command", fixture.Command.Args)
	}
	command := fixture.Command.Args[0]
	for _, invocation := range []string{
		"EventBus.publishWithAccessLevel(new Event_Recipes_Demo__e(), AccessLevel.USER_MODE)",
		"EventBus.publishWithAccessLevel(new Event_Recipes_Demo__e(), null, AccessLevel.USER_MODE)",
		"EventBus.publishWithAccessLevel(new List<SObject>{new Event_Recipes_Demo__e()}, AccessLevel.SYSTEM_MODE)",
		"EventBus.publishWithAccessLevel(new List<SObject>{new Event_Recipes_Demo__e()}, null, AccessLevel.SYSTEM_MODE)",
	} {
		if strings.Count(command, invocation) != 1 {
			t.Errorf("fixture command invocation %q count = %d, want exactly once", invocation, strings.Count(command, invocation))
		}
	}

	evidenceRows, err := BuildEvidenceSnapshot([]string{fixturePath})
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidenceRows)
	byID := rowsByID(ledger.Rows)
	for _, id := range wantIDs {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("fixture-backed merged EventBus row is missing: %s", id)
		}
		if row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture || row.Bucket != BucketImplemented || row.Bucket == BucketExplicitUnsupported {
			t.Errorf("%s merged behavior/evidence/bucket = %s/%s/%s, want supported/fixture/implemented", id, row.GladeBehavior, row.Evidence, row.Bucket)
		}
	}
}

func TestCB65FixtureSourcesRemoveCompileRejectedAliases(t *testing.T) {
	fixtureRoot := filepath.Join("..", "..", "docs", "fixtures")
	paths := []string{
		filepath.Join(fixtureRoot, "integration-messaging-send-options-unsupported.json"),
		filepath.Join(fixtureRoot, "core-runtime-messaging-send-capture-full-local.json"),
		filepath.Join(fixtureRoot, "core-runtime-canvas-datasource-push-unsupported.json"),
		filepath.Join(fixtureRoot, "integration-canvas-unsupported.json"),
		filepath.Join(fixtureRoot, "integration-canvas-lifecycle-unsupported.json"),
		filepath.Join(fixtureRoot, "core-runtime-canvas-local-evidence.json"),
		filepath.Join(fixtureRoot, "async-batchable-passive-contracts.json"),
		filepath.Join(fixtureRoot, "async-options-delay-local-evidence.json"),
		filepath.Join(fixtureRoot, "data-platform-database-dml-frontier-evidence.json"),
		filepath.Join(fixtureRoot, "core-pattern-dialect-flags-stdlib.json"),
		filepath.Join(fixtureRoot, "apex-industry-addon-unsupported-surfaces.json"),
		filepath.Join(fixtureRoot, "core-runtime-messaging-single-email-accessors-broad-evidence.json"),
		filepath.Join(fixtureRoot, "core-runtime-messaging-inbound-email-dto-evidence.json"),
		filepath.Join(fixtureRoot, "examples-apex-rest.json"),
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(evidence)
	for _, id := range []string{
		"apex:Messaging.SendEmailOptions",
		"apex:System.Messaging.SendEmailOptions",
		"apex:System.Messaging.SingleEmailMessage",
		"apex:Canvas.EnvironmentContext.getParameters",
		"apex:Canvas.EnvironmentContext.getParametersAsJSON",
		"apex:Canvas.LifecycleHandler.onRender",
		"apex:Database.BatchableContext.BatchableContext()",
		"apex:System.AsyncOptions.getMinimumQueueableDelayInMinutes()",
		"apex:System.Database.lock",
		"apex:System.Database.unlock",
		"apex:System.QuickAction.describeAvailableActions(String)",
		"apex:System.Pattern.compile(String,Integer)",
	} {
		if _, ok := byID[id]; ok {
			t.Errorf("compile-rejected fixture row remains: %s", id)
		}
	}
	for _, id := range []string{
		"apex:Messaging.SingleEmailMessage",
		"apex:Messaging.InboundEmail.AuthenticationResult.AuthenticationResult()",
		"apex:Messaging.InboundEmail.AuthenticationResultField.AuthenticationResultField()",
		"apex:Messaging.InboundEmail.BinaryAttachment.BinaryAttachment()",
		"apex:Messaging.InboundEmail.TextAttachment.TextAttachment()",
	} {
		if _, ok := byID[id]; !ok {
			t.Errorf("retained fixture row is missing: %s", id)
		}
	}
	policy, err := LoadSupportPolicy(filepath.Join(fixtureRoot, "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var restPolicy *SupportPolicyRule
	for i := range policy.Rules {
		if policy.Rules[i].SurfacePrefix == "apex:RestResource" {
			restPolicy = &policy.Rules[i]
			break
		}
	}
	if restPolicy == nil || restPolicy.Disposition != DispositionLocalRuntimeRequired || restPolicy.Reason != "RestResource annotation surface" {
		t.Fatalf("@RestResource policy = %#v, want retained local-runtime-required annotation rule", restPolicy)
	}
}

func TestCB65GeneratedStubSnapshotOmitsRejectedAndAcquiredAliases(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "generated", "stubs", "STUB_CONTRACTS.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Entries []struct {
			ID string `json:"id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]bool, len(report.Entries))
	for _, entry := range report.Entries {
		byID[entry.ID] = true
	}
	for _, id := range []string{
		"Pattern.CANON_EQ()",
		"Pattern.CASE_INSENSITIVE()",
		"Pattern.COMMENTS()",
		"Pattern.DOTALL()",
		"Pattern.LITERAL()",
		"Pattern.MULTILINE()",
		"Pattern.UNICODE_CASE()",
		"Pattern.UNIX_LINES()",
		"Pattern.compile(String,Integer)",
		"InvalidParameterValueException.<init>()",
		"InvalidParameterValueException.<init>(Exception)",
		"InvalidParameterValueException.<init>(String)",
		"InvalidParameterValueException.<init>(String,String)",
		"NoAccessException.<init>(Exception)",
		"NoAccessException.<init>(String)",
		"NoAccessException.<init>(String,Exception)",
		"NoDataFoundException.<init>(Exception)",
		"NoDataFoundException.<init>(String)",
		"NoDataFoundException.<init>(String,Exception)",
		"NullPointerException.<init>(Exception)",
		"NullPointerException.<init>(String)",
		"NullPointerException.<init>(String,Exception)",
		"PushUpgradeCustomizationRepository.create(String,String,Boolean,Integer)",
		"PushUpgradeCustomizationRepository.getPushUpgradeBlockInitiatedDateForId(String)",
		"PushUpgradeCustomizationRepository.getPushUpgradeBlockInitiatedDateForIndex(String,String)",
		"PushUpgradeCustomizationRepository.getCustomizationSummaryById(String)",
		"PushUpgradeCustomizationRepository.getCustomizationSummaryByIndex(String,String)",
		"PushUpgradeCustomizationRepository.getExpirationDaysForId(String)",
		"PushUpgradeCustomizationRepository.getExpirationDaysForIndex(String,String)",
		"PushUpgradeCustomizationRepository.isBlockingCapabilityExpiredForId(String)",
		"PushUpgradeCustomizationRepository.isBlockingCapabilityExpiredForIndex(String,String)",
		"PushUpgradeCustomizationRepository.listAllCustomizationSummaries()",
		"PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForId(String,Boolean,Integer)",
		"PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForIndex(String,String,Boolean,Integer)",
		"PushUpgradeCustomizationRepository.setExpirationDaysForId(String,Integer)",
		"PushUpgradeCustomizationRepository.setExpirationDaysForIndex(String,String,Integer)",
	} {
		if byID[id] {
			t.Errorf("rejected or acquired alias remains in checked stub snapshot: %s", id)
		}
	}
	for _, id := range []string{
		"NoAccessException.<init>()",
		"NoDataFoundException.<init>()",
		"NullPointerException.<init>()",
		"PushUpgradeCustomizationRepository.create(String,String,Boolean)",
		"PushUpgradeCustomizationRepository.deleteById(String)",
		"PushUpgradeCustomizationRepository.deleteByIndex(String,String)",
		"PushUpgradeCustomizationRepository.getCustomUpgradeAllowedForId(String)",
		"PushUpgradeCustomizationRepository.getCustomUpgradeAllowedForIndex(String,String)",
		"PushUpgradeCustomizationRepository.getCustomUpgradeTypeForId(String)",
		"PushUpgradeCustomizationRepository.getCustomUpgradeTypeForIndex(String,String)",
		"PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForId(String,Boolean)",
		"PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForIndex(String,String,Boolean)",
	} {
		if !byID[id] {
			t.Errorf("retained canonical stub row is missing: %s", id)
		}
	}
}

func TestCB65FreshLedgerKeepsOnlyDeclaredBehaviorGaps(t *testing.T) {
	fixtureRoot := filepath.Join("..", "..", "docs", "fixtures")
	paths := []string{
		filepath.Join(fixtureRoot, "core-blob-crypto-partial-encrypt-unsupported-decrypt-sign-verify.json"),
		filepath.Join(fixtureRoot, "apex-process-support-tail-unsupported-surfaces.json"),
		filepath.Join(fixtureRoot, "core-runtime-cb72-frozen-behavior-local-evidence.json"),
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildOracleEvidenceSnapshot([]string{
		filepath.Join(fixtureRoot, "salesforce-cb72-frozen-behavior-comparisons.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), append(evidence, oracle...))
	policy, err := LoadSupportPolicy(filepath.Join(fixtureRoot, "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile := ComputeSupportProfile(ledger.Rows, policy, nil)
	const target = "apex:System.Security.stripInaccessible(AccessType,List<Object>,Boolean,Id)"
	for _, row := range profile.NonDeferredGaps {
		if row.SurfaceID == target {
			t.Fatalf("stale residual behavior gap remains: %#v", row)
		}
	}
}
