package surfaceledger

import "testing"

func TestBuildGladeSnapshotIncludesKnownStdlibBehavior(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	id := ApexMemberID("System", "String", "contains", []string{"String"})
	row, ok := byID[id]
	if !ok {
		t.Fatalf("missing %s", id)
	}
	if row.GladeShape == ShapeAbsent || row.GladeBehavior == BehaviorNone {
		t.Fatalf("String.contains states = shape:%s behavior:%s", row.GladeShape, row.GladeBehavior)
	}
}

func TestBuildGladeSnapshotIncludesApexNamespaceLanguageRules(t *testing.T) {
	ledger := Merge(nil, nil, BuildGladeSnapshot(), nil)
	byID := rowsByID(ledger.Rows)
	for _, id := range []string{
		ApexLanguageRuleID("SystemNamespaceDefaultImport"),
		ApexLanguageRuleID("SchemaNamespaceImplicitImport"),
		ApexLanguageRuleID("NamespaceClassVariablePrecedence"),
		ApexLanguageRuleID("TypeResolutionSystemNamespace"),
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing language rule row %s", id)
		}
		if row.Product != ProductApex || row.Area != AreaFrontend || row.Kind != KindLanguageRule {
			t.Fatalf("%s product/area/kind = %s/%s/%s", id, row.Product, row.Area, row.Kind)
		}
		if row.Owner != "internal/sema" {
			t.Fatalf("%s owner = %q", id, row.Owner)
		}
		if row.Bucket != BucketImplemented || row.GapClass != "" {
			t.Fatalf("%s bucket/gap = %s/%s", id, row.Bucket, row.GapClass)
		}
	}
}

func TestBuildGladeSnapshotIncludesSurfaceClosureTailShapes(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	tests := []struct {
		id       string
		behavior BehaviorState
	}{
		{ApexMemberID("System", "List", "List", []string{"Set<T>"}), BehaviorPassive},
	}
	for _, tt := range tests {
		row, ok := byID[tt.id]
		if !ok {
			t.Fatalf("missing surface closure tail row %s", tt.id)
		}
		if row.Product != ProductApex || row.GladeShape == ShapeAbsent || row.GladeBehavior != tt.behavior {
			t.Fatalf("%s product/shape/behavior = %s/%s/%s, want apex/non-absent/%s", tt.id, row.Product, row.GladeShape, row.GladeBehavior, tt.behavior)
		}
	}
}

func TestBuildGladeSnapshotUsesHTTPStdlibSignature(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	id := ApexMemberID("System", "Http", "send", []string{"HttpRequest"})
	row, ok := byID[id]
	if !ok {
		t.Fatalf("missing %s", id)
	}
	if row.GladeBehavior != BehaviorSupported {
		t.Fatalf("Http.send(HttpRequest) behavior = %s", row.GladeBehavior)
	}
	if _, ok := byID["apex:System.Http.send local mock callouts"]; ok {
		t.Fatalf("behavior label should not create an Apex surface row")
	}
}

func TestBuildGladeSnapshotMarksGeneratedStandardSObjectShape(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)

	objectID := DataObjectID("AIInsightAction")
	objectRow, ok := byID[objectID]
	if !ok {
		t.Fatalf("missing generated standard object row %s", objectID)
	}
	if objectRow.GladeShape != ShapeGenerated || objectRow.ShapeSource != SourceStandardSObjectGeneratedShape {
		t.Fatalf("object row shape = %s source = %q", objectRow.GladeShape, objectRow.ShapeSource)
	}

	fieldID := DataFieldID("AIInsightAction", "AiRecordInsightId")
	fieldRow, ok := byID[fieldID]
	if !ok {
		t.Fatalf("missing generated standard field row %s", fieldID)
	}
	if fieldRow.GladeShape != ShapeGenerated || fieldRow.ReturnType != "REFERENCE" {
		t.Fatalf("field row shape = %s returnType = %q", fieldRow.GladeShape, fieldRow.ReturnType)
	}
	if fieldRow.GladeBehavior != BehaviorSupported {
		t.Fatalf("field row behavior = %s", fieldRow.GladeBehavior)
	}
}

func TestBuildGladeSnapshotFencesLocalTestLWCServiceModules(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, id := range []string{
		LWCModuleID("Decorators"),
		LWCModuleID("`lightning/graphql`"),
		LWCModuleID("`lightning/uiGraphQLApi`"),
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing LWC row %s", id)
		}
		if row.Product != ProductLWC || row.GladeBehavior != BehaviorUnsupported {
			t.Fatalf("LWC row %s product=%s behavior=%s", id, row.Product, row.GladeBehavior)
		}
	}
}

