package corpusassurance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunCampaignPreflightsWholeGraphThenResumesPassedPhases(t *testing.T) {
	root := t.TempDir()
	one := filepath.Join(root, "one.txt")
	two := filepath.Join(root, "two.txt")
	specPath := filepath.Join(root, "CAMPAIGN.json")
	statePath := filepath.Join(root, "CAMPAIGN_STATE.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "family-wave-01",
		SelectedSurfaceIDs: []string{"apex:System.List.deepClone()"},
		Phases: []CampaignPhase{
			{ID: "fixture", Family: "collections", ProofClass: "local-runtime", CWD: root, Argv: []string{"/bin/sh", "-c", "printf one > " + one}, Log: filepath.Join(root, "fixture.log"), Outputs: []string{one}},
			{ID: "delta", Family: "collections", ProofClass: "identity", DependsOn: []string{"fixture"}, CWD: root, Argv: []string{"/bin/sh", "-c", "printf two > " + two}, Log: filepath.Join(root, "delta.log"), Outputs: []string{two}},
		},
	})

	result, err := RunCampaign(context.Background(), specPath, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed != 2 || result.Total != 2 {
		t.Fatalf("result = %+v", result)
	}
	state := readCampaignState(t, statePath)
	if len(state.Phases[0].Attempts) != 1 || len(state.Phases[1].Attempts) != 1 || state.Phases[0].Status != "passed" || state.Phases[1].Status != "passed" {
		t.Fatalf("state = %+v", state)
	}

	resumed, err := RunCampaign(context.Background(), specPath, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Skipped != 2 || len(readCampaignState(t, statePath).Phases[0].Attempts) != 1 {
		t.Fatalf("resume mutated completed state: result=%+v state=%+v", resumed, readCampaignState(t, statePath))
	}
}

func TestRunCampaignRejectsAnyPendingCollisionBeforeExecuting(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "marker")
	collision := filepath.Join(root, "exists")
	if err := os.WriteFile(collision, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(root, "CAMPAIGN.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "collision",
		SelectedSurfaceIDs: []string{"apex:System.Map.containsValue(Object)"},
		Phases: []CampaignPhase{
			{ID: "first", Family: "map", ProofClass: "local-runtime", CWD: root, Argv: []string{"/bin/sh", "-c", "touch " + marker}, Log: filepath.Join(root, "first.log")},
			{ID: "second", Family: "map", ProofClass: "identity", DependsOn: []string{"first"}, CWD: root, Argv: []string{"/usr/bin/true"}, Log: filepath.Join(root, "second.log"), Outputs: []string{collision}},
		},
	})

	if _, err := RunCampaign(context.Background(), specPath, filepath.Join(root, "state.json")); err == nil {
		t.Fatal("campaign accepted a pending output collision")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("first phase ran before whole-graph preflight: %v", err)
	}
}

func TestRunCampaignRequiresUsageOrderingAndExactJSON(t *testing.T) {
	root := t.TempDir()
	specPath := filepath.Join(root, "CAMPAIGN.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "bad-order",
		SelectedSurfaceIDs: []string{"apex:System.Test.setMock(String,Object)"},
		Phases: []CampaignPhase{
			{ID: "draft", Kind: "usage-draft", Family: "assurance", ProofClass: "usage", CWD: root, Argv: []string{"/usr/bin/true"}, Log: filepath.Join(root, "draft.log")},
			{ID: "raw", Kind: "raw-corpus-usage", Family: "assurance", ProofClass: "usage", CWD: root, Argv: []string{"/usr/bin/true"}, Log: filepath.Join(root, "raw.log")},
		},
	})
	if _, err := RunCampaign(context.Background(), specPath, filepath.Join(root, "state.json")); err == nil {
		t.Fatal("campaign accepted usage-draft before raw-corpus-usage and sealed-profile")
	}

	unknown := []byte(`{"schemaVersion":1,"campaignId":"x","selectedSurfaceIds":["a"],"phases":[],"unknown":true}`)
	if err := os.WriteFile(specPath, unknown, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunCampaign(context.Background(), specPath, filepath.Join(root, "other-state.json")); err == nil {
		t.Fatal("campaign accepted unknown JSON field")
	}
}

