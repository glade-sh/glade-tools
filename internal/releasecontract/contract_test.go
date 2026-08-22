package releasecontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func validContract() Contract {
	return Contract{
		SchemaVersion: 1,
		Defaults:      Defaults{Source: "65.0", Endpoint: "65.0", OrgProfile: "default"},
		Windows: Windows{
			Source: []VersionProof{
				{Version: "65.0", ProofCases: []string{"SOURCE-65"}},
				{Version: "66.0", ProofCases: []string{"SOURCE-66"}},
				{Version: "67.0", ProofCases: []string{"SOURCE-67"}},
			},
			Endpoint: []VersionProof{
				{Version: "60.0", ProductTests: []string{"internal/server/server_test.go:TestEndpoint60"}},
				{Version: "65.0", ProductTests: []string{"internal/server/server_test.go:TestEndpoint65"}},
				{Version: "66.0", ProductTests: []string{"internal/server/server_test.go:TestEndpoint66"}},
				{Version: "67.0", ProductTests: []string{"internal/server/server_test.go:TestEndpoint67"}},
			},
			OrgProfiles: []ProfileProof{{Name: "default", ProductTests: []string{"internal/vm/execution_policy_test.go:TestCurrentSourceExecutionPolicyUsesAPIVersionAndTriggerFrame"}}},
		},
		Releases: []Release{
			{Name: "Winter '26", APIVersion: "65.0", Maturity: "ga", Manifest: "winter.json", Inventory: "winter-inventory.json", SourceReceipt: "winter-source.json", SourceReceiptSHA256: strings.Repeat("a", 64)},
			{Name: "Spring '26", APIVersion: "66.0", Maturity: "ga", Manifest: "spring.json", Inventory: "spring-inventory.json", SourceReceipt: "spring-source.json", SourceReceiptSHA256: strings.Repeat("b", 64), Classifications: "winter-to-spring.json", ChangeInventory: "spring-notes.json", ChangeRoutes: "winter-to-spring-routes.json"},
			{Name: "Summer '26", APIVersion: "67.0", Maturity: "ga", Manifest: "summer.json", Inventory: "summer-inventory.json", SourceReceipt: "summer-source.json", SourceReceiptSHA256: strings.Repeat("c", 64), Classifications: "spring-to-summer.json", ChangeInventory: "summer-notes.json", ChangeRoutes: "spring-to-summer-routes.json"},
		},
		Behaviors: []Behavior{{
			ID: "apex.query.with-security-enforced.removed", Axis: "source", Kind: "removed", Outcome: "supported", Until: "67.0", Maturity: "ga",
			SourceRefs:   []string{"https://help.salesforce.com/s/articleView?id=release-notes.rn_apex_removed_withSecurityEnforced.htm&language=en_US&release=262&type=5"},
			ProofCases:   []string{"APEX-AUDIT-SECURITY-ENFORCED-API66-CONTROL", "APEX-AUDIT-SECURITY-ENFORCED-API67"},
			ProductTests: []string{"internal/sema/version_contracts_test.go:TestSecurityEnforcedQueryIsRejectedAtAPIVersion67AndLater"},
		}},
		NoFallbackProductTests: []string{
			"internal/apexversion/version_test.go:TestResolveSourceRejectsUnsupportedWithoutFallback",
			"internal/lwc/compile/compile_test.go:TestBuildCompileConfigAPIVersionMatrix/unsupported-64.0",
			"internal/lwc/compile/compile_test.go:TestBuildCompileConfigAPIVersionMatrix/missing-component-api-version",
			"internal/server/server_test.go:TestUnsupportedAPIVersionRouting",
			"internal/gladecli/dap_cache_test.go:TestDAPCacheKeyIncludesSourceAPIVersion",
		},
	}
}

