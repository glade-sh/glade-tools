package lwcparity

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type OracleManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	ProjectPath   string          `json:"projectPath"`
	Fixtures      []OracleFixture `json:"fixtures"`
}

type OracleFixture struct {
	ID                          string   `json:"id"`
	Category                    string   `json:"category"`
	Name                        string   `json:"name"`
	ComponentName               string   `json:"componentName"`
	TargetHost                  string   `json:"targetHost"`
	Route                       string   `json:"route"`
	SalesforceDeployable        bool     `json:"salesforceDeployable"`
	SalesforceBrowserCapturable bool     `json:"salesforceBrowserCapturable"`
	Assertions                  []string `json:"assertions"`
}

func WriteOracleFixtures(report Report, outDir string) (OracleManifest, error) {
	outDir = strings.TrimSpace(outDir)
	if outDir == "" {
		return OracleManifest{}, fmt.Errorf("output directory is required")
	}
	manifest := OracleManifest{SchemaVersion: 1, ProjectPath: "."}
	rows := append([]Row(nil), report.Rows...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })

	if err := writeOracleProjectScaffold(outDir); err != nil {
		return OracleManifest{}, err
	}
	for _, row := range rows {
		fixture, err := writeOracleBundle(outDir, row)
		if err != nil {
			return OracleManifest{}, err
		}
		manifest.Fixtures = append(manifest.Fixtures, fixture)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return OracleManifest{}, err
	}
	if err := os.WriteFile(filepath.Join(outDir, "glade-lwc-oracle.json"), append(data, '\n'), 0o644); err != nil {
		return OracleManifest{}, err
	}
	return manifest, nil
}

func writeOracleProjectScaffold(outDir string) error {
	if err := os.MkdirAll(filepath.Join(outDir, "force-app", "main", "default", "lwc"), 0o755); err != nil {
		return err
	}
	for _, dir := range []string{"classes", "labels", "staticresources", "customPermissions", "contentassets"} {
		if err := os.MkdirAll(filepath.Join(outDir, "force-app", "main", "default", dir), 0o755); err != nil {
			return err
		}
	}
	project := `{
  "packageDirectories": [
    {
      "path": "force-app",
      "default": true
    }
  ],
  "sourceApiVersion": "61.0"
}
`
	files := map[string]string{
		"sfdx-project.json": project,
		filepath.Join("force-app", "main", "default", "classes", "GladeLwcOracleController.cls"): `public with sharing class GladeLwcOracleController {
  @AuraEnabled(cacheable=true)
  public static String ping() {
    return 'ok';
  }

  @AuraEnabled(continuation=true cacheable=true)
  public static String continuationPing() {
    return 'ok';
  }
}
`,
		filepath.Join("force-app", "main", "default", "classes", "GladeLwcOracleController.cls-meta.xml"): `<?xml version="1.0" encoding="UTF-8"?>
<ApexClass xmlns="http://soap.sforce.com/2006/04/metadata">
  <apiVersion>61.0</apiVersion>
  <status>Active</status>
</ApexClass>
`,
		filepath.Join("force-app", "main", "default", "labels", "CustomLabels.labels-meta.xml"): `<?xml version="1.0" encoding="UTF-8"?>
<CustomLabels xmlns="http://soap.sforce.com/2006/04/metadata">
  <labels>
    <fullName>GladeLwcOracleLabel</fullName>
    <language>en_US</language>
    <protected>false</protected>
    <shortDescription>Glade LWC Oracle Label</shortDescription>
    <value>Glade LWC Oracle Label</value>
  </labels>
</CustomLabels>
`,
		filepath.Join("force-app", "main", "default", "staticresources", "GladeLwcOracleResource.resource"): `{"ok":true}
`,
		filepath.Join("force-app", "main", "default", "staticresources", "GladeLwcOracleResource.resource-meta.xml"): `<?xml version="1.0" encoding="UTF-8"?>
<StaticResource xmlns="http://soap.sforce.com/2006/04/metadata">
  <cacheControl>Public</cacheControl>
  <contentType>application/json</contentType>
</StaticResource>
`,
		filepath.Join("force-app", "main", "default", "customPermissions", "Glade_Lwc_Oracle.customPermission-meta.xml"): `<?xml version="1.0" encoding="UTF-8"?>
<CustomPermission xmlns="http://soap.sforce.com/2006/04/metadata">
  <label>Glade LWC Oracle</label>
</CustomPermission>
`,
		filepath.Join("force-app", "main", "default", "contentassets", "GladeLwcOracleAsset.asset-meta.xml"): `<?xml version="1.0" encoding="UTF-8"?>
<ContentAsset xmlns="http://soap.sforce.com/2006/04/metadata">
  <isVisibleByExternalUsers>false</isVisibleByExternalUsers>
  <language>en_US</language>
  <masterLabel>GladeLwcOracleAsset</masterLabel>
  <relationships>
    <organization>
      <access>VIEWER</access>
    </organization>
  </relationships>
  <versions>
    <version>
      <number>1</number>
      <pathOnClient>GladeLwcOracleAsset.png</pathOnClient>
    </version>
  </versions>
</ContentAsset>
`,
	}
	for path, contents := range files {
		if err := os.WriteFile(filepath.Join(outDir, path), []byte(contents), 0o644); err != nil {
			return err
		}
	}
	contentAssetPNG, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "force-app", "main", "default", "contentassets", "GladeLwcOracleAsset.asset"), contentAssetPNG, 0o644); err != nil {
		return err
	}
	return nil
}

