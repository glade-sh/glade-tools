package compat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type LwcCaptureOptions struct {
	Project             string
	TargetOrg           string
	Targets             []string
	Hosts               []string
	Out                 string
	SkipDeploy          bool
	BrowserCapture      bool
	LocalBrowserCapture bool
	LocalBaseURL        string
	GladeBin            string
	Runner              LwcCommandRunner
	Browser             LwcBrowserRunner
}

type LwcCommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type lwcCommandRunnerWithDir interface {
	RunInDir(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

type LwcBrowserRunner interface {
	CaptureDOM(ctx context.Context, url string) (LwcBrowserCapture, error)
}

type LwcBrowserCapture struct {
	DOM           string
	ConsoleErrors []string
	PageErrors    []string
	HTTPStatus    int
}

type LwcCaptureReport struct {
	Command   string              `json:"command"`
	OK        bool                `json:"ok"`
	TargetOrg string              `json:"targetOrg"`
	Project   string              `json:"project,omitempty"`
	Mode      string              `json:"mode"`
	Deployed  bool                `json:"deployed"`
	Hosts     []string            `json:"hosts,omitempty"`
	Cases     []LwcCaptureCase    `json:"cases"`
	Support   []LwcSupportRow     `json:"support,omitempty"`
	Counts    LwcCaptureCounts    `json:"counts"`
	Artifacts LwcCaptureArtifacts `json:"artifacts"`
}

type LwcCaptureCase struct {
	Name               string                `json:"name"`
	Feature            string                `json:"feature"`
	Host               string                `json:"host,omitempty"`
	Status             string                `json:"status"`
	TargetURL          string                `json:"targetUrl"`
	Metadata           LwcCaptureMetadata    `json:"metadata"`
	LocalEvidence      *LwcCaptureEvidence   `json:"localEvidence,omitempty"`
	SalesforceEvidence *LwcCaptureEvidence   `json:"salesforceEvidence,omitempty"`
	Comparison         *LwcCaptureComparison `json:"comparison,omitempty"`
	ConsoleErrors      []string              `json:"consoleErrors,omitempty"`
	PageErrors         []string              `json:"pageErrors,omitempty"`
	Notes              string                `json:"notes,omitempty"`
}

type LwcCaptureMetadata struct {
	Route         string   `json:"route,omitempty"`
	Component     string   `json:"component,omitempty"`
	Page          string   `json:"page,omitempty"`
	ObjectAPIName string   `json:"objectApiName,omitempty"`
	RecordID      string   `json:"recordId,omitempty"`
	App           string   `json:"app,omitempty"`
	Tab           string   `json:"tab,omitempty"`
	QuickAction   string   `json:"quickAction,omitempty"`
	Files         []string `json:"files,omitempty"`
	Assertions    []string `json:"assertions,omitempty"`
}

type LwcCaptureEvidence struct {
	Kind          string   `json:"kind"`
	Source        string   `json:"source"`
	Status        string   `json:"status"`
	TargetURL     string   `json:"targetUrl,omitempty"`
	DOM           string   `json:"dom,omitempty"`
	ConsoleErrors []string `json:"consoleErrors,omitempty"`
	PageErrors    []string `json:"pageErrors,omitempty"`
	Notes         string   `json:"notes,omitempty"`
}

type LwcCaptureComparison struct {
	OK         bool                      `json:"ok"`
	DiffCount  int                       `json:"diffCount"`
	Scope      LwcCaptureComparisonScope `json:"scope,omitempty"`
	Local      LwcCaptureComparisonSide  `json:"local"`
	Salesforce LwcCaptureComparisonSide  `json:"salesforce"`
	Diffs      []LwcCaptureDiff          `json:"diffs,omitempty"`
}

type LwcCaptureComparisonScope struct {
	Selector        string `json:"selector,omitempty"`
	LocalFound      bool   `json:"localFound"`
	SalesforceFound bool   `json:"salesforceFound"`
}

type LwcCaptureComparisonSide struct {
	VisibleText           string   `json:"visibleText,omitempty"`
	MountedComponentCount int      `json:"mountedComponentCount"`
	Components            []string `json:"components,omitempty"`
}

type LwcCaptureDiff struct {
	Field      string `json:"field"`
	Local      string `json:"local"`
	Salesforce string `json:"salesforce"`
}

type LwcSupportRow struct {
	Feature  string `json:"feature"`
	Host     string `json:"host"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
	Notes    string `json:"notes,omitempty"`
}

type LwcCaptureCounts struct {
	Targets  int `json:"targets"`
	Prepared int `json:"prepared"`
	Pass     int `json:"pass"`
	Fail     int `json:"fail"`
}

type LwcCaptureArtifacts struct {
	Report string `json:"report,omitempty"`
}

var defaultLwcCaptureCases = []string{
	"direct-component",
	"record-page",
	"app-page",
	"home-page",
	"custom-tab",
	"url-addressable-component",
	"record-quick-action",
	"community-page",
	"community-component",
	"visualforce-lightning-out",
	"apex-wire",
	"visualforce-apex-wire",
	"imperative-apex",
	"visualforce-imperative-apex",
	"lds-read",
	"visualforce-lds-read",
	"ui-object-info",
	"visualforce-ui-object-info",
	"ui-related-list",
	"lds-create-defaults",
	"ui-layout",
	"lds-mutation",
	"visualforce-lds-mutation",
	"navigation",
	"visualforce-navigation",
	"toast",
	"visualforce-toast",
	"lms",
	"visualforce-lms",
	"visualforce-resource-loader",
	"community-context",
	"base-components",
	"visualforce-base-components",
	"phase3-base-components",
}

type lwcCaptureTargetDefinition struct {
	Name          string
	Feature       string
	Host          string
	Route         string
	Component     string
	Page          string
	ObjectAPIName string
	RecordID      string
	App           string
	Tab           string
	QuickAction   string
	DOM           string
	Notes         string
	Files         []string
	Assertions    []string
}

var lwcCaptureTargetDefinitions = map[string]lwcCaptureTargetDefinition{
	"direct-component": {
		Name:       "direct-component",
		Feature:    "lwc.target.direct-component",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/component/c/contextProbe",
		Component:  "contextProbe",
		DOM:        `<section data-probe="context"><h2>Local Shell Context</h2></section>`,
		Notes:      "Direct component route mounts the context probe bundle.",
		Files:      []string{"force-app/main/default/lwc/contextProbe/contextProbe.js", "force-app/main/default/lwc/contextProbe/contextProbe.html"},
		Assertions: []string{"mounted component count", "public api attributes", "label and static resource imports"},
	},
	"record-page": {
		Name:          "record-page",
		Feature:       "lwc.target.record-page",
		Host:          "lightning-shell",
		Route:         "/lwc/preview/record/Account/001000000000001AAA?page=Account_Record_Page",
		Component:     "recordProbe",
		Page:          "Account_Record_Page",
		ObjectAPIName: "Account",
		RecordID:      "001000000000001AAA",
		App:           "Sales",
		DOM:           `<section data-probe="record"><h2>Record Probe</h2></section>`,
		Notes:         "Record page route resolves Account_Record_Page and passes record context.",
		Files:         []string{"force-app/main/default/flexipages/Account_Record_Page.flexipage-meta.xml", "force-app/main/default/lwc/recordProbe/recordProbe.js"},
		Assertions:    []string{"visible text", "record id", "mounted component count"},
	},
	"app-page": {
		Name:       "app-page",
		Feature:    "lwc.target.app-page",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/app/Sales_Dashboard",
		Component:  "wireProbe",
		Page:       "Sales_Dashboard",
		App:        "Lwc_Shell",
		DOM:        `<section data-probe="wire"><h2>Wire Probe</h2></section>`,
		Notes:      "App page route resolves Sales_Dashboard and dashboard region components.",
		Files:      []string{"force-app/main/default/applications/Lwc_Shell.app-meta.xml", "force-app/main/default/permissionsets/Lwc_Shell_Access.permissionset-meta.xml", "force-app/main/default/tabs/Lwc_Probe.tab-meta.xml", "force-app/main/default/flexipages/Sales_Dashboard.flexipage-meta.xml", "force-app/main/default/lwc/wireProbe/wireProbe.js"},
		Assertions: []string{"page region metadata", "mounted component count"},
	},
	"home-page": {
		Name:       "home-page",
		Feature:    "lwc.target.home-page",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/home/Custom_Home",
		Component:  "contextProbe",
		Page:       "Custom_Home",
		App:        "Sales",
		DOM:        `<section data-probe="context"><h2>Home Context</h2></section>`,
		Notes:      "Home route resolves Custom_Home with the local home template.",
		Files:      []string{"force-app/main/default/flexipages/Custom_Home.flexipage-meta.xml", "force-app/main/default/aura/lwcHomeTemplate/lwcHomeTemplate.cmp"},
		Assertions: []string{"page region metadata", "template metadata"},
	},
	"custom-tab": {
		Name:       "custom-tab",
		Feature:    "lwc.target.custom-tab",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/tab/Lwc_Probe",
		Component:  "contextProbe",
		Tab:        "Lwc_Probe",
		App:        "Lwc_Shell",
		DOM:        `<section data-probe="context"><h2>App Context</h2></section>`,
		Notes:      "Custom tab route resolves Lwc_Probe to its flexipage.",
		Files:      []string{"force-app/main/default/applications/Lwc_Shell.app-meta.xml", "force-app/main/default/permissionsets/Lwc_Shell_Access.permissionset-meta.xml", "force-app/main/default/tabs/Lwc_Probe.tab-meta.xml", "force-app/main/default/flexipages/Sales_Dashboard.flexipage-meta.xml"},
		Assertions: []string{"tab metadata", "resolved page route"},
	},
	"url-addressable-component": {
		Name:       "url-addressable-component",
		Feature:    "lwc.target.url-addressable-component",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/cmp/c/actionProbe?c__mode=demo",
		Component:  "actionProbe",
		DOM:        `<section data-probe="context"><h2>Local Shell Context</h2></section>`,
		Notes:      "URL state is preserved in the target metadata for later PageReference comparison.",
		Files:      []string{"force-app/main/default/lwc/actionProbe/actionProbe.js-meta.xml", "glade.lwc.json"},
		Assertions: []string{"PageReference state", "component route"},
	},
	"record-quick-action": {
		Name:          "record-quick-action",
		Feature:       "lwc.target.record-quick-action",
		Host:          "lightning-shell",
		Route:         "/lwc/preview/action/Account/001000000000001AAA/Update_Status",
		Component:     "actionProbe",
		ObjectAPIName: "Account",
		RecordID:      "001000000000001AAA",
		QuickAction:   "Account.Update_Status",
		DOM:           `<section data-probe="action"><h2>Action Probe</h2></section>`,
		Notes:         "Quick action metadata points at c:actionProbe.",
		Files:         []string{"force-app/main/default/quickActions/Account.Update_Status.quickAction-meta.xml", "force-app/main/default/lwc/actionProbe/actionProbe.js"},
		Assertions:    []string{"action metadata", "record context"},
	},
	"community-page": {
		Name:       "community-page",
		Feature:    "lwc.target.community-page",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/community/Partner_Portal/Account",
		Component:  "communityProbe",
		Page:       "Account",
		DOM:        `<section data-probe="community"><h2>Community Probe</h2></section>`,
		Notes:      "Community page route resolves the communityAccount preset, mounts c:communityProbe, and carries Experience Cloud context.",
		Files:      []string{"glade.lwc.json", "force-app/main/default/lwc/communityProbe/communityProbe.js", "force-app/main/default/lwc/communityThemeLayout/communityThemeLayout.js-meta.xml"},
		Assertions: []string{"community context", "comm PageReference", "theme layout boundary"},
	},
	"community-component": {
		Name:       "community-component",
		Feature:    "lwc.target.community-component",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/community/Partner_Portal/cmp/c/communityProbe",
		Component:  "communityProbe",
		DOM:        `<section data-probe="community"><h2>Community Probe</h2></section>`,
		Notes:      "Direct community component route mounts lightningCommunity__Default with local site context and /s base-path fallback.",
		Files:      []string{"force-app/main/default/lwc/communityProbe/communityProbe.js", "force-app/main/default/lwc/communityProbe/communityProbe.js-meta.xml"},
		Assertions: []string{"community direct component route", "basePath fallback"},
	},
	"visualforce-lightning-out": {
		Name:       "visualforce-lightning-out",
		Feature:    "lwc.target.visualforce-lightning-out",
		Host:       "visualforce-lightning-out",
		Route:      "/apex/LwcShellProbe",
		Component:  "contextProbe",
		DOM:        `<div data-host="visualforce-lightning-out"><section data-probe="context"></section></div>`,
		Notes:      "Visualforce host target records the shared Lightning Out runtime lane.",
		Files:      []string{"force-app/main/default/lwc/contextProbe/contextProbe.js"},
		Assertions: []string{"Lightning Out host", "mounted component count"},
	},
	"apex-wire": {
		Name:       "apex-wire",
		Feature:    "lwc.service.apex-wire",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/component/c/wireProbe?feature=apex-wire",
		Component:  "wireProbe",
		DOM:        `<section data-probe="wire"><p>{firstWiredName}</p></section>`,
		Notes:      "wireProbe imports @salesforce/apex/LwcProbeController.wireAccounts.",
		Files:      []string{"force-app/main/default/lwc/wireProbe/wireProbe.js", "force-app/main/default/classes/LwcProbeController.cls"},
		Assertions: []string{"wire payload shape", "visible text"},
	},
	"visualforce-apex-wire": {
		Name:       "visualforce-apex-wire",
		Feature:    "lwc.service.apex-wire",
		Host:       "visualforce-lightning-out",
		Route:      "/apex/MultiWidgetHost",
		Component:  "wireProbe",
		DOM:        `<section data-probe="vf-wire"><p>Local Widget</p></section>`,
		Notes:      "Visualforce Lightning Out c:wireProbe and c:apexWireHost prove Apex wire through the shared runtime.",
		Files:      []string{"force-app/main/default/lwc/wireProbe/wireProbe.js", "force-app/main/default/lwc/apexWireHost/apexWireHost.js", "force-app/main/default/classes/ItemCtrl.cls"},
		Assertions: []string{"Visualforce Apex wire payload", "visible text"},
	},
	"imperative-apex": {
		Name:       "imperative-apex",
		Feature:    "lwc.service.imperative-apex",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/component/c/wireProbe?feature=imperative-apex",
		Component:  "wireProbe",
		DOM:        `<section data-probe="wire"><button>Load Imperative</button></section>`,
		Notes:      "wireProbe imports @salesforce/apex/LwcProbeController.imperativeAccount.",
		Files:      []string{"force-app/main/default/lwc/wireProbe/wireProbe.js", "force-app/main/default/classes/LwcProbeController.cls"},
		Assertions: []string{"imperative payload shape", "visible text after click"},
	},
	"visualforce-imperative-apex": {
		Name:       "visualforce-imperative-apex",
		Feature:    "lwc.service.imperative-apex",
		Host:       "visualforce-lightning-out",
		Route:      "/apex/MultiWidgetHost",
		Component:  "wireProbe",
		DOM:        `<section data-probe="vf-wire"><p>Local Widget</p></section>`,
		Notes:      "Visualforce Lightning Out c:wireProbe invokes @salesforce/apex/ItemCtrl.getItems as an imperative function.",
		Files:      []string{"force-app/main/default/lwc/wireProbe/wireProbe.js", "force-app/main/default/classes/ItemCtrl.cls"},
		Assertions: []string{"Visualforce imperative Apex function call", "visible text"},
	},
	"lds-read": {
		Name:          "lds-read",
		Feature:       "lwc.service.lds-read",
		Host:          "lightning-shell",
		Route:         "/lwc/preview/component/c/recordProbe?recordId=001000000000001AAA",
		Component:     "recordProbe",
		ObjectAPIName: "Account",
		RecordID:      "001000000000001AAA",
		DOM:           `<section data-probe="record"><p>{accountName}</p><p>{industry}</p></section>`,
		Notes:         "Local UI API covers getRecord, getRecords, getObjectInfo, and getObjectInfos with local data and schema limits.",
		Files:         []string{"force-app/main/default/lwc/recordProbe/recordProbe.js", "data/accounts.json"},
		Assertions:    []string{"record fields", "batch record results", "object info results", "wire payload shape"},
	},
	"visualforce-lds-read": {
		Name:          "visualforce-lds-read",
		Feature:       "lwc.service.lds-read",
		Host:          "visualforce-lightning-out",
		Route:         "/apex/MultiWidgetHost",
		Component:     "recordWireHost",
		ObjectAPIName: "Account",
		RecordID:      "001XX0000000001",
		DOM:           `<p class="name">Acme</p>`,
		Notes:         "Visualforce Lightning Out c:recordWireHost proves getRecord against local org state.",
		Files:         []string{"force-app/main/default/lwc/recordWireHost/recordWireHost.js", "data/accounts.json"},
		Assertions:    []string{"Visualforce record wire", "visible text"},
	},
	"ui-object-info": {
		Name:          "ui-object-info",
		Feature:       "lwc.service.ui-object-info",
		Host:          "lightning-shell",
		Route:         "/lwc/preview/component/c/objectInfoProbe",
		Component:     "objectInfoProbe",
		ObjectAPIName: "Account",
		DOM:           `<section data-probe="object-info"><h2>Object Info Probe</h2></section>`,
		Notes:         "Local lightning/uiObjectInfoApi covers getObjectInfo, getObjectInfos, getPicklistValues, and getPicklistValuesByRecordType with local schema limits.",
		Files:         []string{"force-app/main/default/lwc/objectInfoProbe/objectInfoProbe.js", "data/accounts.json"},
		Assertions:    []string{"object info payload", "picklist values", "wire payload shape"},
	},
	"visualforce-ui-object-info": {
		Name:          "visualforce-ui-object-info",
		Feature:       "lwc.service.ui-object-info",
		Host:          "visualforce-lightning-out",
		Route:         "/apex/MultiWidgetHost",
		Component:     "objectInfoHost",
		ObjectAPIName: "Account",
		DOM:           `<p class="label">Account</p>`,
		Notes:         "Visualforce Lightning Out c:objectInfoHost proves getObjectInfo through the shared runtime.",
		Files:         []string{"force-app/main/default/lwc/objectInfoHost/objectInfoHost.js", "data/accounts.json"},
		Assertions:    []string{"Visualforce object info wire", "visible text"},
	},
	"ui-related-list": {
		Name:          "ui-related-list",
		Feature:       "lwc.service.ui-related-list",
		Host:          "lightning-shell",
		Route:         "/lwc/preview/component/c/relatedListProbe",
		Component:     "relatedListProbe",
		ObjectAPIName: "Account",
		RecordID:      "001000000000001AAA",
		DOM:           `<section data-probe="related-list"><h2>Related List Probe</h2></section>`,
		Notes:         "Local lightning/uiRelatedListApi getRelatedListRecords returns deterministic child rows from local relationships.",
		Files:         []string{"force-app/main/default/lwc/relatedListProbe/relatedListProbe.js", "data/accounts.json"},
		Assertions:    []string{"related list rows", "parent record id", "wire payload shape"},
	},
	"lds-create-defaults": {
		Name:          "lds-create-defaults",
		Feature:       "lwc.service.lds-create-defaults",
		Host:          "lightning-shell",
		Route:         "/lwc/preview/component/c/recordProbe?feature=lds-create-defaults",
		Component:     "recordProbe",
		ObjectAPIName: "Account",
		DOM:           `<section data-probe="record" data-create-defaults="prepared-local"></section>`,
		Notes:         "Local UI API covers getRecordCreateDefaults plus create and update record-input helpers for common local form flows. Layout metadata remains pending.",
		Files:         []string{"force-app/main/default/lwc/recordProbe/recordProbe.js", "data/accounts.json"},
		Assertions:    []string{"create defaults payload", "object info fields", "record input filtering"},
	},
	"ui-layout": {
		Name:          "ui-layout",
		Feature:       "lwc.service.ui-layout",
		Host:          "lightning-shell",
		Route:         "/lwc/preview/component/c/layoutProbe",
		Component:     "layoutProbe",
		ObjectAPIName: "Account",
		DOM:           `<section data-probe="layout"><h2>Layout Probe</h2></section>`,
		Notes:         "Local lightning/uiLayoutApi getLayout returns the same Record Layout shape as create defaults.",
		Files:         []string{"force-app/main/default/lwc/layoutProbe/layoutProbe.js", "data/accounts.json"},
		Assertions:    []string{"layout sections", "layout fields", "wire payload shape"},
	},
	"lds-mutation": {
		Name:          "lds-mutation",
		Feature:       "lwc.service.lds-mutation",
		Host:          "lightning-shell",
		Route:         "/lwc/preview/component/c/recordProbe?feature=lds-mutation",
		Component:     "recordProbe",
		ObjectAPIName: "Account",
		RecordID:      "001000000000001AAA",
		DOM:           `<section data-probe="record" data-mutation="prepared-local"></section>`,
		Notes:         "Local server and runtime tests cover createRecord, updateRecord, deleteRecord, refreshApex, and notifyRecordUpdateAvailable. Salesforce browser oracle capture remains pending.",
		Files:         []string{"force-app/main/default/lwc/recordProbe/recordProbe.js"},
		Assertions:    []string{"mutation payload shape", "record cache notification"},
	},
	"visualforce-lds-mutation": {
		Name:          "visualforce-lds-mutation",
		Feature:       "lwc.service.lds-mutation",
		Host:          "visualforce-lightning-out",
		Route:         "/apex/MultiWidgetHost",
		Component:     "recordMutationHost",
		ObjectAPIName: "Account",
		DOM:           `<p class="status">mutation complete</p>`,
		Notes:         "Visualforce Lightning Out c:recordMutationHost proves createRecord, updateRecord, and deleteRecord against local org state.",
		Files:         []string{"force-app/main/default/lwc/recordMutationHost/recordMutationHost.js"},
		Assertions:    []string{"Visualforce LDS mutation helpers", "visible text"},
	},
	"navigation": {
		Name:       "navigation",
		Feature:    "lwc.service.navigation",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/component/c/contextProbe?feature=navigation",
		Component:  "contextProbe",
		DOM:        `<section data-probe="context" data-navigation="pending"></section>`,
		Notes:      "Runtime browser tests cover GenerateUrl, Navigate, CurrentPageReference, and local route diagnostics; strict capture proves the shell route loads without browser errors.",
		Files:      []string{"glade.lwc.json", "force-app/main/default/lwc/contextProbe/contextProbe.js"},
		Assertions: []string{"PageReference JSON", "GenerateUrl result"},
	},
	"visualforce-navigation": {
		Name:       "visualforce-navigation",
		Feature:    "lwc.service.navigation",
		Host:       "visualforce-lightning-out",
		Route:      "/apex/MultiWidgetHost",
		Component:  "serviceHost",
		DOM:        `<section data-probe="vf-services"><p class="page-type">standard__webPage</p></section>`,
		Notes:      "Visualforce Lightning Out c:serviceHost proves CurrentPageReference and local navigation diagnostics.",
		Files:      []string{"force-app/main/default/lwc/serviceHost/serviceHost.js", "force-app/main/default/pages/MultiWidgetHost.page"},
		Assertions: []string{"CurrentPageReference", "unsupported navigation diagnostic"},
	},
	"toast": {
		Name:       "toast",
		Feature:    "lwc.service.toast",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/component/c/contextProbe?feature=toast",
		Component:  "contextProbe",
		DOM:        `<section data-probe="context" data-toast="pending"></section>`,
		Notes:      "Runtime browser tests cover ShowToastEvent capture and rendered shell toast text; strict capture proves the shell route loads without browser errors.",
		Files:      []string{"force-app/main/default/lwc/contextProbe/contextProbe.js"},
		Assertions: []string{"toast event detail", "rendered toast text"},
	},
	"visualforce-toast": {
		Name:       "visualforce-toast",
		Feature:    "lwc.service.toast",
		Host:       "visualforce-lightning-out",
		Route:      "/apex/MultiWidgetHost",
		Component:  "serviceHost",
		DOM:        `<section data-probe="vf-services"><p class="toast-title">VF Toast</p></section>`,
		Notes:      "Visualforce Lightning Out c:serviceHost dispatches ShowToastEvent through the shared local runtime.",
		Files:      []string{"force-app/main/default/lwc/serviceHost/serviceHost.js", "force-app/main/default/pages/MultiWidgetHost.page"},
		Assertions: []string{"toast event detail", "visible toast title"},
	},
	"lms": {
		Name:       "lms",
		Feature:    "lwc.service.lms",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/component/c/contextProbe?feature=lms",
		Component:  "contextProbe",
		DOM:        `<section data-probe="context" data-lms="pending"></section>`,
		Notes:      "Runtime browser tests cover local Lightning Message Service publish, subscribe, and unsubscribe; strict capture proves the shell route loads without browser errors.",
		Files:      []string{"force-app/main/default/lwc/contextProbe/contextProbe.js"},
		Assertions: []string{"message channel payload", "subscriber delivery"},
	},
	"visualforce-lms": {
		Name:       "visualforce-lms",
		Feature:    "lwc.service.lms",
		Host:       "visualforce-lightning-out",
		Route:      "/apex/MultiWidgetHost",
		Component:  "serviceHost",
		DOM:        `<section data-probe="vf-services"><p class="message-record">001XX0000000001</p></section>`,
		Notes:      "Visualforce Lightning Out c:serviceHost imports @salesforce/messageChannel metadata and proves local publish and subscribe.",
		Files:      []string{"force-app/main/default/lwc/serviceHost/serviceHost.js", "force-app/main/default/messageChannels/LwcProbe.messageChannel-meta.xml"},
		Assertions: []string{"message channel token", "subscriber delivery"},
	},
	"visualforce-resource-loader": {
		Name:       "visualforce-resource-loader",
		Feature:    "lwc.service.resource-loader",
		Host:       "visualforce-lightning-out",
		Route:      "/apex/MultiWidgetHost",
		Component:  "serviceHost",
		DOM:        `<section data-probe="vf-services"><p class="resource-status">loaded</p></section>`,
		Notes:      "Visualforce Lightning Out c:serviceHost proves platformResourceLoader loadScript/loadStyle against local static resources.",
		Files:      []string{"force-app/main/default/lwc/serviceHost/serviceHost.js", "force-app/main/default/staticresources/ServiceScript.resource", "force-app/main/default/staticresources/ServiceStyles.resource"},
		Assertions: []string{"static resource script", "static resource stylesheet"},
	},
	"community-context": {
		Name:       "community-context",
		Feature:    "lwc.service.community-context",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/community/Partner_Portal/Account",
		Component:  "communityProbe",
		DOM:        `<section data-probe="community"><p data-field="basePath">/partners</p><p data-field="isGuest">true</p></section>`,
		Notes:      "Community shims export basePath, network ID, site ID, guest mode, and supported comm__ navigation URL generation.",
		Files:      []string{"force-app/main/default/lwc/communityProbe/communityProbe.js", "glade.lwc.json"},
		Assertions: []string{"@salesforce/community", "@salesforce/site", "@salesforce/user/isGuest", "comm navigation"},
	},
	"base-components": {
		Name:       "base-components",
		Feature:    "lwc.service.base-components",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/component/c/baseComponentHost",
		Component:  "baseComponentHost",
		DOM:        `<section data-probe="vf-base"><lightning-card>VF Base Card</lightning-card></section>`,
		Notes:      "Practical base shims render common lightning/* modules and cover click, change, submit, datatable rowaction, tab active, and unsupported-attribute diagnostics.",
		Files:      []string{"force-app/main/default/lwc/baseComponentHost/baseComponentHost.html", "force-app/main/default/lwc/baseComponentHost/baseComponentHost.js"},
		Assertions: []string{"public attributes", "visible text", "event payload"},
	},
	"package-phase1-base-components": {
		Name:       "package-phase1-base-components",
		Feature:    "lwc.service.package-phase1-base-components",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/component/c/baseComponentHost?context=packagePhase1BaseComponents",
		Component:  "baseComponentHost",
		DOM:        `<section data-probe="package-phase1-base-components"><lightning-accordion><lightning-accordion-section label="Package Phase 1"></lightning-accordion-section></lightning-accordion></section>`,
		Notes:      "Package-phase1 base shims render the lightning/* modules found in the prioritized LWC corpus package lane.",
		Files:      []string{"force-app/main/default/lwc/baseComponentHost/baseComponentHost.html", "force-app/main/default/lwc/baseComponentHost/baseComponentHost.js", "glade.lwc.json"},
		Assertions: []string{"package-phase1 context", "visible text", "event payload"},
	},
	"phase3-base-components": {
		Name:       "phase3-base-components",
		Feature:    "lwc.service.phase3-base-components",
		Host:       "lightning-shell",
		Route:      "/lwc/preview/component/c/baseComponentHost?context=phase3BaseComponents",
		Component:  "baseComponentHost",
		DOM:        `<section data-probe="phase3-base-components"><lightning-dual-listbox label="Providers"></lightning-dual-listbox><lightning-tree-grid></lightning-tree-grid></section>`,
		Notes:      "Capability Phase 3 base shims render high-use expanded lightning/* modules including dual listbox, select, slider, rich text input, progress, breadcrumbs, tree grid, map, carousel, and email links.",
		Files:      []string{"force-app/main/default/lwc/baseComponentHost/baseComponentHost.html", "force-app/main/default/lwc/baseComponentHost/baseComponentHost.js", "glade.lwc.json"},
		Assertions: []string{"phase3 context", "visible text", "expanded component tags", "event payload"},
	},
	"visualforce-base-components": {
		Name:       "visualforce-base-components",
		Feature:    "lwc.service.base-components",
		Host:       "visualforce-lightning-out",
		Route:      "/apex/MultiWidgetHost",
		Component:  "baseComponentHost",
		DOM:        `<section data-probe="vf-base"><lightning-card>VF Base Card</lightning-card></section>`,
		Notes:      "Visualforce Lightning Out mounts the same practical base component shims through c:baseComponentHost.",
		Files:      []string{"force-app/main/default/lwc/baseComponentHost/baseComponentHost.html", "force-app/main/default/pages/MultiWidgetHost.page"},
		Assertions: []string{"card text", "button text", "input value", "datatable row"},
	},
}

func RunLwcCapture(ctx context.Context, options LwcCaptureOptions) (LwcCaptureReport, error) {
	if err := ctx.Err(); err != nil {
		return LwcCaptureReport{}, err
	}
	options.TargetOrg = strings.TrimSpace(options.TargetOrg)
	if options.TargetOrg == "" {
		return LwcCaptureReport{}, errors.New("--target-org is required")
	}
	if options.Project == "" {
		options.Project = "."
	}
	if options.Runner == nil {
		options.Runner = shellLwcCommandRunner{}
	}
	absProject, err := filepath.Abs(options.Project)
	if err != nil {
		return LwcCaptureReport{}, err
	}
	if (options.BrowserCapture || options.LocalBrowserCapture) && options.Browser == nil {
		options.Browser = newShellLwcBrowserRunner(absProject)
	}
	targets, err := normalizeLwcCaptureTargets(options.Targets)
	if err != nil {
		return LwcCaptureReport{}, err
	}
	hosts := normalizeLwcCaptureList(options.Hosts)
	report := LwcCaptureReport{
		Command:   "glade compat lwc capture",
		TargetOrg: options.TargetOrg,
		Project:   absProject,
		Mode:      lwcCaptureMode(options.LocalBrowserCapture, options.BrowserCapture),
		Hosts:     hosts,
		Artifacts: LwcCaptureArtifacts{Report: options.Out},
	}
	var localCleanup func()
	if options.LocalBrowserCapture && strings.TrimSpace(options.LocalBaseURL) == "" && strings.TrimSpace(options.GladeBin) != "" {
		baseURL, cleanup, err := startLocalLWCCaptureServer(ctx, options.GladeBin, absProject, firstLwcLocalCaptureRoute(targets, hosts))
		if err != nil {
			report.Counts.Fail = len(targets)
			report.OK = false
			return report, err
		}
		options.LocalBaseURL = baseURL
		localCleanup = cleanup
	}
	if localCleanup != nil {
		defer localCleanup()
	}
	if !options.SkipDeploy {
		if err := runLwcCaptureDeploy(ctx, options.Runner, absProject, options.TargetOrg); err != nil {
			report.Counts.Fail = len(targets)
			report.OK = false
			return report, err
		}
		report.Deployed = true
	}
	report.Cases = buildLwcCaptureCases(targets, hosts, options.TargetOrg)
	if options.LocalBrowserCapture {
		captureLwcLocalBrowserEvidence(ctx, &report, options)
	}
	if options.BrowserCapture {
		captureLwcSalesforceBrowserEvidence(ctx, &report, options)
	}
	compareLwcCapturedEvidence(&report)
	applyLwcComparisonStatuses(&report)
	report.Counts.Targets = len(report.Cases)
	for _, c := range report.Cases {
		if c.Status == "prepared" {
			report.Counts.Prepared++
		} else if c.Status == "pass" {
			report.Counts.Pass++
		} else {
			report.Counts.Fail++
		}
	}
	report.OK = report.Counts.Fail == 0
	report.Support = buildLwcSupportRows(report.Cases, hosts, options.Out)
	if options.Out != "" {
		if err := WriteLwcCaptureJSON(options.Out, report); err != nil {
			return report, err
		}
	}
	if (options.BrowserCapture || options.LocalBrowserCapture) && !report.OK {
		return report, fmt.Errorf("LWC browser capture failed for %d target(s)", report.Counts.Fail)
	}
	return report, nil
}

func applyLwcComparisonStatuses(report *LwcCaptureReport) {
	if report == nil {
		return
	}
	for i := range report.Cases {
		if report.Cases[i].Comparison != nil && !report.Cases[i].Comparison.OK {
			report.Cases[i].Status = "fail"
		}
	}
}

func lwcCaptureMode(localBrowserCapture, salesforceBrowserCapture bool) string {
	if localBrowserCapture && salesforceBrowserCapture {
		return "browser-capture"
	}
	if salesforceBrowserCapture {
		return "browser-capture"
	}
	if localBrowserCapture {
		return "local-browser-capture"
	}
	return "fixture-evidence-stubs"
}

func WriteLwcCaptureJSON(path string, report LwcCaptureReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func WriteLwcCaptureText(w io.Writer, report LwcCaptureReport) {
	mode := strings.TrimSpace(report.Mode)
	if mode == "" {
		mode = "fixture-evidence-stubs"
	}
	fmt.Fprintf(w, "prepared %d LWC %s targets: prepared=%d pass=%d fail=%d support=%d artifacts=%s\n", report.Counts.Targets, mode, report.Counts.Prepared, report.Counts.Pass, report.Counts.Fail, len(report.Support), report.Artifacts.Report)
}

func normalizeLwcCaptureTargets(values []string) ([]string, error) {
	targets := normalizeLwcCaptureList(values)
	if len(targets) == 0 {
		return append([]string(nil), defaultLwcCaptureCases...), nil
	}
	for _, target := range targets {
		if _, ok := lwcCaptureTargetDefinitions[target]; !ok {
			return nil, fmt.Errorf("unknown LWC capture target %q", target)
		}
	}
	return targets, nil
}

func normalizeLwcCaptureList(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			result = append(result, part)
		}
	}
	return result
}

func buildLwcCaptureCases(targets, hosts []string, targetOrg string) []LwcCaptureCase {
	result := make([]LwcCaptureCase, 0, len(targets))
	for _, target := range targets {
		def, ok := lwcCaptureTargetDefinitions[target]
		if !ok {
			continue
		}
		host := lwcCaptureHostForTarget(def, hosts)
		if host == "" {
			continue
		}
		result = append(result, LwcCaptureCase{
			Name:      target,
			Feature:   def.Feature,
			Host:      host,
			Status:    "prepared",
			TargetURL: "fixture://lwc/" + target + lwcCaptureHostQuery(host),
			Metadata: LwcCaptureMetadata{
				Route:         def.Route,
				Component:     def.Component,
				Page:          def.Page,
				ObjectAPIName: def.ObjectAPIName,
				RecordID:      def.RecordID,
				App:           def.App,
				Tab:           def.Tab,
				QuickAction:   def.QuickAction,
				Files:         append([]string(nil), def.Files...),
				Assertions:    append([]string(nil), def.Assertions...),
			},
			LocalEvidence: &LwcCaptureEvidence{
				Kind:      "local-dom-stub",
				Source:    "local-fixture",
				Status:    "prepared",
				TargetURL: def.Route,
				DOM:       def.DOM,
				Notes:     "Prepared from fixture metadata; browser capture remains a separate local run.",
			},
			SalesforceEvidence: &LwcCaptureEvidence{
				Kind:      "salesforce-dom-stub",
				Source:    targetOrg,
				Status:    "pending-org-capture",
				TargetURL: lwcSalesforceTargetPath(def),
				DOM:       def.DOM,
				Notes:     lwcSalesforceEvidenceNotes(def),
			},
			Notes: def.Notes,
		})
	}
	return result
}

func lwcSalesforceTargetPath(def lwcCaptureTargetDefinition) string {
	switch def.Name {
	case "url-addressable-component":
		return "/lightning/cmp/c__actionProbe?c__mode=demo"
	case "record-page":
		if def.ObjectAPIName != "" && def.RecordID != "" {
			return "/lightning/r/" + def.ObjectAPIName + "/" + def.RecordID + "/view"
		}
	case "record-quick-action":
		if def.ObjectAPIName != "" && def.RecordID != "" && def.QuickAction != "" {
			return "/lightning/r/" + def.ObjectAPIName + "/" + def.RecordID + "/view?quickAction=" + def.QuickAction
		}
	case "app-page":
		if def.Page == "Sales_Dashboard" && def.App != "" {
			return "/lightning/app/" + lwcSalesforceAppRouteName(def.App) + "/n/Lwc_Probe"
		}
	case "custom-tab":
		if def.Tab != "" {
			if def.App != "" {
				return "/lightning/app/" + lwcSalesforceAppRouteName(def.App) + "/n/" + def.Tab
			}
			return "/lightning/n/" + def.Tab
		}
	case "visualforce-lightning-out", "visualforce-base-components":
		return def.Route
	}
	if strings.HasPrefix(def.Route, "/apex/") || strings.HasPrefix(def.Route, "/lightning/") {
		return def.Route
	}
	return "salesforce://local-only" + def.Route
}

func lwcSalesforceAppRouteName(app string) string {
	app = strings.TrimSpace(app)
	if app == "" {
		return ""
	}
	if strings.HasPrefix(app, "standard__") || strings.HasPrefix(app, "c__") {
		return app
	}
	if strings.EqualFold(app, "Sales") {
		return "standard__Sales"
	}
	return "c__" + app
}

func lwcSalesforceEvidenceNotes(def lwcCaptureTargetDefinition) string {
	path := lwcSalesforceTargetPath(def)
	if strings.HasPrefix(path, "salesforce://local-only") {
		return "No direct Salesforce URL exists for this Glade local shell route; browser oracle must compare the nearest deployed component or page target."
	}
	return "Stable Salesforce path only; sf org open frontdoor login URLs are used only in memory and are not written to reports."
}

func captureLwcLocalBrowserEvidence(ctx context.Context, report *LwcCaptureReport, options LwcCaptureOptions) {
	baseURL := strings.TrimSpace(options.LocalBaseURL)
	if baseURL == "" {
		for i := range report.Cases {
			caseReport := &report.Cases[i]
			caseReport.Status = "fail"
			if caseReport.LocalEvidence != nil {
				caseReport.LocalEvidence = &LwcCaptureEvidence{
					Kind:      "local-browser-dom",
					Source:    "local-glade",
					Status:    "capture-failed",
					TargetURL: caseReport.LocalEvidence.TargetURL,
					Notes:     "--local-base-url is required for local browser capture",
				}
			}
			caseReport.PageErrors = append(caseReport.PageErrors, "--local-base-url is required for local browser capture")
		}
		return
	}
	for i := range report.Cases {
		caseReport := &report.Cases[i]
		if caseReport.LocalEvidence == nil {
			continue
		}
		stablePath := strings.TrimSpace(caseReport.LocalEvidence.TargetURL)
		if stablePath == "" {
			continue
		}
		captureURL := joinLwcLocalCaptureURL(baseURL, stablePath)
		capture, err := options.Browser.CaptureDOM(ctx, captureURL)
		if err != nil {
			caseReport.Status = "fail"
			caseReport.LocalEvidence = &LwcCaptureEvidence{
				Kind:      "local-browser-dom",
				Source:    "local-glade",
				Status:    "capture-failed",
				TargetURL: stablePath,
				Notes:     err.Error(),
			}
			caseReport.PageErrors = append(caseReport.PageErrors, err.Error())
			continue
		}
		caseReport.ConsoleErrors = append(caseReport.ConsoleErrors, capture.ConsoleErrors...)
		caseReport.PageErrors = append(caseReport.PageErrors, capture.PageErrors...)
		applyLwcBrowserCaptureStatus(caseReport, &capture)
		caseReport.LocalEvidence = &LwcCaptureEvidence{
			Kind:          "local-browser-dom",
			Source:        "local-glade",
			Status:        "captured",
			TargetURL:     stablePath,
			DOM:           capture.DOM,
			ConsoleErrors: append([]string(nil), capture.ConsoleErrors...),
			PageErrors:    append([]string(nil), capture.PageErrors...),
			Notes:         "Captured from the local Glade LWC shell; the transient server URL is not written to this report.",
		}
	}
}

func joinLwcLocalCaptureURL(baseURL, stablePath string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if stablePath == "" {
		return baseURL
	}
	if !strings.HasPrefix(stablePath, "/") {
		stablePath = "/" + stablePath
	}
	return baseURL + stablePath
}

func captureLwcSalesforceBrowserEvidence(ctx context.Context, report *LwcCaptureReport, options LwcCaptureOptions) {
	for i := range report.Cases {
		caseReport := &report.Cases[i]
		if caseReport.SalesforceEvidence == nil {
			continue
		}
		stablePath := strings.TrimSpace(caseReport.SalesforceEvidence.TargetURL)
		if stablePath == "" || strings.HasPrefix(stablePath, "salesforce://local-only") {
			continue
		}
		openURL, err := openLwcSalesforceBrowserURL(ctx, options.Runner, options.TargetOrg, stablePath)
		if err != nil {
			caseReport.Status = "fail"
			caseReport.SalesforceEvidence = &LwcCaptureEvidence{
				Kind:      "salesforce-browser-dom",
				Source:    options.TargetOrg,
				Status:    "capture-failed",
				TargetURL: stablePath,
				Notes:     err.Error(),
			}
			caseReport.PageErrors = append(caseReport.PageErrors, err.Error())
			continue
		}
		capture, err := options.Browser.CaptureDOM(ctx, openURL)
		if err != nil {
			caseReport.Status = "fail"
			caseReport.SalesforceEvidence = &LwcCaptureEvidence{
				Kind:      "salesforce-browser-dom",
				Source:    options.TargetOrg,
				Status:    "capture-failed",
				TargetURL: stablePath,
				Notes:     err.Error(),
			}
			caseReport.PageErrors = append(caseReport.PageErrors, err.Error())
			continue
		}
		caseReport.ConsoleErrors = append(caseReport.ConsoleErrors, capture.ConsoleErrors...)
		caseReport.PageErrors = append(caseReport.PageErrors, capture.PageErrors...)
		applyLwcBrowserCaptureStatus(caseReport, &capture)
		caseReport.SalesforceEvidence = &LwcCaptureEvidence{
			Kind:          "salesforce-browser-dom",
			Source:        options.TargetOrg,
			Status:        "captured",
			TargetURL:     stablePath,
			DOM:           capture.DOM,
			ConsoleErrors: append([]string(nil), capture.ConsoleErrors...),
			PageErrors:    append([]string(nil), capture.PageErrors...),
			Notes:         "Captured through an authenticated browser session; the sf org open URL is not written to this report.",
		}
	}
}

func applyLwcBrowserCaptureStatus(caseReport *LwcCaptureCase, capture *LwcBrowserCapture) {
	if capture.HTTPStatus >= 400 {
		message := fmt.Sprintf("browser capture returned HTTP status %d", capture.HTTPStatus)
		capture.PageErrors = append(capture.PageErrors, message)
		caseReport.PageErrors = append(caseReport.PageErrors, message)
		caseReport.Status = "fail"
		return
	}
	if selector := expectedLwcCaptureSelector(*caseReport); selector != "" && !strings.Contains(strings.ToLower(capture.DOM), "<"+selector) {
		message := "browser capture missing expected component selector " + selector
		capture.PageErrors = append(capture.PageErrors, message)
		caseReport.PageErrors = append(caseReport.PageErrors, message)
		caseReport.Status = "fail"
		return
	}
	if len(capture.ConsoleErrors) > 0 || len(capture.PageErrors) > 0 {
		caseReport.Status = "fail"
		return
	}
	if caseReport.Status != "fail" {
		caseReport.Status = "pass"
	}
}

func expectedLwcCaptureSelector(caseReport LwcCaptureCase) string {
	component := strings.TrimSpace(caseReport.Metadata.Component)
	if component == "" {
		return ""
	}
	return "c-" + lwcComponentNameToKebab(component)
}

func lwcComponentNameToKebab(name string) string {
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func openLwcSalesforceBrowserURL(ctx context.Context, runner LwcCommandRunner, targetOrg, stablePath string) (string, error) {
	output, err := runner.Run(ctx, "sf", "org", "open", "--target-org", targetOrg, "--url-only", "--path", stablePath, "--json")
	if err != nil {
		return "", fmt.Errorf("sf org open failed for %s: %w: %s", stablePath, err, trimCommandOutput(output))
	}
	var parsed struct {
		Status int `json:"status"`
		Result struct {
			URL string `json:"url"`
		} `json:"result"`
	}
	if err := json.Unmarshal(extractLwcJSONOutput(output), &parsed); err != nil {
		return "", fmt.Errorf("sf org open returned invalid JSON for %s: %w", stablePath, err)
	}
	if parsed.Status != 0 {
		return "", fmt.Errorf("sf org open returned status %d for %s", parsed.Status, stablePath)
	}
	if strings.TrimSpace(parsed.Result.URL) == "" {
		return "", fmt.Errorf("sf org open returned no URL for %s", stablePath)
	}
	return parsed.Result.URL, nil
}

func extractLwcJSONOutput(output []byte) []byte {
	trimmed := strings.TrimSpace(string(output))
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return []byte(trimmed)
	}
	start := strings.Index(trimmed, "{")
	if start < 0 {
		start = strings.Index(trimmed, "[")
	}
	if start < 0 {
		return []byte(trimmed)
	}
	return []byte(trimmed[start:])
}

func lwcCaptureHostForTarget(def lwcCaptureTargetDefinition, hosts []string) string {
	if len(hosts) == 0 || containsLwcCaptureHost(hosts, def.Host) {
		return def.Host
	}
	return ""
}

func containsLwcCaptureHost(hosts []string, want string) bool {
	for _, host := range hosts {
		if host == want {
			return true
		}
	}
	return false
}

func lwcCaptureHostQuery(host string) string {
	if host == "" {
		return ""
	}
	return "?host=" + host
}

func buildLwcSupportRows(cases []LwcCaptureCase, requestedHosts []string, reportPath string) []LwcSupportRow {
	hostSet := map[string]bool{}
	for _, host := range requestedHosts {
		if strings.TrimSpace(host) != "" {
			hostSet[host] = true
		}
	}
	for _, c := range cases {
		if c.Host != "" {
			hostSet[c.Host] = true
		}
	}
	var hosts []string
	for host := range hostSet {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	rows := make([]LwcSupportRow, 0, len(hosts)+len(cases))
	for _, host := range hosts {
		rows = append(rows, LwcSupportRow{
			Feature:  "lwc.host." + host,
			Host:     host,
			Status:   lwcSupportStatusForHost(host, cases),
			Evidence: lwcSupportEvidenceRef(reportPath, "lwc.host."+host, host),
			Notes:    lwcSupportHostNotes(host, cases),
		})
	}
	for _, c := range cases {
		def, ok := lwcCaptureTargetDefinitions[c.Name]
		if !ok {
			feature := strings.TrimSpace(c.Feature)
			if feature == "" {
				feature = "lwc.target." + strings.TrimSpace(c.Name)
			}
			rows = append(rows, LwcSupportRow{
				Feature:  feature,
				Host:     c.Host,
				Status:   "failed",
				Evidence: lwcSupportEvidenceRef(reportPath, feature, c.Host),
				Notes:    "Unknown LWC capture target; support evidence cannot be classified.",
			})
			continue
		}
		rows = append(rows, LwcSupportRow{
			Feature:  c.Feature,
			Host:     c.Host,
			Status:   lwcSupportStatusForCase(c),
			Evidence: lwcSupportEvidenceRef(reportPath, c.Feature, c.Host),
			Notes:    def.Notes,
		})
	}
	return rows
}

func lwcSupportStatusForHost(host string, cases []LwcCaptureCase) string {
	status := "prepared-local"
	for _, c := range cases {
		if c.Host != host {
			continue
		}
		caseStatus := lwcSupportStatusForCase(c)
		switch caseStatus {
		case "failed":
			return "failed"
		case "supported":
			status = "supported"
		case "supported-local":
			if status != "supported" {
				status = "supported-local"
			}
		case "supported-salesforce":
			if status == "prepared-local" {
				status = "supported-salesforce"
			}
		}
	}
	return status
}

func lwcSupportHostNotes(host string, cases []LwcCaptureCase) string {
	switch lwcSupportStatusForHost(host, cases) {
	case "supported":
		return "Host lane has passing local and Salesforce browser evidence for its captured targets."
	case "supported-local":
		return "Host lane has passing local browser evidence; live Salesforce parity remains target-specific."
	case "supported-salesforce":
		return "Host lane has passing Salesforce browser evidence; local browser evidence was not captured in this report."
	case "failed":
		return "One or more host targets failed browser capture or comparison."
	default:
		return "Host lane has a prepared target manifest and DOM stub entry."
	}
}

func lwcSupportStatusForCase(c LwcCaptureCase) string {
	if c.Status == "fail" {
		return "failed"
	}
	localCaptured := c.LocalEvidence != nil && c.LocalEvidence.Status == "captured"
	salesforceCaptured := c.SalesforceEvidence != nil && c.SalesforceEvidence.Status == "captured"
	if c.Comparison != nil {
		if c.Comparison.OK && localCaptured && salesforceCaptured {
			return "supported"
		}
		return "failed"
	}
	if localCaptured {
		return "supported-local"
	}
	if salesforceCaptured {
		return "supported-salesforce"
	}
	if lwcCaseRequiresOrgSetup(c) {
		return "org-setup-required"
	}
	return "prepared-local"
}

func lwcCaseRequiresOrgSetup(c LwcCaptureCase) bool {
	if c.SalesforceEvidence == nil || !strings.HasPrefix(strings.TrimSpace(c.SalesforceEvidence.TargetURL), "salesforce://local-only") {
		return false
	}
	switch c.Name {
	case "community-page", "community-component", "community-context":
		return true
	}
	return strings.Contains(c.Feature, ".community-") || strings.Contains(c.Feature, "community-context")
}

func lwcSupportEvidenceRef(reportPath, feature, host string) string {
	if strings.TrimSpace(reportPath) != "" {
		return reportPath + "#/support/" + feature + "/" + host
	}
	return "capture://lwc/support/" + feature + lwcCaptureHostQuery(host)
}

func runLwcCaptureDeploy(ctx context.Context, runner LwcCommandRunner, project, targetOrg string) error {
	deployProject, cleanup, err := prepareLwcCaptureDeployProject(project)
	if err != nil {
		return err
	}
	defer cleanup()
	output, err := runLwcCommandInDir(ctx, runner, deployProject, "sf", "project", "deploy", "start", "--target-org", targetOrg, "--source-dir", ".", "--ignore-conflicts", "--json")
	if err != nil {
		return fmt.Errorf("sf project deploy start failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var status struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal(output, &status); err == nil && status.Status != 0 {
		return fmt.Errorf("sf project deploy start returned status %d: %s", status.Status, strings.TrimSpace(string(output)))
	}
	for _, name := range lwcCapturePermissionSetNames(deployProject) {
		output, err := runLwcCommandInDir(ctx, runner, deployProject, "sf", "org", "assign", "permset", "--name", name, "--target-org", targetOrg, "--json")
		if err != nil {
			if isLwcDuplicatePermissionSetAssignment(output) {
				continue
			}
			return fmt.Errorf("sf org assign permset failed for %s: %w: %s", name, err, strings.TrimSpace(string(output)))
		}
		var assignStatus struct {
			Status int `json:"status"`
		}
		if err := json.Unmarshal(extractLwcJSONOutput(output), &assignStatus); err == nil && assignStatus.Status != 0 {
			return fmt.Errorf("sf org assign permset returned status %d for %s: %s", assignStatus.Status, name, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func isLwcDuplicatePermissionSetAssignment(output []byte) bool {
	return strings.Contains(string(output), "Duplicate PermissionSetAssignment")
}

func lwcCapturePermissionSetNames(project string) []string {
	var names []string
	for _, dir := range []string{
		filepath.Join(project, "force-app", "main", "default", "permissionsets"),
		filepath.Join(project, "force-app", "main", "default", "permissionset"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			for _, suffix := range []string{".permissionset-meta.xml", ".permissionset"} {
				if strings.HasSuffix(name, suffix) {
					names = append(names, strings.TrimSuffix(name, suffix))
					break
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

type shellLwcCommandRunner struct{}

func (shellLwcCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func (shellLwcCommandRunner) RunInDir(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PWD="+dir)
	return cmd.CombinedOutput()
}

func runLwcCommandInDir(ctx context.Context, runner LwcCommandRunner, dir, name string, args ...string) ([]byte, error) {
	if runnerWithDir, ok := runner.(lwcCommandRunnerWithDir); ok {
		return runnerWithDir.RunInDir(ctx, dir, name, args...)
	}
	return runner.Run(ctx, name, args...)
}

func prepareLwcCaptureDeployProject(project string) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "glade-lwc-capture-deploy-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	err = filepath.WalkDir(project, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(project, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(tmp, rel), 0o755)
		}
		if shouldSkipLwcCaptureDeployFile(rel) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(tmp, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode().Perm())
	})
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return tmp, cleanup, nil
}

func shouldSkipLwcCaptureDeployFile(rel string) bool {
	base := filepath.Base(rel)
	if !strings.HasSuffix(base, ".quickAction-meta.xml") && !strings.HasSuffix(base, ".quickaction-meta.xml") {
		return false
	}
	if !strings.Contains(filepath.ToSlash(rel), "/quickActions/") {
		return false
	}
	name := strings.TrimSuffix(strings.TrimSuffix(base, ".quickAction-meta.xml"), ".quickaction-meta.xml")
	return !strings.Contains(name, ".")
}

func LwcCaptureCaseNames() []string {
	names := append([]string(nil), defaultLwcCaptureCases...)
	sort.Strings(names)
	return names
}
