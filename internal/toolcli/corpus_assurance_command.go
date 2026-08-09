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
	case "oracle-plan":
		flags := flag.NewFlagSet("corpus assurance oracle-plan", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		sourceProfile, usage := flags.String("source-profile", "", ""), flags.String("sealed-usage", "", "")
		ledger, fixtures := flags.String("ledger", "", ""), flags.String("fixture-manifest", "", "")
		proof, directives := flags.String("local-proof", "", ""), flags.String("directives", "", "")
		profileOutput, output := flags.String("profile-output", "", ""), flags.String("output", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*sourceProfile, *usage, *ledger, *fixtures, *proof, *directives, *profileOutput, *output); err != nil {
			return err
		}
		if _, err := corpusassurance.BuildAssuranceProfile(*sourceProfile, *usage, *ledger, *fixtures, *proof, *profileOutput); err != nil {
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

func printCorpusAssuranceHelp(w io.Writer) {
	fmt.Fprint(w, `Run the sealed private-corpus assurance workflow.

Usage:
  glade-tools corpus assurance prepare --inventory-spec <IN_SCOPE.json> --output <new-dir>
  glade-tools corpus assurance usage --inventory-spec <IN_SCOPE.json> --ledger <ledger.json> --manifest <MANIFEST.json> --profile <source-profile.json> --policy <support-policy.json> --decisions <USAGE_DECISIONS.json> --output <CORPUS_USAGE.json>
  glade-tools corpus assurance oracle-plan --source-profile <source-profile.json> --sealed-usage <CORPUS_USAGE.json> --ledger <ledger.json> --fixture-manifest <fixtures.json> --local-proof <LOCAL_PROOF.json> --directives <directives.json> --profile-output <ASSURANCE_PROFILE.json> --output <ORACLE_PLAN.json>
  glade-tools corpus assurance exclusion-request --plan <ORACLE_PLAN.json> --profile <ASSURANCE_PROFILE.json> --sealed-usage <CORPUS_USAGE.json> --output <EXCLUSION_REQUEST.json>
  glade-tools corpus assurance authorize-exclusions --request <EXCLUSION_REQUEST.json> --plan <ORACLE_PLAN.json> --profile <ASSURANCE_PROFILE.json> --sealed-usage <CORPUS_USAGE.json> --policy <policy.json> --output <EXCLUSION_AUTHORITY.json>
`)
}