func TestValidateAcceptsMovingReleaseContract(t *testing.T) {
	contract := validContract()
	if got := versions(contract.Windows.Source); !reflect.DeepEqual(got, []string{"65.0", "66.0", "67.0"}) {
		t.Fatalf("source versions = %#v", got)
	}
	if got := versions(contract.Windows.Endpoint); !reflect.DeepEqual(got, []string{"60.0", "65.0", "66.0", "67.0"}) {
		t.Fatalf("endpoint versions = %#v", got)
	}
	if contract.Defaults != (Defaults{Source: "65.0", Endpoint: "65.0", OrgProfile: "default"}) {
		t.Fatalf("defaults = %#v", contract.Defaults)
	}
	if got := profileNames(contract.Windows.OrgProfiles); !reflect.DeepEqual(got, []string{"default"}) {
		t.Fatalf("org profiles = %#v", got)
	}
	if err := contract.Validate(t.TempDir()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateAcceptsBehaviorDocumentedAfterEffectiveVersion(t *testing.T) {
	contract := validContract()
	contract.Behaviors[0].Since = "66.0"
	contract.Behaviors[0].Until = ""
	contract.Behaviors[0].DocumentedIn = "67.0"
	if err := contract.Validate(t.TempDir()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsInvalidContractFields(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Contract)
	}{
		{"schema version", func(c *Contract) { c.SchemaVersion = 2 }},
		{"default outside source window", func(c *Contract) { c.Defaults.Source = "64.0" }},
		{"source version without release snapshot", func(c *Contract) {
			c.Windows.Source = append(c.Windows.Source, VersionProof{Version: "68.0", ProofCases: []string{"SOURCE-68"}})
		}},
		{"non-baseline release missing change inventory", func(c *Contract) { c.Releases[1].ChangeInventory = "" }},
		{"non-baseline release missing change routes", func(c *Contract) { c.Releases[1].ChangeRoutes = "" }},
		{"duplicate release API", func(c *Contract) { c.Releases[1].APIVersion = c.Releases[0].APIVersion }},
		{"preview release in stable source window", func(c *Contract) { c.Releases[1].Maturity = "preview" }},
		{"absolute release path", func(c *Contract) { c.Releases[0].Manifest = "/tmp/winter.json" }},
		{"escaping release path", func(c *Contract) { c.Releases[0].Inventory = "../winter-inventory.json" }},
		{"backslash release path", func(c *Contract) { c.Releases[0].Manifest = `..\outside.json` }},
		{"missing source receipt", func(c *Contract) { c.Releases[0].SourceReceipt = "" }},
		{"invalid source receipt SHA", func(c *Contract) { c.Releases[0].SourceReceiptSHA256 = "not-a-sha" }},
		{"absolute surface corrections path", func(c *Contract) { c.SurfaceCorrections = "/tmp/corrections.json" }},
		{"duplicate behavior ID", func(c *Contract) { c.Behaviors = append(c.Behaviors, c.Behaviors[0]) }},
		{"behavior without proof", func(c *Contract) { c.Behaviors[0].ProofCases = nil; c.Behaviors[0].ProductTests = nil }},
		{"non-Salesforce source URL", func(c *Contract) { c.Behaviors[0].SourceRefs = []string{"https://example.com/release"} }},
		{"behavior boundary order", func(c *Contract) { c.Behaviors[0].Since, c.Behaviors[0].Until = "67.0", "66.0" }},
		{"invalid documented release", func(c *Contract) { c.Behaviors[0].DocumentedIn = "Summer '26" }},
		{"unknown documented release", func(c *Contract) { c.Behaviors[0].DocumentedIn = "68.0" }},
		{"empty no-fallback binding", func(c *Contract) { c.NoFallbackProductTests[0] = "" }},
		{"empty no-fallback list", func(c *Contract) { c.NoFallbackProductTests = nil }},
		{"escaping no-fallback binding", func(c *Contract) { c.NoFallbackProductTests[0] = "../version_test.go:TestNoFallback" }},
		{"backslash no-fallback binding", func(c *Contract) { c.NoFallbackProductTests[0] = `..\outside_test.go:TestX` }},
		{"duplicate no-fallback binding", func(c *Contract) {
			c.NoFallbackProductTests = append(c.NoFallbackProductTests, c.NoFallbackProductTests[0])
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract := validContract()
			test.edit(&contract)
			if err := contract.Validate(t.TempDir()); err == nil {
				t.Fatal("Validate unexpectedly succeeded")
			}
		})
	}
}

func versions(entries []VersionProof) []string {
	result := make([]string, len(entries))
	for index, entry := range entries {
		result[index] = entry.Version
	}
	return result
}

func profileNames(entries []ProfileProof) []string {
	result := make([]string, len(entries))
	for index, entry := range entries {
		result[index] = entry.Name
	}
	return result
}

func TestValidateRejectsUnknownBehaviorValues(t *testing.T) {
	fields := []struct {
		name string
		edit func(*Behavior)
	}{
		{"axis", func(b *Behavior) { b.Axis = "unknown" }},
		{"kind", func(b *Behavior) { b.Kind = "unknown" }},
		{"outcome", func(b *Behavior) { b.Outcome = "unknown" }},
		{"maturity", func(b *Behavior) { b.Maturity = "unknown" }},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			contract := validContract()
			field.edit(&contract.Behaviors[0])
			if err := contract.Validate(t.TempDir()); err == nil {
				t.Fatal("Validate unexpectedly succeeded")
			}
		})
	}
}

func TestValidateRejectsMaturityBehaviorOutsideAdvertisedWindows(t *testing.T) {
	for _, test := range []struct {
		axis     string
		boundary string
	}{
		{axis: "source", boundary: "64.0"},
		{axis: "endpoint", boundary: "64.0"},
	} {
		t.Run(test.axis, func(t *testing.T) {
			contract := validContract()
			behavior := contract.Behaviors[0]
			behavior.ID = "maturity-outside-" + test.axis
			behavior.Axis = test.axis
			behavior.Kind = "maturity"
			behavior.Since = test.boundary
			behavior.Until = ""
			contract.Behaviors = []Behavior{behavior}
			if err := contract.Validate(t.TempDir()); err == nil {
				t.Fatal("Validate unexpectedly succeeded")
			}
		})
	}
}