func writeOracleBundle(outDir string, row Row) (OracleFixture, error) {
	component := oracleComponentName(row)
	dir := filepath.Join(outDir, "force-app", "main", "default", "lwc", component)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return OracleFixture{}, err
	}
	js, html := oracleBundleSource(row, component)
	files := map[string]string{
		component + ".js":          js,
		component + ".html":        html,
		component + ".js-meta.xml": oracleMetaXML(row),
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			return OracleFixture{}, err
		}
	}
	return OracleFixture{
		ID:                          row.ID,
		Category:                    row.Category,
		Name:                        row.Name,
		ComponentName:               component,
		TargetHost:                  "lightning-shell",
		Route:                       "/lwc/preview/component/c/" + component,
		SalesforceDeployable:        row.Status != StatusLocalOnly,
		SalesforceBrowserCapturable: oracleSalesforceBrowserCapturable(row),
		Assertions:                  []string{"imports", "renders", "no console errors"},
	}, nil
}

func oracleBundleSource(row Row, component string) (string, string) {
	switch row.Category {
	case CategoryAPIModule:
		return oracleAPIModuleJS(row.Name), oracleHTML(row.Name)
	case CategorySalesforceModule:
		return oracleSalesforceModuleJS(row.Name), oracleHTML(row.Name)
	case CategoryPageReference:
		return oraclePageReferenceJS(row.Name), oracleHTML(row.Name)
	case CategoryBaseComponent:
		return oracleBaseComponentJS(row.Name), oracleBaseComponentHTML(row.Name)
	default:
		return oracleGenericJS(row.Name), oracleHTML(row.Name)
	}
}

func oracleAPIModuleJS(name string) string {
	switch name {
	case "lightning/uiAppsApi":
		return `import { LightningElement, wire } from "lwc";
import { getNavItems } from "lightning/uiAppsApi";

export default class Oracle extends LightningElement {
  label = "` + name + `";
  @wire(getNavItems, {}) navItems;
}
`
	case "lightning/uiRecordApi":
		return `import { LightningElement, wire } from "lwc";
import { getRecord, getFieldValue } from "lightning/uiRecordApi";

export default class Oracle extends LightningElement {
  label = "` + name + `";
  recordId = "001000000000001AAA";
  @wire(getRecord, { recordId: "$recordId", fields: ["Account.Name"] }) record;
  get value() {
    return getFieldValue(this.record?.data, "Account.Name") || "not loaded";
  }
}
`
	case "lightning/uiListsApi":
		return `import { LightningElement, wire } from "lwc";
import { getListInfosByObjectName } from "lightning/uiListsApi";

export default class Oracle extends LightningElement {
  label = "` + name + `";
  @wire(getListInfosByObjectName, { objectApiName: "Account" }) listInfos;
}
`
	case "lightning/graphql", "lightning/uiGraphQLApi":
		return `import { LightningElement } from "lwc";
import { gql, graphql } from "` + name + `";

export default class Oracle extends LightningElement {
  label = "` + name + `";
  query = gql` + "`" + `query AccountOracle { uiapi { query { Account { edges { node { Id Name { value } } } } } } }` + "`" + `;
  connectedCallback() {
    this.result = graphql ? "graphql ready" : "graphql missing";
  }
}
`
	default:
		return oracleGenericJSImport(name)
	}
}

func oracleSalesforceModuleJS(name string) string {
	local := "value"
	importName := oracleSalesforceImportName(name)
	switch {
	case strings.HasPrefix(name, "@salesforce/userPermission/"):
		local = "permission"
	case strings.Contains(name, "activeLanguages"):
		local = "activeLanguages"
	case strings.Contains(name, "apexContinuation"):
		local = "continuation"
	}
	return `import { LightningElement } from "lwc";
import value from "` + importName + `";

export default class Oracle extends LightningElement {
  label = "` + name + `";
  ` + local + ` = value;
}
`
}

func oracleSalesforceImportName(name string) string {
	switch name {
	case "@salesforce/apex/":
		return "@salesforce/apex/GladeLwcOracleController.ping"
	case "@salesforce/apexContinuation":
		return "@salesforce/apexContinuation/GladeLwcOracleController.continuationPing"
	case "@salesforce/community/":
		return "@salesforce/community/basePath"
	case "@salesforce/contentAssetUrl/":
		return "@salesforce/contentAssetUrl/GladeLwcOracleAsset"
	case "@salesforce/customPermission/":
		return "@salesforce/customPermission/Glade_Lwc_Oracle"
	case "@salesforce/i18n/":
		return "@salesforce/i18n/locale"
	case "@salesforce/label/":
		return "@salesforce/label/c.GladeLwcOracleLabel"
	case "@salesforce/resourceUrl/":
		return "@salesforce/resourceUrl/GladeLwcOracleResource"
	case "@salesforce/schema/":
		return "@salesforce/schema/Account"
	case "@salesforce/site/":
		return "@salesforce/site/Id"
	case "@salesforce/user/":
		return "@salesforce/user/Id"
	case "@salesforce/userPermission/":
		return "@salesforce/userPermission/ViewSetup"
	}
	return name
}