func TestRunCampaignRequiresDependenciesToPrecedeTheirConsumers(t *testing.T) {
	root := t.TempDir()
	specPath := filepath.Join(root, "CAMPAIGN.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "not-topological",
		SelectedSurfaceIDs: []string{"apex:System.Set.deepClone()"},
		Phases: []CampaignPhase{
			{ID: "consumer", Family: "set", ProofClass: "delta", DependsOn: []string{"producer"}, CWD: root, Argv: []string{"/usr/bin/true"}, Log: filepath.Join(root, "consumer.log")},
			{ID: "producer", Family: "set", ProofClass: "fixture", CWD: root, Argv: []string{"/usr/bin/true"}, Log: filepath.Join(root, "producer.log")},
		},
	})
	if _, err := RunCampaign(context.Background(), specPath, filepath.Join(root, "state.json")); err == nil {
		t.Fatal("campaign accepted a dependency after its consumer")
	}
}

func TestPromoteCampaignCreatesCompactCreateOnlyHandoff(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "out.txt")
	specPath := filepath.Join(root, "CAMPAIGN.json")
	statePath := filepath.Join(root, "CAMPAIGN_STATE.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "promotion",
		SelectedSurfaceIDs: []string{"apex:System.Set.deepClone()"},
		Phases:             []CampaignPhase{{ID: "family", Family: "set", ProofClass: "local-runtime", CWD: root, Argv: []string{"/bin/sh", "-c", "printf ok > " + out}, Log: filepath.Join(root, "family.log"), Outputs: []string{out}}},
	})
	if _, err := RunCampaign(context.Background(), specPath, statePath); err != nil {
		t.Fatal(err)
	}
	promotionRoot := filepath.Join(root, "promotion-root")
	receipt, err := PromoteCampaign(specPath, statePath, promotionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "ready-for-promotion" || len(receipt.SelectedSurfaceIDs) != 1 {
		t.Fatalf("receipt = %+v", receipt)
	}
	for _, name := range []string{"CAMPAIGN.json", "CAMPAIGN_STATE.json", "CAMPAIGN_PROMOTION.json"} {
		if _, err := os.Stat(filepath.Join(promotionRoot, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	if _, err := PromoteCampaign(specPath, statePath, promotionRoot); err == nil {
		t.Fatal("promotion overwrote create-only root")
	}
}

func TestCampaignLockBlocksConcurrentRunAndPromotion(t *testing.T) {
	root := t.TempDir()
	specPath := filepath.Join(root, "CAMPAIGN.json")
	statePath := filepath.Join(root, "CAMPAIGN_STATE.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "locked",
		SelectedSurfaceIDs: []string{"apex:System.Map.containsValue(Object)"},
		Phases:             []CampaignPhase{{ID: "check", Family: "map", ProofClass: "fixture", CWD: root, Argv: []string{"/usr/bin/true"}, Log: filepath.Join(root, "check.log")}},
	})
	if err := os.WriteFile(statePath+".lock", []byte("active\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunCampaign(context.Background(), specPath, statePath); err == nil {
		t.Fatal("concurrent campaign run ignored active lock")
	}
	if _, err := PromoteCampaign(specPath, statePath, filepath.Join(root, "promotion")); err == nil {
		t.Fatal("campaign promotion ignored active lock")
	}
}

func TestRunCampaignRejectsStateAsProducerOutput(t *testing.T) {
	root := t.TempDir()
	specPath := filepath.Join(root, "CAMPAIGN.json")
	statePath := filepath.Join(root, "CAMPAIGN_STATE.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "state-collision",
		SelectedSurfaceIDs: []string{"apex:System.Set.deepClone()"},
		Phases: []CampaignPhase{{
			ID: "bad", Family: "set", ProofClass: "fixture", CWD: root,
			Argv: []string{"/bin/sh", "-c", "printf owned > " + statePath},
			Log:  filepath.Join(root, "bad.log"), Outputs: []string{statePath},
		}},
	})
	if _, err := RunCampaign(context.Background(), specPath, statePath); err == nil {
		t.Fatal("campaign allowed a producer to own its state file")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("campaign created or corrupted state before rejecting collision: %v", err)
	}
}

func TestRunCampaignRejectsChangedBindingAndExecutable(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "phase-command")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf ok > \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "output")
	specPath := filepath.Join(root, "CAMPAIGN.json")
	statePath := filepath.Join(root, "CAMPAIGN_STATE.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "bound",
		SelectedSurfaceIDs: []string{"apex:System.Set.deepClone()"},
		Phases:             []CampaignPhase{{ID: "phase", Family: "set", ProofClass: "fixture", CWD: root, Argv: []string{executable, output}, Log: filepath.Join(root, "phase.log"), Outputs: []string{output}}},
	})
	if _, err := RunCampaign(context.Background(), specPath, statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf changed > \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := RunCampaign(context.Background(), specPath, statePath); err == nil {
		t.Fatal("campaign skipped a passed phase after its executable changed")
	}

	spec := readCampaignSpec(t, specPath)
	if err := os.WriteFile(spec.Bindings[0].Path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteCampaign(specPath, statePath, filepath.Join(root, "promotion")); err == nil {
		t.Fatal("campaign promoted after a candidate/tools/input binding changed")
	}
}

func TestRunCampaignFailsPhaseThatMutatesBinding(t *testing.T) {
	root := t.TempDir()
	specPath := filepath.Join(root, "CAMPAIGN.json")
	statePath := filepath.Join(root, "CAMPAIGN_STATE.json")
	spec := CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "mutates-binding",
		SelectedSurfaceIDs: []string{"apex:System.Set.deepClone()"},
		Phases:             []CampaignPhase{{ID: "bad", Family: "set", ProofClass: "fixture", CWD: root, Argv: []string{"/bin/sh", "-c", "printf changed > " + filepath.Join(root, "candidate.bin")}, Log: filepath.Join(root, "bad.log")}},
	}
	writeCampaignSpec(t, specPath, spec)
	if _, err := RunCampaign(context.Background(), specPath, statePath); err == nil {
		t.Fatal("campaign passed a phase that mutated a bound input")
	}
	state := readCampaignState(t, statePath)
	if state.Phases[0].Status != "failed" || state.Phases[0].Attempts[0].Status != "failed" {
		t.Fatalf("binding mutation did not produce retained failure: %+v", state.Phases[0])
	}
}

