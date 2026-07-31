package surfaceledger

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
)

func TestBuildDocsSnapshotKeepsProductPathInIdentity(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "apex/apex_methods_system_object.md", "# Object Class\n\n## length()\n")
	writeDoc(t, root, "lightning-aura/ref_attr_types_object.md", "# Object\n\nAura guide text.\n")

	rows, err := BuildDocsSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	if _, ok := byID[ApexTypeID("System", "Object")]; !ok {
		t.Fatalf("missing Apex Object row in %#v", rows)
	}
	if _, ok := byID["aura:ref_attr_types_object"]; !ok {
		t.Fatalf("missing Aura Object row in %#v", rows)
	}
	if byID[ApexTypeID("System", "Object")].DocsSource != "apex/apex_methods_system_object.md" {
		t.Fatalf("apex docs source = %q", byID[ApexTypeID("System", "Object")].DocsSource)
	}
}

func TestBuildDocsSnapshotExtractsAPIVersionText(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "apex/apex_methods_system_label.md", "# Label Class\n\nAvailable in API version 60.0 and later.\n")

	rows, err := BuildDocsSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	row := rowsByID(rows)[ApexTypeID("System", "Label")]
	if row.APIVersion != "60.0" {
		t.Fatalf("api version = %q, want 60.0", row.APIVersion)
	}
}

func TestBuildDocsSnapshotClassifiesDataReferenceDocs(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "object-reference/sforce_api_objects_asyncapexjob.md", "# AsyncApexJob\n\n### Status\n")

	rows, err := BuildDocsSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	if row, ok := byID[DataObjectID("AsyncApexJob")]; !ok || row.Product != ProductDataRef || row.Area != AreaData {
		t.Fatalf("AsyncApexJob object row = %#v, ok=%v", row, ok)
	}
	if row, ok := byID[DataFieldID("AsyncApexJob", "Status")]; !ok || row.Kind != KindField {
		t.Fatalf("AsyncApexJob.Status field row = %#v, ok=%v", row, ok)
	}
}

func TestBuildDocsSnapshotKeepsEventLogFileEventTypeRowsUnderEventLogFile(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "object-reference/sforce_api_objects_eventlogfile_apitotalusage.md", "# API Total Usage\n\n### API_VERSION\n")

	rows, err := BuildDocsSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	if _, ok := byID[DataObjectID("API")]; ok {
		t.Fatalf("unexpected fake API object row: %#v", byID[DataObjectID("API")])
	}
	if row, ok := byID[DataObjectID("EventLogFile")]; !ok || row.Product != ProductDataRef {
		t.Fatalf("EventLogFile row = %#v, ok=%v", row, ok)
	}
	if row, ok := byID[DataFieldID("EventLogFile", "API_VERSION")]; !ok || row.Kind != KindField {
		t.Fatalf("EventLogFile.API_VERSION field row = %#v, ok=%v", row, ok)
	}
}

func TestBuildDocsSnapshotUsesDataReferenceObjectStemAndSkipsGuideRows(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "object-reference/access_for_fields.md", "# API Field Properties\n\nReference text.\n")
	writeDoc(t, root, "object-reference/sforce_api_objects_businessprocess.md", "# Business Process\n\n### Id\n")
	writeDoc(t, root, "object-reference/sforce_api_objects_custom_object__c.md", "# Custom Object \\_\\_c \\_\\_c\n")
	writeDoc(t, root, "object-reference/sforce_api_objects_customobject__feed.md", "# Custom Object\\_\\_Feed\n")
	writeDoc(t, root, "field-reference/salesforce_field_reference_Airport__Share.md", "# Airport\\_\\_Share\n\n### RowCause\n")

	rows, err := BuildDocsSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	if _, ok := byID[DataObjectID("API")]; ok {
		t.Fatalf("unexpected guide title row: %#v", byID[DataObjectID("API")])
	}
	if _, ok := byID[DataObjectID("Airport__Share")]; ok {
		t.Fatalf("unexpected field-reference top-level object row: %#v", byID[DataObjectID("Airport__Share")])
	}
	if _, ok := byID[DataObjectID("CustomObject__Feed")]; ok {
		t.Fatalf("unexpected custom object template row: %#v", byID[DataObjectID("CustomObject__Feed")])
	}
	if _, ok := byID[DataObjectID("CustomObject__c__c")]; ok {
		t.Fatalf("unexpected custom object suffix template row: %#v", byID[DataObjectID("CustomObject__c__c")])
	}
	if row, ok := byID[DataObjectID("BusinessProcess")]; !ok || row.Kind != KindType || row.GladeShape != ShapeAbsent || row.GladeBehavior != BehaviorNone {
		t.Fatalf("BusinessProcess object row = %#v, ok=%v", row, ok)
	}
	if row, ok := byID[DataFieldID("BusinessProcess", "Id")]; !ok || row.Kind != KindField {
		t.Fatalf("BusinessProcess.Id field row = %#v, ok=%v", row, ok)
	}
}