func TestBuildGladeSnapshotIncludesLocalTestVisualforceComponentShape(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, id := range []string{
		"visualforce:pages_compref_pageBlockTable",
		"visualforce:pages_compref_commandButton",
		"visualforce:pages_compref_includeLightning",
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing Visualforce component row %s", id)
		}
		if row.Product != ProductVisualforce || row.Area != AreaUI || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorPassive {
			t.Fatalf("%s product/area/shape/behavior = %s/%s/%s/%s", id, row.Product, row.Area, row.GladeShape, row.GladeBehavior)
		}
	}
}

func TestBuildGladeSnapshotIncludesLocalTestAuraMetadataShape(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, id := range []string{
		"unknown:apex_classes_annotation_AuraEnabled",
		"unknown:meta_auradefinitionbundle",
		"unknown:ref_aura_application",
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing Aura metadata row %s", id)
		}
		if row.Area != AreaUI || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorPassive {
			t.Fatalf("%s area/shape/behavior = %s/%s/%s", id, row.Area, row.GladeShape, row.GladeBehavior)
		}
	}
}

func TestBuildGladeSnapshotIncludesEmbeddedOrgDescribeStandardSObjectShape(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)

	fieldID := DataFieldID("CareProgram", "ParentProgramId")
	fieldRow, ok := byID[fieldID]
	if !ok {
		t.Fatalf("missing embedded describe-backed standard field row %s", fieldID)
	}
	if fieldRow.GladeShape != ShapeGenerated || fieldRow.ReturnType != "REFERENCE" {
		t.Fatalf("field row shape = %s returnType = %q", fieldRow.GladeShape, fieldRow.ReturnType)
	}
	if fieldRow.GladeBehavior != BehaviorSupported {
		t.Fatalf("field row behavior = %s", fieldRow.GladeBehavior)
	}
}

func TestBuildGladeSnapshotIncludesReferenceBackedStandardObjectNames(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, objectName := range []string{"ApexInlineEventLog", "ConsumptionSchedule", "DataDetectPolicySnapshot", "ForecastingColumnDefinitionFormulaFieldDetails", "RpaRobot", "feedSignal"} {
		id := DataObjectID(objectName)
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing reference-backed standard object row %s", id)
		}
		if row.GladeShape != ShapeGenerated || row.ShapeSource != SourceStandardSObjectGeneratedShape || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s row states = shape:%s source:%q behavior:%s", id, row.GladeShape, row.ShapeSource, row.GladeBehavior)
		}
	}
}

func TestBuildGladeSnapshotUsesPropertyIDWithoutCallParens(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	propertyID := ApexMemberID("ApexPages", "Component", "childComponents", nil)
	callID := ApexMemberID("ApexPages", "Component", "childComponents", []string{})
	if byID[propertyID].GladeShape == ShapeAbsent {
		t.Fatalf("missing property row %s", propertyID)
	}
	if _, ok := byID[callID]; ok {
		t.Fatalf("property should not also appear as zero-arg call %s", callID)
	}
}

func TestBuildGladeSnapshotIncludesMessagingLocalTestDTOShapes(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, id := range []string{
		ApexMemberID("Messaging", "Email", "setTemplateID", []string{"Id"}),
		ApexTypeID("Messaging", "InboundEmail.AuthenticationResult"),
		ApexMemberID("Messaging", "InboundEmail.AuthenticationResult", "AuthenticationResult", []string{}),
		ApexMemberID("Messaging", "InboundEmail.AuthenticationResult", "authenticationResultFields", nil),
		ApexMemberID("Messaging", "InboundEmail.AuthenticationResult", "method", nil),
		ApexMemberID("Messaging", "InboundEmail.AuthenticationResult", "result", nil),
		ApexTypeID("Messaging", "InboundEmail.AuthenticationResultField"),
		ApexMemberID("Messaging", "InboundEmail.AuthenticationResultField", "AuthenticationResultField", []string{}),
		ApexMemberID("Messaging", "InboundEmail.AuthenticationResultField", "name", nil),
		ApexMemberID("Messaging", "InboundEmail.AuthenticationResultField", "value", nil),
		ApexMemberID("Messaging", "InboundEmail.BinaryAttachment", "BinaryAttachment", []string{}),
		ApexMemberID("Messaging", "InboundEmail.TextAttachment", "TextAttachment", []string{}),
		ApexMemberID("Messaging", "SingleEmailMessage", "setDocumentAttachments", []string{"List<Id>"}),
		ApexMemberID("Messaging", "SingleEmailMessage", "setFileAttachments", []string{"List<Messaging.EmailFileAttachment>"}),
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing Messaging local-test row %s", id)
		}
		if row.GladeShape == ShapeAbsent {
			t.Fatalf("Messaging local-test row %s has absent shape", id)
		}
	}
}

