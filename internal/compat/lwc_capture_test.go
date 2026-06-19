package compat

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeLwcCommandRunner struct {
	calls   []fakeLwcCommandCall
	output  []byte
	outputs [][]byte
	errors  []error
}

type fakeLwcCommandCall struct {
	name string
	args []string
}

func (r *fakeLwcCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, fakeLwcCommandCall{name: name, args: append([]string(nil), args...)})
	if r.output != nil {
		return append([]byte(nil), r.output...), nil
	}
	return []byte(`{"status":0,"result":{"url":"https://example.my.salesforce.com/secur/frontdoor.jsp?sid=SECRET&otp=ONE-TIME&startURL=%2Flightning%2Fcmp%2Fc__actionProbe%3Fc__mode%3Ddemo"}}`), nil
}

func (r *fakeLwcCommandRunner) RunInDir(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	callArgs := append([]string{"dir=" + dir}, args...)
	r.calls = append(r.calls, fakeLwcCommandCall{name: name, args: callArgs})
	if len(r.outputs) > 0 {
		output := r.outputs[0]
		r.outputs = r.outputs[1:]
		var err error
		if len(r.errors) > 0 {
			err = r.errors[0]
			r.errors = r.errors[1:]
		}
		return append([]byte(nil), output...), err
	}
	return []byte(`{"status":0}`), nil
}

type fakeLwcBrowserRunner struct {
	urls          []string
	err           error
	domByURL      map[string]string
	salesforceDOM string
	consoleErrors []string
	pageErrors    []string
	httpStatus    int
}

func (r *fakeLwcBrowserRunner) CaptureDOM(ctx context.Context, url string) (LwcBrowserCapture, error) {
	r.urls = append(r.urls, url)
	if r.err != nil {
		return LwcBrowserCapture{}, r.err
	}
	if r.domByURL != nil {
		if dom, ok := r.domByURL[url]; ok {
			return r.capture(dom), nil
		}
	}
	if r.salesforceDOM != "" && strings.Contains(url, "frontdoor.jsp") {
		return r.capture(r.salesforceDOM), nil
	}
	switch {
	case strings.Contains(url, "Lwc_Probe"):
		return r.capture(`<main><c-context-probe>App Context</c-context-probe></main>`), nil
	case strings.Contains(url, "actionProbe"):
		return r.capture(`<main><c-action-probe>Action Probe</c-action-probe></main>`), nil
	case strings.Contains(url, "/lwc/preview/tab/Lwc_Probe") || strings.Contains(url, "/lightning/app/"):
		return r.capture(`<main><c-context-probe>App Context</c-context-probe></main>`), nil
	case strings.Contains(url, "/lwc/preview/app/Sales_Dashboard"):
		return r.capture(`<main><c-wire-probe>Wire Probe</c-wire-probe></main>`), nil
	case strings.Contains(url, "/lwc/preview/record/"):
		return r.capture(`<main><c-record-probe>Record Probe</c-record-probe></main>`), nil
	case strings.Contains(url, "/lwc/preview/component/c/recordProbe"):
		return r.capture(`<main><c-record-probe>Record Probe</c-record-probe></main>`), nil
	case strings.Contains(url, "/lwc/preview/component/c/wireProbe"):
		return r.capture(`<main><c-wire-probe>Wire Probe</c-wire-probe></main>`), nil
	case strings.Contains(url, "/lwc/preview/component/c/objectInfoProbe"):
		return r.capture(`<main><c-object-info-probe>Object Info Probe</c-object-info-probe></main>`), nil
	case strings.Contains(url, "/lwc/preview/component/c/relatedListProbe"):
		return r.capture(`<main><c-related-list-probe>Related List Probe</c-related-list-probe></main>`), nil
	case strings.Contains(url, "/lwc/preview/component/c/layoutProbe"):
		return r.capture(`<main><c-layout-probe>Layout Probe</c-layout-probe></main>`), nil
	case strings.Contains(url, "/lwc/preview/community/"):
		return r.capture(`<main><c-community-theme-layout><div id="glade-lwc-main-0"><c-community-probe>Community Probe /partners 0DB000000000001 0DM000000000001 true comm__namedPage /lwc/preview/community/Partner_Portal/Account?c__view=summary</c-community-probe></div></c-community-theme-layout></main>`), nil
	case strings.Contains(url, "/lwc/preview/component/c/baseComponentHost") || strings.Contains(url, "/apex/MultiWidgetHost"):
		return r.capture(`<main><c-base-component-host>Base Components</c-base-component-host></main>`), nil
	case strings.Contains(url, "/apex/LwcShellProbe") || strings.Contains(url, "/lwc/preview/component/c/contextProbe"):
		return r.capture(`<main><c-context-probe>Context Probe</c-context-probe></main>`), nil
	}
	return r.capture(`<main><c-action-probe>Action Probe</c-action-probe></main>`), nil
}

func (r *fakeLwcBrowserRunner) capture(dom string) LwcBrowserCapture {
	return LwcBrowserCapture{
		DOM:           dom,
		ConsoleErrors: append([]string(nil), r.consoleErrors...),
		PageErrors:    append([]string(nil), r.pageErrors...),
		HTTPStatus:    r.httpStatus,
	}
}

func TestRunLWCCaptureBrowserCaptureAcceptsSalesforceWarningBanner(t *testing.T) {
	root := t.TempDir()
	runner := &fakeLwcCommandRunner{
		output: []byte(" ›   Warning: @salesforce/cli update available.\n" + `{"status":0,"result":{"url":"https://example.my.salesforce.com/secur/frontdoor.jsp?sid=SECRET&otp=ONE-TIME&startURL=%2Flightning%2Fn%2FLwc_Probe"}}`),
	}
	browser := &fakeLwcBrowserRunner{}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:        root,
		TargetOrg:      "oaer-probe-max",
		Targets:        []string{"custom-tab"},
		SkipDeploy:     true,
		BrowserCapture: true,
		Runner:         runner,
		Browser:        browser,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := findLwcCaptureCase(t, report, "custom-tab")
	if c.SalesforceEvidence == nil || c.SalesforceEvidence.Status != "captured" {
		t.Fatalf("salesforce evidence = %#v", c.SalesforceEvidence)
	}
	if len(browser.urls) != 1 || !strings.Contains(browser.urls[0], "frontdoor.jsp") {
		t.Fatalf("browser urls = %#v", browser.urls)
	}
}