func TestRowsFromDocsInventoryIdentifiesServerAndUIDTOFamiliesWithoutClosingDocsOnlyRows(t *testing.T) {
	rows := RowsFromDocsInventory(apexdocs.Inventory{
		Documents: []apexdocs.Document{{
			SourcePath: "connect-rest-api/responses_feed_items.md",
			Name:       "FeedItemsResponse",
			Members: []apexdocs.Member{{
				Kind: "property",
				Name: "items",
			}},
		}, {
			SourcePath: "tooling-api/intro_rest_resources_testing_runner_sync.md",
			Name:       "Run",
			Members: []apexdocs.Member{{
				Kind: "property",
				Name: "status",
			}},
		}, {
			SourcePath: "soap-api/sforce_api_calls_describesobjects_describesobjectresult.md",
			Name:       "DescribeSObjectResult",
			Members: []apexdocs.Member{{
				Kind: "property",
				Name: "fields",
			}},
		}, {
			SourcePath: "metadata-api/meta_deployresult.md",
			Name:       "DeployResult",
			Members: []apexdocs.Member{{
				Kind: "property",
				Name: "done",
			}},
		}, {
			SourcePath: "metadata-api/meta_retrieveresult.md",
			Name:       "RetrieveResult",
			Members: []apexdocs.Member{{
				Kind: "property",
				Name: "zipFile",
			}},
		}, {
			SourcePath: "ui-api/responses_record_ui.md",
			Name:       "RecordUiResponse",
			Members: []apexdocs.Member{{
				Kind: "property",
				Name: "records",
			}},
		}, {
			SourcePath: "commerce-cli-reference/commerce_cli_commands.md",
			Name:       "CommerceCliCommand",
			Members: []apexdocs.Member{{
				Kind: "property",
				Name: "flags",
			}},
		}},
	})
	ledger := Merge(rows, nil, nil, nil)
	byID := rowsByID(ledger.Rows)

	for _, tc := range []struct {
		id     string
		family string
		area   string
	}{
		{id: "connect-rest-api:responses_feed_items.items", family: "connect-rest-api", area: AreaServer},
		{id: ToolingFieldID("Run", "status"), family: "tooling-api", area: AreaServer},
		{id: "soap-api:sforce_api_calls_describesobjects_describesobjectresult.fields", family: "soap-api", area: AreaServer},
		{id: "metadata-api:meta_deployresult.done", family: "metadata-api", area: AreaServer},
		{id: "metadata-api:meta_retrieveresult.zipFile", family: "metadata-api", area: AreaServer},
		{id: "ui-api:responses_record_ui.records", family: "ui-api", area: AreaUI},
		{id: "commerce-cli-reference:commerce_cli_commands.flags", family: "commerce-cli-reference", area: AreaServer},
	} {
		row, ok := byID[tc.id]
		if !ok {
			t.Fatalf("missing row %s in %#v", tc.id, ledger.Rows)
		}
		if row.SalesforceSurfaceFamily != tc.family || row.Area != tc.area {
			t.Fatalf("%s family/area = %s/%s, want %s/%s", tc.id, row.SalesforceSurfaceFamily, row.Area, tc.family, tc.area)
		}
		if row.Bucket != BucketGap || row.GapClass != GapMissingShape || row.GladeBehavior != BehaviorNone || row.GladeShape != ShapeAbsent {
			t.Fatalf("%s bucket/gap/behavior/shape = %s/%s/%s/%s, want gap/missing-shape/none/absent", tc.id, row.Bucket, row.GapClass, row.GladeBehavior, row.GladeShape)
		}
	}
}

func TestRowsFromDocsInventoryIdentifiesServiceConnectorWithoutClosingDocsOnlyRows(t *testing.T) {
	rows := RowsFromDocsInventory(apexdocs.Inventory{
		Documents: []apexdocs.Document{{
			SourcePath: "service-connector-api-reference/service_connector_methods.md",
			Name:       "ServiceConnector",
			Members: []apexdocs.Member{{
				Kind: "method",
				Name: "invoke",
			}},
		}},
	})
	ledger := Merge(rows, nil, nil, nil)
	byID := rowsByID(ledger.Rows)
	row, ok := byID["service-connector-api-reference:service_connector_methods.invoke"]
	if !ok {
		t.Fatalf("missing service connector row in %#v", ledger.Rows)
	}
	if row.SalesforceSurfaceFamily != "service-connector-api-reference" || row.Area != AreaServer {
		t.Fatalf("family/area = %s/%s", row.SalesforceSurfaceFamily, row.Area)
	}
	if row.Bucket != BucketGap || row.GapClass != GapMissingShape || row.GladeBehavior != BehaviorNone || row.GladeShape != ShapeAbsent {
		t.Fatalf("bucket/gap/behavior/shape = %s/%s/%s/%s, want gap/missing-shape/none/absent", row.Bucket, row.GapClass, row.GladeBehavior, row.GladeShape)
	}
}

