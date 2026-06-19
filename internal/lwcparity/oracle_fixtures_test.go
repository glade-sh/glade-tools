package lwcparity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteOracleFixturesGeneratesProjectAndBundles(t *testing.T) {
	report := Report{
		SchemaVersion: SchemaVersion,
		Rows: []Row{
			{ID: rowID(CategoryAPIModule, "lightning/uiRecordApi"), Category: CategoryAPIModule, Name: "lightning/uiRecordApi", Status: StatusSupportedLocal},
			{ID: rowID(CategoryAPIModule, "lightning/uiAppsApi"), Category: CategoryAPIModule, Name: "lightning/uiAppsApi", Status: StatusDocsOnly},
			{ID: rowID(CategoryPageReference, "standard__flow"), Category: CategoryPageReference, Name: "standard__flow", Status: StatusDocsOnly},
			{ID: rowID(CategorySalesforceModule, "@salesforce/apexContinuation"), Category: CategorySalesforceModule, Name: "@salesforce/apexContinuation", Status: StatusUnsupportedLocal},
			{ID: rowID(CategorySalesforceModule, "@salesforce/site/activeLanguages"), Category: CategorySalesforceModule, Name: "@salesforce/site/activeLanguages", Status: StatusUnsupportedLocal},
			{ID: rowID(CategoryAPIModule, "lightning/serviceCloudVoiceToolkitApi"), Category: CategoryAPIModule, Name: "lightning/serviceCloudVoiceToolkitApi", Status: StatusPartialLocal},
			{ID: rowID(CategoryAPIModule, "experience/blockBuilderApi"), Category: CategoryAPIModule, Name: "experience/blockBuilderApi", Status: StatusPartialLocal},
			{ID: rowID(CategoryBaseComponent, "lightning/button"), Category: CategoryBaseComponent, Name: "lightning/button", Status: StatusLocalOnly},
		},
	}
	out := t.TempDir()

	manifest, err := WriteOracleFixtures(report, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Fixtures) != 8 {
		t.Fatalf("fixtures = %d", len(manifest.Fixtures))
	}
	assertOracleFileContains(t, out, "sfdx-project.json", `"sourceApiVersion"`)
	assertOracleFileContains(t, out, "force-app/main/default/lwc/uiRecordApiOracle/uiRecordApiOracle.js", "lightning/uiRecordApi")
	assertOracleFileContains(t, out, "force-app/main/default/lwc/uiAppsApiOracle/uiAppsApiOracle.js", "getNavItems")
	assertOracleFileContains(t, out, "force-app/main/default/lwc/standardFlowOracle/standardFlowOracle.js", "standard__flow")
	assertOracleFileContains(t, out, "force-app/main/default/classes/GladeLwcOracleController.cls", "@AuraEnabled(continuation=true cacheable=true)")
	assertOracleFileContains(t, out, "force-app/main/default/contentassets/GladeLwcOracleAsset.asset-meta.xml", "<pathOnClient>GladeLwcOracleAsset.png</pathOnClient>")
	assertOracleFileContains(t, out, "force-app/main/default/lwc/salesforceApexContinuationOracle/salesforceApexContinuationOracle.js", "@salesforce/apexContinuation/GladeLwcOracleController.continuationPing")
	assertOracleFileContains(t, out, "force-app/main/default/lwc/salesforceSiteActiveLanguagesOracle/salesforceSiteActiveLanguagesOracle.js", "@salesforce/site/activeLanguages")
	assertOracleFileContains(t, out, "force-app/main/default/lwc/serviceCloudVoiceToolkitApiOracle/serviceCloudVoiceToolkitApiOracle.js-meta.xml", "lightning__ServiceCloudVoiceToolkitApi")
	assertOracleFileContains(t, out, "force-app/main/default/lwc/experienceBlockBuilderApiOracle/experienceBlockBuilderApiOracle.js-meta.xml", "<apiVersion>66.0</apiVersion>")
	assertOracleFileContains(t, out, "force-app/main/default/lwc/lightningButtonOracle/lightningButtonOracle.html", "lightning-button")
	dataAsset, err := os.ReadFile(filepath.Join(out, "force-app/main/default/contentassets/GladeLwcOracleAsset.asset"))
	if err != nil {
		t.Fatalf("read content asset body: %v", err)
	}
	if !bytes.HasPrefix(dataAsset, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("content asset is not a PNG: %x", dataAsset[:min(len(dataAsset), 8)])
	}

	data, err := os.ReadFile(filepath.Join(out, "glade-lwc-oracle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded OracleManifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("manifest JSON: %v\n%s", err, string(data))
	}
	if decoded.SchemaVersion != 1 || decoded.ProjectPath != "." {
		t.Fatalf("manifest = %#v", decoded)
	}
	assertOracleFixture(t, decoded, "api-module:experience/blockBuilderApi", true, false)
	assertOracleFixture(t, decoded, "salesforce-module:@salesforce/site/activeLanguages", true, false)
	assertOracleFixture(t, decoded, "base-component:lightning/button", false, false)
}

func assertOracleFileContains(t *testing.T, root, name, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s omitted %q:\n%s", name, want, string(data))
	}
}

func assertOracleFixture(t *testing.T, manifest OracleManifest, id string, deployable, browserCapturable bool) {
	t.Helper()
	for _, fixture := range manifest.Fixtures {
		if fixture.ID != id {
			continue
		}
		if fixture.SalesforceDeployable != deployable || fixture.SalesforceBrowserCapturable != browserCapturable {
			t.Fatalf("%s deploy/browser = %v/%v", id, fixture.SalesforceDeployable, fixture.SalesforceBrowserCapturable)
		}
		return
	}
	t.Fatalf("fixture %s not found", id)
}