func TestValidateMaturityBoundaryDiagnosticsCheckSinceFirst(t *testing.T) {
	contract := validContract()
	behavior := contract.Behaviors[0]
	behavior.ID = "maturity-boundaries-outside"
	behavior.Kind = "maturity"
	behavior.Since = "64.0"
	behavior.Until = "68.0"
	contract.Behaviors = []Behavior{behavior}
	if err := contract.Validate(t.TempDir()); err == nil || !strings.Contains(err.Error(), ".since") {
		t.Fatalf("Validate error = %v, want since diagnostic", err)
	}
}

func TestValidateAllowsOrgCapabilityMaturityWithoutNumericWindow(t *testing.T) {
	contract := validContract()
	behavior := contract.Behaviors[0]
	behavior.ID = "org-capability-maturity"
	behavior.Axis = "org-capability"
	behavior.Kind = "maturity"
	behavior.Since = "64.0"
	behavior.Until = "68.0"
	contract.Behaviors = []Behavior{behavior}
	if err := contract.Validate(t.TempDir()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsInvalidSourceReferenceForms(t *testing.T) {
	for _, sourceRef := range []string{
		"ftp://salesforce.com/release",
		"//salesforce.com/release",
		"https://user@salesforce.com/release",
	} {
		t.Run(sourceRef, func(t *testing.T) {
			contract := validContract()
			contract.Behaviors[0].SourceRefs = []string{sourceRef}
			if err := contract.Validate(t.TempDir()); err == nil {
				t.Fatal("Validate unexpectedly succeeded")
			}
		})
	}
}

func TestValidateRejectsDuplicateWindowsAndMissingProofs(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Contract)
	}{
		{"duplicate source version", func(c *Contract) { c.Windows.Source[1].Version = c.Windows.Source[0].Version }},
		{"source version without proof", func(c *Contract) { c.Windows.Source[0].ProofCases = nil }},
		{"duplicate endpoint version", func(c *Contract) { c.Windows.Endpoint[1].Version = c.Windows.Endpoint[0].Version }},
		{"duplicate org profile", func(c *Contract) { c.Windows.OrgProfiles = append(c.Windows.OrgProfiles, c.Windows.OrgProfiles[0]) }},
		{"org profile without proof", func(c *Contract) { c.Windows.OrgProfiles[0].ProductTests = nil }},
		{"empty proof ID", func(c *Contract) { c.Windows.Source[0].ProofCases[0] = " " }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract := validContract()
			test.edit(&contract)
			if err := contract.Validate(t.TempDir()); err == nil {
				t.Fatal("Validate unexpectedly succeeded")
			}
		})
	}
}

func TestLoadRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contract.json")
	validJSON, err := json.Marshal(validContract())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		data string
	}{
		{"unknown field", strings.TrimSuffix(string(validJSON), "}") + `,"unknown":true}`},
		{"trailing JSON", string(validJSON) + ` {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := Load(path); err == nil {
				t.Fatal("Load unexpectedly succeeded")
			}
		})
	}
}

func TestLoadRejectsDuplicateAndCaseVariantJSONKeys(t *testing.T) {
	validJSON, err := json.Marshal(validContract())
	if err != nil {
		t.Fatal(err)
	}
	valid := string(validJSON)
	tests := []struct {
		name string
		data string
	}{
		{"duplicate top-level key", strings.Replace(valid, `{"schemaVersion":1,`, `{"schemaVersion":1,"schemaVersion":1,`, 1)},
		{"duplicate nested key", strings.Replace(valid, `{"source":"65.0","endpoint"`, `{"source":"65.0","source":"65.0","endpoint"`, 1)},
		{"case-variant top-level key", strings.Replace(valid, `"schemaVersion"`, `"SchemaVersion"`, 1)},
		{"case-variant nested key", strings.Replace(valid, `"apiVersion"`, `"ApiVersion"`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "contract.json")
			if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := Load(path); err == nil {
				t.Fatal("Load unexpectedly succeeded")
			}
		})
	}
}

func TestLoadReturnsAbsoluteContractDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "contract.json")
	data, err := json.Marshal(validContract())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, gotRoot, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantRoot, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot != wantRoot || !filepath.IsAbs(gotRoot) {
		t.Fatalf("Load root = %q, want absolute %q", gotRoot, wantRoot)
	}
}

func TestBehaviorUsesSurfaceIdsJSONName(t *testing.T) {
	data, err := json.Marshal(Behavior{SurfaceIDs: []string{"apex.query"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"id":"","axis":"","kind":"","outcome":"","maturity":"","surfaceIds":["apex.query"],"sourceRefs":null}` {
		t.Fatalf("behavior JSON = %s", data)
	}
}