func TestBuildGladeSnapshotUsesSchemaDescribePropertyIDs(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	id := ApexMemberID("Schema", "DescribeTabSetResult", "name", nil)
	row, ok := byID[id]
	if !ok {
		t.Fatalf("missing Schema describe property row %s", id)
	}
	if row.Kind != KindProperty || row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported {
		t.Fatalf("property row = kind:%s shape:%s behavior:%s", row.Kind, row.GladeShape, row.GladeBehavior)
	}
}

func TestStdlibAPIIDParsesQualifiedSchemaMethods(t *testing.T) {
	got := idFromStdlibAPI("Schema.describeDataCategoryGroups(List<String>)")
	want := ApexMemberID("System", "Schema", "describeDataCategoryGroups", []string{"List<String>"})
	if got != want {
		t.Fatalf("id = %q, want %q", got, want)
	}

	got = idFromStdlibAPI("Schema.describeDataCategoryGroupStructures(List<Schema.DataCategoryGroupSobjectTypePair>,Boolean)")
	want = ApexMemberID("System", "Schema", "describeDataCategoryGroupStructures", []string{"List<Schema.DataCategoryGroupSobjectTypePair>", "Boolean"})
	if got != want {
		t.Fatalf("id = %q, want %q", got, want)
	}
}

func TestBuildGladeSnapshotPromotesBusinessHoursLocalContract(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	id := ApexMemberID("", "BusinessHours", "add", []string{"Id", "Datetime", "Long"})
	row, ok := byID[id]
	if !ok {
		t.Fatalf("missing BusinessHours row %s", id)
	}
	if row.GladeBehavior != BehaviorSupported {
		t.Fatalf("BusinessHours.add behavior = %s, want %s", row.GladeBehavior, BehaviorSupported)
	}

	id = ApexTypeID("System", "BusinessHours malformed local holiday metadata")
	if _, ok = byID[id]; ok {
		t.Fatalf("synthetic BusinessHours boundary row remains: %s", id)
	}
}

func TestBuildGladeSnapshotMarksNonLocalQueryDocsUnsupported(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, id := range []string{
		"unknown:salesforce_app_limits_platform_soslsoql",
		"unknown:sforce_api_calls_describesoqllistviewsrequest",
		"unknown:sforce_api_calls_soql_feeds_url_syntax",
		"unknown:sforce_api_calls_soql_relationships_query_datacat",
		"unknown:sforce_api_calls_soql_relationships_query_hist",
		"unknown:sforce_api_calls_soql_select_set_options",
		"unknown:sforce_api_calls_soql_select_with_datacategory",
		"unknown:sforce_api_calls_soql_select_with_datacategory_catselection",
		"unknown:sforce_api_calls_soql_select_with_recordvisibilitycontext",
		"unknown:sforce_api_calls_soql_typos",
		"unknown:sforce_api_calls_sosl_limits_external_objects",
		"unknown:sforce_api_calls_sosl_typos",
		"unknown:sforce_api_calls_sosl_update_tracking",
		"unknown:sforce_api_calls_sosl_update_viewstat",
		"unknown:sforce_api_calls_sosl_using_listview",
		"unknown:sforce_api_calls_sosl_with",
		"unknown:sforce_api_calls_sosl_with_data_category",
		"unknown:sforce_api_calls_sosl_with_metadata",
		"unknown:supported_soql",
		"unknown:unsupported_soql_statements",
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing unsupported query docs row %s", id)
		}
		if row.GladeBehavior != BehaviorUnsupported {
			t.Fatalf("%s behavior = %s, want %s", id, row.GladeBehavior, BehaviorUnsupported)
		}
	}
}