func TestRunLWCCaptureBrowserCaptureReturnsErrorWhenCaptureFails(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "capture.json")
	runner := &fakeLwcCommandRunner{}
	browser := &fakeLwcBrowserRunner{err: os.ErrPermission}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:        root,
		TargetOrg:      "oaer-probe-max",
		Targets:        []string{"url-addressable-component"},
		Out:            out,
		SkipDeploy:     true,
		BrowserCapture: true,
		Runner:         runner,
		Browser:        browser,
	})
	if err == nil || !strings.Contains(err.Error(), "LWC browser capture failed for 1 target") {
		t.Fatalf("err = %v", err)
	}
	if report.OK {
		t.Fatalf("report ok = true")
	}
	c := findLwcCaptureCase(t, report, "url-addressable-component")
	if c.SalesforceEvidence == nil || c.SalesforceEvidence.Status != "capture-failed" {
		t.Fatalf("salesforce evidence = %#v", c.SalesforceEvidence)
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Fatalf("report was not written: %v", statErr)
	}
}

func TestRunLWCCaptureBrowserCaptureUsesSanitizedSalesforcePath(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "capture.json")
	runner := &fakeLwcCommandRunner{}
	browser := &fakeLwcBrowserRunner{}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:        root,
		TargetOrg:      "oaer-probe-max",
		Targets:        []string{"url-addressable-component"},
		Out:            out,
		SkipDeploy:     true,
		BrowserCapture: true,
		Runner:         runner,
		Browser:        browser,
	})
	if err != nil {
		t.Fatal(err)
	}

	if report.Mode != "browser-capture" {
		t.Fatalf("mode = %q", report.Mode)
	}
	if report.Counts.Targets != 1 || report.Counts.Pass != 1 || report.Counts.Prepared != 0 || report.Counts.Fail != 0 {
		t.Fatalf("counts = %#v", report.Counts)
	}
	c := findLwcCaptureCase(t, report, "url-addressable-component")
	if c.Status != "pass" {
		t.Fatalf("status = %q", c.Status)
	}
	if c.SalesforceEvidence == nil {
		t.Fatalf("missing salesforce evidence")
	}
	if c.SalesforceEvidence.Kind != "salesforce-browser-dom" || c.SalesforceEvidence.Status != "captured" {
		t.Fatalf("salesforce evidence = %#v", c.SalesforceEvidence)
	}
	if c.SalesforceEvidence.TargetURL != "/lightning/cmp/c__actionProbe?c__mode=demo" {
		t.Fatalf("target url = %q", c.SalesforceEvidence.TargetURL)
	}
	if !strings.Contains(c.SalesforceEvidence.DOM, "Action Probe") {
		t.Fatalf("dom = %q", c.SalesforceEvidence.DOM)
	}
	if len(browser.urls) != 1 || !strings.Contains(browser.urls[0], "frontdoor.jsp") {
		t.Fatalf("browser urls = %#v", browser.urls)
	}
	if strings.Contains(c.SalesforceEvidence.TargetURL, "frontdoor.jsp") || strings.Contains(c.SalesforceEvidence.TargetURL, "otp=") {
		t.Fatalf("frontdoor leaked into target url: %#v", c.SalesforceEvidence)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "frontdoor.jsp") || strings.Contains(string(data), "otp=") || strings.Contains(string(data), "SECRET") {
		t.Fatalf("frontdoor leaked into json: %s", data)
	}
	if len(runner.calls) != 1 || runner.calls[0].name != "sf" || !reflect.DeepEqual(runner.calls[0].args, []string{"org", "open", "--target-org", "oaer-probe-max", "--url-only", "--path", "/lightning/cmp/c__actionProbe?c__mode=demo", "--json"}) {
		t.Fatalf("runner calls = %#v", runner.calls)
	}
}