func TestMergeClassifiesServiceConnectorExplicitUnsupportedOnlyWithPolicyEvidence(t *testing.T) {
	id := "service-connector-api-reference:service_connector_methods.invoke"
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{
			SurfaceID: id,
			Product:   ProductServiceConnectorAPIRef,
			Area:      AreaServer,
			Kind:      KindMethod,
		})},
		nil,
		nil,
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{
			SurfaceID:     id,
			Product:       ProductServiceConnectorAPIRef,
			Area:          AreaServer,
			Kind:          KindMethod,
			GladeBehavior: BehaviorUnsupported,
			Evidence:      EvidenceFixture,
			Sources:       []string{"service-connector-local-impossible-policy"},
			Notes:         "Service Connector API invokes hosted Service Cloud connector services outside the local Glade runtime.",
		})},
	)
	row := rowsByID(ledger.Rows)[id]
	if row.Bucket != BucketExplicitUnsupported || row.GladeBehavior != BehaviorUnsupported || row.Evidence != EvidenceFixture {
		t.Fatalf("bucket/behavior/evidence = %s/%s/%s, want explicitUnsupported/unsupported/fixture", row.Bucket, row.GladeBehavior, row.Evidence)
	}
}

func TestBuildDocsSnapshotSkipsApexReleaseNotes(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "apex/apex_releasenotes.md", "# Apex Release Notes\n\n## Insert\n")

	rows, err := BuildDocsSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(rows); got != 0 {
		t.Fatalf("release note rows = %d, want 0: %#v", got, rows)
	}
}

func TestBuildDocsSnapshotUsesApexSignatureParameterTypes(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "apex/apex_class_System_FeatureManagement.md", "# FeatureManagement Class\n\n### checkPackageBooleanValue(apiName)\n\n#### Signature\n\n`public static Boolean checkPackageBooleanValue(String\napiName)`\n")
	writeDoc(t, root, "apex/apex_methods_system_database.md", "# Database Class\n\n### executeBatch(batchClassObject)\n\n#### Signature\n\n`public static ID executeBatch(Object\nbatchClassObject)`\n\n### executeBatch(batchClassObject, scope)\n\n#### Signature\n\n`public static ID executeBatch(Object\nbatchClassObject, Integer scope)`\n")

	rows, err := BuildDocsSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	if _, ok := byID[ApexMemberID("System", "FeatureManagement", "checkPackageBooleanValue", []string{"String"})]; !ok {
		t.Fatalf("FeatureManagement docs row did not use typed parameter list: %#v", rows)
	}
	if _, ok := byID[ApexMemberID("System", "Database", "executeBatch", []string{"Object", "Integer"})]; !ok {
		t.Fatalf("Database.executeBatch docs row did not use typed parameter list: %#v", rows)
	}
	if _, ok := byID[ApexMemberID("System", "Database", "executeBatch", []string{"Object"})]; !ok {
		t.Fatalf("Database.executeBatch single-argument docs row did not use typed parameter list: %#v", rows)
	}
}

func TestDocsSourceStemStripsHiddenFormatMarks(t *testing.T) {
	if got := sourceStem("ui-api/picklistAtrributes\u200bValueType.md"); got != "picklistAtrributesValueType" {
		t.Fatalf("source stem = %q", got)
	}
}

func TestBuildDocsSnapshotSkipsApexGuidePages(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "apex/apex_dml_section.md", "# Apex DML Operations\n\n## Apex DML Statements\n\n### Insert Statement\n\n#### Syntax\n\n`insert sObject`\n")
	writeDoc(t, root, "apex/apex_appendices.md", "# Appendices\n\n- **[Reserved Keywords](./apex_reserved_words.md)**\n")
	writeDoc(t, root, "apex/apex_qs_conventions.md", "# Documentation Typographical Conventions\n\n| Convention | Description |\n| --- | --- |\n")
	writeDoc(t, root, "apex/apex_ref_guide.md", "# Apex Reference Guide\n\n- **[System Namespace](./apex_namespace_System.md)**\n")
	writeDoc(t, root, "apex/apex_namespace_Auth.md", "# Auth Namespace\n\n- **[AuthConfiguration Class](./apex_class_Auth_AuthConfiguration.md)**\n")
	writeDoc(t, root, "apex/apex_releasenotes.md", "# Apex Release Notes\n\nFor new and changed Apex classes, methods, exceptions and interfaces, see Apex: New and Changed Items.\n")
	writeDoc(t, root, "apex/apex_shopping_cart_example.md", "# Shipping Invoice Example\n\nThis appendix provides an example of an Apex application.\n")
	writeDoc(t, root, "apex/versioned_behavior_changes.md", "# Apex Versioned Behavior Changes\n\n## Version 67.0\n\nDatabase Operations in User Mode by Default\n")
	writeDoc(t, root, "apex/apex_class_System_Label.md", "# Label Class\n\n## Namespace\n\nSystem\n\n### get(namespace, name)\n\n#### Signature\n\n`public static String get(String namespace, String name)`\n")

	rows, err := BuildDocsSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	for _, id := range []string{
		ApexTypeID("System", "Apex"),
		ApexMemberID("System", "Apex", "Insert", nil),
		ApexTypeID("System", "Appendices"),
		ApexTypeID("System", "Documentation"),
		ApexTypeID("System", "Shipping"),
		ApexTypeID("Auth", "Auth"),
	} {
		if _, ok := byID[id]; ok {
			t.Fatalf("guide page inferred false Apex surface %s in %#v", id, rows)
		}
	}
	if _, ok := byID[ApexMemberID("System", "Label", "get", []string{"String", "String"})]; !ok {
		t.Fatalf("real Apex class docs were filtered out: %#v", rows)
	}
}