func TestBuildGladeSnapshotAddsFixtureBackedStringAliasRows(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, id := range []string{
		ApexMemberID("System", "String", "escapeCsv", nil),
		ApexMemberID("System", "URL", "getAuthority", nil),
		ApexMemberID("System", "Map", "containsKey", nil),
		ApexMemberID("System", "JSONParser", "getBooleanValue", nil),
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing stdlib alias row %s", id)
		}
		if row.GladeShape != ShapeSignatureKnown || row.GladeBehavior != BehaviorSupported || row.Kind != KindMethod {
			t.Fatalf("%s shape/behavior/kind = %s/%s/%s, want signature-known/supported/method", id, row.GladeShape, row.GladeBehavior, row.Kind)
		}
		if !hasSource(row.Sources, "stdlib-fixture-alias") {
			t.Fatalf("%s sources = %#v, want stdlib-fixture-alias", id, row.Sources)
		}
	}
}

func TestBuildGladeSnapshotAddsFixtureBackedSystemAliasRows(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, tc := range []struct {
		id       string
		kind     string
		behavior BehaviorState
	}{
		{id: "apex:System.Crypto.areEqualConstantTime(Blob,Blob)", kind: KindMethod, behavior: BehaviorSupported},
		{id: "apex:System.CustomMetadataType.getAll", kind: KindMethod, behavior: BehaviorSupported},
		{id: "apex:System.Messaging.MassEmailMessage", kind: KindType, behavior: BehaviorPassive},
		{id: "apex:System.Matcher.hasTransparentBounds", kind: KindMethod, behavior: BehaviorSupported},
		{id: "apex:System.Matcher.useTransparentBounds", kind: KindMethod, behavior: BehaviorSupported},
		{id: "apex:System.PageReference(record)", kind: KindMethod, behavior: BehaviorSupported},
		{id: "apex:System.Integer.MAX_VALUE", kind: KindProperty, behavior: BehaviorSupported},
		{id: "apex:System.TxnSecurity.EventCondition.evaluate(SObject)", kind: KindMethod, behavior: BehaviorSupported},
		{id: "apex:System.TxnSecurity.PolicyCondition.evaluate(TxnSecurity.Event)", kind: KindMethod, behavior: BehaviorSupported},
		{id: "apex:System.Search.find", kind: KindMethod, behavior: BehaviorPartial},
		{id: "apex:System.Search.find(String,Object)", kind: KindMethod, behavior: BehaviorPartial},
		{id: "apex:System.Search.query(String,Object)", kind: KindMethod, behavior: BehaviorPartial},
		{id: "apex:System.Search.suggest(String,String,Object)", kind: KindMethod, behavior: BehaviorPartial},
		{id: "apex:System.Search.suggest(String,String,Object,Object)", kind: KindMethod, behavior: BehaviorPartial},
	} {
		row, ok := byID[tc.id]
		if !ok {
			t.Fatalf("missing system alias row %s", tc.id)
		}
		if row.GladeShape == ShapeAbsent || row.GladeBehavior != tc.behavior || row.Kind != tc.kind {
			t.Fatalf("%s shape/behavior/kind = %s/%s/%s, want non-absent/%s/%s", tc.id, row.GladeShape, row.GladeBehavior, row.Kind, tc.behavior, tc.kind)
		}
		if !hasSource(row.Sources, "system-fixture-alias") {
			t.Fatalf("%s sources = %#v, want system-fixture-alias", tc.id, row.Sources)
		}
		if tc.id == "apex:System.CustomMetadataType.getAll" && (row.Namespace != "System" || row.TypeName != "CustomMetadataType" || row.MemberName != "getAll") {
			t.Fatalf("%s namespace/type/member = %s/%s/%s, want System/CustomMetadataType/getAll", tc.id, row.Namespace, row.TypeName, row.MemberName)
		}
	}
}

func TestBuildGladeSnapshotAddsFixtureBackedApexAliasRows(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, tc := range []struct {
		id       string
		member   string
		typ      string
		behavior BehaviorState
	}{
		{id: "apex:TxnSecurity.Event.Event()", typ: "Event", member: "Event", behavior: BehaviorSupported},
		{id: "apex:Messaging.SingleEmailMessage.setFileAttachments(List<EmailFileAttachment>)", typ: "SingleEmailMessage", member: "setFileAttachments", behavior: BehaviorSupported},
		{id: "apex:Support.EmailTemplateSelector.getDefaultTemplateId(Id)", typ: "EmailTemplateSelector", member: "getDefaultTemplateId", behavior: BehaviorSupported},
	} {
		row, ok := byID[tc.id]
		if !ok {
			t.Fatalf("missing apex alias row %s", tc.id)
		}
		if row.GladeShape == ShapeAbsent || row.GladeBehavior != tc.behavior || row.Kind != KindMethod {
			t.Fatalf("%s shape/behavior/kind = %s/%s/%s, want non-absent/%s/method", tc.id, row.GladeShape, row.GladeBehavior, row.Kind, tc.behavior)
		}
		if !hasSource(row.Sources, "apex-fixture-alias") {
			t.Fatalf("%s sources = %#v, want apex-fixture-alias", tc.id, row.Sources)
		}
		if row.TypeName != tc.typ || row.MemberName != tc.member {
			t.Fatalf("%s type/member = %s/%s, want %s/%s", tc.id, row.TypeName, row.MemberName, tc.typ, tc.member)
		}
	}
}