func TestRunLWCCaptureLocalBrowserCaptureUsesStableLocalRoute(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "capture.json")
	browser := &fakeLwcBrowserRunner{}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:             root,
		TargetOrg:           "oaer-probe-max",
		Targets:             []string{"custom-tab"},
		Out:                 out,
		SkipDeploy:          true,
		LocalBrowserCapture: true,
		LocalBaseURL:        "http://127.0.0.1:34567",
		Browser:             browser,
	})
	if err != nil {
		t.Fatal(err)
	}

	if report.Mode != "local-browser-capture" {
		t.Fatalf("mode = %q", report.Mode)
	}
	if report.Counts.Targets != 1 || report.Counts.Pass != 1 || report.Counts.Prepared != 0 || report.Counts.Fail != 0 {
		t.Fatalf("counts = %#v", report.Counts)
	}
	c := findLwcCaptureCase(t, report, "custom-tab")
	if c.Status != "pass" {
		t.Fatalf("status = %q", c.Status)
	}
	if c.LocalEvidence == nil || c.LocalEvidence.Kind != "local-browser-dom" || c.LocalEvidence.Status != "captured" {
		t.Fatalf("local evidence = %#v", c.LocalEvidence)
	}
	if c.LocalEvidence.TargetURL != "/lwc/preview/tab/Lwc_Probe" {
		t.Fatalf("local target = %q", c.LocalEvidence.TargetURL)
	}
	if len(browser.urls) != 1 || browser.urls[0] != "http://127.0.0.1:34567/lwc/preview/tab/Lwc_Probe" {
		t.Fatalf("browser urls = %#v", browser.urls)
	}
	if c.SalesforceEvidence == nil || c.SalesforceEvidence.Status != "pending-org-capture" {
		t.Fatalf("salesforce evidence should stay prepared: %#v", c.SalesforceEvidence)
	}
	assertSupportRow(t, report, "lwc.host.lightning-shell", "lightning-shell", "supported-local")
	assertSupportRow(t, report, "lwc.target.custom-tab", "lightning-shell", "supported-local")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"local-browser-dom"`) {
		t.Fatalf("capture json = %s", data)
	}
}

func TestRunLWCCaptureLocalBrowserCaptureFailsOnBrowserErrors(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "capture.json")
	browser := &fakeLwcBrowserRunner{
		consoleErrors: []string{"console from browser"},
		pageErrors:    []string{"page from browser"},
	}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:             root,
		TargetOrg:           "oaer-probe-max",
		Targets:             []string{"custom-tab"},
		Out:                 out,
		SkipDeploy:          true,
		LocalBrowserCapture: true,
		LocalBaseURL:        "http://127.0.0.1:34567",
		Browser:             browser,
	})
	if err == nil || !strings.Contains(err.Error(), "LWC browser capture failed for 1 target") {
		t.Fatalf("err = %v", err)
	}
	if report.OK || report.Counts.Fail != 1 || report.Counts.Pass != 0 {
		t.Fatalf("counts/ok = %#v ok=%t", report.Counts, report.OK)
	}
	c := findLwcCaptureCase(t, report, "custom-tab")
	if c.Status != "fail" {
		t.Fatalf("status = %q", c.Status)
	}
	if !reflect.DeepEqual(c.ConsoleErrors, []string{"console from browser"}) || !reflect.DeepEqual(c.PageErrors, []string{"page from browser"}) {
		t.Fatalf("case errors = console %#v page %#v", c.ConsoleErrors, c.PageErrors)
	}
	if c.LocalEvidence == nil || !reflect.DeepEqual(c.LocalEvidence.ConsoleErrors, []string{"console from browser"}) || !reflect.DeepEqual(c.LocalEvidence.PageErrors, []string{"page from browser"}) {
		t.Fatalf("local evidence = %#v", c.LocalEvidence)
	}
	data, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(data), `"console from browser"`) || !strings.Contains(string(data), `"page from browser"`) {
		t.Fatalf("capture json = %s", data)
	}
}

func TestRunLWCCaptureLocalBrowserCaptureFailsOnHTTPErrorStatus(t *testing.T) {
	root := t.TempDir()
	browser := &fakeLwcBrowserRunner{
		httpStatus: 404,
		domByURL: map[string]string{
			"http://127.0.0.1:34567/lwc/preview/tab/Lwc_Probe": `<main>not found</main>`,
		},
	}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:             root,
		TargetOrg:           "oaer-probe-max",
		Targets:             []string{"custom-tab"},
		SkipDeploy:          true,
		LocalBrowserCapture: true,
		LocalBaseURL:        "http://127.0.0.1:34567",
		Browser:             browser,
	})
	if err == nil || !strings.Contains(err.Error(), "LWC browser capture failed for 1 target") {
		t.Fatalf("err = %v", err)
	}
	c := findLwcCaptureCase(t, report, "custom-tab")
	if c.Status != "fail" || c.LocalEvidence == nil || !hasString(c.LocalEvidence.PageErrors, "browser capture returned HTTP status 404") {
		t.Fatalf("case = %#v", c)
	}
}

func TestRunLWCCaptureLocalBrowserCaptureFailsWhenExpectedComponentMissing(t *testing.T) {
	root := t.TempDir()
	browser := &fakeLwcBrowserRunner{
		domByURL: map[string]string{
			"http://127.0.0.1:34567/lwc/preview/tab/Lwc_Probe": `<main><p>shell only</p></main>`,
		},
	}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:             root,
		TargetOrg:           "oaer-probe-max",
		Targets:             []string{"custom-tab"},
		SkipDeploy:          true,
		LocalBrowserCapture: true,
		LocalBaseURL:        "http://127.0.0.1:34567",
		Browser:             browser,
	})
	if err == nil || !strings.Contains(err.Error(), "LWC browser capture failed for 1 target") {
		t.Fatalf("err = %v", err)
	}
	c := findLwcCaptureCase(t, report, "custom-tab")
	if c.Status != "fail" || c.LocalEvidence == nil || !hasString(c.LocalEvidence.PageErrors, "browser capture missing expected component selector c-context-probe") {
		t.Fatalf("case = %#v", c)
	}
}

func TestRunLWCCaptureSalesforceBrowserCaptureFailsOnBrowserErrors(t *testing.T) {
	root := t.TempDir()
	runner := &fakeLwcCommandRunner{}
	browser := &fakeLwcBrowserRunner{
		consoleErrors: []string{"salesforce console from browser"},
		pageErrors:    []string{"salesforce page from browser"},
	}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:        root,
		TargetOrg:      "oaer-probe-max",
		Targets:        []string{"url-addressable-component"},
		SkipDeploy:     true,
		BrowserCapture: true,
		Runner:         runner,
		Browser:        browser,
	})
	if err == nil || !strings.Contains(err.Error(), "LWC browser capture failed for 1 target") {
		t.Fatalf("err = %v", err)
	}
	if report.OK || report.Counts.Fail != 1 || report.Counts.Pass != 0 {
		t.Fatalf("counts/ok = %#v ok=%t", report.Counts, report.OK)
	}
	c := findLwcCaptureCase(t, report, "url-addressable-component")
	if c.Status != "fail" {
		t.Fatalf("status = %q", c.Status)
	}
	if !reflect.DeepEqual(c.ConsoleErrors, []string{"salesforce console from browser"}) || !reflect.DeepEqual(c.PageErrors, []string{"salesforce page from browser"}) {
		t.Fatalf("case errors = console %#v page %#v", c.ConsoleErrors, c.PageErrors)
	}
	if c.SalesforceEvidence == nil || !reflect.DeepEqual(c.SalesforceEvidence.ConsoleErrors, []string{"salesforce console from browser"}) || !reflect.DeepEqual(c.SalesforceEvidence.PageErrors, []string{"salesforce page from browser"}) {
		t.Fatalf("salesforce evidence = %#v", c.SalesforceEvidence)
	}
}

func TestRunLWCCaptureLocalBrowserCaptureStartsGladeDevLWC(t *testing.T) {
	root := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	termFile := filepath.Join(t.TempDir(), "terminated.txt")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_LWC", "1")
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_LWC_ARGS", argsFile)
	t.Setenv("GLADE_TOOLS_FAKE_GLADE_LWC_TERM", termFile)
	browser := &fakeLwcBrowserRunner{}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:             root,
		TargetOrg:           "oaer-probe-max",
		Targets:             []string{"url-addressable-component"},
		SkipDeploy:          true,
		LocalBrowserCapture: true,
		GladeBin:            os.Args[0],
		Browser:             browser,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK {
		t.Fatalf("report ok = false: %#v", report)
	}
	if len(browser.urls) != 1 || !strings.Contains(browser.urls[0], "/lwc/preview/cmp/c/actionProbe?c__mode=demo") {
		t.Fatalf("browser urls = %#v", browser.urls)
	}
	argsData, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dev", "lwc", "--project", root, "--addr", "--ready-file"} {
		if !strings.Contains(string(argsData), want) {
			t.Fatalf("args missing %q: %s", want, argsData)
		}
	}
	waitForFile(t, termFile)
}

func TestRunLWCCaptureComparesLocalAndSalesforceBrowserEvidence(t *testing.T) {
	root := t.TempDir()
	runner := &fakeLwcCommandRunner{}
	browser := &fakeLwcBrowserRunner{}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:             root,
		TargetOrg:           "oaer-probe-max",
		Targets:             []string{"url-addressable-component"},
		SkipDeploy:          true,
		LocalBrowserCapture: true,
		LocalBaseURL:        "http://127.0.0.1:34567",
		BrowserCapture:      true,
		Runner:              runner,
		Browser:             browser,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := findLwcCaptureCase(t, report, "url-addressable-component")
	if c.Comparison == nil {
		t.Fatalf("missing comparison: %#v", c)
	}
	if !c.Comparison.OK || c.Comparison.DiffCount != 0 {
		t.Fatalf("comparison = %#v", c.Comparison)
	}
	if c.Comparison.Scope.Selector != "c-action-probe" || !c.Comparison.Scope.LocalFound || !c.Comparison.Scope.SalesforceFound {
		t.Fatalf("scope = %#v", c.Comparison.Scope)
	}
	if c.Comparison.Local.VisibleText != "Action Probe" || c.Comparison.Salesforce.VisibleText != "Action Probe" {
		t.Fatalf("visible text = %#v", c.Comparison)
	}
	if c.Comparison.Local.MountedComponentCount != 1 || c.Comparison.Salesforce.MountedComponentCount != 1 {
		t.Fatalf("component counts = %#v", c.Comparison)
	}
	assertSupportRow(t, report, "lwc.host.lightning-shell", "lightning-shell", "supported")
	assertSupportRow(t, report, "lwc.target.url-addressable-component", "lightning-shell", "supported")
}

func TestRunLWCCaptureCommunityLocalBrowserEvidenceIncludesContext(t *testing.T) {
	root := t.TempDir()
	runner := &fakeLwcCommandRunner{}
	browser := &fakeLwcBrowserRunner{}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:             root,
		TargetOrg:           "oaer-probe-max",
		Targets:             []string{"community-context"},
		SkipDeploy:          true,
		LocalBrowserCapture: true,
		LocalBaseURL:        "http://127.0.0.1:34567",
		Runner:              runner,
		Browser:             browser,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := findLwcCaptureCase(t, report, "community-context")
	if c.LocalEvidence == nil || c.LocalEvidence.Status != "captured" {
		t.Fatalf("local evidence = %#v", c.LocalEvidence)
	}
	for _, want := range []string{"/partners", "0DB000000000001", "0DM000000000001", "true", "comm__namedPage", "c-community-theme-layout"} {
		if !strings.Contains(c.LocalEvidence.DOM, want) {
			t.Fatalf("missing %q in community DOM: %s", want, c.LocalEvidence.DOM)
		}
	}
	assertSupportRow(t, report, "lwc.service.community-context", "lightning-shell", "supported-local")
}

func TestRunLWCCaptureMarksCaseFailedWhenBrowserComparisonDiffs(t *testing.T) {
	root := t.TempDir()
	runner := &fakeLwcCommandRunner{}
	browser := &fakeLwcBrowserRunner{
		domByURL: map[string]string{
			"http://127.0.0.1:34567/lwc/preview/tab/Lwc_Probe": `<main><c-context-probe>App Context</c-context-probe></main>`,
		},
		salesforceDOM: `<main><p>Page doesn't exist</p></main>`,
	}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:             root,
		TargetOrg:           "oaer-probe-max",
		Targets:             []string{"custom-tab"},
		Out:                 filepath.Join(root, "capture.json"),
		SkipDeploy:          true,
		LocalBrowserCapture: true,
		LocalBaseURL:        "http://127.0.0.1:34567",
		BrowserCapture:      true,
		Runner:              runner,
		Browser:             browser,
	})
	if err == nil || !strings.Contains(err.Error(), "LWC browser capture failed for 1 target") {
		t.Fatalf("err = %v", err)
	}
	if report.OK || report.Counts.Fail != 1 || report.Counts.Pass != 0 {
		t.Fatalf("counts/ok = %#v ok=%t", report.Counts, report.OK)
	}
	c := findLwcCaptureCase(t, report, "custom-tab")
	if c.Status != "fail" {
		t.Fatalf("status = %q", c.Status)
	}
	if c.Comparison == nil || c.Comparison.OK || !hasLwcComparisonDiff(c.Comparison.Diffs, "scope") {
		t.Fatalf("comparison = %#v", c.Comparison)
	}
}