func TestBuildDocsSnapshotKeepsFSCCashFlowPagesOutOfSystem(t *testing.T) {
	root := t.TempDir()
	fsccashflowPages := map[string]string{
		"apex/apex_fsccashflow_calculateincomeexpensesummary.md": "CalculateIncomeExpenseSummary",
		"apex/apex_fsccashflow_checkcrudonexpense.md":            "CheckCrudOnExpense",
		"apex/apex_fsccashflow_checkcrudonincome.md":             "CheckCrudOnIncome",
		"apex/apex_fsccashflow_checkreadaccess.md":               "CheckReadAccess",
		"apex/apex_fsccashflow_getdurationdaterange.md":          "GetDurationDateRange",
		"apex/apex_fsccashflow_getpartyexpensefrequencylabel.md": "GetPartyExpenseFrequencyLabel",
		"apex/apex_fsccashflow_GetPartyExpenseStatusLabel.md":    "GetPartyExpenseStatusLabel",
		"apex/apex_fsccashflow_getpartyexpensetypelabel.md":      "GetPartyExpenseTypeLabel",
		"apex/apex_fsccashflow_getpartyincomefrequencylabel.md":  "GetPartyIncomeFrequencyLabel",
		"apex/apex_fsccashflow_getpartyincomestatuslabel.md":     "GetPartyIncomeStatusLabel",
		"apex/apex_fsccashflow_getpartyincometypelabel.md":       "GetPartyIncomeTypeLabel",
		"apex/apex_fsccashflow_handleupserterror.md":             "HandleUpsertError",
	}
	for path, title := range fsccashflowPages {
		writeDoc(t, root, path, "# "+title+"\n\n## Signature\n\n`call(String action, Map<String, Object> args`\n")
	}
	writeDoc(t, root, "apex/apex_fsccashflowutil_methods.md", "# FSCCashFlowUtil Methods\n\n- **[GetPartyIncomeFrequencyLabel](./apex_fsccashflow_getpartyincomefrequencylabel.md)**\n")

	rows, err := BuildDocsSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	for _, id := range []string{
		ApexTypeID("System", "FSCCashFlowUtil"),
	} {
		if _, ok := byID[id]; ok {
			t.Fatalf("FSC cash flow docs inferred false System surface %s in %#v", id, rows)
		}
	}
	if _, ok := byID[ApexTypeID("fsccashflow", "FSCCashFlowUtil")]; !ok {
		t.Fatalf("FSC cash flow docs did not keep namespaced FSCCashFlowUtil row in %#v", rows)
	}
	for _, title := range fsccashflowPages {
		systemID := ApexTypeID("System", title)
		if _, ok := byID[systemID]; ok {
			t.Fatalf("FSC cash flow docs inferred false System surface %s in %#v", systemID, rows)
		}
		namespacedID := ApexTypeID("fsccashflow", title)
		if _, ok := byID[namespacedID]; !ok {
			t.Fatalf("FSC cash flow docs did not keep namespaced surface %s in %#v", namespacedID, rows)
		}
	}
}

func TestBuildDocsSnapshotSkipsApexReferenceSummaryPages(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "apex/apex_Reports_exceptions.md", "# Reports Exceptions\n\nThe `Reports` namespace contains exception classes.\n")
	writeDoc(t, root, "apex/apex_reserved_words.md", "# Reserved Keywords\n\nThese words can be used only as keywords.\n")
	writeDoc(t, root, "apex/apex_class_System_Label.md", "# Label Class\n\n## Namespace\n\nSystem\n\n### get(namespace, name)\n\n#### Signature\n\n`public static String get(String namespace, String name)`\n")

	rows, err := BuildDocsSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	for _, id := range []string{
		ApexTypeID("System", "Reports"),
		ApexTypeID("System", "Reserved"),
	} {
		if _, ok := byID[id]; ok {
			t.Fatalf("reference summary page inferred false Apex surface %s in %#v", id, rows)
		}
	}
	if _, ok := byID[ApexMemberID("System", "Label", "get", []string{"String", "String"})]; !ok {
		t.Fatalf("real Apex system method docs were filtered out: %#v", rows)
	}
}