func TestRunCampaignDoesNotInheritAmbientEnvironment(t *testing.T) {
	t.Setenv("CAMPAIGN_AMBIENT_LEAK", "present")
	root := t.TempDir()
	specPath := filepath.Join(root, "CAMPAIGN.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "empty-env",
		SelectedSurfaceIDs: []string{"apex:System.Set.deepClone()"},
		Phases:             []CampaignPhase{{ID: "phase", Family: "set", ProofClass: "fixture", CWD: root, Argv: []string{"/bin/sh", "-c", "test -z \"$CAMPAIGN_AMBIENT_LEAK\""}, Log: filepath.Join(root, "phase.log")}},
	})
	if _, err := RunCampaign(context.Background(), specPath, filepath.Join(root, "state.json")); err != nil {
		t.Fatalf("campaign inherited ambient environment: %v", err)
	}
}

func TestPromoteCampaignRejectsMalformedStateWithoutPanicking(t *testing.T) {
	root := t.TempDir()
	specPath := filepath.Join(root, "CAMPAIGN.json")
	statePath := filepath.Join(root, "CAMPAIGN_STATE.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "malformed-state",
		SelectedSurfaceIDs: []string{"apex:System.Set.deepClone()"},
		Phases:             []CampaignPhase{{ID: "phase", Family: "set", ProofClass: "fixture", CWD: root, Argv: []string{"/usr/bin/true"}, Log: filepath.Join(root, "phase.log")}},
	})
	if _, err := RunCampaign(context.Background(), specPath, statePath); err != nil {
		t.Fatal(err)
	}
	state := readCampaignState(t, statePath)
	state.Phases = nil
	writeCampaignState(t, statePath, state)
	if _, err := PromoteCampaign(specPath, statePath, filepath.Join(root, "promotion")); err == nil {
		t.Fatal("promotion accepted malformed campaign state")
	}
}