func TestCompareLWCBrowserEvidenceScopesToTargetComponent(t *testing.T) {
	comparison := compareLwcBrowserEvidenceForComponent(
		&LwcCaptureEvidence{Status: "captured", DOM: `<body><nav>Local Chrome</nav><c-action-probe>Action Probe</c-action-probe></body>`},
		&LwcCaptureEvidence{Status: "captured", DOM: `<body><nav>Salesforce Chrome</nav><c-action-probe>Action Probe</c-action-probe></body>`},
		"actionProbe",
	)
	if comparison == nil {
		t.Fatalf("comparison nil")
	}
	if !comparison.OK || comparison.DiffCount != 0 {
		t.Fatalf("comparison = %#v", comparison)
	}
	if comparison.Scope.Selector != "c-action-probe" || !comparison.Scope.LocalFound || !comparison.Scope.SalesforceFound {
		t.Fatalf("scope = %#v", comparison.Scope)
	}
	if comparison.Local.VisibleText != "Action Probe" || comparison.Salesforce.VisibleText != "Action Probe" {
		t.Fatalf("comparison text = %#v", comparison)
	}
}

func TestCompareLWCBrowserEvidenceReportsMissingScopedComponent(t *testing.T) {
	comparison := compareLwcBrowserEvidenceForComponent(
		&LwcCaptureEvidence{Status: "captured", DOM: `<body><c-action-probe>Action Probe</c-action-probe></body>`},
		&LwcCaptureEvidence{Status: "captured", DOM: `<body><main>Action Probe</main></body>`},
		"actionProbe",
	)
	if comparison == nil {
		t.Fatalf("comparison nil")
	}
	if comparison.OK {
		t.Fatalf("comparison unexpectedly ok: %#v", comparison)
	}
	if !hasLwcComparisonDiff(comparison.Diffs, "scope") {
		t.Fatalf("diffs = %#v", comparison.Diffs)
	}
	if comparison.Scope.SalesforceFound {
		t.Fatalf("scope = %#v", comparison.Scope)
	}
}

