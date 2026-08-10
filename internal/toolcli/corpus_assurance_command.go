package toolcli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

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
	switch args[0] {
	case "candidate-authority":
		flags := flag.NewFlagSet("corpus assurance candidate-authority", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		runRoot, rebind, output := flags.String("run-root", "", ""), flags.String("rebind", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*runRoot, *rebind, *output); err != nil {
			return err
		}
		if _, err := corpusassurance.CreateCandidateAuthority(corpusassurance.CandidateAuthorityRequest{RunRoot: *runRoot, RebindPath: *rebind, OutputPath: *output}); err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "candidate-authority", 1, *output)
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
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*inventory, *ledger, *manifest, *profile, *policy, *output); err != nil {
			return err
		}
		draft, err := corpusassurance.DraftUsageDecisions(*inventory, *ledger, *manifest, *profile, *policy, *output)
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
		profileOutput, output := flags.String("profile-output", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
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
		filter, scratch, toolsRoot, output := flags.String("filter-script", "", ""), flags.String("scratch-definition", "", ""), flags.String("tools-root", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*attempt, *devHubAuthority, *profile, *plan, *authority, *release, *proof, *fixtures, *filter, *scratch, *toolsRoot, *output); err != nil {
			return err
		}
		bundle, err := corpusassurance.BuildOracleBundle(corpusassurance.OracleBundleRequest{AttemptPath: *attempt, DevHubAuthorityPath: *devHubAuthority, ProfilePath: *profile, PlanPath: *plan, AuthorityPath: *authority, ReleaseValidationPath: *release, LocalProofPath: *proof, FixtureManifestPath: *fixtures, FilterScriptPath: *filter, ScratchDefinitionPath: *scratch, ToolsRoot: *toolsRoot, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "oracle-bundle", len(bundle.Fixtures), *output)
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
		var shards, dispatches, creations, cleanups, preflights assurancePathList
		flags.Var(&shards, "shard", "")
		flags.Var(&dispatches, "dispatch", "")
		flags.Var(&creations, "creation", "")
		flags.Var(&cleanups, "cleanup", "")
		flags.Var(&preflights, "preflight", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*plan); err != nil || len(shards) == 0 || len(shards) != len(dispatches) || len(shards) != len(creations) || len(shards) != len(cleanups) || len(shards) != len(preflights) {
			if err != nil {
				return err
			}
			return errors.New("paired Salesforce shard, dispatch, creation, and cleanup paths are required")
		}
		files := make([]corpusassurance.SalesforceShardFiles, len(shards))
		for index := range shards {
			files[index] = corpusassurance.SalesforceShardFiles{ShardPath: shards[index], DispatchPath: dispatches[index], CreationPath: creations[index], CleanupPath: cleanups[index], PreflightPath: preflights[index]}
		}
		if err := corpusassurance.ValidateSalesforceShardFiles(*plan, files); err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "salesforce-reconcile", len(files), *plan)
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
		jsonOutput, htmlOutput, receiptOutput := flags.String("output", "", ""), flags.String("html-output", "", ""), flags.String("receipt-output", "", "")
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
		if err := requiredAssuranceFlags(*inventory, *root, *ledger, *sourceProfile, *policy, *decisions, *usage, *profile, *fixtures, *replay, *attempt, *proof, *plan, *exclusionRequest, *exclusionPolicy, *authority, *release, *bundle, *filter, *scratch, *toolsAMD64, *jsonOutput, *htmlOutput, *receiptOutput); err != nil || len(shards) == 0 || len(shards) != len(dispatches) || len(shards) != len(creations) || len(shards) != len(cleanups) || len(shards) != len(preflights) || len(remoteCleanups) != 2 || len(remoteAuthorities) != 2 || len(replayHosts) != 2 || len(replayShards) != 2 {
			if err != nil {
				return err
			}
			return errors.New("complete paired replay, Salesforce, and remote cleanup paths are required")
		}
		files := make([]corpusassurance.SalesforceShardFiles, len(shards))
		for index := range files {
			files[index] = corpusassurance.SalesforceShardFiles{ShardPath: shards[index], DispatchPath: dispatches[index], CreationPath: creations[index], CleanupPath: cleanups[index], PreflightPath: preflights[index]}
		}
		receipt, err := corpusassurance.BuildAssuranceReport(corpusassurance.AssuranceReportRequest{InventoryPath: *inventory, RootManifestPath: *root, LedgerPath: *ledger, SourceProfilePath: *sourceProfile, PolicyPath: *policy, DecisionPath: *decisions, UsagePath: *usage, ProfilePath: *profile, FixtureManifestPath: *fixtures, ReplayPath: *replay, ReplayHostManifestPaths: replayHosts, ReplayShardPaths: replayShards, AttemptPath: *attempt, LocalProofPath: *proof, OraclePlanPath: *plan, ExclusionRequestPath: *exclusionRequest, ExclusionPolicyPath: *exclusionPolicy, AuthorityPath: *authority, ReleaseValidationPath: *release, BundlePath: *bundle, FilterScriptPath: *filter, ScratchDefinitionPath: *scratch, ToolsAMD64Path: *toolsAMD64, SalesforceFiles: files, RemoteCleanupPaths: remoteCleanups, RemoteCleanupAuthorityPaths: remoteAuthorities, JSONPath: *jsonOutput, HTMLPath: *htmlOutput, ReceiptPath: *receiptOutput})
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
	default:
		return errors.New("unknown corpus assurance command")
	}
}