func TestCampaignStateRejectsImpossibleNonFinalAttemptStatus(t *testing.T) {
	root := t.TempDir()
	specPath := filepath.Join(root, "CAMPAIGN.json")
	statePath := filepath.Join(root, "CAMPAIGN_STATE.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "impossible-history",
		SelectedSurfaceIDs: []string{"apex:System.Set.deepClone()"},
		Phases:             []CampaignPhase{{ID: "phase", Family: "set", ProofClass: "fixture", CWD: root, Argv: []string{"/usr/bin/true"}, Log: filepath.Join(root, "phase.log")}},
	})
	if _, err := RunCampaign(context.Background(), specPath, statePath); err != nil {
		t.Fatal(err)
	}
	spec := readCampaignSpec(t, specPath)
	state := readCampaignState(t, statePath)
	duplicate := state.Phases[0].Attempts[0]
	duplicate.Number = 2
	state.Phases[0].Attempts = append(state.Phases[0].Attempts, duplicate)
	if err := validateCampaignState(spec, state); err == nil {
		t.Fatal("campaign state accepted a non-final passed attempt")
	}
}

func TestRunCampaignCanonicalizesControlPathAliases(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "alias"), 0o700); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(root, "CAMPAIGN.json")
	statePath := filepath.Join(root, "CAMPAIGN_STATE.json")
	stateAlias := filepath.Join(root, "alias", "..", "CAMPAIGN_STATE.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "alias",
		SelectedSurfaceIDs: []string{"apex:System.Set.deepClone()"},
		Phases:             []CampaignPhase{{ID: "phase", Family: "set", ProofClass: "fixture", CWD: root, Argv: []string{"/usr/bin/true"}, Log: filepath.Join(root, "phase.log"), Outputs: []string{stateAlias}}},
	})
	if _, err := RunCampaign(context.Background(), specPath, statePath); err == nil {
		t.Fatal("campaign accepted an aliased state output")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("campaign touched state before rejecting alias: %v", err)
	}
}

func TestRunCampaignRetainsFailedAttemptWhenRetryPasses(t *testing.T) {
	root := t.TempDir()
	counter := filepath.Join(root, "counter")
	output := filepath.Join(root, "output")
	specPath := filepath.Join(root, "CAMPAIGN.json")
	statePath := filepath.Join(root, "CAMPAIGN_STATE.json")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "retry",
		SelectedSurfaceIDs: []string{"apex:System.Set.deepClone()"},
		Phases:             []CampaignPhase{{ID: "flaky", Family: "set", ProofClass: "fixture", CWD: root, Argv: []string{"/bin/sh", "-c", "if [ ! -e " + counter + " ]; then touch " + counter + "; exit 9; fi; printf ok > " + output}, Log: filepath.Join(root, "flaky.log"), Outputs: []string{output}}},
	})
	if _, err := RunCampaign(context.Background(), specPath, statePath); err == nil {
		t.Fatal("flaky phase unexpectedly passed first attempt")
	}
	if _, err := RunCampaign(context.Background(), specPath, statePath); err != nil {
		t.Fatal(err)
	}
	state := readCampaignState(t, statePath)
	if len(state.Phases[0].Attempts) != 2 || state.Phases[0].Attempts[0].Status != "failed" || state.Phases[0].Attempts[1].Status != "passed" {
		t.Fatalf("retry history was not retained: %+v", state.Phases[0])
	}
}