func TestBuildDocsSnapshotSkipsGeneratedCustomObjectMethodPages(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "apex/apex_methods_system_custom_metadata_types.md", "# Custom Metadata Type Methods\n\n## getAll()\n\n### Signature\n\n`public Map<String, CustomMetadataType__mdt> getAll()`\n")
	writeDoc(t, root, "apex/apex_methods_system_custom_settings.md", "# Custom Settings Methods\n\n## getInstance(name)\n\n### Signature\n\n`public CustomSetting__c getInstance(String name)`\n")
	writeDoc(t, root, "apex/apex_methods_system_string.md", "# String Class\n\n## Namespace\n\nSystem\n\n### contains(substring)\n\n#### Signature\n\n`public Boolean contains(String substring)`\n")

	rows, err := BuildDocsSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	byID := rowsByID(rows)
	for _, id := range []string{
		ApexTypeID("System", "Custom"),
		ApexMemberID("System", "Custom", "getAll", []string{}),
		ApexMemberID("System", "Custom", "getInstance", []string{"String"}),
	} {
		if _, ok := byID[id]; ok {
			t.Fatalf("generated custom-object methods inferred false Apex surface %s in %#v", id, rows)
		}
	}
	if _, ok := byID[ApexMemberID("System", "String", "contains", []string{"String"})]; !ok {
		t.Fatalf("real Apex system method docs were filtered out: %#v", rows)
	}
}

func TestRowsFromDocsInventoryCanonicalizesApexNamespace(t *testing.T) {
	rows := RowsFromDocsInventory(apexdocs.Inventory{
		Documents: []apexdocs.Document{{
			SourcePath: "apex/apex_interface_database_batchable.md",
			Kind:       "interface",
			Namespace:  "database",
			Name:       "Batchable",
			Members: []apexdocs.Member{{
				Kind:      "method",
				Name:      "execute",
				Signature: "execute(Database.BatchableContext, List<Object>)",
			}},
		}},
	})

	byID := rowsByID(rows)
	if _, ok := byID[ApexTypeID("Database", "Batchable")]; !ok {
		t.Fatalf("docs type row did not canonicalize Database namespace: %#v", rows)
	}
	if _, ok := byID[ApexMemberID("Database", "Batchable", "execute", []string{"Database.BatchableContext", "List<Object>"})]; !ok {
		t.Fatalf("docs member row did not canonicalize Database namespace: %#v", rows)
	}
}

func TestRowsFromDocsInventoryCanonicalizesSystemDatabaseResultDTOs(t *testing.T) {
	rows := RowsFromDocsInventory(apexdocs.Inventory{
		Documents: []apexdocs.Document{{
			SourcePath: "apex/apex_class_System_DeleteResult.md",
			Kind:       "class",
			Namespace:  "System",
			Name:       "DeleteResult",
			Members: []apexdocs.Member{{
				Kind:      "method",
				Name:      "getErrors",
				Signature: "getErrors()",
			}},
		}, {
			SourcePath: "apex/apex_class_System_EmptyRecycleBinResult.md",
			Kind:       "class",
			Namespace:  "System",
			Name:       "EmptyRecycleBinResult",
			Members: []apexdocs.Member{{
				Kind:      "method",
				Name:      "isSuccess",
				Signature: "isSuccess()",
			}},
		}, {
			SourcePath: "apex/apex_class_System_Error.md",
			Kind:       "class",
			Namespace:  "System",
			Name:       "Error",
			Members: []apexdocs.Member{{
				Kind:      "method",
				Name:      "getStatusCode",
				Signature: "getStatusCode()",
			}},
		}},
	})

	byID := rowsByID(rows)
	for _, id := range []string{
		ApexTypeID("Database", "DeleteResult"),
		ApexMemberID("Database", "DeleteResult", "getErrors", []string{}),
		ApexTypeID("Database", "EmptyRecycleBinResult"),
		ApexMemberID("Database", "EmptyRecycleBinResult", "isSuccess", []string{}),
		ApexTypeID("Database", "Error"),
		ApexMemberID("Database", "Error", "getStatusCode", []string{}),
	} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("docs row did not canonicalize System result DTO to %s: %#v", id, rows)
		}
	}
}

