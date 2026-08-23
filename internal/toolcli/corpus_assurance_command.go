package toolcli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/glade-sh/glade/tools/internal/corpusassurance"
)

func runCorpusAssurance(ctx context.Context, args []string, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) == 0 || isHelpArg(args[0]) {
		printCorpusAssuranceHelp(w)
		return nil
	}
	if len(args) > 1 && isHelpArg(args[1]) {
		printCorpusAssuranceHelp(w)
		return nil
	}
	switch args[0] {
	case "orchestrator":
		return runCorpusAssuranceOrchestrator(ctx, args[1:], w)
	case "campaign":
		flags := flag.NewFlagSet("corpus assurance campaign", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		spec, state := flags.String("spec", "", ""), flags.String("state", "", "")
		promote := flags.Bool("promote", false, "")
		output := flags.String("out", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*spec, *state); err != nil {
			return err
		}
		if *promote {
			if err := requiredAssuranceFlags(*output); err != nil {
				return err
			}
			receipt, err := corpusassurance.PromoteCampaign(*spec, *state, *output)
			if err != nil {
				return err
			}
			return writeCorpusAssuranceResult(w, "campaign promote", len(receipt.SelectedSurfaceIDs), *output)
		}
		if *output != "" {
			return errors.New("campaign --out requires --promote")
		}
		result, err := corpusassurance.RunCampaign(ctx, *spec, *state)
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "campaign", result.Passed, *state)
	case "candidate-build":
		flags := flag.NewFlagSet("corpus assurance candidate-build", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		candidateRoot, toolsRoot := flags.String("candidate-root", "", ""), flags.String("tools-root", "", "")
		candidateRef, toolsRef := flags.String("candidate-ref", "", ""), flags.String("tools-ref", "", "")
		candidateOutput, toolsOutput := flags.String("candidate-output", "", ""), flags.String("tools-output", "", "")
		receipt, review, freeze := flags.String("receipt-output", "", ""), flags.String("review-output", "", ""), flags.String("tools-freeze-output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*candidateRoot, *toolsRoot, *candidateRef, *toolsRef, *candidateOutput, *toolsOutput, *receipt, *review, *freeze); err != nil {
			return err
		}
		_, err := corpusassurance.CreateCandidateBuildReceipt(corpusassurance.CandidateBuildRequest{CandidateRoot: *candidateRoot, ToolsRoot: *toolsRoot, CandidateRef: *candidateRef, ToolsRef: *toolsRef, CandidateOutput: *candidateOutput, ToolsOutput: *toolsOutput, ReceiptOutput: *receipt, ReviewOutput: *review, ToolsFreezeOutput: *freeze})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "candidate-build", 2, *receipt)
	case "candidate-authority":
		flags := flag.NewFlagSet("corpus assurance candidate-authority", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		candidateRoot, toolsRoot := flags.String("candidate-root", "", ""), flags.String("tools-root", "", "")
		receipt, review := flags.String("receipt", "", ""), flags.String("review", "", "")
		output := flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*candidateRoot, *toolsRoot, *receipt, *review, *output); err != nil {
			return err
		}
		if _, err := corpusassurance.CreateCandidateAuthority(corpusassurance.CandidateAuthorityRequest{CandidateRoot: *candidateRoot, ToolsRoot: *toolsRoot, ReceiptPath: *receipt, ReviewPath: *review, OutputPath: *output}); err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "candidate-authority", 1, *output)
	case "attempt-init":
		flags := flag.NewFlagSet("corpus assurance attempt-init", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		inventory, authority := flags.String("inventory-spec", "", ""), flags.String("candidate-authority", "", "")
		candidate, candidateRoot := flags.String("candidate", "", ""), flags.String("candidate-root", "", "")
		tools, toolsRoot := flags.String("tools", "", ""), flags.String("tools-root", "", "")
		replayHost, replayParent := flags.String("replay-host", "", ""), flags.String("replay-parent", "", "")
		salesforceHost, salesforceParent := flags.String("salesforce-host", "", ""), flags.String("salesforce-parent", "", "")
		runID, outputDir := flags.String("run-id", "", ""), flags.String("output-dir", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*inventory, *authority, *candidate, *candidateRoot, *tools, *toolsRoot, *replayHost, *replayParent, *salesforceHost, *salesforceParent, *runID, *outputDir); err != nil {
			return err
		}
		_, err := corpusassurance.CreateAssuranceAttemptWithAuthorities(corpusassurance.AssuranceAttemptInitRequest{InventoryPath: *inventory, CandidateAuthorityPath: *authority, CandidatePath: *candidate, CandidateRoot: *candidateRoot, ToolsPath: *tools, ToolsRoot: *toolsRoot, ReplayHost: *replayHost, ReplayParent: *replayParent, SalesforceHost: *salesforceHost, SalesforceParent: *salesforceParent, RunID: *runID, OutputDir: *outputDir})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "attempt-init", 3, *outputDir)
	case "attempt":
		flags := flag.NewFlagSet("corpus assurance attempt", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		inventory, authority := flags.String("inventory-spec", "", ""), flags.String("candidate-authority", "", "")
		candidate, candidateRoot := flags.String("candidate", "", ""), flags.String("candidate-root", "", "")
		tools, toolsRoot, output := flags.String("tools", "", ""), flags.String("tools-root", "", ""), flags.String("output", "", "")
		replayCleanup, salesforceCleanup := flags.String("replay-cleanup-authority", "", ""), flags.String("salesforce-cleanup-authority", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*inventory, *authority, *candidate, *candidateRoot, *tools, *toolsRoot, *replayCleanup, *salesforceCleanup, *output); err != nil {
			return err
		}
		_, err := corpusassurance.CreateAssuranceAttempt(corpusassurance.AssuranceAttemptRequest{InventoryPath: *inventory, CandidateAuthorityPath: *authority, CandidatePath: *candidate, CandidateRoot: *candidateRoot, ToolsPath: *tools, ToolsRoot: *toolsRoot, RemoteCleanupAuthorityPaths: map[string]string{"replay-worker": *replayCleanup, "salesforce-worker": *salesforceCleanup}, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "attempt", 1, *output)
	case "prepare":
		flags := flag.NewFlagSet("corpus assurance prepare", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		inventory, attempt, output := flags.String("inventory-spec", "", ""), flags.String("attempt", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*inventory, *attempt, *output); err != nil {
			return err
		}
		manifest, err := corpusassurance.PrepareInventory(*inventory, *attempt, *output)
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "prepare", len(manifest.Repositories), *output)
	case "usage-draft":
		flags := flag.NewFlagSet("corpus assurance usage-draft", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		inventory, ledger := flags.String("inventory-spec", "", ""), flags.String("ledger", "", "")
		manifest, profile := flags.String("manifest", "", ""), flags.String("profile", "", "")
		policy, output := flags.String("policy", "", ""), flags.String("output", "", "")
		decisionTemplate := flags.String("decision-template", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*inventory, *ledger, *manifest, *profile, *policy, *output); err != nil {
			return err
		}
		var draft corpusassurance.UsageDecisionDraft
		var err error
		if *decisionTemplate == "" {
			draft, err = corpusassurance.DraftUsageDecisions(*inventory, *ledger, *manifest, *profile, *policy, *output)
		} else {
			draft, err = corpusassurance.DraftUsageDecisionsWithTemplate(*inventory, *ledger, *manifest, *profile, *policy, *output, *decisionTemplate)
		}
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "usage-draft", len(draft.Automatic)+len(draft.Unresolved), *output)
	case "usage":
		flags := flag.NewFlagSet("corpus assurance usage", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		inventory, ledger := flags.String("inventory-spec", "", ""), flags.String("ledger", "", "")
		manifest, profile := flags.String("manifest", "", ""), flags.String("profile", "", "")
		policy := flags.String("policy", "", "")
		decisions, output := flags.String("decisions", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*inventory, *ledger, *manifest, *profile, *policy, *decisions, *output); err != nil {
			return err
		}
		usage, err := corpusassurance.BuildSealedCorpusUsage(*inventory, *ledger, *manifest, *profile, *policy, *decisions, *output)
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "usage", len(usage.Reconciliation.Usage), *output)
	case "replay":
		flags := flag.NewFlagSet("corpus assurance replay", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		host := flags.String("host", "", "")
		inventory, root, manifest := flags.String("inventory-spec", "", ""), flags.String("root-manifest", "", ""), flags.String("host-manifest", "", "")
		candidatePath := flags.String("candidate", "", "")
		toolsPath := flags.String("tools", "", "")
		output := flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*host, *inventory, *root, *manifest, *candidatePath, *toolsPath, *output); err != nil {
			return err
		}
		shard, err := corpusassurance.RunReplay(corpusassurance.ReplayRequest{Host: *host, CandidatePath: *candidatePath, ToolsPath: *toolsPath, InventoryPath: *inventory, RootManifestPath: *root, HostManifestPath: *manifest, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "replay", len(shard.Repositories), *output)
	case "merge-replay":
		flags := flag.NewFlagSet("corpus assurance merge-replay", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		inventory, root, output := flags.String("inventory-spec", "", ""), flags.String("root-manifest", "", ""), flags.String("output", "", "")
		var manifests, shards assurancePathList
		flags.Var(&manifests, "host-manifest", "")
		flags.Var(&shards, "shard", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*inventory, *root, *output); err != nil || len(manifests) == 0 || len(shards) == 0 {
			if err != nil {
				return err
			}
			return errors.New("host manifests and replay shards are required")
		}
		merge, err := corpusassurance.MergeReplayFromFiles(*inventory, *root, manifests, shards, *output)
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "merge-replay", len(merge.Repositories), *output)
	case "surface-scope":
		flags := flag.NewFlagSet("corpus assurance surface-scope", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		sourceProfile, profile := flags.String("source-profile", "", ""), flags.String("profile", "", "")
		oraclePlan := flags.String("oracle-plan", "", "")
		ledger, policy := flags.String("ledger", "", ""), flags.String("policy", "", "")
		output := flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *oraclePlan != "" || *profile != "" {
			if *sourceProfile != "" || *ledger != "" || *policy != "" {
				return errors.New("Oracle-plan and source-profile surface-scope modes cannot be combined")
			}
			if err := requiredAssuranceFlags(*oraclePlan, *profile, *output); err != nil {
				return err
			}
			scope, err := corpusassurance.BuildSurfaceOracleCampaignScope(*oraclePlan, *profile, *output)
			if err != nil {
				return err
			}
			return writeCorpusAssuranceResult(w, "surface-scope", scope.Total, *output)
		}
		if err := requiredAssuranceFlags(*sourceProfile, *ledger, *policy, *output); err != nil {
			return err
		}
		scope, err := corpusassurance.BuildSurfaceOracleScope(*sourceProfile, *ledger, *policy, *output)
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "surface-scope", scope.Total, *output)
	case "surface-terminal-authority":
		flags := flag.NewFlagSet("corpus assurance surface-terminal-authority", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		scope, coverage := flags.String("surface-scope", "", ""), flags.String("coverage", "", "")
		ledger, policy := flags.String("ledger", "", ""), flags.String("policy", "", "")
		fixtureRoot, classifications := flags.String("fixture-root", "", ""), flags.String("classifications", "", "")
		output := flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*scope, *coverage, *ledger, *policy, *fixtureRoot, *classifications, *output); err != nil {
			return err
		}
		authority, err := corpusassurance.CreateSurfaceTerminalAuthority(corpusassurance.SurfaceTerminalAuthorityRequest{ScopePath: *scope, CoveragePath: *coverage, LedgerPath: *ledger, SupportPolicyPath: *policy, FixtureRoot: *fixtureRoot, ClassificationPath: *classifications, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "surface-terminal-authority", authority.Count, *output)
	case "surface-local-proof-plan":
		flags := flag.NewFlagSet("corpus assurance surface-local-proof-plan", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		scope, profile := flags.String("surface-scope", "", ""), flags.String("source-profile", "", "")
		ledger, policy := flags.String("ledger", "", ""), flags.String("policy", "", "")
		fixtureRoot := flags.String("fixture-root", "", "")
		localProfile, usage := flags.String("profile-output", "", ""), flags.String("usage-output", "", "")
		decision, manifest := flags.String("decision-output", "", ""), flags.String("manifest-output", "", "")
		coverage := flags.String("coverage-output", "", "")
		terminalAuthority := flags.String("terminal-authority", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*scope, *profile, *ledger, *policy, *fixtureRoot, *localProfile, *usage, *decision, *manifest, *coverage); err != nil {
			return err
		}
		_, result, err := corpusassurance.BuildSurfaceLocalProofPlan(corpusassurance.SurfaceLocalProofPlanRequest{ScopePath: *scope, SourceProfilePath: *profile, LedgerPath: *ledger, PolicyPath: *policy, FixtureRoot: *fixtureRoot, ProfilePath: *localProfile, UsagePath: *usage, LocalDecisionPath: *decision, ManifestPath: *manifest, CoveragePath: *coverage, TerminalAuthorityPath: *terminalAuthority})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "surface-local-proof-plan", result.Covered, *coverage)
	case "surface-oracle-index":
		flags := flag.NewFlagSet("corpus assurance surface-oracle-index", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		scope, output := flags.String("scope", "", ""), flags.String("output", "", "")
		var runtimeBatches assurancePathList
		flags.Var(&runtimeBatches, "reviewed-runtime-batch", "")
		if err := rejectDuplicateAssuranceFlags(args[1:], map[string]bool{"reviewed-runtime-batch": true}); err != nil {
			return err
		}
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*scope, *output); err != nil {
			return err
		}
		if len(runtimeBatches) == 0 {
			return errors.New("at least one reviewed runtime batch is required")
		}
		if err := rejectDuplicateRuntimeBatchRoots(runtimeBatches); err != nil {
			return err
		}
		index, err := corpusassurance.CreateSurfaceOracleIndex(corpusassurance.SurfaceOracleIndexRequest{ScopePath: *scope, RuntimeBatchRoots: runtimeBatches, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "surface-oracle-index", len(index.Rows), *output)
	case "local-proof":
		flags := flag.NewFlagSet("corpus assurance local-proof", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		profile, usage, decision, fixtures := flags.String("profile", "", ""), flags.String("usage", "", ""), flags.String("decision", "", ""), flags.String("fixture-manifest", "", "")
		attemptPath := flags.String("attempt", "", "")
		candidatePath := flags.String("candidate", "", "")
		toolsPath := flags.String("tools", "", "")
		output := flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*attemptPath, *profile, *usage, *decision, *fixtures, *candidatePath, *toolsPath, *output); err != nil {
			return err
		}
		proof, err := corpusassurance.RunLocalProof(corpusassurance.LocalProofRequest{AttemptPath: *attemptPath, ProfilePath: *profile, UsagePath: *usage, DecisionPath: *decision, FixtureManifestPath: *fixtures, CandidatePath: *candidatePath, ToolsPath: *toolsPath, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "local-proof", len(proof.Surfaces), *output)
	case "local-proof-plan":
		flags := flag.NewFlagSet("corpus assurance local-proof-plan", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		inventory, root := flags.String("inventory-spec", "", ""), flags.String("root-manifest", "", "")
		sourceProfile, sealedUsage := flags.String("source-profile", "", ""), flags.String("sealed-usage", "", "")
		ledger, policy, decisions := flags.String("ledger", "", ""), flags.String("policy", "", ""), flags.String("decisions", "", "")
		fixtureRoot := flags.String("fixture-root", "", "")
		profile, usage, localDecision, manifest := flags.String("profile-output", "", ""), flags.String("usage-output", "", ""), flags.String("decision-output", "", ""), flags.String("manifest-output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*inventory, *root, *sourceProfile, *sealedUsage, *ledger, *policy, *decisions, *fixtureRoot, *profile, *usage, *localDecision, *manifest); err != nil {
			return err
		}
		planned, err := corpusassurance.BuildLocalProofPlan(corpusassurance.LocalProofPlanRequest{InventoryPath: *inventory, RootManifestPath: *root, SourceProfilePath: *sourceProfile, SealedUsagePath: *sealedUsage, LedgerPath: *ledger, PolicyPath: *policy, DecisionPath: *decisions, FixtureRoot: *fixtureRoot, ProfilePath: *profile, UsagePath: *usage, LocalDecisionPath: *localDecision, ManifestPath: *manifest})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "local-proof-plan", len(planned.Fixtures), *manifest)
	case "release-validate":
		flags := flag.NewFlagSet("corpus assurance release-validate", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		attempt := flags.String("attempt", "", "")
		gladeRoot, candidate := flags.String("glade-root", "", ""), flags.String("candidate", "", "")
		toolsRoot, tools := flags.String("tools-root", "", ""), flags.String("tools", "", "")
		freeze, output := flags.String("tools-freeze", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*attempt, *gladeRoot, *candidate, *toolsRoot, *tools, *freeze, *output); err != nil {
			return err
		}
		validation, err := corpusassurance.RunReleaseValidation(corpusassurance.ReleaseValidationRequest{AttemptPath: *attempt, GladeRoot: *gladeRoot, CandidatePath: *candidate, ToolsRoot: *toolsRoot, ToolsPath: *tools, ToolsFreezePath: *freeze, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "release-validate", len(validation.Commands), *output)
	case "oracle-profile":
		flags := flag.NewFlagSet("corpus assurance oracle-profile", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		inventory, root, sourceProfile, usage := flags.String("inventory-spec", "", ""), flags.String("root-manifest", "", ""), flags.String("source-profile", "", ""), flags.String("sealed-usage", "", "")
		ledger, policy, decisions := flags.String("ledger", "", ""), flags.String("policy", "", ""), flags.String("decisions", "", "")
		localProfile, localUsage, localDecision := flags.String("local-profile", "", ""), flags.String("local-usage", "", ""), flags.String("local-decision", "", "")
		fixtures, proof := flags.String("fixture-manifest", "", ""), flags.String("local-proof", "", "")
		candidate, toolsPath, output := flags.String("candidate", "", ""), flags.String("tools", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*inventory, *root, *sourceProfile, *usage, *ledger, *policy, *decisions, *localProfile, *localUsage, *localDecision, *fixtures, *proof, *candidate, *toolsPath, *output); err != nil {
			return err
		}
		profile, err := corpusassurance.BuildAssuranceProfile(*inventory, *root, *sourceProfile, *usage, *ledger, *policy, *decisions, *localProfile, *localUsage, *localDecision, *fixtures, *proof, *candidate, *toolsPath, *output)
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "oracle-profile", len(profile.Rows), *output)
	case "oracle-directives-draft":
		flags := flag.NewFlagSet("corpus assurance oracle-directives-draft", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		profile, usage, proof, output := flags.String("profile", "", ""), flags.String("sealed-usage", "", ""), flags.String("local-proof", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*profile, *usage, *proof, *output); err != nil {
			return err
		}
		draft, err := corpusassurance.BuildOracleDirectiveDraft(*profile, *usage, *proof, *output)
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "oracle-directives-draft", len(draft.Directives), *output)
	case "oracle-plan":
		flags := flag.NewFlagSet("corpus assurance oracle-plan", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		inventory, rootManifest := flags.String("inventory-spec", "", ""), flags.String("root-manifest", "", "")
		sourceProfile, usage := flags.String("source-profile", "", ""), flags.String("sealed-usage", "", "")
		ledger, policy := flags.String("ledger", "", ""), flags.String("policy", "", "")
		decisions, fixtures := flags.String("decisions", "", ""), flags.String("fixture-manifest", "", "")
		localProfile, localUsage := flags.String("local-profile", "", ""), flags.String("local-usage", "", "")
		localDecision := flags.String("local-decision", "", "")
		candidate, toolsPath := flags.String("candidate", "", ""), flags.String("tools", "", "")
		proof, directives := flags.String("local-proof", "", ""), flags.String("directives", "", "")
		profileInput := flags.String("profile", "", "")
		profileOutput, output := flags.String("profile-output", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *profileInput != "" {
			if err := requiredAssuranceFlags(*profileInput, *usage, *fixtures, *proof, *directives, *output); err != nil {
				return err
			}
			plan, err := corpusassurance.PlanOracleFromFiles(*profileInput, *usage, *fixtures, *proof, *directives, *output)
			if err != nil {
				return err
			}
			return writeCorpusAssuranceResult(w, "oracle-plan", len(plan.Rows), *output)
		}
		if err := requiredAssuranceFlags(*inventory, *rootManifest, *sourceProfile, *usage, *ledger, *policy, *decisions, *localProfile, *localUsage, *localDecision, *fixtures, *proof, *candidate, *toolsPath, *directives, *profileOutput, *output); err != nil {
			return err
		}
		if _, err := corpusassurance.BuildAssuranceProfile(*inventory, *rootManifest, *sourceProfile, *usage, *ledger, *policy, *decisions, *localProfile, *localUsage, *localDecision, *fixtures, *proof, *candidate, *toolsPath, *profileOutput); err != nil {
			return err
		}
		plan, err := corpusassurance.PlanOracleFromFiles(*profileOutput, *usage, *fixtures, *proof, *directives, *output)
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "oracle-plan", len(plan.Rows), *output)
	case "exclusion-request":
		flags := flag.NewFlagSet("corpus assurance exclusion-request", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		plan, profile := flags.String("plan", "", ""), flags.String("profile", "", "")
		usage, output := flags.String("sealed-usage", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*plan, *profile, *usage, *output); err != nil {
			return err
		}
		request, err := corpusassurance.BuildExclusionRequest(*plan, *profile, *usage, *output)
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "exclusion-request", len(request.Rows), *output)
	case "authorize-exclusions":
		flags := flag.NewFlagSet("corpus assurance authorize-exclusions", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		request, plan := flags.String("request", "", ""), flags.String("plan", "", "")
		profile, usage := flags.String("profile", "", ""), flags.String("sealed-usage", "", "")
		policy, output := flags.String("policy", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*request, *plan, *profile, *usage, *policy, *output); err != nil {
			return err
		}
		authority, err := corpusassurance.AuthorizeExclusionsFromFiles(*request, *plan, *profile, *usage, *policy, *output)
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "authorize-exclusions", len(authority.Rows), *output)
	case "oracle-bundle":
		flags := flag.NewFlagSet("corpus assurance oracle-bundle", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		profile, plan, authority := flags.String("profile", "", ""), flags.String("oracle-plan", "", ""), flags.String("exclusion-authority", "", "")
		attempt, devHubAuthority, release, proof, fixtures := flags.String("attempt", "", ""), flags.String("dev-hub-authority", "", ""), flags.String("release-validation", "", ""), flags.String("local-proof", "", ""), flags.String("fixture-manifest", "", "")
		remoteAuthority := flags.String("remote-cleanup-authority", "", "")
		filter, scratch, toolsRoot, output := flags.String("filter-script", "", ""), flags.String("scratch-definition", "", ""), flags.String("tools-root", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*attempt, *remoteAuthority, *devHubAuthority, *profile, *plan, *authority, *release, *proof, *fixtures, *filter, *scratch, *toolsRoot, *output); err != nil {
			return err
		}
		bundle, err := corpusassurance.BuildOracleBundle(corpusassurance.OracleBundleRequest{AttemptPath: *attempt, RemoteCleanupAuthorityPath: *remoteAuthority, DevHubAuthorityPath: *devHubAuthority, ProfilePath: *profile, PlanPath: *plan, AuthorityPath: *authority, ReleaseValidationPath: *release, LocalProofPath: *proof, FixtureManifestPath: *fixtures, FilterScriptPath: *filter, ScratchDefinitionPath: *scratch, ToolsRoot: *toolsRoot, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "oracle-bundle", len(bundle.Fixtures), *output)
	case "dev-hub-authority":
		flags := flag.NewFlagSet("corpus assurance dev-hub-authority", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		alias, sfbin, python := flags.String("target-org", "", ""), flags.String("sf-bin", "", ""), flags.String("python-bin", "", "")
		home, path, tmpdir, output := flags.String("home", "", ""), flags.String("path", "", ""), flags.String("tmpdir", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*alias, *sfbin, *python, *home, *path, *tmpdir, *output); err != nil {
			return err
		}
		authority, err := corpusassurance.CreateSalesforceDevHubAuthority(corpusassurance.SalesforceDevHubAuthorityRequest{TargetOrg: *alias, SFBin: *sfbin, PythonBin: *python, Home: *home, Path: *path, TmpDir: *tmpdir, Output: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "dev-hub-authority", 1, authority.Alias)
	case "org-preflight":
		flags := flag.NewFlagSet("corpus assurance org-preflight", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		bundle, target, sfbin, output := flags.String("bundle", "", ""), flags.String("target-org", "", ""), flags.String("sf-bin", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*bundle, *target, *sfbin, *output); err != nil {
			return err
		}
		preflight, err := corpusassurance.RunSalesforceOrgPreflight(corpusassurance.SalesforceOrgPreflightRequest{BundlePath: *bundle, TargetOrg: *target, SFBin: *sfbin, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "org-preflight", len(preflight.Inventory.Counts), *output)
	case "org-create":
		flags := flag.NewFlagSet("corpus assurance org-create", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		bundle, alias := flags.String("bundle", "", ""), flags.String("alias", "", "")
		sfbin, output := flags.String("sf-bin", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*bundle, *alias, *sfbin, *output); err != nil {
			return err
		}
		_, err := corpusassurance.RunSalesforceOrgCreate(corpusassurance.SalesforceOrgCreateRequest{BundlePath: *bundle, Alias: *alias, SFBin: *sfbin, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "org-create", 1, *output)
	case "salesforce-dispatch":
		flags := flag.NewFlagSet("corpus assurance salesforce-dispatch", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		bundle, target := flags.String("bundle", "", ""), flags.String("target-org", "", "")
		executor, runID := flags.String("executor-root", "", ""), flags.String("run-id", "", "")
		shardIndex, shardCount := flags.Int("shard-index", -1, ""), flags.Int("shard-count", 0, "")
		output := flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*bundle, *target, *executor, *runID, *output); err != nil {
			return err
		}
		_, err := corpusassurance.CreateSalesforceDispatch(corpusassurance.SalesforceDispatchRequest{BundlePath: *bundle, OrgAlias: *target, ExecutorRoot: *executor, RunID: *runID, ShardIndex: *shardIndex, ShardCount: *shardCount, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "salesforce-dispatch", 1, *output)
	case "salesforce-run":
		flags := flag.NewFlagSet("corpus assurance salesforce-run", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		bundle, dispatch, preflight := flags.String("bundle", "", ""), flags.String("dispatch", "", ""), flags.String("org-preflight", "", "")
		target, sfbin := flags.String("target-org", "", ""), flags.String("sf-bin", "", "")
		output := flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*bundle, *dispatch, *preflight, *target, *sfbin, *output); err != nil {
			return err
		}
		shard, err := corpusassurance.RunSalesforceShard(corpusassurance.SalesforceShardRequest{BundlePath: *bundle, DispatchPath: *dispatch, PreflightPath: *preflight, TargetOrg: *target, SFBin: *sfbin, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "salesforce-run", len(shard.Results), *output)
	case "org-cleanup":
		flags := flag.NewFlagSet("corpus assurance org-cleanup", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		bundle, creation, preflight := flags.String("bundle", "", ""), flags.String("creation", "", ""), flags.String("org-preflight", "", "")
		target, sfbin, output := flags.String("target-org", "", ""), flags.String("sf-bin", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*bundle, *creation, *target, *sfbin, *output); err != nil {
			return err
		}
		cleanup, err := corpusassurance.RunSalesforceOrgCleanup(corpusassurance.SalesforceOrgCleanupRequest{BundlePath: *bundle, CreationPath: *creation, PreflightPath: *preflight, TargetOrg: *target, SFBin: *sfbin, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "org-cleanup", len(cleanup.Commands), *output)
	case "salesforce-reconcile":
		flags := flag.NewFlagSet("corpus assurance salesforce-reconcile", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		plan := flags.String("oracle-plan", "", "")
		receipt, packet, packetOutput, output := flags.String("receipt", "", ""), flags.String("packet", "", ""), flags.String("packet-output", "", ""), flags.String("output", "", "")
		var shards, dispatches, creations, cleanups, preflights assurancePathList
		flags.Var(&shards, "shard", "")
		flags.Var(&dispatches, "dispatch", "")
		flags.Var(&creations, "creation", "")
		flags.Var(&cleanups, "cleanup", "")
		flags.Var(&preflights, "preflight", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*plan); err != nil {
			return err
		}
		if *receipt != "" || *packet != "" {
			if err := requiredAssuranceFlags(*receipt, *packet); err != nil {
				return err
			}
			if len(shards) != 0 || *packetOutput != "" || *output != "" {
				return errors.New("verification mode accepts only --oracle-plan, --receipt, and --packet")
			}
			return corpusassurance.VerifySalesforceReconciliation(*plan, *receipt, *packet)
		}
		if err := requiredAssuranceFlags(*packetOutput, *output); err != nil || len(shards) == 0 || len(shards) != len(dispatches) || len(shards) != len(creations) || len(shards) != len(cleanups) || len(shards) != len(preflights) {
			if err != nil {
				return err
			}
			return errors.New("paired Salesforce shard, dispatch, creation, and cleanup paths are required")
		}
		files := make([]corpusassurance.SalesforceShardFiles, len(shards))
		for index := range shards {
			files[index] = corpusassurance.SalesforceShardFiles{ShardPath: shards[index], DispatchPath: dispatches[index], CreationPath: creations[index], CleanupPath: cleanups[index], PreflightPath: preflights[index]}
		}
		if _, err := corpusassurance.CreateSalesforceReconciliation(corpusassurance.SalesforceReconciliationRequest{OraclePlanPath: *plan, ShardFiles: files, PacketOutput: *packetOutput, OutputPath: *output}); err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "salesforce-reconcile", len(files), *output)
	case "report":
		flags := flag.NewFlagSet("corpus assurance report", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		inventory, root := flags.String("inventory-spec", "", ""), flags.String("root-manifest", "", "")
		ledger, sourceProfile, policy, decisions := flags.String("ledger", "", ""), flags.String("source-profile", "", ""), flags.String("policy", "", ""), flags.String("decisions", "", "")
		usage, profile, fixtures := flags.String("sealed-usage", "", ""), flags.String("profile", "", ""), flags.String("fixture-manifest", "", "")
		replay, attempt, proof, plan := flags.String("replay", "", ""), flags.String("attempt", "", ""), flags.String("local-proof", "", ""), flags.String("oracle-plan", "", "")
		exclusionRequest, exclusionPolicy, authority := flags.String("exclusion-request", "", ""), flags.String("exclusion-policy", "", ""), flags.String("exclusion-authority", "", "")
		release, bundle := flags.String("release-validation", "", ""), flags.String("bundle", "", "")
		filter, scratch, toolsAMD64 := flags.String("filter-script", "", ""), flags.String("scratch-definition", "", ""), flags.String("tools-amd64", "", "")
		jsonOutput, htmlOutput, receiptOutput, packetOutput := flags.String("output", "", ""), flags.String("html-output", "", ""), flags.String("receipt-output", "", ""), flags.String("packet-output", "", "")
		salesforceReconciliation, salesforcePacket := flags.String("salesforce-reconciliation", "", ""), flags.String("salesforce-packet", "", "")
		var shards, dispatches, creations, cleanups, preflights, remoteCleanups, replayHosts, replayShards assurancePathList
		flags.Var(&shards, "shard", "")
		flags.Var(&dispatches, "dispatch", "")
		flags.Var(&creations, "creation", "")
		flags.Var(&cleanups, "cleanup", "")
		flags.Var(&preflights, "preflight", "")
		flags.Var(&remoteCleanups, "remote-cleanup", "")
		var remoteAuthorities assurancePathList
		flags.Var(&remoteAuthorities, "remote-cleanup-authority", "")
		flags.Var(&replayHosts, "replay-host-manifest", "")
		flags.Var(&replayShards, "replay-shard", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*inventory, *root, *ledger, *sourceProfile, *policy, *decisions, *usage, *profile, *fixtures, *replay, *attempt, *proof, *plan, *exclusionRequest, *exclusionPolicy, *authority, *release, *bundle, *filter, *scratch, *toolsAMD64, *jsonOutput, *htmlOutput, *receiptOutput, *packetOutput); err != nil || requireRetainedSalesforceReconciliation(*salesforceReconciliation, *salesforcePacket) != nil || len(shards) != 0 || len(dispatches) != 0 || len(creations) != 0 || len(cleanups) != 0 || len(preflights) != 0 || len(remoteCleanups) != 2 || len(remoteAuthorities) != 2 || len(replayHosts) != 2 || len(replayShards) != 2 {
			if err != nil {
				return err
			}
			return errors.New("complete paired replay, Salesforce, and remote cleanup paths are required")
		}
		files := make([]corpusassurance.SalesforceShardFiles, len(shards))
		for index := range files {
			files[index] = corpusassurance.SalesforceShardFiles{ShardPath: shards[index], DispatchPath: dispatches[index], CreationPath: creations[index], CleanupPath: cleanups[index], PreflightPath: preflights[index]}
		}
		receipt, err := corpusassurance.BuildAssuranceReport(corpusassurance.AssuranceReportRequest{InventoryPath: *inventory, RootManifestPath: *root, LedgerPath: *ledger, SourceProfilePath: *sourceProfile, PolicyPath: *policy, DecisionPath: *decisions, UsagePath: *usage, ProfilePath: *profile, FixtureManifestPath: *fixtures, ReplayPath: *replay, ReplayHostManifestPaths: replayHosts, ReplayShardPaths: replayShards, AttemptPath: *attempt, LocalProofPath: *proof, OraclePlanPath: *plan, ExclusionRequestPath: *exclusionRequest, ExclusionPolicyPath: *exclusionPolicy, AuthorityPath: *authority, ReleaseValidationPath: *release, BundlePath: *bundle, FilterScriptPath: *filter, ScratchDefinitionPath: *scratch, ToolsAMD64Path: *toolsAMD64, SalesforceFiles: files, SalesforceReconciliationPath: *salesforceReconciliation, SalesforcePacketPath: *salesforcePacket, RemoteCleanupPaths: remoteCleanups, RemoteCleanupAuthorityPaths: remoteAuthorities, JSONPath: *jsonOutput, HTMLPath: *htmlOutput, ReceiptPath: *receiptOutput, PacketPath: *packetOutput})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "report", len(receipt.InputsSHA256), *jsonOutput)
	case "cleanup":
		flags := flag.NewFlagSet("corpus assurance cleanup", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		attempt, binding, output := flags.String("attempt", "", ""), flags.String("binding", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*attempt, *binding, *output); err != nil {
			return err
		}
		_, err := corpusassurance.RunRemoteAttemptCleanup(corpusassurance.RemoteAttemptCleanupRequest{AttemptPath: *attempt, BindingPath: *binding, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "cleanup", 1, *output)
	case "review-index":
		flags := flag.NewFlagSet("corpus assurance review-index", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		attempt, predecessor, output := flags.String("attempt", "", ""), flags.String("predecessor", "", ""), flags.String("output", "", "")
		indexPath, verify := flags.String("index", "", ""), flags.Bool("verify", false, "")
		var artifacts assurancePathList
		flags.Var(&artifacts, "artifact", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *verify {
			if err := requiredAssuranceFlags(*indexPath); err != nil {
				return err
			}
			if *attempt != "" || *predecessor != "" || *output != "" || len(artifacts) != 0 {
				return errors.New("review-index verification accepts only --verify and --index")
			}
			index, err := corpusassurance.VerifyReviewIndex(*indexPath)
			if err != nil {
				return err
			}
			return writeCorpusAssuranceResult(w, "review-index-verify", len(index.Artifacts), *indexPath)
		}
		if *indexPath != "" {
			return errors.New("--index requires --verify")
		}
		if err := requiredAssuranceFlags(*attempt, *output); err != nil || len(artifacts) == 0 {
			if err != nil {
				return err
			}
			return errors.New("at least one review artifact is required")
		}
		index, err := corpusassurance.CreateReviewIndex(corpusassurance.ReviewIndexRequest{AttemptPath: *attempt, PredecessorPath: *predecessor, ArtifactPaths: artifacts, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "review-index", len(index.Artifacts), *output)
	case "remote-failure-preserve":
		flags := flag.NewFlagSet("corpus assurance remote-failure-preserve", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		attempt, binding := flags.String("attempt", "", ""), flags.String("binding", "", "")
		phase, phaseExit := flags.String("phase", "", ""), flags.Int("phase-exit", 0, "")
		handoff, output := flags.String("handoff", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*attempt, *binding, *phase, *handoff, *output); err != nil || *phaseExit == 0 {
			if err != nil {
				return err
			}
			return errors.New("nonzero phase exit is required")
		}
		receipt, err := corpusassurance.PreserveRemoteFailure(corpusassurance.RemoteFailurePreserveRequest{AttemptPath: *attempt, BindingPath: *binding, Phase: *phase, PhaseExit: *phaseExit, HandoffPath: *handoff, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "remote-failure-preserve", 1, receipt.Status)
	default:
		return errors.New("unknown corpus assurance command")
	}
}

func requireRetainedSalesforceReconciliation(reconciliation, packet string) error {
	if reconciliation == "" || packet == "" {
		return errors.New("new reports require retained Salesforce reconciliation and packet evidence")
	}
	return nil
}

func requiredAssuranceFlags(values ...string) error {
	for _, value := range values {
		if value == "" {
			return errors.New("required corpus assurance flag is missing")
		}
	}
	return nil
}

func rejectDuplicateAssuranceFlags(args []string, repeatable map[string]bool) error {
	seen := make(map[string]struct{})
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-")
		if equal := strings.IndexByte(name, '='); equal >= 0 {
			name = name[:equal]
		}
		if repeatable[name] {
			continue
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate flag --%s", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func rejectDuplicateRuntimeBatchRoots(paths []string) error {
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if _, exists := seen[path]; exists {
			return fmt.Errorf("duplicate reviewed runtime batch: %s", path)
		}
		seen[path] = struct{}{}
	}
	return nil
}

func writeCorpusAssuranceResult(w io.Writer, command string, rows int, output string) error {
	_, err := fmt.Fprintf(w, "%s: rows=%d output=%s\n", command, rows, output)
	return err
}

type assurancePathList []string

func (paths *assurancePathList) String() string { return "" }

func (paths *assurancePathList) Set(value string) error {
	if value == "" {
		return errors.New("assurance path is required")
	}
	*paths = append(*paths, value)
	return nil
}

func printCorpusAssuranceHelp(w io.Writer) {
	fmt.Fprint(w, `Run the sealed private-corpus assurance workflow.

Usage:
  glade-tools corpus assurance orchestrator <plan|init|enqueue|status|lease|heartbeat|reserve|receipt|worker-transfer|cleanup-takeover|cleanup-claim> [fixed flags]
  glade-tools corpus assurance campaign --spec <CAMPAIGN.json> --state <CAMPAIGN_STATE.json> [--promote --out <new-root>]
  glade-tools corpus assurance candidate-build --candidate-root <glade-root> --tools-root <glade-tools-root> --candidate-ref <ref> --tools-ref <ref> --candidate-output <glade> --tools-output <glade-tools> --receipt-output <CANDIDATE_BUILD_RECEIPT.json> --review-output <REVIEW.md> --tools-freeze-output <TOOLS_COMMIT>
  glade-tools corpus assurance candidate-authority --candidate-root <glade-root> --tools-root <glade-tools-root> --receipt <candidate-receipt.json> --review <REVIEW.md> --output <CANDIDATE_AUTHORITY.json>
  glade-tools corpus assurance attempt-init --inventory-spec <IN_SCOPE.json> --candidate-authority <CANDIDATE_AUTHORITY.json> --candidate <glade> --candidate-root <glade-root> --tools <glade-tools> --tools-root <glade-tools-root> --replay-host <operator@replay-worker> --replay-parent <absolute-parent> --salesforce-host <operator@salesforce-worker> --salesforce-parent <absolute-parent> --run-id <run-id> --output-dir <attempt-bindings-dir>
  glade-tools corpus assurance attempt --inventory-spec <IN_SCOPE.json> --candidate-authority <CANDIDATE_AUTHORITY.json> --candidate <glade> --candidate-root <glade-root> --tools <glade-tools> --tools-root <glade-tools-root> --replay-cleanup-authority <REMOTE_CLEANUP_AUTHORITY.json> --salesforce-cleanup-authority <REMOTE_CLEANUP_AUTHORITY.json> --output <ATTEMPT.json>
  glade-tools corpus assurance prepare --inventory-spec <IN_SCOPE.json> --attempt <ATTEMPT.json> --output <new-dir>
  glade-tools corpus assurance usage-draft --inventory-spec <IN_SCOPE.json> --ledger <ledger.json> --manifest <MANIFEST.json> --profile <source-profile.json> --policy <support-policy.json> --output <USAGE_DECISION_DRAFT.json> [--decision-template <USAGE_DECISIONS.json>]
  glade-tools corpus assurance usage --inventory-spec <IN_SCOPE.json> --ledger <ledger.json> --manifest <MANIFEST.json> --profile <source-profile.json> --policy <support-policy.json> --decisions <USAGE_DECISIONS.json> --output <CORPUS_USAGE.json>
  glade-tools corpus assurance replay --host <local|replay-worker> --inventory-spec <IN_SCOPE.json> --root-manifest <MANIFEST.json> --host-manifest <manifest.json> --candidate <glade> --tools <glade-tools> --output <REPLAY_SHARD.json>
  glade-tools corpus assurance merge-replay --inventory-spec <IN_SCOPE.json> --root-manifest <MANIFEST.json> --host-manifest <manifest.json> --host-manifest <manifest.json> --shard <REPLAY_SHARD.json> --shard <REPLAY_SHARD.json> --output <REPLAY.json>
  glade-tools corpus assurance surface-scope --source-profile <SOURCE_PROFILE.json> --ledger <SURFACE_LEDGER.json> --policy <support-policy.json> --output <SURFACE_ORACLE_SCOPE.json>
  glade-tools corpus assurance surface-scope --oracle-plan <ORACLE_PLAN.json> --profile <ASSURANCE_PROFILE.json> --output <SURFACE_ORACLE_SCOPE.json>
  glade-tools corpus assurance surface-terminal-authority --surface-scope <SURFACE_ORACLE_SCOPE.json> --coverage <SURFACE_LOCAL_PROOF_COVERAGE.json> --ledger <SURFACE_LEDGER.json> --policy <support-policy.json> --fixture-root <docs/fixtures> --classifications <EXCLUSION_POLICY.json> --output <SURFACE_TERMINAL_AUTHORITY.json>
  glade-tools corpus assurance surface-local-proof-plan --surface-scope <SURFACE_ORACLE_SCOPE.json> --source-profile <SOURCE_PROFILE.json> --ledger <SURFACE_LEDGER.json> --policy <support-policy.json> --fixture-root <docs/fixtures> --profile-output <profile.json> --usage-output <usage.json> --decision-output <decision.json> --manifest-output <fixtures.json> --coverage-output <SURFACE_LOCAL_PROOF_COVERAGE.json> [--terminal-authority <SURFACE_TERMINAL_AUTHORITY.json>]
  glade-tools corpus assurance surface-oracle-index --scope <SURFACE_ORACLE_SCOPE.json> --reviewed-runtime-batch <root> [--reviewed-runtime-batch <root> ...] --output <SURFACE_ORACLE_INDEX.json>
  glade-tools corpus assurance local-proof-plan --inventory-spec <IN_SCOPE.json> --root-manifest <MANIFEST.json> --source-profile <source-profile.json> --sealed-usage <CORPUS_USAGE.json> --ledger <ledger.json> --policy <policy.json> --decisions <USAGE_DECISIONS.json> --fixture-root <docs/fixtures> --profile-output <profile.json> --usage-output <usage.json> --decision-output <decision.json> --manifest-output <fixtures.json>
  glade-tools corpus assurance local-proof --attempt <ATTEMPT.json> --profile <profile.json> --usage <usage.json> --decision <decision.json> --fixture-manifest <fixtures.json> --candidate <glade> --tools <glade-tools> --output <LOCAL_PROOF.json>
  glade-tools corpus assurance release-validate --attempt <ATTEMPT.json> --glade-root <glade-root> --candidate <glade> --tools-root <glade-tools-root> --tools <glade-tools> --tools-freeze <FINAL_TOOLS_COMMIT> --output <RELEASE_VALIDATION.json>
  glade-tools corpus assurance oracle-profile --inventory-spec <IN_SCOPE.json> --root-manifest <MANIFEST.json> --source-profile <SOURCE_PROFILE.json> --sealed-usage <CORPUS_USAGE.json> --ledger <ledger.json> --policy <support-policy.json> --decisions <decisions.json> --local-profile <profile.json> --local-usage <usage.json> --local-decision <decision.json> --fixture-manifest <fixtures.json> --local-proof <LOCAL_PROOF.json> --candidate <glade> --tools <glade-tools> --output <ASSURANCE_PROFILE.json>
  glade-tools corpus assurance oracle-directives-draft --profile <ASSURANCE_PROFILE.json> --sealed-usage <CORPUS_USAGE.json> --local-proof <LOCAL_PROOF.json> --output <ORACLE_DIRECTIVES.json>
  glade-tools corpus assurance oracle-plan --inventory-spec <IN_SCOPE.json> --root-manifest <MANIFEST.json> --source-profile <source-profile.json> --sealed-usage <CORPUS_USAGE.json> --ledger <ledger.json> --policy <policy.json> --decisions <decisions.json> --local-profile <profile.json> --local-usage <usage.json> --local-decision <decision.json> --fixture-manifest <fixtures.json> --local-proof <LOCAL_PROOF.json> --candidate <glade> --tools <glade-tools> --directives <directives.json> --profile-output <ASSURANCE_PROFILE.json> --output <ORACLE_PLAN.json>
  glade-tools corpus assurance oracle-plan --profile <ASSURANCE_PROFILE.json> --sealed-usage <CORPUS_USAGE.json> --fixture-manifest <fixtures.json> --local-proof <LOCAL_PROOF.json> --directives <directives.json> --output <ORACLE_PLAN.json>
  glade-tools corpus assurance exclusion-request --plan <ORACLE_PLAN.json> --profile <ASSURANCE_PROFILE.json> --sealed-usage <CORPUS_USAGE.json> --output <EXCLUSION_REQUEST.json>
  glade-tools corpus assurance authorize-exclusions --request <EXCLUSION_REQUEST.json> --plan <ORACLE_PLAN.json> --profile <ASSURANCE_PROFILE.json> --sealed-usage <CORPUS_USAGE.json> --policy <policy.json> --output <EXCLUSION_AUTHORITY.json>
  glade-tools corpus assurance dev-hub-authority --target-org <approved-dev-hub-alias> --sf-bin <absolute-sf> --python-bin <absolute-python> --home <worker-home> --path <exact-path> --tmpdir <worker-tmp> --output <DEV_HUB_AUTHORITY.json>
  glade-tools corpus assurance oracle-bundle --attempt <ATTEMPT.json> --remote-cleanup-authority <SALESFORCE_REMOTE_CLEANUP_AUTHORITY.json> --dev-hub-authority <DEV_HUB_AUTHORITY.json> --profile <ASSURANCE_PROFILE.json> --oracle-plan <ORACLE_PLAN.json> --exclusion-authority <EXCLUSION_AUTHORITY.json> --release-validation <RELEASE_VALIDATION.json> --local-proof <LOCAL_PROOF.json> --fixture-manifest <fixtures.json> --filter-script <filter.py> --scratch-definition <scratch.json> --tools-root <clean-glade-tools-root> --output <new-dir>
  glade-tools corpus assurance org-create --bundle <bundle.json> --alias <scratch-alias> --sf-bin <absolute-sf> --output <ORG_CREATION.json>
  glade-tools corpus assurance org-preflight --bundle <bundle.json> --target-org <scratch-alias> --sf-bin <absolute-sf> --output <ORG_PREFLIGHT.json>
  glade-tools corpus assurance salesforce-dispatch --bundle <bundle.json> --target-org <scratch-alias> --executor-root <attempt/executor/shard-N> --run-id <attempt-shard-N> --shard-index <0|1> --shard-count 2 --output <SALESFORCE_DISPATCH.json>
  glade-tools corpus assurance salesforce-run --bundle <bundle.json> --dispatch <SALESFORCE_DISPATCH.json> --org-preflight <ORG_PREFLIGHT.json> --target-org <scratch-alias> --sf-bin <absolute-sf> --output <SALESFORCE_SHARD.json>
  glade-tools corpus assurance org-cleanup --bundle <bundle.json> --creation <ORG_CREATION.json>[.invalidated] [--org-preflight <ORG_PREFLIGHT.json>] --target-org <scratch-alias> --sf-bin <absolute-sf> --output <ORG_CLEANUP.json>
  glade-tools corpus assurance salesforce-reconcile --oracle-plan <ORACLE_PLAN.json> --shard <SALESFORCE_SHARD.json> --dispatch <SALESFORCE_DISPATCH.json> --preflight <ORG_PREFLIGHT.json> --creation <ORG_CREATION.json> --cleanup <ORG_CLEANUP.json> --packet-output <packet-dir> --output <SALESFORCE_RECONCILIATION.json>
  glade-tools corpus assurance salesforce-reconcile --oracle-plan <ORACLE_PLAN.json> --receipt <SALESFORCE_RECONCILIATION.json> --packet <packet-dir>
  glade-tools corpus assurance remote-failure-preserve --attempt <ATTEMPT.json> --binding <SALESFORCE_REMOTE_CLEANUP_AUTHORITY.json> --phase <phase> --phase-exit <nonzero> --handoff <HANDOFF.md> --output <failure-dir>
  glade-tools corpus assurance review-index --attempt <ATTEMPT.json> [--predecessor <REVIEW_INDEX.json>] --artifact <retained-evidence> [--artifact <retained-evidence>] --output <REVIEW_INDEX.json>
  glade-tools corpus assurance review-index --verify --index <REVIEW_INDEX.json>
  glade-tools corpus assurance cleanup --attempt <ATTEMPT.json> --binding <REMOTE_CLEANUP_AUTHORITY.json> --output <REMOTE_CLEANUP.json>
  glade-tools corpus assurance report --inventory-spec <IN_SCOPE.json> --replay-host-manifest <manifest.json> --replay-host-manifest <manifest.json> --replay-shard <REPLAY_SHARD.json> --replay-shard <REPLAY_SHARD.json> [remaining sealed evidence flags] --salesforce-reconciliation <SALESFORCE_RECONCILIATION.json> --salesforce-packet <packet-dir> --packet-output <packet-dir> --output <ASSURANCE.json> --html-output <ASSURANCE.html> --receipt-output <RECEIPT.json>
`)
}