func TestPromoteCampaignVerifiesHistoricalAttemptArtifacts(t *testing.T) {
	root := t.TempDir()
	counter := filepath.Join(root, "counter")
	specPath := filepath.Join(root, "CAMPAIGN.json")
	statePath := filepath.Join(root, "CAMPAIGN_STATE.json")
	logPath := filepath.Join(root, "flaky.log")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "historical-artifacts",
		SelectedSurfaceIDs: []string{"apex:System.Set.deepClone()"},
		Phases:             []CampaignPhase{{ID: "flaky", Family: "set", ProofClass: "fixture", CWD: root, Argv: []string{"/bin/sh", "-c", "if [ ! -e " + counter + " ]; then touch " + counter + "; exit 9; fi"}, Log: logPath}},
	})
	if _, err := RunCampaign(context.Background(), specPath, statePath); err == nil {
		t.Fatal("flaky phase unexpectedly passed first attempt")
	}
	if _, err := RunCampaign(context.Background(), specPath, statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteCampaign(specPath, statePath, filepath.Join(root, "promotion")); err == nil {
		t.Fatal("campaign promoted after historical failed log changed")
	}
}

func TestRunCampaignRecoversAbandonedRunningAttempt(t *testing.T) {
	root := t.TempDir()
	specPath := filepath.Join(root, "CAMPAIGN.json")
	statePath := filepath.Join(root, "CAMPAIGN_STATE.json")
	logPath := filepath.Join(root, "phase.log")
	writeCampaignSpec(t, specPath, CampaignSpec{
		SchemaVersion:      1,
		CampaignID:         "interrupted",
		SelectedSurfaceIDs: []string{"apex:System.Set.deepClone()"},
		Phases:             []CampaignPhase{{ID: "phase", Family: "set", ProofClass: "fixture", CWD: root, Argv: []string{"/usr/bin/true"}, Log: logPath}},
	})
	if err := os.WriteFile(logPath, []byte("partial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	executableSHA, err := sha256File("/usr/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	startedAt := "2026-08-18T00:00:00Z"
	writeCampaignState(t, statePath, CampaignState{
		SchemaVersion: 1, CampaignID: "interrupted", SpecSHA256: replayBytesSHA256(specBytes),
		Phases: []CampaignPhaseState{{ID: "phase", Status: "running", Attempts: []CampaignAttempt{{Number: 1, Status: "running", ExecutableSHA256: executableSHA, StartedAt: startedAt}}}},
	})
	if _, err := RunCampaign(context.Background(), specPath, statePath); err != nil {
		t.Fatal(err)
	}
	state := readCampaignState(t, statePath)
	canonicalLog, err := canonicalCampaignPath(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Phases[0].Attempts) != 2 || state.Phases[0].Attempts[0].Status != "interrupted" || state.Phases[0].Attempts[0].Log.Path != canonicalLog || state.Phases[0].Attempts[1].Status != "passed" {
		t.Fatalf("interrupted attempt was not retained: %+v", state.Phases[0])
	}
}

func writeCampaignSpec(t *testing.T, path string, spec CampaignSpec) {
	t.Helper()
	if len(spec.Bindings) == 0 {
		root := filepath.Dir(path)
		for _, name := range []string{"candidate", "tools"} {
			bindingPath := filepath.Join(root, name+".bin")
			if err := os.WriteFile(bindingPath, []byte(name), 0o600); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(bindingPath)
			if err != nil {
				t.Fatal(err)
			}
			spec.Bindings = append(spec.Bindings, CampaignBinding{Name: name, Path: bindingPath, SHA256: replayBytesSHA256(data)})
		}
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readCampaignSpec(t *testing.T, path string) CampaignSpec {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var spec CampaignSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	return spec
}

func writeCampaignState(t *testing.T, path string, state CampaignState) {
	t.Helper()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readCampaignState(t *testing.T, path string) CampaignState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state CampaignState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}