func TestRowsFromDocsInventoryUsesSourcePathNamespaces(t *testing.T) {
	rows := RowsFromDocsInventory(apexdocs.Inventory{
		Documents: []apexdocs.Document{{
			SourcePath: "apex/apex_canvas_ApplicationContext_methods.md",
			Kind:       "class",
			Name:       "ApplicationContext",
			Members: []apexdocs.Member{{
				Kind:      "method",
				Name:      "getCanvasUrl",
				Signature: "public String getCanvasUrl()",
			}},
		}, {
			SourcePath: "apex/apex_System_Cases_generateThreadingMessageId.md",
			Kind:       "class",
			Name:       "Cases",
			Members: []apexdocs.Member{{
				Kind:      "method",
				Name:      "generateThreadingMessageId",
				Signature: "public static String generateThreadingMessageId(Id caseId)",
			}},
		}, {
			SourcePath: "apex/apex_System_EmailMessages_getRecordIdFromEmail.md",
			Kind:       "class",
			Name:       "EmailMessages",
			Members: []apexdocs.Member{{
				Kind:      "method",
				Name:      "getRecordIdFromEmail",
				Signature: "public static Id getRecordIdFromEmail(String subject, String plainTextBody, String htmlBody)",
			}},
		}},
	})

	byID := rowsByID(rows)
	for _, id := range []string{
		ApexTypeID("Canvas", "ApplicationContext"),
		ApexMemberID("Canvas", "ApplicationContext", "getCanvasUrl", []string{}),
		ApexMemberID("System", "Cases", "generateThreadingMessageId", []string{"Id"}),
		ApexMemberID("System", "EmailMessages", "getRecordIdFromEmail", []string{"String", "String", "String"}),
	} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing canonical docs row %s in %#v", id, rows)
		}
	}
	for _, id := range []string{
		ApexTypeID("System", "ApplicationContext"),
		ApexMemberID("System", "", "getCanvasUrl", []string{}),
		ApexMemberID("System", "", "generateThreadingMessageId", []string{"caseId"}),
	} {
		if _, ok := byID[id]; ok {
			t.Fatalf("kept false System docs row %s in %#v", id, rows)
		}
	}
}

func TestRowsFromDocsInventoryUsesConstructorSignatures(t *testing.T) {
	rows := RowsFromDocsInventory(apexdocs.Inventory{
		Documents: []apexdocs.Document{{
			SourcePath: "apex/apex_System_PageReference_ctor_2.md",
			Kind:       "class",
			Namespace:  "System",
			Name:       "PageReference",
			Members: []apexdocs.Member{{
				Kind:      "constructor",
				Name:      "PageReference",
				Signature: "public PageReference(String partialURL)",
			}},
		}},
	})

	byID := rowsByID(rows)
	want := ApexMemberID("System", "PageReference", "PageReference", []string{"String"})
	if _, ok := byID[want]; !ok {
		t.Fatalf("missing constructor docs row %s in %#v", want, rows)
	}
	if bad := ApexMemberID("System", "PageReference", "PageReference", []string{"partialURL"}); byID[bad].SurfaceID != "" {
		t.Fatalf("kept false constructor docs row %s in %#v", bad, rows)
	}
}

func TestRowsFromDocsInventoryUsesDocsContractTypes(t *testing.T) {
	rows := RowsFromDocsInventory(apexdocs.Inventory{
		Documents: []apexdocs.Document{{
			SourcePath: "apex/apex_connectapi_output_ManagedContentVersionCollection.md",
			Kind:       "output",
			Namespace:  "ConnectApi",
			Name:       "ManagedContentVersionCollection",
			Members: []apexdocs.Member{{
				Kind:         "property",
				Name:         "items",
				Signature:    "items",
				PropertyType: "List<ConnectApi.ManagedContentVersion>",
			}, {
				Kind:       "method",
				Name:       "equals",
				Signature:  "public Boolean equals(Object obj)",
				ReturnType: "Boolean",
				Parameters: []string{"Object"},
			}},
		}},
	})

	byID := rowsByID(rows)
	propertyID := ApexMemberID("ConnectApi", "ManagedContentVersionCollection", "items", nil)
	if got := byID[propertyID].DocsReturnType; got != "List<ConnectApi.ManagedContentVersion>" {
		t.Fatalf("docs property return type = %q", got)
	}
	methodID := ApexMemberID("ConnectApi", "ManagedContentVersionCollection", "equals", []string{"Object"})
	if got := byID[methodID].DocsReturnType; got != "Boolean" {
		t.Fatalf("docs method return type = %q", got)
	}
	if got := byID[methodID].DocsParameters; len(got) != 1 || got[0] != "Object" {
		t.Fatalf("docs method parameters = %#v", got)
	}
}

