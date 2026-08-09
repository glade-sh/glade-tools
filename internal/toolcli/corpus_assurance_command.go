package toolcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

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
	case "prepare":
		flags := flag.NewFlagSet("corpus assurance prepare", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		inventory, output := flags.String("inventory-spec", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*inventory, *output); err != nil {
			return err
		}
		manifest, err := corpusassurance.PrepareInventory(*inventory, *output)
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "prepare", len(manifest.Repositories), *output)
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
		candidatePath, candidateCommit := flags.String("candidate", "", ""), flags.String("candidate-commit", "", "")
		toolsPath, toolsCommit := flags.String("tools", "", ""), flags.String("tools-commit", "", "")
		output := flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*host, *inventory, *root, *manifest, *candidatePath, *candidateCommit, *toolsPath, *toolsCommit, *output); err != nil {
			return err
		}
		candidate, err := assuranceRuntimeArtifact(*candidatePath, *candidateCommit)
		if err != nil {
			return err
		}
		tools, err := assuranceRuntimeArtifact(*toolsPath, *toolsCommit)
		if err != nil {
			return err
		}
		shard, err := corpusassurance.RunReplay(corpusassurance.ReplayRequest{Host: *host, Candidate: candidate, CandidatePath: *candidatePath, Tools: tools, ToolsPath: *toolsPath, InventoryPath: *inventory, RootManifestPath: *root, HostManifestPath: *manifest, OutputPath: *output})
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
		candidatePath, candidateCommit := flags.String("candidate", "", ""), flags.String("candidate-commit", "", "")
		toolsPath, toolsCommit := flags.String("tools", "", ""), flags.String("tools-commit", "", "")
		output := flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*profile, *usage, *decision, *fixtures, *candidatePath, *candidateCommit, *toolsPath, *toolsCommit, *output); err != nil {
			return err
		}
		candidate, err := assuranceRuntimeArtifact(*candidatePath, *candidateCommit)
		if err != nil {
			return err
		}
		tools, err := assuranceRuntimeArtifact(*toolsPath, *toolsCommit)
		if err != nil {
			return err
		}
		proof, err := corpusassurance.RunLocalProof(corpusassurance.LocalProofRequest{ProfilePath: *profile, UsagePath: *usage, DecisionPath: *decision, FixtureManifestPath: *fixtures, Candidate: candidate, CandidatePath: *candidatePath, Tools: tools, ToolsPath: *toolsPath, OutputPath: *output})
		if err != nil {
			return err
		}
		return writeCorpusAssuranceResult(w, "local-proof", len(proof.Surfaces), *output)
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

func assuranceRuntimeArtifact(path, commit string) (corpusassurance.RuntimeArtifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return corpusassurance.RuntimeArtifact{}, err
	}
	sum := sha256.Sum256(data)
	return corpusassurance.RuntimeArtifact{Commit: commit, OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: hex.EncodeToString(sum[:])}, nil
}

func printCorpusAssuranceHelp(w io.Writer) {
	fmt.Fprint(w, `Run the sealed private-corpus assurance workflow.

Usage:
  glade-tools corpus assurance prepare --inventory-spec <IN_SCOPE.json> --output <new-dir>
  glade-tools corpus assurance usage --inventory-spec <IN_SCOPE.json> --ledger <ledger.json> --manifest <MANIFEST.json> --profile <source-profile.json> --policy <support-policy.json> --decisions <USAGE_DECISIONS.json> --output <CORPUS_USAGE.json>
  glade-tools corpus assurance replay --host <local|casper> --inventory-spec <IN_SCOPE.json> --root-manifest <MANIFEST.json> --host-manifest <manifest.json> --candidate <glade> --candidate-commit <commit> --tools <glade-tools> --tools-commit <commit> --output <REPLAY_SHARD.json>
  glade-tools corpus assurance merge-replay --inventory-spec <IN_SCOPE.json> --root-manifest <MANIFEST.json> --host-manifest <manifest.json> --host-manifest <manifest.json> --shard <REPLAY_SHARD.json> --shard <REPLAY_SHARD.json> --output <REPLAY.json>
  glade-tools corpus assurance local-proof --profile <profile.json> --usage <usage.json> --decision <decision.json> --fixture-manifest <fixtures.json> --candidate <glade> --candidate-commit <commit> --tools <glade-tools> --tools-commit <commit> --output <LOCAL_PROOF.json>
  glade-tools corpus assurance oracle-plan --inventory <IN_SCOPE.json> --root-manifest <MANIFEST.json> --source-profile <source-profile.json> --sealed-usage <CORPUS_USAGE.json> --ledger <ledger.json> --policy <policy.json> --decisions <decisions.json> --local-profile <profile.json> --local-usage <usage.json> --local-decision <decision.json> --fixture-manifest <fixtures.json> --local-proof <LOCAL_PROOF.json> --candidate <glade> --tools <glade-tools> --directives <directives.json> --profile-output <ASSURANCE_PROFILE.json> --output <ORACLE_PLAN.json>
  glade-tools corpus assurance exclusion-request --plan <ORACLE_PLAN.json> --profile <ASSURANCE_PROFILE.json> --sealed-usage <CORPUS_USAGE.json> --output <EXCLUSION_REQUEST.json>
  glade-tools corpus assurance authorize-exclusions --request <EXCLUSION_REQUEST.json> --plan <ORACLE_PLAN.json> --profile <ASSURANCE_PROFILE.json> --sealed-usage <CORPUS_USAGE.json> --policy <policy.json> --output <EXCLUSION_AUTHORITY.json>
`)
}
