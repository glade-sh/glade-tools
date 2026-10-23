package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/internal/storage"
	"github.com/glade-sh/glade/tools/internal/capability"
)

// The catalog exposes both FeedSignal and feedSignal lookup names for one
// canonical definition. Exercise the full command routes against that shape.
func TestStandardObjectCoverageCommandRoutesCountCanonicalObjectsOnce(t *testing.T) {
	coverage := capability.BuildStandardObjectCoverageReport()
	unique := coverage
	unique.Objects = nil
	unique.Totals = capability.StandardObjectCoverageTotals{}
	seen := make(map[string]bool)
	for _, row := range coverage.Objects {
		if seen[strings.ToLower(row.Object)] {
			continue
		}
		seen[strings.ToLower(row.Object)] = true
		unique.Objects = append(unique.Objects, row)
		unique.Totals.Objects++
		unique.Totals.ShapeObjects++
		if row.Coverage == capability.StandardObjectCoverageBehavior {
			unique.Totals.BehaviorObjects++
		}
		if row.KeyPrefix != "" {
			unique.Totals.KeyPrefixes++
		}
		unique.Totals.Fields += row.Fields
		unique.Totals.Relationships += row.Relationships
		unique.Totals.RecordTypes += row.RecordTypes
		unique.Totals.Picklists += row.Picklists
		unique.Totals.References += row.References
	}
	var md, js bytes.Buffer
	if err := capability.WriteStandardObjectCoverageMarkdown(&md, unique); err != nil {
		t.Fatal(err)
	}
	if err := capability.WriteStandardObjectCoverageJSON(&js, unique); err != nil {
		t.Fatal(err)
	}
	summary := fmt.Sprintf("objects: %d\nfields: %d\nrelationships: %d\nrecordTypes: %d\n",
		unique.Totals.Objects, unique.Totals.Fields, unique.Totals.Relationships, unique.Totals.RecordTypes)
	testCanonicalReportCommandRoutes(t, "standard-objects", nil, summary, js.Bytes(), md.Bytes())
}

func TestStubInventoryCommandRoutesCountCanonicalObjectsOnce(t *testing.T) {
	root := t.TempDir()
	stubs := filepath.Join(root, "apex-sobject-stubs")
	if err := os.MkdirAll(stubs, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"FeedSignal", "feedSignal"} {
		dir := filepath.Join(stubs, fmt.Sprint(i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".cls"), []byte("global class "+name+" extends SObject {}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	report, err := capability.BuildStubInventoryReport(root)
	if err != nil {
		t.Fatal(err)
	}
	// Independently count canonical storage definitions, including feature fields.
	var active capability.StubInventoryActive
	seen := make(map[string]bool)
	for _, name := range storage.KnownStandardObjectNames() {
		definition, ok := storage.StandardObjectDefinition(name)
		if !ok || seen[strings.ToLower(definition.APIName)] {
			continue
		}
		seen[strings.ToLower(definition.APIName)] = true
		active.StandardObjects++
		active.StandardObjectFields += len(definition.Fields)
		active.StandardRelationships += len(definition.Relations)
		active.StandardRecordTypes += len(definition.RecordTypes)
		if definition.KeyPrefix != "" {
			active.StandardKeyPrefixes++
		}
		featureDefinition := storage.ObjectDefinition{APIName: definition.APIName}
		storage.EnsureStandardObjectFieldsForFeatures(&featureDefinition, []string{
			"PersonAccounts", "MultiCurrency", "Sites", "Communities", "StateAndCountryPicklist",
			"ContactsToMultipleAccounts", "PlatformCache", "EnableSetPasswordInApi", "AddCustomApps",
			"AnalyticsAdminPerms", "HealthCloud", "LightningExperience", "Chatter",
		})
		active.StandardObjectFieldsWithFeatures += len(featureDefinition.Fields)
	}
	report.Active = active
	var md, js bytes.Buffer
	if err := capability.WriteStubInventoryMarkdown(&md, report); err != nil {
		t.Fatal(err)
	}
	if err := capability.WriteStubInventoryJSON(&js, report); err != nil {
		t.Fatal(err)
	}
	summary := fmt.Sprintf("systemStubClasses: %d\ngeneratedPlatformTypes: %d\nsobjectStubClasses: %d\nactiveStandardObjects: %d\nsystemSourceMissingGeneratedTypeCount: %d\nsobjectSourceMissingActiveCount: %d\n",
		report.Source.SystemStubClasses, report.Generated.PlatformTypes, report.Source.SObjectStubClasses,
		active.StandardObjects, report.Gaps.SystemSourceMissingGeneratedTypeCount, report.Gaps.SObjectSourceMissingActiveCount)
	testCanonicalReportCommandRoutes(t, "stub-inventory", []string{"--source", root}, summary, js.Bytes(), md.Bytes())
}

func testCanonicalReportCommandRoutes(t *testing.T, command string, flags []string, summary string, js, md []byte) {
	t.Helper()
	for _, entrypoint := range []string{"direct", "compat"} {
		t.Run(entrypoint, func(t *testing.T) {
			baseArgs := []string{command}
			if entrypoint == "compat" {
				baseArgs = append([]string{"compat"}, baseArgs...)
			}
			baseArgs = append(baseArgs, flags...)
			for _, route := range []string{"summary", "json", "output-md", "output-json", "check-md", "check-json"} {
				t.Run(route, func(t *testing.T) {
					args := append([]string(nil), baseArgs...)
					want := []byte(summary)
					var path string
					switch route {
					case "json":
						args = append(args, "--json")
						want = js
					case "output-md", "output-json", "check-md", "check-json":
						ext := ".md"
						want = md
						if strings.HasSuffix(route, "json") {
							ext = ".JSON"
							want = js
						}
						path = filepath.Join(t.TempDir(), "report"+ext)
						if strings.HasPrefix(route, "check") {
							if err := os.WriteFile(path, want, 0o644); err != nil {
								t.Fatal(err)
							}
							args = append(args, "--check", path)
							want = []byte(path + ": up to date\n")
						} else {
							args = append(args, "--output", path)
						}
					}
					var stdout, stderr bytes.Buffer
					if code := Run(context.Background(), args, &stdout, &stderr); code != 0 {
						t.Fatalf("command exited %d: %s", code, stderr.String())
					}
					got := stdout.Bytes()
					if strings.HasPrefix(route, "output") {
						var err error
						got, err = os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
					}
					if !bytes.Equal(got, want) {
						// Keep catalog-sized reports out of failure output.
						t.Fatalf("%s output differs from canonical-once report (got %d bytes, want %d)", route, len(got), len(want))
					}
					if route == "json" && !json.Valid(got) {
						t.Fatal("invalid JSON")
					}
				})
			}
		})
	}
}