func TestBuildGladeSnapshotAddsFixtureBackedMirrorAliasRows(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, tc := range []struct {
		id       string
		kind     string
		behavior BehaviorState
	}{
		{id: "apex:pref.LoadFormData.addOption(String,String,String)", kind: KindMethod, behavior: BehaviorPassive},
		{id: "apex:ise.DynamicMenuItem.Label", kind: KindProperty, behavior: BehaviorPassive},
		{id: "apex:commercepayments.PostAuthorizationRequest_amount", kind: KindType, behavior: BehaviorPassive},
		{id: "apex:industriesNlpSvc.NlpResponse_errors", kind: KindType, behavior: BehaviorPassive},
		{id: "apex:setup.FlowPerformanceSetupDetails", kind: KindType, behavior: BehaviorPassive},
		{id: "apex:sfdc.LearningItemEvaluationHandler.evaluate(Sfdc_enablement.LearningEvaluation)", kind: KindMethod, behavior: BehaviorStubNoOp},
		{id: "apex:sfdc.SurveyInvitationLinkShortener.getShortenedURL(String)", kind: KindMethod, behavior: BehaviorPassive},
		{id: "apex:RichMessaging.ProcessFormHandler.processFormRequest", kind: KindMethod, behavior: BehaviorStubNoOp},
	} {
		row, ok := byID[tc.id]
		if !ok {
			t.Fatalf("missing mirror alias row %s", tc.id)
		}
		if row.GladeShape == ShapeAbsent || row.GladeBehavior != tc.behavior || row.Kind != tc.kind {
			t.Fatalf("%s shape/behavior/kind = %s/%s/%s, want non-absent/%s/%s", tc.id, row.GladeShape, row.GladeBehavior, row.Kind, tc.behavior, tc.kind)
		}
		if !hasSource(row.Sources, "apex-mirror-alias") {
			t.Fatalf("%s sources = %#v, want apex-mirror-alias", tc.id, row.Sources)
		}
	}
}

func TestSurfaceIDKeyNormalizesSystemQualifiedRuntimeParameters(t *testing.T) {
	left := surfaceIDKey(ApexMemberID("System", "Test", "createSoqlStub", []string{"Schema.SObjectType", "System.SoqlStubProvider"}))
	right := surfaceIDKey(ApexMemberID("System", "Test", "createSoqlStub", []string{"Schema.SObjectType", "SoqlStubProvider"}))
	if left != right {
		t.Fatalf("keys differ: %q != %q", left, right)
	}

	left = surfaceIDKey("apex:System.Test.createSoqlStub(Schema.SObjectType,System.SoqlStubProvider)")
	right = surfaceIDKey("apex:System.Test.createSoqlStub(Schema.SObjectType,SoqlStubProvider)")
	if left != right {
		t.Fatalf("raw keys differ: %q != %q", left, right)
	}

	left = surfaceIDKey(ApexMemberID("System", "StubProvider", "handleMethodCall", []string{"Object", "String", "Type", "List<System.Type>", "List<String>", "List<Object>"}))
	right = surfaceIDKey(ApexMemberID("System", "StubProvider", "handleMethodCall", []string{"Object", "String", "Type", "List<Type>", "List<String>", "List<Object>"}))
	if left != right {
		t.Fatalf("generic keys differ: %q != %q", left, right)
	}
}

func TestBuildGladeSnapshotUsesParameterizedDataCategoryStdlibRows(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	typedID := ApexMemberID("Schema", "Schema", "describeDataCategoryGroups", []string{"List<String>"})
	if _, ok := byID[typedID]; !ok {
		t.Fatalf("missing typed data category row %s", typedID)
	}
	coarseID := ApexMemberID("Schema", "Schema", "describeDataCategoryGroups", nil)
	if _, ok := byID[coarseID]; ok {
		t.Fatalf("found unparameterized data category stdlib row %s", coarseID)
	}
}