func TestCompareLWCBrowserEvidenceReportsVisibleTextAndComponentDiffs(t *testing.T) {
	comparison := compareLwcBrowserEvidence(
		&LwcCaptureEvidence{Status: "captured", DOM: `<main><c-action-probe>Action Probe</c-action-probe></main>`},
		&LwcCaptureEvidence{Status: "captured", DOM: `<main><c-context-probe>Context Probe</c-context-probe><c-wire-probe></c-wire-probe></main>`},
	)
	if comparison == nil {
		t.Fatalf("comparison nil")
	}
	if comparison.OK || comparison.DiffCount != 2 {
		t.Fatalf("comparison = %#v", comparison)
	}
	if !hasLwcComparisonDiff(comparison.Diffs, "visibleText") || !hasLwcComparisonDiff(comparison.Diffs, "mountedComponentCount") {
		t.Fatalf("diffs = %#v", comparison.Diffs)
	}
}

func TestCompareLWCBrowserEvidenceNormalizesStaticResourceCacheToken(t *testing.T) {
	comparison := compareLwcBrowserEvidenceForComponent(
		&LwcCaptureEvidence{Status: "captured", DOM: `<c-context-probe>App Context /resource/LwcProbeAssets</c-context-probe>`},
		&LwcCaptureEvidence{Status: "captured", DOM: `<c-context-probe>App Context /resource/1781451532000/LwcProbeAssets</c-context-probe>`},
		"contextProbe",
	)
	if comparison == nil {
		t.Fatalf("comparison nil")
	}
	if !comparison.OK || comparison.DiffCount != 0 {
		t.Fatalf("comparison = %#v", comparison)
	}
}

