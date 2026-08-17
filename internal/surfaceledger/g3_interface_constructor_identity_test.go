package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var g3InterfaceConstructorStaleIDs = []string{
	"apex:Messaging.NotificationActionHandler.NotificationActionHandler()",
	"apex:Process.Plugin.Plugin()",
	"apex:System.SandboxPostCopy.SandboxPostCopy()",
	"apex:eventbus.EventPublishFailureCallback.EventPublishFailureCallback()",
	"apex:eventbus.EventPublishSuccessCallback.EventPublishSuccessCallback()",
}

func TestG3InterfaceConstructorFixturesDoNotClaimAbstractConstruction(t *testing.T) {
	root := filepath.Join("..", "..")
	fixturesDir := filepath.Join(root, "docs", "fixtures")
	owners := map[string][]string{
		"current-base-deterministic-mock-required-messaging-001-api67.json": {"apex:Messaging.NotificationActionHandler"},
		"current-base-process-001-api67.json":                               {"apex:Process.Plugin"},
		"current-base-residual-runtime-api67.json":                          {"apex:System.SandboxPostCopy"},
		"current-base-eventbus-001-contracts-api67.json":                    {"apex:eventbus.EventPublishFailureCallback", "apex:eventbus.EventPublishSuccessCallback"},
	}
	ownerStaleIDs := map[string][]string{
		"current-base-deterministic-mock-required-messaging-001-api67.json": {g3InterfaceConstructorStaleIDs[0]},
		"current-base-process-001-api67.json":                               {g3InterfaceConstructorStaleIDs[1]},
		"current-base-residual-runtime-api67.json":                          {g3InterfaceConstructorStaleIDs[2]},
		"current-base-eventbus-001-contracts-api67.json":                    {g3InterfaceConstructorStaleIDs[3], g3InterfaceConstructorStaleIDs[4]},
	}

	paths, err := filepath.Glob(filepath.Join(fixturesDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no fixture documents found")
	}
	rawCounts := make(map[string]int, len(g3InterfaceConstructorStaleIDs))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Evidence []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, evidence := range document.Evidence {
			for _, id := range g3InterfaceConstructorStaleIDs {
				if evidence.SurfaceID == id {
					rawCounts[id]++
				}
			}
		}
	}
	for _, id := range g3InterfaceConstructorStaleIDs {
		if rawCounts[id] != 0 {
			t.Errorf("raw fixture ownership retains %s %d time(s)", id, rawCounts[id])
		}
	}

	allEvidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	assertG3InterfaceConstructorIDsAbsent(t, rowsByID(allEvidence), g3InterfaceConstructorStaleIDs, "full evidence snapshot")
	for _, id := range []string{
		"apex:Messaging.NotificationActionHandler.executeAction(Messaging.ActionableNotification)",
		"apex:Process.Plugin.describe()",
		"apex:Process.Plugin.invoke(Process.PluginRequest)",
		"apex:System.SandboxPostCopy.runApexClass(SandboxContext)",
		"apex:eventbus.EventPublishFailureCallback.onFailure(eventbus.FailureResult)",
		"apex:eventbus.EventPublishSuccessCallback.onSuccess(eventbus.SuccessResult)",
	} {
		if _, ok := rowsByID(allEvidence)[id]; !ok {
			t.Errorf("full evidence snapshot lost truthful callback evidence %s", id)
		}
	}

	for name, retainedTypeIDs := range owners {
		name, retainedTypeIDs := name, retainedTypeIDs
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(fixturesDir, name)
			fixture, err := compat.LoadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := compat.Validate(fixture); err != nil {
				t.Fatal(err)
			}
			rawOwnerCounts := make(map[string]int, len(fixture.Evidence))
			for _, evidence := range fixture.Evidence {
				rawOwnerCounts[evidence.SurfaceID]++
				for _, staleID := range ownerStaleIDs[name] {
					if evidence.SurfaceID == staleID {
						t.Fatalf("former owner retains stale constructor identity %s", staleID)
					}
				}
			}
			for id, count := range rawOwnerCounts {
				if count > 1 {
					t.Fatalf("raw fixture ownership duplicates %s %d time(s)", id, count)
				}
			}
			rows, err := BuildEvidenceSnapshot([]string{path})
			if err != nil {
				t.Fatal(err)
			}
			byID := rowsByID(rows)
			assertG3InterfaceConstructorIDsAbsent(t, byID, ownerStaleIDs[name], "former owner evidence snapshot")
			for _, retainedTypeID := range retainedTypeIDs {
				if _, ok := byID[retainedTypeID]; !ok {
					t.Fatalf("former owner lost truthful interface type evidence %s", retainedTypeID)
				}
			}
			if name == "current-base-residual-runtime-api67.json" {
				if len(fixture.Source) != 1 || fixture.Source[0].Path != "anonymous.apex" {
					t.Fatalf("SandboxPostCopy fixture must retain one anonymous source: %#v", fixture.Source)
				}
				for _, program := range append([]string{fixture.Command.Args[0]}, fixture.Source[0].Content) {
					if strings.Contains(program, "new SandboxPostCopy()") {
						t.Fatalf("SandboxPostCopy fixture directly constructs the interface: %q", program)
					}
					if strings.Contains(program, ".runApexClass(") {
						t.Fatalf("SandboxPostCopy fixture claims a callback invocation it does not execute: %q", program)
					}
				}
			}
			if result, err := compat.Run(fixture); err != nil || !result.OK {
				t.Fatalf("fixture execution = %#v, error = %v", result, err)
			}
		})
	}

	for _, marker := range []string{
		"implements Messaging.NotificationActionHandler",
		"implements Process.Plugin",
		"implements eventbus.EventPublishFailureCallback",
		"implements eventbus.EventPublishSuccessCallback",
	} {
		found := false
		for name := range owners {
			data, err := os.ReadFile(filepath.Join(fixturesDir, name))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), marker) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("fixture sources lack concrete interface witness %q", marker)
		}
	}
}

func assertG3InterfaceConstructorIDsAbsent(t *testing.T, rows map[string]SurfaceLedgerRow, ids []string, source string) {
	t.Helper()
	for _, id := range ids {
		if _, ok := rows[id]; ok {
			t.Errorf("%s retains stale interface constructor identity %s", source, id)
		}
	}
}
