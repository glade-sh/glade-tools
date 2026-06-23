package corpuscheck

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCheckWritesClassifiedTSVs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "alpha")
	writeProject(t, root, "beta")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte(`#!/bin/sh
case "$3" in
  */alpha) printf '{"diagnostics":[{"code":"APEXPARSE001","message":"Unexpected token","file":"force-app/main/default/classes/A.cls","line":3,"column":4},{"code":"GLADETYPE001","message":"duplicate declaration","file":"A.cls"}]}' ;;
  */beta) printf '{"diagnostics":[{"code":"GLADEPERF001","message":"slow check","file":"B.cls"},{"code":"GLADESEMA009","message":"No overload matches return-type mismatch contract","file":"B.cls"}]}' ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")

	report, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["source-parse-error"] != 1 || report.Counts["project-discovery-duplicate"] != 1 || report.Counts["performance-advisory"] != 1 || report.Counts["docs-contract-mismatch"] != 1 {
		t.Fatalf("counts = %#v", report.Counts)
	}
	for _, name := range []string{"summary.tsv", "diagnostics.tsv", "by_code.tsv", "by_project_code.tsv", "by_stem.tsv", "classified.tsv"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
	diagnostics, err := os.ReadFile(filepath.Join(out, "diagnostics.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(diagnostics), "APEXPARSE001") || !strings.Contains(string(diagnostics), "duplicate declaration") {
		t.Fatalf("diagnostics.tsv did not preserve raw diagnostics:\n%s", diagnostics)
	}
}

func TestCheckClassifiesInvalidJSONAsUnclassified(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses sh")
	}
	root := t.TempDir()
	writeProject(t, root, "broken")
	glade := filepath.Join(root, "fake-glade.sh")
	if err := os.WriteFile(glade, []byte("#!/bin/sh\nprintf 'not-json'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Check(context.Background(), Options{Root: root, Glade: glade, OutDir: filepath.Join(root, "out"), FailOnUnclassified: true, MaxUnclassified: 0})
	if err == nil || !strings.Contains(err.Error(), "unclassified=1 exceeds max 0") {
		t.Fatalf("expected unclassified failure, got %v", err)
	}
}

func TestReportDisallowedFindingsForPublicCheckClosure(t *testing.T) {
	report := Report{Counts: map[string]int{
		"performance-advisory":        3,
		"project-metadata-missing":    2,
		"project-source-invalid":      2,
		"semantic-contract-gap":       1,
		"source-parse-error":          1,
		"project-discovery-duplicate": 1,
	}}
	got := DisallowedForCheckClosure(report)
	want := map[string]int{
		"semantic-contract-gap": 1,
		"source-parse-error":    1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DisallowedForCheckClosure() = %#v, want %#v", got, want)
	}
}

func TestDiscoverProjectsSkipsAggregateRootWhenNestedProjectsExist(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, "LightningFlowComponents")
	writeProject(t, filepath.Join(root, "LightningFlowComponents", "flow_action_components"), "PostRichChatter")
	writeProject(t, filepath.Join(root, "LightningFlowComponents", "flow_screen_components"), "QuickQuery")
	writeProject(t, filepath.Join(root, "LightningFlowComponents", "zz_after_sfdx_project"), "NestedAfterRootManifest")

	projects, err := discoverProjects(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range projects {
		if filepath.Base(project) == "LightningFlowComponents" {
			t.Fatalf("aggregate root should not be checked when nested projects exist: %#v", projects)
		}
	}
	if len(projects) != 3 {
		t.Fatalf("projects = %#v, want 3 nested projects", projects)
	}
}

func TestClassifyPrivateCorpusProductShapedDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		diag ClassifiedDiagnostic
		want string
	}{
		{
			name: "string literal method return mismatch stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA019", Message: `method "hashCode" has invalid return: returns String from Integer method`},
			want: "semantic-contract-gap",
		},
		{
			name: "string split assignment stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "run" initializes List<String> local "parts" with String`},
			want: "semantic-contract-gap",
		},
		{
			name: "static field fluent string call stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA008", Message: `method "run" calls unknown method "FieldNames.State.toLowerCase"`},
			want: "semantic-contract-gap",
		},
		{
			name: "builder overload miss stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA009", Message: `method "run" has no matching overload for call "Q.condition" with 1 argument(s)`},
			want: "semantic-contract-gap",
		},
		{
			name: "parameter local name stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA009", Message: `method "parse" has no matching overload for call "IProviderParameter.parseList" with 1 argument(s)`},
			want: "semantic-contract-gap",
		},
		{
			name: "current parameters variable stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "process" initializes List<Object> local "currentParameters" with Object`},
			want: "semantic-contract-gap",
		},
		{
			name: "generated variable name stays semantic",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "handleEvent" initializes List<znu.OrderLine> local "generatedLines" with znu.OrderLine`},
			want: "semantic-contract-gap",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.diag); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyPrivateCorpusMetadataDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		diag ClassifiedDiagnostic
		want string
	}{
		{
			name: "unknown custom object type is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA002", Message: `field "record" references unknown type "Package__Order__c"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown namespaced package type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA004", Message: `method "run" parameter "line" references unknown type "pkg.OrderLine"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown arbitrary namespaced package type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA004", Message: `method "run" parameter "line" references unknown type "namz.OrderLine"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown fflib package source type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA002", Message: `method "run" references unknown type "fflib_QueryFactory"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown dependency inner enum is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA004", Message: `constructor "ApplicationSObjectSelector" parameter "dataAccess" references unknown type "DataAccess"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown dependency namespace type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "configure" constructs unknown type "di_Module"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown dependency nested exception is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "configure" constructs unknown type "ModuleException"`},
			want: "project-metadata-missing",
		},
		{
			name: "missing fflib enum expression is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA013", Message: `method "testConstructors" reads unknown variable "fflib_SObjectSelector.DataAccess.LEGACY"`},
			want: "project-metadata-missing",
		},
		{
			name: "interface cascade from missing fflib type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA017", Message: `concrete class "TestAccountsSelector" must implement interface method "configureQueryFactoryFields" from "IApplicationSObjectSelector"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown mock helper is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "test" constructs unknown type "MockHttpResponseGenerator"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown method on custom field path is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA008", Message: `method "generateDescriptionText" calls unknown method "agreement.namz__StartDate__c.format"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown custom object expression type is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "create" references unknown expression type "NU__Product__c"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown namespaced expression type is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "setAgreementFieldsFromCartLine" references unknown expression type "namz.OrderLineAgreement"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown private expression type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "fetchAccountDto" references unknown expression type "AccountBase"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown namespaced super constructor target is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA011", Message: `constructor "ProgramAgreementSetter" has invalid super(...) call: unknown constructor target "namz.AgreementSetter"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown relationship path is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA_QUERY_RELATIONSHIP", Message: `SOQL query references unknown relationship path "Parent__r.Name" on Child__c`},
			want: "project-metadata-missing",
		},
		{
			name: "private package DTO type is metadata until source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA006", Message: `method "buildDto" declares enhanced-for local "product" with unknown type "Product"`},
			want: "project-metadata-missing",
		},
		{
			name: "private znu assignment is metadata until package source is present",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "storePayment" assigns detailsToStore with znu.CreditCardDetail`},
			want: "project-metadata-missing",
		},
		{
			name: "private relationship metadata mismatch is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA024", Message: `method "buildOfferings" enhanced-for assigns znu__ParentProduct__c elements to znu__ProductLink__c variable "parentProductLink"`},
			want: "project-metadata-missing",
		},
		{
			name: "unknown custom query object is metadata",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA_QUERY_OBJECT", Message: `SOQL query references unknown SObject "ASR_Survey_Log__c"`},
			want: "project-metadata-missing",
		},
		{
			name: "duplicate symbol is project discovery duplicate",
			diag: ClassifiedDiagnostic{Code: "GLADETYPE001", Message: `duplicate top-level symbol "DuplicateType"; first seen in /repo/first.cls`},
			want: "project-discovery-duplicate",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.diag); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyPublicCorpusProjectSourceInvalidDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		diag ClassifiedDiagnostic
		want string
	}{
		{
			name: "map initialized from list return is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "getPicklistValuesByObjectFieldTest" initializes Map<String, String> local "sourceMap" with List<String>`},
			want: "project-source-invalid",
		},
		{
			name: "removed old helper method is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA008", Message: `method "getPicklistValuesByObjectFieldTestOld" calls unknown method "GovComponentHelper.getPicklistValuesByObjectFieldOld"`},
			want: "project-source-invalid",
		},
		{
			name: "fabricated test helper scalar initialization is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "createProductWithPriceComponents" initializes fabricatedPriceComponent with Decimal`},
			want: "project-source-invalid",
		},
		{
			name: "product test helper scalar initialization is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA018", Message: `method "availableTicketsWithConflictsConflictWithEachOtherWhenConflictsOverlap" initializes product1 with String`},
			want: "project-source-invalid",
		},
		{
			name: "missing IModel source return cascade is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA019", Message: `method "getModel" has invalid return: returns BatchSObjectWrapper from IModel method`},
			want: "project-source-invalid",
		},
		{
			name: "missing IModel list source return cascade is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA019", Message: `method "newListForType" has invalid return: returns List<BatchSObjectWrapper> from List<IModel> method`},
			want: "project-source-invalid",
		},
		{
			name: "missing query plugin collection cascade is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA023", Message: `method "getFilterPlugins" has invalid collection call "add" with 1 argument(s)`},
			want: "project-source-invalid",
		},
		{
			name: "static method through instance is project source invalid",
			diag: ClassifiedDiagnostic{Code: "GLADESEMA027", Message: `method "run" has invalid static access for "selector.getRows": static method called through an instance`},
			want: "project-source-invalid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.diag); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func writeProject(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sfdx-project.json"), []byte(`{"packageDirectories":[{"path":"force-app","default":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
}