func TestRunLWCCaptureSkipDeployWritesFixtureReport(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "capture.json")

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:    root,
		TargetOrg:  "oaer-probe-max",
		Hosts:      []string{"lightning-shell", "visualforce-lightning-out"},
		Out:        out,
		SkipDeploy: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if report.Command != "glade compat lwc capture" {
		t.Fatalf("command = %q", report.Command)
	}
	if report.TargetOrg != "oaer-probe-max" || report.Deployed {
		t.Fatalf("target/deploy = %#v", report)
	}
	if report.Mode != "fixture-evidence-stubs" {
		t.Fatalf("mode = %q", report.Mode)
	}
	if !report.OK {
		t.Fatalf("ok = false")
	}
	if report.Counts.Targets != 35 || report.Counts.Prepared != 35 || report.Counts.Pass != 0 || report.Counts.Fail != 0 {
		t.Fatalf("counts = %#v", report.Counts)
	}
	if report.Artifacts.Report != out {
		t.Fatalf("artifacts = %#v", report.Artifacts)
	}

	gotNames := make([]string, 0, len(report.Cases))
	for _, c := range report.Cases {
		gotNames = append(gotNames, c.Name)
		if c.Status != "prepared" {
			t.Fatalf("%s status = %q", c.Name, c.Status)
		}
		if !strings.HasPrefix(c.TargetURL, "fixture://lwc/") {
			t.Fatalf("%s target URL = %q", c.Name, c.TargetURL)
		}
	}
	wantNames := []string{
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
		"package-phase1-base-components",
		"visualforce-base-components",
		"phase3-base-components",
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("case names = %#v", gotNames)
	}
	if len(report.Support) != 37 {
		t.Fatalf("support rows = %d, want 37: %#v", len(report.Support), report.Support)
	}
	assertSupportRow(t, report, "lwc.host.lightning-shell", "lightning-shell", "prepared-local")
	assertSupportRow(t, report, "lwc.host.visualforce-lightning-out", "visualforce-lightning-out", "prepared-local")
	assertSupportRow(t, report, "lwc.service.ui-object-info", "lightning-shell", "prepared-local")
	assertSupportRow(t, report, "lwc.service.apex-wire", "visualforce-lightning-out", "prepared-local")
	assertSupportRow(t, report, "lwc.service.imperative-apex", "visualforce-lightning-out", "prepared-local")
	assertSupportRow(t, report, "lwc.service.lds-read", "visualforce-lightning-out", "prepared-local")
	assertSupportRow(t, report, "lwc.service.package-phase1-base-components", "lightning-shell", "prepared-local")
	assertSupportRow(t, report, "lwc.service.ui-object-info", "visualforce-lightning-out", "prepared-local")
	assertSupportRow(t, report, "lwc.service.ui-related-list", "lightning-shell", "prepared-local")
	assertSupportRow(t, report, "lwc.service.lds-create-defaults", "lightning-shell", "prepared-local")
	assertSupportRow(t, report, "lwc.service.ui-layout", "lightning-shell", "prepared-local")
	assertSupportRow(t, report, "lwc.service.lds-mutation", "lightning-shell", "prepared-local")
	assertSupportRow(t, report, "lwc.service.lds-mutation", "visualforce-lightning-out", "prepared-local")
	assertSupportRow(t, report, "lwc.service.navigation", "visualforce-lightning-out", "prepared-local")
	assertSupportRow(t, report, "lwc.service.toast", "visualforce-lightning-out", "prepared-local")
	assertSupportRow(t, report, "lwc.service.lms", "visualforce-lightning-out", "prepared-local")
	assertSupportRow(t, report, "lwc.service.resource-loader", "visualforce-lightning-out", "prepared-local")
	assertSupportRow(t, report, "lwc.target.community-page", "lightning-shell", "org-setup-required")
	assertSupportRow(t, report, "lwc.target.community-component", "lightning-shell", "org-setup-required")
	assertSupportRow(t, report, "lwc.service.community-context", "lightning-shell", "org-setup-required")
	assertSupportRow(t, report, "lwc.service.base-components", "visualforce-lightning-out", "prepared-local")
	assertSupportRow(t, report, "lwc.service.phase3-base-components", "lightning-shell", "prepared-local")

	recordCase := findLwcCaptureCase(t, report, "record-page")
	if recordCase.Metadata.Route != "/lwc/preview/record/Account/001000000000001AAA?page=Account_Record_Page" {
		t.Fatalf("record route = %q", recordCase.Metadata.Route)
	}
	if recordCase.LocalEvidence == nil || recordCase.LocalEvidence.Status != "prepared" || !strings.Contains(recordCase.LocalEvidence.DOM, `data-probe="record"`) {
		t.Fatalf("record local evidence = %#v", recordCase.LocalEvidence)
	}
	if recordCase.SalesforceEvidence == nil || recordCase.SalesforceEvidence.Status != "pending-org-capture" || recordCase.SalesforceEvidence.Source != "oaer-probe-max" {
		t.Fatalf("record salesforce evidence = %#v", recordCase.SalesforceEvidence)
	}
	if recordCase.SalesforceEvidence.TargetURL != "/lightning/r/Account/001000000000001AAA/view" {
		t.Fatalf("record salesforce target = %q", recordCase.SalesforceEvidence.TargetURL)
	}
	appCase := findLwcCaptureCase(t, report, "app-page")
	if appCase.Metadata.App != "Lwc_Shell" {
		t.Fatalf("app page app = %q", appCase.Metadata.App)
	}
	if appCase.SalesforceEvidence == nil || appCase.SalesforceEvidence.TargetURL != "/lightning/app/c__Lwc_Shell/n/Lwc_Probe" {
		t.Fatalf("app page salesforce evidence = %#v", appCase.SalesforceEvidence)
	}
	tabCase := findLwcCaptureCase(t, report, "custom-tab")
	if tabCase.Metadata.App != "Lwc_Shell" {
		t.Fatalf("custom tab app = %q", tabCase.Metadata.App)
	}
	if tabCase.SalesforceEvidence == nil || tabCase.SalesforceEvidence.TargetURL != "/lightning/app/c__Lwc_Shell/n/Lwc_Probe" {
		t.Fatalf("custom tab salesforce evidence = %#v", tabCase.SalesforceEvidence)
	}
	urlCase := findLwcCaptureCase(t, report, "url-addressable-component")
	if urlCase.SalesforceEvidence == nil || urlCase.SalesforceEvidence.TargetURL != "/lightning/cmp/c__actionProbe?c__mode=demo" {
		t.Fatalf("url-addressable salesforce evidence = %#v", urlCase.SalesforceEvidence)
	}
	vfCase := findLwcCaptureCase(t, report, "visualforce-lightning-out")
	if vfCase.SalesforceEvidence == nil || vfCase.SalesforceEvidence.TargetURL != "/apex/LwcShellProbe" {
		t.Fatalf("visualforce salesforce evidence = %#v", vfCase.SalesforceEvidence)
	}
	actionCase := findLwcCaptureCase(t, report, "record-quick-action")
	if actionCase.SalesforceEvidence == nil || actionCase.SalesforceEvidence.TargetURL != "/lightning/r/Account/001000000000001AAA/view?quickAction=Account.Update_Status" {
		t.Fatalf("quick action salesforce evidence = %#v", actionCase.SalesforceEvidence)
	}
	communityCase := findLwcCaptureCase(t, report, "community-page")
	if communityCase.Metadata.Route != "/lwc/preview/community/Partner_Portal/Account" || communityCase.Metadata.Component != "communityProbe" {
		t.Fatalf("community page metadata = %#v", communityCase.Metadata)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"ok": true`) || !strings.Contains(string(data), `"visualforce-lightning-out"`) ||
		!strings.Contains(string(data), `"lwc.service.toast"`) {
		t.Fatalf("capture json = %s", data)
	}
}

func TestRunLWCCaptureConsumesOracleManifest(t *testing.T) {
	root := t.TempDir()
	writeLwcCaptureTestFile(t, filepath.Join(root, "glade-lwc-oracle.json"), `{
  "schemaVersion": 1,
  "fixtures": [
    {
      "id": "api-module:lightning/uiAppsApi",
      "category": "api-module",
      "name": "lightning/uiAppsApi",
      "componentName": "uiAppsApiOracle",
      "targetHost": "lightning-shell",
      "route": "/lwc/preview/component/c/uiAppsApiOracle",
      "assertions": ["imports", "renders"]
    }
  ]
}`)
	browser := &fakeLwcBrowserRunner{
		domByURL: map[string]string{
			"http://127.0.0.1:18081/lwc/preview/component/c/uiAppsApiOracle": `<main><c-ui-apps-api-oracle>lightning/uiAppsApi</c-ui-apps-api-oracle></main>`,
		},
	}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:             root,
		TargetOrg:           "oaer-probe-max",
		Manifest:            "glade-lwc-oracle.json",
		SkipDeploy:          true,
		LocalBrowserCapture: true,
		LocalBaseURL:        "http://127.0.0.1:18081",
		Browser:             browser,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.Targets != 1 || report.Counts.Pass != 1 || report.Counts.Fail != 0 {
		t.Fatalf("counts = %#v", report.Counts)
	}
	c := findLwcCaptureCase(t, report, "api-module:lightning/uiAppsApi")
	if c.Feature != "lwc.native-api.api-module" || c.Metadata.Component != "uiAppsApiOracle" {
		t.Fatalf("case = %#v", c)
	}
	if c.SalesforceEvidence == nil || c.SalesforceEvidence.TargetURL != "/lightning/cmp/c__uiAppsApiOracle" {
		t.Fatalf("salesforce evidence = %#v", c.SalesforceEvidence)
	}
	if got := browser.urls; len(got) != 1 || got[0] != "http://127.0.0.1:18081/lwc/preview/component/c/uiAppsApiOracle" {
		t.Fatalf("browser urls = %#v", got)
	}
}

func TestRunLWCCaptureSkipsStandardBrowserForHostScopedOracle(t *testing.T) {
	root := t.TempDir()
	writeLwcCaptureTestFile(t, filepath.Join(root, "glade-lwc-oracle.json"), `{
  "schemaVersion": 1,
  "fixtures": [
    {
      "id": "api-module:experience/blockBuilderApi",
      "category": "api-module",
      "name": "experience/blockBuilderApi",
      "componentName": "experienceBlockBuilderApiOracle",
      "targetHost": "lightning-shell",
      "route": "/lwc/preview/component/c/experienceBlockBuilderApiOracle",
      "salesforceDeployable": true,
      "salesforceBrowserCapturable": false,
      "assertions": ["imports", "renders"]
    }
  ]
}`)
	browser := &fakeLwcBrowserRunner{
		domByURL: map[string]string{
			"http://127.0.0.1:18081/lwc/preview/component/c/experienceBlockBuilderApiOracle": `<main><c-experience-block-builder-api-oracle>experience/blockBuilderApi</c-experience-block-builder-api-oracle></main>`,
		},
	}

	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:             root,
		TargetOrg:           "oaer-probe-max",
		Manifest:            "glade-lwc-oracle.json",
		SkipDeploy:          true,
		BrowserCapture:      true,
		LocalBrowserCapture: true,
		LocalBaseURL:        "http://127.0.0.1:18081",
		Browser:             browser,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := findLwcCaptureCase(t, report, "api-module:experience/blockBuilderApi")
	if c.SalesforceEvidence == nil || !strings.HasPrefix(c.SalesforceEvidence.TargetURL, "salesforce://host-context-only") {
		t.Fatalf("salesforce evidence = %#v", c.SalesforceEvidence)
	}
	if c.LocalEvidence == nil || c.LocalEvidence.Status != "captured" {
		t.Fatalf("local evidence = %#v", c.LocalEvidence)
	}
	if got := browser.urls; len(got) != 1 || got[0] != "http://127.0.0.1:18081/lwc/preview/component/c/experienceBlockBuilderApiOracle" {
		t.Fatalf("browser urls = %#v", got)
	}
}

func TestLWCCaptureSelectorKeepsAcronymsTogether(t *testing.T) {
	c := LwcCaptureCase{
		Metadata: LwcCaptureMetadata{Component: "uiGraphQLApiOracle"},
	}
	got := expectedLwcCaptureSelector(c)
	if got != "c-ui-graph-q-l-api-oracle" {
		t.Fatalf("selector = %q", got)
	}
	aliases := expectedLwcCaptureSelectors(c)
	if len(aliases) != 2 || aliases[1] != "c-ui-graph-qlapi-oracle" {
		t.Fatalf("aliases = %#v", aliases)
	}
}

func TestCompareLwcBrowserEvidenceUsesSelectorAliasesPerSide(t *testing.T) {
	comparison := compareLwcBrowserEvidenceForComponent(
		&LwcCaptureEvidence{Status: "captured", DOM: `<main><c-ui-graph-qlapi-oracle><p>lightning/uiGraphQLApi</p></c-ui-graph-qlapi-oracle></main>`},
		&LwcCaptureEvidence{Status: "captured", DOM: `<main><c-ui-graph-q-l-api-oracle><p>lightning/uiGraphQLApi</p></c-ui-graph-q-l-api-oracle></main>`},
		"uiGraphQLApiOracle",
	)
	if comparison == nil || !comparison.OK || comparison.Scope.LocalFound != true || comparison.Scope.SalesforceFound != true {
		t.Fatalf("comparison = %#v", comparison)
	}
}

func TestBuildLwcSupportRowsClassifiesManifestCaptureCase(t *testing.T) {
	rows := buildLwcSupportRows([]LwcCaptureCase{{
		Name:   "missing-target",
		Host:   "lightning-shell",
		Status: "pass",
	}}, []string{"lightning-shell"}, "/tmp/lwc.json")

	found := false
	for _, row := range rows {
		if row.Feature != "lwc.target.missing-target" {
			continue
		}
		found = true
		if row.Host != "lightning-shell" || row.Status != "prepared-local" {
			t.Fatalf("unknown row = %#v", row)
		}
	}
	if !found {
		t.Fatalf("unknown support row missing in %#v", rows)
	}
}

func hasLwcComparisonDiff(diffs []LwcCaptureDiff, field string) bool {
	for _, diff := range diffs {
		if diff.Field == field {
			return true
		}
	}
	return false
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestPrepareLWCCaptureDeployProjectSkipsGlobalQuickActions(t *testing.T) {
	root := t.TempDir()
	writeLwcCaptureTestFile(t, filepath.Join(root, "force-app/main/default/quickActions/Account.Update_Status.quickAction-meta.xml"), "<QuickAction/>")
	writeLwcCaptureTestFile(t, filepath.Join(root, "force-app/main/default/quickActions/Global_Status.quickAction-meta.xml"), "<QuickAction/>")
	writeLwcCaptureTestFile(t, filepath.Join(root, "force-app/main/default/lwc/actionProbe/actionProbe.js"), "export default class ActionProbe {}")
	writeLwcCaptureTestFile(t, filepath.Join(root, "force-app/main/default/lwc/localOnlyProbe/localOnlyProbe.js"), "export default class LocalOnlyProbe {}")
	writeLwcCaptureTestFile(t, filepath.Join(root, "force-app/main/default/lwc/omittedProbe/omittedProbe.js"), "export default class OmittedProbe {}")
	writeLwcCaptureTestFile(t, filepath.Join(root, "glade-lwc-oracle.json"), `{"fixtures":[
  {"id":"deployable","componentName":"actionProbe","route":"/lwc/preview/component/c/actionProbe","salesforceDeployable":true},
  {"id":"local-only","componentName":"localOnlyProbe","route":"/lwc/preview/component/c/localOnlyProbe","salesforceDeployable":false}
]}`)

	skipComponents, err := lwcCaptureNonDeployableComponents(root, "glade-lwc-oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	deployRoot, cleanup, err := prepareLwcCaptureDeployProject(root, skipComponents)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if _, err := os.Stat(filepath.Join(deployRoot, "force-app/main/default/quickActions/Account.Update_Status.quickAction-meta.xml")); err != nil {
		t.Fatalf("record quick action missing from deploy copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(deployRoot, "force-app/main/default/quickActions/Global_Status.quickAction-meta.xml")); !os.IsNotExist(err) {
		t.Fatalf("global quick action should be skipped, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(deployRoot, "force-app/main/default/lwc/actionProbe/actionProbe.js")); err != nil {
		t.Fatalf("lwc file missing from deploy copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(deployRoot, "force-app/main/default/lwc/localOnlyProbe/localOnlyProbe.js")); !os.IsNotExist(err) {
		t.Fatalf("local-only lwc should be skipped, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(deployRoot, "force-app/main/default/lwc/omittedProbe/omittedProbe.js")); !os.IsNotExist(err) {
		t.Fatalf("lwc omitted from manifest should be skipped, err=%v", err)
	}
}

func TestRunLWCCaptureDeployAssignsFixturePermissionSet(t *testing.T) {
	root := t.TempDir()
	writeLwcCaptureTestFile(t, filepath.Join(root, "force-app/main/default/permissionsets/Lwc_Shell_Access.permissionset-meta.xml"), `<PermissionSet xmlns="http://soap.sforce.com/2006/04/metadata">
  <label>LWC Shell Access</label>
</PermissionSet>`)
	runner := &fakeLwcCommandRunner{}

	if err := runLwcCaptureDeploy(context.Background(), runner, root, "oaer-probe-max", ""); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %#v", runner.calls)
	}
	assign := runner.calls[1]
	wantArgs := []string{"org", "assign", "permset", "--name", "Lwc_Shell_Access", "--target-org", "oaer-probe-max", "--json"}
	if assign.name != "sf" || len(assign.args) < 1 || assign.args[0] == "" || !reflect.DeepEqual(assign.args[1:], wantArgs) {
		t.Fatalf("assign call = %#v", assign)
	}
}

func TestRunLWCCaptureDeployTreatsDuplicatePermissionSetAssignmentAsAssigned(t *testing.T) {
	root := t.TempDir()
	writeLwcCaptureTestFile(t, filepath.Join(root, "force-app/main/default/permissionsets/Lwc_Shell_Access.permissionset-meta.xml"), `<PermissionSet xmlns="http://soap.sforce.com/2006/04/metadata">
  <label>LWC Shell Access</label>
</PermissionSet>`)
	runner := &fakeLwcCommandRunner{
		outputs: [][]byte{
			[]byte(`{"status":0}`),
			[]byte(`{"status":1,"result":{"failures":[{"message":"Duplicate PermissionSetAssignment. Assignee: 005; Permission Set: 0PS"}]}}`),
		},
		errors: []error{nil, os.ErrExist},
	}

	if err := runLwcCaptureDeploy(context.Background(), runner, root, "oaer-probe-max", ""); err != nil {
		t.Fatalf("duplicate assignment should be ignored, got %v", err)
	}
}

func TestRunLWCCaptureRequiresTargetOrg(t *testing.T) {
	_, err := RunLwcCapture(context.Background(), LwcCaptureOptions{SkipDeploy: true})
	if err == nil || !strings.Contains(err.Error(), "--target-org is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunLWCCaptureFiltersTargetsByIncludedHosts(t *testing.T) {
	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:    t.TempDir(),
		TargetOrg:  "oaer-probe-max",
		Targets:    []string{"visualforce-lightning-out"},
		Hosts:      []string{"lightning-shell"},
		SkipDeploy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.Targets != 0 || len(report.Cases) != 0 {
		t.Fatalf("lightning-shell host should filter Visualforce target: %#v", report)
	}

	report, err = RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:    t.TempDir(),
		TargetOrg:  "oaer-probe-max",
		Targets:    []string{"direct-component"},
		Hosts:      []string{"visualforce-lightning-out"},
		SkipDeploy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.Targets != 0 || len(report.Cases) != 0 {
		t.Fatalf("visualforce host should filter direct component target: %#v", report)
	}
}

func TestWriteLWCCaptureTextReportsCountsAndArtifact(t *testing.T) {
	var out bytes.Buffer
	WriteLwcCaptureText(&out, LwcCaptureReport{
		Counts: LwcCaptureCounts{Targets: 16, Prepared: 16, Pass: 0, Fail: 0},
		Artifacts: LwcCaptureArtifacts{
			Report: "/tmp/glade-lwc-capture.json",
		},
	})

	if got, want := out.String(), "prepared 16 LWC fixture-evidence-stubs targets: prepared=16 pass=0 fail=0 support=0 artifacts=/tmp/glade-lwc-capture.json\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestRunLWCCaptureWritesSupportRowsForRequestedFeature(t *testing.T) {
	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:    t.TempDir(),
		TargetOrg:  "oaer-probe-max",
		Targets:    []string{"toast"},
		SkipDeploy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.Targets != 1 || len(report.Cases) != 1 {
		t.Fatalf("report = %#v", report)
	}
	toastCase := report.Cases[0]
	if toastCase.Metadata.Component != "contextProbe" || toastCase.Metadata.Route != "/lwc/preview/component/c/contextProbe?feature=toast" {
		t.Fatalf("toast metadata = %#v", toastCase.Metadata)
	}
	assertSupportRow(t, report, "lwc.service.toast", "lightning-shell", "prepared-local")
}

func TestRunLWCCaptureIncludesPackagePhase1BaseComponentsTarget(t *testing.T) {
	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:    t.TempDir(),
		TargetOrg:  "oaer-probe-max",
		Targets:    []string{"package-phase1-base-components"},
		SkipDeploy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.Targets != 1 || len(report.Cases) != 1 {
		t.Fatalf("report = %#v", report)
	}
	c := report.Cases[0]
	if c.Feature != "lwc.service.package-phase1-base-components" || c.Metadata.Component != "baseComponentHost" || c.Metadata.Route != "/lwc/preview/component/c/baseComponentHost?context=packagePhase1BaseComponents" {
		t.Fatalf("package phase1 metadata = %#v", c)
	}
	if c.SalesforceEvidence == nil || !strings.Contains(c.SalesforceEvidence.DOM, `data-probe="package-phase1-base-components"`) || !strings.Contains(c.SalesforceEvidence.DOM, "Package Phase 1") {
		t.Fatalf("package phase1 prepared DOM = %#v", c.SalesforceEvidence)
	}
	assertSupportRow(t, report, "lwc.service.package-phase1-base-components", "lightning-shell", "prepared-local")
}

func TestRunLWCCaptureIncludesPhase3BaseComponentsTarget(t *testing.T) {
	report, err := RunLwcCapture(context.Background(), LwcCaptureOptions{
		Project:    t.TempDir(),
		TargetOrg:  "oaer-probe-max",
		Targets:    []string{"phase3-base-components"},
		SkipDeploy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.Targets != 1 || len(report.Cases) != 1 {
		t.Fatalf("report = %#v", report)
	}
	c := report.Cases[0]
	if c.Feature != "lwc.service.phase3-base-components" || c.Metadata.Component != "baseComponentHost" || c.Metadata.Route != "/lwc/preview/component/c/baseComponentHost?context=phase3BaseComponents" {
		t.Fatalf("phase3 metadata = %#v", c)
	}
	if c.SalesforceEvidence == nil || !strings.Contains(c.SalesforceEvidence.DOM, `data-probe="phase3-base-components"`) || !strings.Contains(c.SalesforceEvidence.DOM, "Providers") {
		t.Fatalf("phase3 prepared DOM = %#v", c.SalesforceEvidence)
	}
	assertSupportRow(t, report, "lwc.service.phase3-base-components", "lightning-shell", "prepared-local")
}

func findLwcCaptureCase(t *testing.T, report LwcCaptureReport, name string) LwcCaptureCase {
	t.Helper()
	for _, c := range report.Cases {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("missing case %q in %#v", name, report.Cases)
	return LwcCaptureCase{}
}

func assertSupportRow(t *testing.T, report LwcCaptureReport, feature, host, status string) {
	t.Helper()
	for _, row := range report.Support {
		if row.Feature == feature && row.Host == host {
			if row.Status != status {
				t.Fatalf("%s/%s status = %q, want %q", feature, host, row.Status, status)
			}
			if row.Evidence == "" {
				t.Fatalf("%s/%s evidence is empty", feature, host)
			}
			return
		}
	}
	t.Fatalf("missing support row %s/%s in %#v", feature, host, report.Support)
}

func writeLwcCaptureTestFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