func TestRowsFromDocsInventoryInfersApexFileIdentities(t *testing.T) {
	rows := RowsFromDocsInventory(apexdocs.Inventory{
		Documents: []apexdocs.Document{{
			SourcePath: "apex/apex_Messaging_PushNotification_methods.md",
			Kind:       "document",
			Name:       "PushNotification",
			Title:      "PushNotification Methods",
		}, {
			SourcePath: "apex/apex_industriesNlpSvc_NlpResponse_properties.md",
			Kind:       "document",
			Name:       "NlpResponse",
			Title:      "NlpResponse Properties",
		}, {
			SourcePath: "apex/apex_commercepayments_PostAuthorizationRequest_properties.md",
			Kind:       "document",
			Name:       "PostAuthorizationRequest",
			Title:      "PostAuthorizationRequest Properties",
		}, {
			SourcePath: "apex/apex_System_PageReference_getContent.md",
			Kind:       "document",
			Name:       "getContent()",
			Title:      "getContent()",
		}, {
			SourcePath: "apex/apex_System_Cases_generateThreadingMessageId.md",
			Kind:       "document",
			Name:       "generateThreadingMessageId(caseId)",
			Title:      "generateThreadingMessageId(caseId)",
		}},
	})

	byID := rowsByID(rows)
	for _, id := range []string{
		ApexTypeID("Messaging", "PushNotification"),
		ApexTypeID("industriesNlpSvc", "NlpResponse"),
		ApexTypeID("commercepayments", "PostAuthorizationRequest"),
		ApexMemberID("System", "PageReference", "getContent", []string{}),
		ApexMemberID("System", "Cases", "generateThreadingMessageId", []string{"caseId"}),
	} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing inferred docs row %s in %#v", id, rows)
		}
	}
	for _, id := range []string{
		ApexTypeID("System", "PushNotification"),
		ApexTypeID("System", "NlpResponse"),
		ApexTypeID("System", "PostAuthorizationRequest"),
		ApexTypeID("System", "getContent()"),
		ApexTypeID("System", "generateThreadingMessageId(caseId)"),
	} {
		if _, ok := byID[id]; ok {
			t.Fatalf("kept false docs row %s in %#v", id, rows)
		}
	}
}

func TestRowsFromDocsInventorySkipsApexPreviewBulletSignatures(t *testing.T) {
	rows := RowsFromDocsInventory(apexdocs.Inventory{
		Documents: []apexdocs.Document{{
			SourcePath: "apex/apex_class_System_Security.md",
			Kind:       "class",
			Namespace:  "System",
			Name:       "Security",
			Members: []apexdocs.Member{{
				Kind:      "method",
				Name:      "stripInaccessible",
				Signature: "stripInaccessible(accessCheckType, sourceRecords, enforceRootObjectCRUD, permissionSetId)(Developer Preview)",
			}, {
				Kind:      "method",
				Name:      "stripInaccessible",
				Signature: "public static System.SObjectAccessDecision stripInaccessible(System.AccessType accessCheckType, List<SObject> sourceRecords, Boolean enforceRootObjectCRUD, Id permissionSetId)",
			}},
		}, {
			SourcePath: "apex/apex_class_System_UserManagement.md",
			Kind:       "class",
			Namespace:  "System",
			Name:       "UserManagement",
			Members: []apexdocs.Member{{
				Kind:      "method",
				Name:      "initVerificationMethod",
				Signature: "initVerificationMethod(method, actionName, extras)",
			}, {
				Kind:      "method",
				Name:      "initVerificationMethod",
				Signature: "public static String initVerificationMethod(Auth.VerificationMethod method, String actionName, Map<String,String> extras)",
			}},
		}, {
			SourcePath: "apex/apex_methods_system_test.md",
			Kind:       "class",
			Namespace:  "System",
			Name:       "Test",
			Members: []apexdocs.Member{{
				Kind:      "method",
				Name:      "testNotificationActionHandler",
				Signature: "testNotificationActionHandler (handler, actionableNotification)",
			}, {
				Kind:      "method",
				Name:      "testNotificationActionHandler",
				Signature: "public static Messaging.ActionResult testNotificationActionHandler(Messaging.NotificationActionHandler handler, Messaging.ActionableNotification actionableNotification)",
			}},
		}, {
			SourcePath: "apex/apex_methods_system_string.md",
			Kind:       "class",
			Namespace:  "System",
			Name:       "String",
			Members: []apexdocs.Member{{
				Kind:      "method",
				Name:      "template",
				Signature: "public String template(Map<String, Object> valueMap)",
			}},
		}, {
			SourcePath: "apex/apex_class_Auth_LoginDiscoveryHandler.md",
			Kind:       "interface",
			Namespace:  "Auth",
			Name:       "LoginDiscoveryHandler",
			Members: []apexdocs.Member{{
				Kind:      "method",
				Name:      "login",
				Signature: "public PageReference login(String identifier, String startUrl, Map<String,String>requestAttributes)",
			}},
		}},
	})

	byID := rowsByID(rows)
	good := ApexMemberID("System", "Security", "stripInaccessible", []string{"AccessType", "List<Object>", "Boolean", "Id"})
	if _, ok := byID[good]; !ok {
		t.Fatalf("missing canonical Security row %s in %#v", good, rows)
	}
	bad := ApexMemberID("System", "Security", "stripInaccessible", []string{"accessCheckType", "sourceRecords", "enforceRootObjectCRUD", "permissionSetId)(Developer"})
	if _, ok := byID[bad]; ok {
		t.Fatalf("kept preview bullet signature row %s in %#v", bad, rows)
	}
	for _, id := range []string{
		ApexMemberID("System", "UserManagement", "initVerificationMethod", []string{"Auth.VerificationMethod", "String", "Map<String,String>"}),
		ApexMemberID("System", "Test", "testNotificationActionHandler", []string{"Messaging.NotificationActionHandler", "Messaging.ActionableNotification"}),
		ApexMemberID("System", "String", "template", []string{"Map<String,Object>"}),
		ApexMemberID("Auth", "LoginDiscoveryHandler", "login", []string{"String", "String", "Map<String,String>"}),
	} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing canonical heading replacement row %s in %#v", id, rows)
		}
	}
	for _, id := range []string{
		ApexMemberID("System", "UserManagement", "initVerificationMethod", []string{"method", "actionName", "extras"}),
		ApexMemberID("System", "Test", "testNotificationActionHandler", []string{"handler", "actionableNotification"}),
	} {
		if _, ok := byID[id]; ok {
			t.Fatalf("kept heading-only docs row %s in %#v", id, rows)
		}
	}
}