func TestBuildGladeSnapshotUsesCanonicalSchemaDescribeStdlibRows(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, id := range []string{
		ApexMemberID("Schema", "Schema", "getGlobalDescribe", []string{}),
		ApexMemberID("Schema", "Schema", "describeSObjects", []string{"List<String>"}),
	} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing canonical schema stdlib row %s", id)
		}
	}
	for _, id := range []string{
		ApexMemberID("Schema", "Schema", "getGlobalDescribe", nil),
		ApexMemberID("Schema", "Schema", "describeSObjects", nil),
	} {
		if _, ok := byID[id]; ok {
			t.Fatalf("found unparameterized schema stdlib row %s", id)
		}
	}
}

func TestBuildGladeSnapshotIncludesBatchQueryLocatorOverloads(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, id := range []string{
		ApexTypeID("Database", "QueryLocator"),
		ApexMemberID("Database", "QueryLocator", "getQuery", []string{}),
		ApexMemberID("Database", "QueryLocator", "iterator", []string{}),
		ApexMemberID("System", "Database", "getQueryLocator", []string{"List<Object>"}),
		ApexMemberID("System", "Database", "getQueryLocator", []string{"List<Object>", "System.AccessLevel"}),
		ApexMemberID("System", "Database", "getQueryLocator", []string{"Object"}),
		ApexMemberID("System", "Database", "getQueryLocator", []string{"Object", "System.AccessLevel"}),
		ApexMemberID("System", "Database", "getQueryLocatorWithBinds", []string{"String", "Map", "System.AccessLevel"}),
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing batch query locator row %s", id)
		}
		if row.GladeShape == ShapeAbsent {
			t.Fatalf("batch query locator row %s has absent shape", id)
		}
	}
}

func TestMergeClosesCoreRuntimeCollectionObjectGenericShapeRows(t *testing.T) {
	docs := []SurfaceLedgerRow{
		docShapeRow("apex:System.Comparator.compare(T,T)", KindMethod),
		docShapeRow("apex:System.Enum", KindType),
		docShapeRow("apex:System.List.List<T>()", KindMethod),
		docShapeRow("apex:System.List.List<T>(List<T>)", KindMethod),
		docShapeRow("apex:System.List.equals(List)", KindMethod),
		docShapeRow("apex:System.Map.Map<ID,sObject>(List<sObject>)", KindMethod),
		docShapeRow("apex:System.Map.Map<T1,T2>()", KindMethod),
		docShapeRow("apex:System.Map.Map<T1,T2>(mapToCopy)", KindMethod),
		docShapeRow("apex:System.Map.equals(Map)", KindMethod),
		docShapeRow("apex:System.Map.remove(Key)", KindMethod),
		docShapeRow("apex:System.Object", KindType),
		docShapeRow("apex:System.Object.equals(Object)", KindMethod),
		docShapeRow("apex:System.Object.hashCode()", KindMethod),
		docShapeRow("apex:System.Object.toString()", KindMethod),
		docShapeRow("apex:System.Set.Set<T>()", KindMethod),
		docShapeRow("apex:System.Set.Set<T>(Set<T>)", KindMethod),
		docShapeRow("apex:System.Set.addAll(List<Object>)", KindMethod),
		docShapeRow("apex:System.Set.containsAll(List<Object>)", KindMethod),
		docShapeRow("apex:System.Set.equals(Set<Object>)", KindMethod),
		docShapeRow("apex:System.Set.removeAll(List<Object>)", KindMethod),
		docShapeRow("apex:System.Set.retainAll(List<Object>)", KindMethod),
	}
	ledger := Merge(docs, nil, BuildGladeSnapshot(), nil)
	byID := rowsByID(ledger.Rows)
	for _, doc := range docs {
		row, ok := byID[doc.SurfaceID]
		if !ok {
			t.Fatalf("missing merged row %s", doc.SurfaceID)
		}
		if row.GladeShape == ShapeAbsent || row.GapClass == GapMissingShape {
			t.Fatalf("%s shape = %s gap = %s", doc.SurfaceID, row.GladeShape, row.GapClass)
		}
	}
}

func docShapeRow(id, kind string) SurfaceLedgerRow {
	return RowFromDocs(SurfaceLedgerRow{
		SurfaceID: id,
		Product:   ProductApex,
		Area:      AreaRuntime,
		Kind:      kind,
	})
}

