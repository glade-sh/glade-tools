package toolcli

import (
	"context"
	"fmt"
	"io"
	"strings"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpArg(args[0]) {
		printHelp(stdout)
		return 0
	}
	if args[0] == "manifest" {
		if len(args) != 2 || args[1] != "--json" {
			fmt.Fprintln(stderr, "glade-tools: usage: glade-plugin-compat manifest --json")
			return 1
		}
		if err := writeCompatManifest(stdout); err != nil {
			fmt.Fprintf(stderr, "glade-tools: %v\n", err)
			return 1
		}
		return 0
	}
	if args[0] == "orgpackage" {
		return RunOrgPackage(ctx, args, stdout, stderr)
	}
	if args[0] == "apex-rules" {
		if err := runApexRules(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "glade-tools: %v\n", err)
			return 1
		}
		return 0
	}
	if args[0] == "compat" {
		args = args[1:]
	}
	if err := runCompat(ctx, args, stdout); err != nil {
		fmt.Fprintf(stderr, "glade-tools: %v\n", err)
		return 1
	}
	return 0
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `glade-tools contains Glade maintenance commands.

Usage:
  glade-tools <command> [flags]

Entrypoints:
  glade-tools <command> [flags]
  glade-plugin-compat <command> [flags]
  glade compat <command> [flags]

Help:
  glade-tools <command> --help
  glade compat local-tests --help
  glade compat local-tests compare --help
  glade compat visualforce --help
  glade compat lwc --help

Commands:
  validate           Validate compatibility fixture files.
  run                Validate and execute fixtures.
  matrix             Print the full capability matrix.
  mvp                Print MVP readiness status.
  local-tests        Report local Apex test execution readiness.
  examples           Scan example projects and report support status.
  post-parity        Scan a project for unsupported surfaces.
  replay             Replay checked run bundles.
  ui-controllers     Discover Visualforce controller surfaces.
  server-examples    Probe checked server route examples.
  surface            Refresh and inspect the Salesforce surface ledger.
  corpus             Run Glade over a public corpus and classify diagnostics.
  visualforce        Capture scratch-org Visualforce rendering evidence.
  lwc                Capture LWC shell evidence and native API parity.
  dashboard          Generate compatibility dashboard.
  gaps               Generate known gaps document.
  stdlib             Generate standard library coverage document.
  docs-inventory     Inventory Salesforce docs.
  catalog            Build a capability catalog.
  reconcile          Reconcile docs inventory with the catalog.
  doc-contracts      Report Salesforce docs behavior contracts.
  declaration-contracts  Export docs declaration shapes for generated stubs.
  salesforce-coverage  Generate Salesforce coverage manifests.
  standard-objects   Report generated standard object coverage.
  stub-contracts     Report generated stub behavioral contract policy.
  stub-behavior      Report generated platform stub behavior status.
  stub-inventory     Compare stub source with generated shapes.
  product-namespaces Report product namespace coverage.
  tooling-fixtures   Summarize tooling fixture reports.
  evidence           Compare fixture evidence with a catalog.
  oracle-stdlib      Run scratch-org standard-library oracle probes.
  orgpackage         Capture installed package artifacts from a Salesforce org.

Compatibility:
  glade-tools compat <command> is accepted for old scripts.
  Prefer glade compat <command> after installing @glade/compat.
`)
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help"
}

func printCompatHelp(w io.Writer) {
	fmt.Fprintf(w, "%s\n\nExamples:\n  glade-tools matrix --json\n  glade-tools local-tests --project . --json\n  glade compat local-tests --project . --json\n", compatUsage())
}

func printCompatLocalTestsHelp(w io.Writer) {
	fmt.Fprint(w, strings.TrimSpace(`
Report local Apex test execution readiness.

Usage:
  glade-tools local-tests [--project <root>] [--class <name>] [--class-list <a,b>] [--class-file <path>] [--method <name>] [--parallel <n|auto>] [--json]
  glade compat local-tests [--project <root>] [--class <name>] [--class-list <a,b>] [--class-file <path>] [--method <name>] [--parallel <n|auto>] [--json]

Common flags:
  --project <root>          Project root. Defaults to current directory.
  --class <name>            Run one Apex test class.
  --class-list <a,b>        Run a comma-separated list of test classes.
  --class-file <path>       Run classes listed in a file.
  --start-class <name>      Resume from a class name in sorted order.
  --method <name>           Run one test method when paired with --class.
  --changed-since <ref>     Select tests affected since a git ref.
  --blockers-only           Print only blocking failures.
  --top-failures <n>        Limit failure groups in human output.
  --max-failure-groups <n>  Limit grouped failure output.
  --timeout <ms-per-test>   Per-test timeout in milliseconds.
  --parallel <n|auto>       Run test classes with n workers.
  --parallel-methods        Run test methods in parallel within each class.
  --trace-blockers          Include blocked-test traces in JSON output.
  --slow-test-ms <n>        Include traces/profiles for tests at or above n ms.
  --shard-count <n|auto>    Select one shard from a balanced class plan.
  --shard-index <i|auto>    Shard index to execute.
  --write-class-shards <dir>  Write balanced class shard files and exit.
  --duration-history <path> Optional perf JSON used to weight class sharding.
  --analyze                 Force project analysis before running tests.
  --profile-on-timeout      Capture profiles for timed-out tests.
  --cpu-profile <path>      Write a CPU profile for the local-test run.
  --mem-profile <path>      Write an allocation profile for the local-test run.
  --perf-json <path>        Write per-class timing data for future sharding.
  --progress                Print progress while running.
  --json                    Write JSON readiness results.
  --check <path>            Compare results with a checked baseline.

Examples:
  glade-tools local-tests --project . --json
  glade compat local-tests --project . --json
  glade compat local-tests --project . --class AccountServiceTest
  glade compat local-tests --project . --class-file tests.txt --top-failures 10

Comparison:
  glade-tools local-tests compare --help
  glade compat local-tests compare --help
`)+"\n")
}

func printCompatLocalTestsCompareHelp(w io.Writer) {
	fmt.Fprint(w, strings.TrimSpace(`
Compare local Apex test performance with an external target manifest.

Usage:
  glade-tools local-tests compare --base-bin <path> --candidate-bin <path> --project <root> --out <new-dir> --workers <n> --runs 5 --manifest <path> [--json]
  glade compat local-tests compare --base-bin <path> --candidate-bin <path> --project <root> --out <new-dir> --workers <n> --runs 5 --manifest <path> [--json]

Required flags:
  --base-bin <path>       Base glade-tools compat executable.
  --candidate-bin <path>  Candidate glade-tools compat executable.
  --project <root>        Source project copied for every cold invocation.
  --out <new-dir>         New private output directory; it must not exist.
  --workers <n>           Explicit local-test worker count.
  --runs 5                Exactly five cold alternating pairs per target.
  --manifest <path>       External target manifest containing any selectors.

Optional flags:
  --json                   Mirror deterministic summary.json to stdout.

Each target runs five cold alternating pairs in AB, BA, AB, BA, AB order.
Requested profiles run afterward as diagnostics; profiles are excluded from timing samples.
`)+"\n")
}