func TestRowsFromDocsInventorySkipsApexHeadingAliasesWithoutTypedDeclarations(t *testing.T) {
	rows := RowsFromDocsInventory(apexdocs.Inventory{
		Documents: []apexdocs.Document{{
			SourcePath: "apex/apex_interface_mock_MyAdapter.md",
			Kind:       "interface",
			Namespace:  "mock",
			Name:       "MyAdapter",
			Members: []apexdocs.Member{
				{
					Kind:      "member",
					Name:      "Response",
					Signature: "Response Mappings for Header Attributes",
					Section:   "Tax Mappings",
				},
				{
					Kind:       "method",
					Name:       "processRequest",
					Signature:  "public String processRequest(MockContext ctx)",
					ReturnType: "String",
					Parameters: []string{"MockContext"},
				},
				{
					Kind:         "property",
					Name:         "isValid",
					Signature:    "public Boolean isValid {get; set;}",
					PropertyType: "Boolean",
				},
			},
		}, {
			SourcePath: "apex/apex_class_mock_InnerType.md",
			Kind:       "class",
			Namespace:  "mock",
			Name:       "InnerType",
			Members: []apexdocs.Member{{
				Kind:       "method",
				Name:       "valueOf",
				Signature:  "public static InnerType valueOf(String s)",
				ReturnType: "InnerType",
				Parameters: []string{"String"},
			}},
		}},
	})
	byID := rowsByID(rows)

	// Heading alias must be excluded.
	bad := ApexMemberID("mock", "MyAdapter", "Response", nil)
	if _, ok := byID[bad]; ok {
		t.Fatalf("kept heading-alias member row %s", bad)
	}

	// Real method must remain.
	goodMethod := ApexMemberID("mock", "MyAdapter", "processRequest", []string{"MockContext"})
	if _, ok := byID[goodMethod]; !ok {
		t.Fatalf("missing real method row %s", goodMethod)
	}

	// Real property must remain.
	goodProp := ApexMemberID("mock", "MyAdapter", "isValid", nil)
	if row, ok := byID[goodProp]; !ok || row.Kind != KindProperty {
		t.Fatalf("missing or misclassified property row %s: %#v", goodProp, row)
	}

	// Standalone inner type must remain.
	goodType := ApexTypeID("mock", "InnerType")
	if _, ok := byID[goodType]; !ok {
		t.Fatalf("missing standalone inner type row %s", goodType)
	}

	// Inner type's method must remain.
	goodInnerMember := ApexMemberID("mock", "InnerType", "valueOf", []string{"String"})
	if _, ok := byID[goodInnerMember]; !ok {
		t.Fatalf("missing inner type member row %s", goodInnerMember)
	}
}

func TestSortRowsPreservesDuplicateSurfaceOrder(t *testing.T) {
	rows := make([]SurfaceLedgerRow, 0, 100)
	var want []string
	for i := 0; i < 100; i++ {
		if i%3 == 0 {
			signature := fmt.Sprintf("overload-%03d", i)
			rows = append(rows, SurfaceLedgerRow{
				SurfaceID: "apex:mock.Adapter.overload",
				Signature: signature,
			})
			want = append(want, signature)
			continue
		}
		rows = append(rows, SurfaceLedgerRow{
			SurfaceID: fmt.Sprintf("apex:mock.Type%03d", 100-i),
		})
	}

	sortRows(rows)

	var got []string
	for _, row := range rows {
		if row.SurfaceID == "apex:mock.Adapter.overload" {
			got = append(got, row.Signature)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("duplicate surface order changed:\ngot  %v\nwant %v", got, want)
	}
}

func writeDoc(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rowsByID(rows []SurfaceLedgerRow) map[string]SurfaceLedgerRow {
	out := map[string]SurfaceLedgerRow{}
	for _, row := range rows {
		out[row.SurfaceID] = row
	}
	return out
}