func TestMergeGladeBehaviorKeepsSupportedOverGenericUnsupported(t *testing.T) {
	got := mergeGladeBehavior(BehaviorSupported, BehaviorUnsupported)
	if got != BehaviorSupported {
		t.Fatalf("behavior = %q, want %q", got, BehaviorSupported)
	}
	got = mergeGladeBehavior(BehaviorPartial, BehaviorUnsupported)
	if got != BehaviorPartial {
		t.Fatalf("behavior = %q, want %q", got, BehaviorPartial)
	}
}

func TestBuildGladeSnapshotIncludesSummer26ReleaseAliases(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	for _, tc := range []struct {
		id   string
		kind string
	}{
		{id: "apex:System.Database.convertLead(leadsToConvert,accessLevel)", kind: KindMethod},
		{id: "apex:System.Database.convertLead(leadToConvert,accessLevel)", kind: KindMethod},
		{id: "apex:System.String.template(valueMap)", kind: KindMethod},
		{id: "apex:System.System.attachFinalizer(finalizer)", kind: KindMethod},
	} {
		row, ok := byID[tc.id]
		if !ok {
			t.Fatalf("missing Summer '26 release alias row %s", tc.id)
		}
		if row.GladeShape != ShapeSignatureKnown || row.GladeBehavior != BehaviorSupported {
			t.Fatalf("%s shape/behavior = %s/%s, want %s/%s", tc.id, row.GladeShape, row.GladeBehavior, ShapeSignatureKnown, BehaviorSupported)
		}
		if row.Kind != tc.kind {
			t.Fatalf("%s kind = %s, want %s", tc.id, row.Kind, tc.kind)
		}
		if !hasSource(row.Sources, "apex-mirror-alias") && !hasSource(row.Sources, "apex-fixture-alias") {
			t.Fatalf("%s sources = %#v, want release alias source", tc.id, row.Sources)
		}
	}
}

// --- CB17: method-family shape reconciliation tests ---

func TestCB17_FamilyRowPromotedFromExactShapedSibling(t *testing.T) {
	// A signatureless Apex method-family row (no '(' in surfaceId) becomes
	// type-known when a sibling overload with the same namespace, type, member,
	// and kind has an explicit parameter list in its surfaceId.
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)

	tests := []struct {
		familyID string
	}{
		{ApexMemberID("System", "Assert", "areEqual", nil)},
		{ApexMemberID("System", "Test", "startTest", nil)},
		{ApexMemberID("System", "JSON", "deserialize", nil)},
	}
	for _, tt := range tests {
		row, ok := byID[tt.familyID]
		if !ok {
			t.Fatalf("missing family row %s", tt.familyID)
		}
		if row.GladeShape != ShapeTypeKnown {
			t.Fatalf("%s gladeShape = %s, want %s", tt.familyID, row.GladeShape, ShapeTypeKnown)
		}
		if !hasSource(row.Sources, "standard-symbol-family") {
			t.Fatalf("%s sources = %#v, want standard-symbol-family", tt.familyID, row.Sources)
		}
	}
}

func TestCB17_FamilyReconciliationIndependentOfOrder(t *testing.T) {
	// Multiple calls to BuildGladeSnapshot produce identical shape and behavior.
	var first map[string]SurfaceLedgerRow
	familyIDs := []string{
		ApexMemberID("System", "Assert", "areEqual", nil),
		ApexMemberID("System", "Test", "startTest", nil),
		ApexMemberID("System", "JSON", "deserialize", nil),
	}
	for i := 0; i < 5; i++ {
		byID := rowsByID(BuildGladeSnapshot())
		if first == nil {
			first = byID
			continue
		}
		for _, id := range familyIDs {
			r1, ok1 := first[id]
			r2, ok2 := byID[id]
			if ok1 != ok2 {
				t.Fatalf("run %d: presence of %s changed", i, id)
			}
			if r1.GladeShape != r2.GladeShape || r1.GladeBehavior != r2.GladeBehavior {
				t.Fatalf("run %d: %s shape/behavior changed", i, id)
			}
		}
	}
}