func requiredAssuranceFlags(values ...string) error {
	for _, value := range values {
		if value == "" {
			return errors.New("required corpus assurance flag is missing")
		}
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
  glade-tools corpus assurance candidate-authority --run-root <run-root> --rebind <current-base-candidate-rebind.json> --output <CANDIDATE_AUTHORITY.json>
  glade-tools corpus assurance attempt --inventory-spec <IN_SCOPE.json> --candidate-authority <RECONCILIATION.json> --candidate <glade> --candidate-root <glade-root> --tools <glade-tools> --tools-root <glade-tools-root> --replay-cleanup-authority <REMOTE_CLEANUP_AUTHORITY.json> --salesforce-cleanup-authority <REMOTE_CLEANUP_AUTHORITY.json> --output <ATTEMPT.json>
  glade-tools corpus assurance prepare --inventory-spec <IN_SCOPE.json> --attempt <ATTEMPT.json> --output <new-dir>
  glade-tools corpus assurance usage-draft --inventory-spec <IN_SCOPE.json> --ledger <ledger.json> --manifest <MANIFEST.json> --profile <source-profile.json> --policy <support-policy.json> --output <USAGE_DECISION_DRAFT.json>
  glade-tools corpus assurance usage --inventory-spec <IN_SCOPE.json> --ledger <ledger.json> --manifest <MANIFEST.json> --profile <source-profile.json> --policy <support-policy.json> --decisions <USAGE_DECISIONS.json> --output <CORPUS_USAGE.json>
  glade-tools corpus assurance replay --host <local|replay-worker> --inventory-spec <IN_SCOPE.json> --root-manifest <MANIFEST.json> --host-manifest <manifest.json> --candidate <glade> --tools <glade-tools> --output <REPLAY_SHARD.json>
  glade-tools corpus assurance merge-replay --inventory-spec <IN_SCOPE.json> --root-manifest <MANIFEST.json> --host-manifest <manifest.json> --host-manifest <manifest.json> --shard <REPLAY_SHARD.json> --shard <REPLAY_SHARD.json> --output <REPLAY.json>
  glade-tools corpus assurance local-proof-plan --inventory-spec <IN_SCOPE.json> --root-manifest <MANIFEST.json> --source-profile <source-profile.json> --sealed-usage <CORPUS_USAGE.json> --ledger <ledger.json> --policy <policy.json> --decisions <USAGE_DECISIONS.json> --fixture-root <docs/fixtures> --profile-output <profile.json> --usage-output <usage.json> --decision-output <decision.json> --manifest-output <fixtures.json>
  glade-tools corpus assurance local-proof --attempt <ATTEMPT.json> --profile <profile.json> --usage <usage.json> --decision <decision.json> --fixture-manifest <fixtures.json> --candidate <glade> --tools <glade-tools> --output <LOCAL_PROOF.json>
	  glade-tools corpus assurance release-validate --attempt <ATTEMPT.json> --glade-root <glade-root> --candidate <glade> --tools-root <glade-tools-root> --tools <glade-tools> --tools-freeze <FINAL_TOOLS_COMMIT> --output <RELEASE_VALIDATION.json>
  glade-tools corpus assurance oracle-plan --inventory-spec <IN_SCOPE.json> --root-manifest <MANIFEST.json> --source-profile <source-profile.json> --sealed-usage <CORPUS_USAGE.json> --ledger <ledger.json> --policy <policy.json> --decisions <decisions.json> --local-profile <profile.json> --local-usage <usage.json> --local-decision <decision.json> --fixture-manifest <fixtures.json> --local-proof <LOCAL_PROOF.json> --candidate <glade> --tools <glade-tools> --directives <directives.json> --profile-output <ASSURANCE_PROFILE.json> --output <ORACLE_PLAN.json>
  glade-tools corpus assurance exclusion-request --plan <ORACLE_PLAN.json> --profile <ASSURANCE_PROFILE.json> --sealed-usage <CORPUS_USAGE.json> --output <EXCLUSION_REQUEST.json>
  glade-tools corpus assurance authorize-exclusions --request <EXCLUSION_REQUEST.json> --plan <ORACLE_PLAN.json> --profile <ASSURANCE_PROFILE.json> --sealed-usage <CORPUS_USAGE.json> --policy <policy.json> --output <EXCLUSION_AUTHORITY.json>
  glade-tools corpus assurance oracle-bundle --attempt <ATTEMPT.json> --dev-hub-authority <DEV_HUB_AUTHORITY.json> --profile <ASSURANCE_PROFILE.json> --oracle-plan <ORACLE_PLAN.json> --exclusion-authority <EXCLUSION_AUTHORITY.json> --release-validation <RELEASE_VALIDATION.json> --local-proof <LOCAL_PROOF.json> --fixture-manifest <fixtures.json> --filter-script <filter.py> --scratch-definition <scratch.json> --tools-root <clean-glade-tools-root> --output <new-dir>
  glade-tools corpus assurance org-create --bundle <bundle.json> --alias <scratch-alias> --sf-bin /usr/local/bin/sf --output <ORG_CREATION.json>
  glade-tools corpus assurance org-preflight --bundle <bundle.json> --target-org <scratch-alias> --sf-bin /usr/local/bin/sf --output <ORG_PREFLIGHT.json>
  glade-tools corpus assurance salesforce-dispatch --bundle <bundle.json> --target-org <scratch-alias> --executor-root <attempt/executor/shard-N> --run-id <attempt-shard-N> --shard-index <0|1> --shard-count 2 --output <SALESFORCE_DISPATCH.json>
  glade-tools corpus assurance salesforce-run --bundle <bundle.json> --dispatch <SALESFORCE_DISPATCH.json> --org-preflight <ORG_PREFLIGHT.json> --target-org <scratch-alias> --sf-bin /usr/local/bin/sf --output <SALESFORCE_SHARD.json>
  glade-tools corpus assurance org-cleanup --bundle <bundle.json> --creation <ORG_CREATION.json>[.invalidated] [--org-preflight <ORG_PREFLIGHT.json>] --target-org <scratch-alias> --sf-bin /usr/local/bin/sf --output <ORG_CLEANUP.json>
  glade-tools corpus assurance salesforce-reconcile --oracle-plan <ORACLE_PLAN.json> --shard <SALESFORCE_SHARD.json> --dispatch <SALESFORCE_DISPATCH.json> --preflight <ORG_PREFLIGHT.json> --creation <ORG_CREATION.json> --cleanup <ORG_CLEANUP.json> --shard <SALESFORCE_SHARD.json> --dispatch <SALESFORCE_DISPATCH.json> --preflight <ORG_PREFLIGHT.json> --creation <ORG_CREATION.json> --cleanup <ORG_CLEANUP.json>
  glade-tools corpus assurance report --inventory-spec <IN_SCOPE.json> --root-manifest <MANIFEST.json> --ledger <ledger.json> --source-profile <source-profile.json> --policy <support-policy.json> --decisions <USAGE_DECISIONS.json> --sealed-usage <CORPUS_USAGE.json> --profile <ASSURANCE_PROFILE.json> --fixture-manifest <fixtures.json> --replay <REPLAY.json> --attempt <ATTEMPT.json> --local-proof <LOCAL_PROOF.json> --oracle-plan <ORACLE_PLAN.json> --exclusion-request <EXCLUSION_REQUEST.json> --exclusion-policy <exclusion-policy.json> --exclusion-authority <EXCLUSION_AUTHORITY.json> --release-validation <RELEASE_VALIDATION.json> --bundle <bundle.json> --filter-script <filter.py> --scratch-definition <scratch.json> --tools-amd64 <glade-tools-amd64> --remote-cleanup <REMOTE_CLEANUP.json> --remote-cleanup <REMOTE_CLEANUP.json> --remote-cleanup-authority <REMOTE_CLEANUP_AUTHORITY.json> --remote-cleanup-authority <REMOTE_CLEANUP_AUTHORITY.json> --replay-host-manifest <manifest.json> --replay-host-manifest <manifest.json> --replay-shard <REPLAY_SHARD.json> --replay-shard <REPLAY_SHARD.json> --shard <SALESFORCE_SHARD.json> --dispatch <SALESFORCE_DISPATCH.json> --preflight <ORG_PREFLIGHT.json> --creation <ORG_CREATION.json> --cleanup <ORG_CLEANUP.json> --shard <SALESFORCE_SHARD.json> --dispatch <SALESFORCE_DISPATCH.json> --preflight <ORG_PREFLIGHT.json> --creation <ORG_CREATION.json> --cleanup <ORG_CLEANUP.json> --output <ASSURANCE.json> --html-output <ASSURANCE.html> --receipt-output <RECEIPT.json>
	  glade-tools corpus assurance cleanup --attempt <ATTEMPT.json> --binding <REMOTE_CLEANUP_AUTHORITY.json> --output <REMOTE_CLEANUP.json>
`)
}
