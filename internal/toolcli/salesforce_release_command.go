package toolcli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/glade-sh/glade/tools/internal/releasecontract"
)

type salesforceReleaseOptions struct {
	Contract  string
	GladeRoot string
	JSON      bool
	Write     bool
	Check     bool
}

func runSalesforceRelease(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && isHelpArg(args[0]) {
		printSalesforceReleaseHelp(stdout)
		return 0
	}
	opts, err := parseSalesforceReleaseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "glade-tools: %v\n", err)
		return 1
	}
	if opts.Write || opts.Check {
		fmt.Fprintln(stderr, "glade-tools: release generation is not available until the generator is added")
		return 1
	}
	analysis, err := releasecontract.Analyze(opts.Contract)
	if err != nil {
		fmt.Fprintf(stderr, "glade-tools: %v\n", err)
		return 1
	}
	if opts.JSON {
		if err := json.NewEncoder(stdout).Encode(analysis.Report); err != nil {
			fmt.Fprintf(stderr, "glade-tools: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "status: %s\n", analysis.Report.Status)
	}
	if analysis.Report.Status != "pass" {
		return 1
	}
	return 0
}

func parseSalesforceReleaseFlags(args []string) (salesforceReleaseOptions, error) {
	opts := salesforceReleaseOptions{}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--contract", "--glade-root":
			if seen[args[i]] {
				return opts, fmt.Errorf("duplicate flag: %s", args[i])
			}
			if i+1 >= len(args) {
				return opts, fmt.Errorf("required value missing for %s", args[i])
			}
			if args[i] == "--contract" {
				opts.Contract = args[i+1]
			} else {
				opts.GladeRoot = args[i+1]
			}
			seen[args[i]] = true
			i++
		case "--json", "--write", "--check":
			if seen[args[i]] {
				return opts, fmt.Errorf("duplicate flag: %s", args[i])
			}
			seen[args[i]] = true
			if args[i] == "--json" {
				opts.JSON = true
			}
			if args[i] == "--write" {
				opts.Write = true
			}
			if args[i] == "--check" {
				opts.Check = true
			}
		default:
			return opts, fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	if strings.TrimSpace(opts.Contract) == "" {
		return opts, fmt.Errorf("required flag missing: --contract")
	}
	if opts.Write && opts.Check {
		return opts, fmt.Errorf("cannot combine --write and --check")
	}
	if opts.JSON && (opts.Write || opts.Check) {
		return opts, fmt.Errorf("cannot combine --json with --write or --check")
	}
	if !opts.JSON && !opts.Write && !opts.Check {
		return opts, fmt.Errorf("one of --json, --write, or --check is required")
	}
	if (opts.Write || opts.Check) && strings.TrimSpace(opts.GladeRoot) == "" {
		return opts, fmt.Errorf("generation mode requires --glade-root")
	}
	return opts, nil
}

func printSalesforceReleaseHelp(w io.Writer) {
	fmt.Fprint(w, "usage: glade-tools salesforce release --contract <path> [--json] [--glade-root <path>] [--write|--check]\n")
}
