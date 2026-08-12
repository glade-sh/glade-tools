package surfaceledger

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func TestCB28AsyncInfoContextGuardEvidenceClosesSupportedSurface(t *testing.T) {
	fixturePath := func(name string) string {
		return filepath.Join("..", "..", "docs", "fixtures", name)
	}

	corePath := fixturePath("core-runtime-asyncinfo-context-unsupported.json")
	contextPath := fixturePath("async-unsupported-context-edges.json")
	delayPath := fixturePath("async-options-delay-local-evidence.json")
	core := loadCB28Fixture(t, corePath)
	context := loadCB28Fixture(t, contextPath)
	delay := loadCB28Fixture(t, delayPath)

	coreProgram := "Boolean caughtHasMax = false;\ntry {\n  AsyncInfo.hasMaxStackDepth();\n} catch (AsyncException e) {\n  caughtHasMax = e.getMessage().contains('hasMaxStackDepth');\n}\nSystem.assert(caughtHasMax);\nBoolean caughtCurrent = false;\ntry {\n  AsyncInfo.getCurrentQueueableStackDepth();\n} catch (AsyncException e) {\n  caughtCurrent = e.getMessage().contains('getCurrentQueueableStackDepth');\n}\nSystem.assert(caughtCurrent);\nBoolean caughtMaximum = false;\ntry {\n  AsyncInfo.getMaximumQueueableStackDepth();\n} catch (AsyncException e) {\n  caughtMaximum = e.getMessage().contains('getMaximumQueueableStackDepth');\n}\nSystem.assert(caughtMaximum);\nBoolean caughtMinimum = false;\ntry {\n  AsyncInfo.getMinimumQueueableDelayInMinutes();\n} catch (AsyncException e) {\n  caughtMinimum = e.getMessage().contains('getMinimumQueueableDelayInMinutes');\n}\nSystem.assert(caughtMinimum);\n"
	assertCB28FixtureProgram(t, core, coreProgram)
	if core.Expected.Error != nil {
		t.Fatalf("core fixture expected error = %#v, want successful result", core.Expected.Error)
	}
	var coreResult struct {
		Debug any  `json:"debug"`
		OK    bool `json:"ok"`
	}
	if err := json.Unmarshal(core.Expected.Result, &coreResult); err != nil {
		t.Fatalf("decode core fixture result: %v", err)
	}
	if coreResult.Debug != nil || !coreResult.OK {
		t.Fatalf("core fixture result = %#v, want debug:null and ok:true", coreResult)
	}

	contextProgram := "AsyncInfo.getCurrentQueueableStackDepth();"
	assertCB28FixtureProgram(t, context, contextProgram)
	if context.Expected.Error == nil || context.Expected.Error.Type != "System.AsyncException" || context.Expected.Error.Message != "getCurrentQueueableStackDepth is not allowed outside a Queueable or Finalizer execution" {
		t.Fatalf("context fixture expected error = %#v, want unchanged AsyncException contract", context.Expected.Error)
	}

	if len(delay.Source) != 2 || !strings.Contains(delay.Source[0].Content, "seenDelay = System.AsyncInfo.getMinimumQueueableDelayInMinutes()") || !strings.Contains(delay.Source[1].Content, "System.assertEquals(7, DelayFixture.seenDelay)") {
		t.Fatalf("delay fixture no longer proves an in-context value of 7: %#v", delay.Source)
	}
	if delay.Expected.Error != nil {
		t.Fatalf("delay fixture expected error = %#v, want successful test result", delay.Expected.Error)
	}
	var delayResult struct {
		OK     bool `json:"ok"`
		Total  int  `json:"total"`
		Passed int  `json:"passed"`
		Failed int  `json:"failed"`
		Errors int  `json:"errors"`
	}
	if err := json.Unmarshal(delay.Expected.Result, &delayResult); err != nil {
		t.Fatalf("decode delay fixture result: %v", err)
	}
	if !delayResult.OK || delayResult.Total != 1 || delayResult.Passed != 1 || delayResult.Failed != 0 || delayResult.Errors != 0 {
		t.Fatalf("delay fixture result = %#v, want one passing test", delayResult)
	}

	assertCB28ExecutableEvidence(t, core, map[string]bool{
		"apex:System.AsyncInfo":                                     true,
		"apex:System.AsyncInfo.hasMaxStackDepth()":                  true,
		"apex:System.AsyncInfo.getCurrentQueueableStackDepth()":     true,
		"apex:System.AsyncInfo.getMaximumQueueableStackDepth()":     true,
		"apex:System.AsyncInfo.getMinimumQueueableDelayInMinutes()": true,
	})
	assertCB28ExecutableEvidence(t, context, map[string]bool{
		"apex:System.AsyncInfo": true,
	})

	evidence, err := BuildEvidenceSnapshot([]string{corePath, contextPath, delayPath})
	if err != nil {
		t.Fatal(err)
	}
	ledger := Merge(nil, nil, BuildGladeSnapshot(), evidence)
	byID := rowsByID(ledger.Rows)
	for _, id := range []string{
		"apex:System.AsyncInfo",
		"apex:System.AsyncInfo.hasMaxStackDepth()",
		"apex:System.AsyncInfo.getCurrentQueueableStackDepth()",
		"apex:System.AsyncInfo.getMaximumQueueableStackDepth()",
		"apex:System.AsyncInfo.getMinimumQueueableDelayInMinutes()",
	} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing AsyncInfo target row %s", id)
		}
		if row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture || row.GapClass != "" {
			t.Fatalf("%s merged state = shape:%s behavior:%s evidence:%s gap:%s", id, row.GladeShape, row.GladeBehavior, row.Evidence, row.GapClass)
		}
	}
}

func loadCB28Fixture(t *testing.T, path string) compat.Fixture {
	t.Helper()
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func assertCB28FixtureProgram(t *testing.T, fixture compat.Fixture, want string) {
	t.Helper()
	if fixture.Command.Kind != "exec" || len(fixture.Command.Args) != 1 || fixture.Command.Args[0] != want {
		t.Fatalf("%s command = %#v, want unchanged exec program", fixture.Name, fixture.Command)
	}
	if len(fixture.Source) != 1 || fixture.Source[0].Path != "anonymous.apex" || fixture.Source[0].Content != want {
		t.Fatalf("%s source = %#v, want unchanged assertion program", fixture.Name, fixture.Source)
	}
}

func assertCB28ExecutableEvidence(t *testing.T, fixture compat.Fixture, want map[string]bool) {
	t.Helper()
	got := map[string]compat.FixtureEvidence{}
	for _, evidence := range fixture.Evidence {
		id := evidence.SurfaceID
		if id == "" && evidence.Symbol == "AsyncInfo" {
			id = "apex:System.AsyncInfo"
		}
		if want[id] {
			got[id] = evidence
		}
	}
	for id := range want {
		evidence, ok := got[id]
		if !ok {
			t.Errorf("%s missing executable evidence claim %s", fixture.Name, id)
			continue
		}
		if evidence.Kind != "exec" {
			t.Errorf("%s evidence %s kind = %q, want exec", fixture.Name, id, evidence.Kind)
		}
		if !strings.Contains(strings.ToLower(evidence.Notes), "out-of-context asyncexception") {
			t.Errorf("%s evidence %s notes = %q, want executed context-guard contract explanation", fixture.Name, id, evidence.Notes)
		}
	}
}