func oraclePageReferenceJS(name string) string {
	return `import { LightningElement } from "lwc";
import { NavigationMixin } from "lightning/navigation";

export default class Oracle extends NavigationMixin(LightningElement) {
  label = "` + name + `";
  connectedCallback() {
    this[NavigationMixin.GenerateUrl]({ type: "` + name + `", attributes: {}, state: { c__oracle: "1" } })
      .then((url) => { this.url = url; })
      .catch((error) => { this.url = error?.message || "navigation unavailable"; });
  }
}
`
}

func oracleBaseComponentJS(name string) string {
	return `import { LightningElement } from "lwc";

export default class Oracle extends LightningElement {
  label = "` + name + `";
}
`
}

func oracleGenericJSImport(name string) string {
	return `import { LightningElement } from "lwc";
import * as api from "` + name + `";

export default class Oracle extends LightningElement {
  label = "` + name + `";
  exports = Object.keys(api || {}).join(",");
}
`
}

func oracleGenericJS(name string) string {
	return `import { LightningElement } from "lwc";

export default class Oracle extends LightningElement {
  label = "` + name + `";
}
`
}

func oracleHTML(name string) string {
	return `<template>
  <section data-oracle="` + htmlEscape(name) + `">
    <p>{label}</p>
  </section>
</template>
`
}

func oracleBaseComponentHTML(name string) string {
	tag := strings.ReplaceAll(name, "/", "-")
	return `<template>
  <section data-oracle="` + htmlEscape(name) + `">
    <p>{label}</p>
    <` + tag + ` label="Oracle"></` + tag + `>
  </section>
</template>
`
}

func oracleMetaXML(row Row) string {
	capabilities := oracleCapabilities(row)
	capabilityXML := ""
	if len(capabilities) > 0 {
		var b strings.Builder
		b.WriteString("  <capabilities>\n")
		for _, capability := range capabilities {
			b.WriteString("    <capability>")
			b.WriteString(capability)
			b.WriteString("</capability>\n")
		}
		b.WriteString("  </capabilities>\n")
		capabilityXML = b.String()
	}
	apiVersion := oracleAPIVersion(row)
	return `<?xml version="1.0" encoding="UTF-8"?>
<LightningComponentBundle xmlns="http://soap.sforce.com/2006/04/metadata">
  <apiVersion>` + apiVersion + `</apiVersion>
  <isExposed>true</isExposed>
` + capabilityXML + `  <targets>
    <target>lightning__UrlAddressable</target>
    <target>lightning__AppPage</target>
    <target>lightning__RecordPage</target>
    <target>lightning__HomePage</target>
  </targets>
</LightningComponentBundle>
`
}

func oracleAPIVersion(row Row) string {
	switch row.Name {
	case "experience/blockBuilderApi":
		return "66.0"
	default:
		return "61.0"
	}
}

func oracleSalesforceBrowserCapturable(row Row) bool {
	switch row.Name {
	case "experience/blockBuilderApi",
		"@salesforce/community/",
		"@salesforce/community/Id",
		"@salesforce/community/basePath",
		"@salesforce/site/",
		"@salesforce/site/Id",
		"@salesforce/site/activeLanguages":
		return false
	default:
		return row.Status != StatusLocalOnly
	}
}

func oracleCapabilities(row Row) []string {
	switch row.Name {
	case "lightning/serviceCloudVoiceToolkitApi":
		return []string{"lightning__ServiceCloudVoiceToolkitApi"}
	default:
		return nil
	}
}

var nonIdentifierRE = regexp.MustCompile(`[^A-Za-z0-9]+`)

func oracleComponentName(row Row) string {
	name := row.Name
	if row.Category == CategoryAPIModule {
		if strings.HasPrefix(name, "lightning/") {
			name = strings.TrimPrefix(name, "lightning/")
		} else if strings.HasPrefix(name, "experience/") {
			name = "experience/" + strings.TrimPrefix(name, "experience/")
		}
	}
	if row.Category == CategorySalesforceModule {
		name = strings.TrimPrefix(name, "@")
	}
	name = strings.TrimPrefix(name, "@")
	name = strings.ReplaceAll(name, "__", "_")
	parts := nonIdentifierRE.Split(name, -1)
	out := strings.Builder{}
	for _, part := range parts {
		if part == "" {
			continue
		}
		if out.Len() == 0 {
			out.WriteString(lowerFirst(part))
			continue
		}
		out.WriteString(upperFirst(part))
	}
	if out.Len() == 0 {
		out.WriteString("lwc")
	}
	out.WriteString("Oracle")
	return out.String()
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