func TestCB17_BehaviorStatesPreserved(t *testing.T) {
	// Shape reconciliation must not alter GladeBehavior byte-for-byte.
	// Rows promoted from absent to type-known keep their original behavior.
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)

	tests := []struct {
		id       string
		behavior BehaviorState
	}{
		// startTest: promoted from absent stdlib-matrix row, behavior supported
		{ApexMemberID("System", "Test", "startTest", nil), BehaviorSupported},
		// deserialize: promoted from absent stdlib-matrix row, behavior supported
		{ApexMemberID("System", "JSON", "deserialize", nil), BehaviorSupported},
		// areEqual: promoted from absent stdlib-matrix row, behavior supported
		{ApexMemberID("System", "Assert", "areEqual", nil), BehaviorSupported},
	}
	for _, tt := range tests {
		row, ok := byID[tt.id]
		if !ok {
			t.Fatalf("missing family row %s", tt.id)
		}
		if row.GladeShape != ShapeTypeKnown {
			t.Fatalf("%s gladeShape = %s, want %s", tt.id, row.GladeShape, ShapeTypeKnown)
		}
		if row.GladeBehavior != tt.behavior {
			t.Fatalf("%s behavior = %s, want %s", tt.id, row.GladeBehavior, tt.behavior)
		}
	}
}

func TestCB17_FamilyRowNotPromotedOnMismatch(t *testing.T) {
	// A family row is not promoted when the shaped sibling differs by
	// namespace, type, member, or kind.
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)

	// Each member's family row is promoted from its own shaped siblings,
	// not from a different member's siblings.
	tests := []struct {
		familyID string
	}{
		{ApexMemberID("System", "Assert", "areEqual", nil)},
		{ApexMemberID("System", "Assert", "isTrue", nil)},
	}
	for _, tt := range tests {
		row, ok := byID[tt.familyID]
		if !ok || row.GladeShape != ShapeTypeKnown {
			t.Fatalf("%s shape = %s, want type-known", tt.familyID, row.GladeShape)
		}
	}

	// A type row must not be promoted by a method sibling.
	assertTypeID := ApexTypeID("System", "Assert")
	if row, ok := byID[assertTypeID]; ok && row.GladeShape == ShapeSignatureKnown {
		t.Fatalf("Assert type row shape = %s, should not be promoted by method sibling", row.GladeShape)
	}
}

func TestCB17_FamilyRowNotPromotedFromSignaturelessOrAbsent(t *testing.T) {
	// A family row must not be promoted by another signatureless sibling
	// or by an absent sibling.
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)

	// publishAfterCommit (nil params) exists via system-fixture-alias.
	// It must not be promoted by another signatureless sibling like publish.
	pubAfterID := ApexMemberID("System", "EventBus", "publishAfterCommit", nil)
	if row, ok := byID[pubAfterID]; ok && row.GladeShape == ShapeTypeKnown {
		// It must NOT be type-known — none of its shaped siblings are
		// parameterized overloads.
		if !hasSource(row.Sources, "system-fixture-alias") {
			t.Fatalf("publishAfterCommit shape = %s from non-fixture source", row.GladeShape)
		}
	}
}

func TestCB17_JSONDeserializeBehaviorPreserved(t *testing.T) {
	// System.JSON.deserialize has behavior supported in BuildGladeSnapshot
	// (from stdlib). Shape reconciliation must not alter this.
	// The merged ledger becomes unsupported via explicit fixture evidence.
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)

	familyID := ApexMemberID("System", "JSON", "deserialize", nil)
	row, ok := byID[familyID]
	if !ok {
		t.Fatalf("missing family row %s", familyID)
	}
	if row.GladeShape != ShapeTypeKnown {
		t.Fatalf("%s gladeShape = %s, want %s", familyID, row.GladeShape, ShapeTypeKnown)
	}
	if row.GladeBehavior != BehaviorSupported {
		t.Fatalf("%s behavior = %s, want %s", familyID, row.GladeBehavior, BehaviorSupported)
	}
}

func TestBuildGladeSnapshotReportsAnswersFindSimilarSupported(t *testing.T) {
	rows := BuildGladeSnapshot()
	byID := rowsByID(rows)
	id := "apex:Answers.findSimilar(Object)"
	row, ok := byID[id]
	if !ok {
		t.Fatalf("missing Answers.findSimilar(Object) in Glade snapshot")
	}
	if row.GladeBehavior != BehaviorSupported {
		t.Fatalf("Answers.findSimilar(Object) behavior = %s, want %s", row.GladeBehavior, BehaviorSupported)
	}
	if row.GladeShape == ShapeAbsent {
		t.Fatalf("Answers.findSimilar(Object) shape is absent, want non-absent")
	}
}
