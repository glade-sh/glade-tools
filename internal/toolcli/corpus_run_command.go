package toolcli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/glade-sh/glade/tools/internal/corpusrun"
)

// runCorpusV2 handles `corpus run` and `corpus golden`. It returns the exit
// code: 0 clean, 1 NEW, 2 infrastructure or usage.
func runCorpusV2(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "glade-tools: corpus run|golden ...")
		return corpusrun.ExitInfra
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "glade-tools: %v\n", err)
		return corpusrun.ExitInfra
	}
	switch args[0] {
	case "run":
		fs := flag.NewFlagSet("corpus run", flag.ContinueOnError)
		fs.SetOutput(stderr)
		tier := fs.String("tier", "", "tier A, B or C (tiers are nested)")
		gladeBin := fs.String("glade", "", "glade binary")
		config := fs.String("config", "", "private corpus config directory")
		only := fs.String("only", "", "comma-separated repos to narrow the tier")
		jsonOut := fs.String("json", "", "write the full run report here (feeds `corpus golden`)")
		scratch := fs.String("scratch", "", "scratch parent directory (default: temp)")
		timeout := fs.Duration("step-timeout", 2*time.Hour, "timeout per glade command")
		parallelism := fs.Int("parallelism", 1, "glade test --parallelism")
		if err := fs.Parse(args[1:]); err != nil {
			return corpusrun.ExitInfra
		}
		if *tier == "" || *gladeBin == "" || *config == "" {
			return fail(fmt.Errorf("--tier, --glade and --config are required"))
		}
		var names []string
		if *only != "" {
			names = strings.Split(*only, ",")
		}
		report, err := corpusrun.Run(ctx, corpusrun.Options{
			Glade: *gladeBin, ConfigDir: *config, Tier: *tier, Only: names, Scratch: *scratch,
			StepTimeout: *timeout, Parallelism: *parallelism,
			Progress: func(line string) { fmt.Fprintln(stderr, line) },
		})
		if err != nil {
			return fail(err)
		}
		if *jsonOut != "" {
			data, err := json.MarshalIndent(report, "", "  ")
			if err == nil {
				err = os.WriteFile(*jsonOut, append(data, '\n'), 0o644)
			}
			if err != nil {
				return fail(err)
			}
		}
		report.WriteText(stdout)
		return report.ExitCode
	case "golden":
		fs := flag.NewFlagSet("corpus golden", flag.ContinueOnError)
		fs.SetOutput(stderr)
		config := fs.String("config", "", "private corpus config directory")
		repo := fs.String("repo", "", "repo to refresh")
		run := fs.String("from-run", "", "run report written by `corpus run --json`")
		if err := fs.Parse(args[1:]); err != nil {
			return corpusrun.ExitInfra
		}
		if *config == "" || *repo == "" || *run == "" {
			return fail(fmt.Errorf("--config, --repo and --from-run are required"))
		}
		stats, err := corpusrun.BuildFromRun(*config, *repo, *run)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "golden written: %d unclassified, %d dropped\n", stats.Unclassified, stats.Dropped)
		return corpusrun.ExitClean
	}
	return fail(fmt.Errorf("unknown corpus subcommand %q", args[0]))
}
