package perftool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/glade-sh/glade/internal/flagparse"
	"github.com/glade-sh/glade/tools/internal/perfscan"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpArg(args[0]) {
		printHelp(stdout)
		return 0
	}
	if args[0] == "manifest" {
		if len(args) != 2 || args[1] != "--json" {
			fmt.Fprintln(stderr, "glade-plugin-performance: usage: glade-plugin-performance manifest --json")
			return 1
		}
		if err := writeManifest(stdout); err != nil {
			fmt.Fprintf(stderr, "glade-plugin-performance: %v\n", err)
			return 1
		}
		return 0
	}
	if err := run(ctx, args, stdout); err != nil {
		fmt.Fprintf(stderr, "glade-plugin-performance: %v\n", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, args []string, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) < 2 || args[0] != "performance" || args[1] != "scan" {
		return errors.New("usage: glade performance scan [--project <root>] [--trace <path>] [--json] [--top <n>]")
	}
	return runScan(args[2:], w)
}

func runScan(args []string, w io.Writer) error {
	for _, arg := range args {
		if isHelpArg(arg) {
			printHelp(w)
			return nil
		}
	}
	root := "."
	topN := 0
	parsed, err := flagparse.New("glade performance scan").
		String("project", "p").
		String("trace", "t").
		String("top", "").
		Bool("json", "j").
		Parse(args)
	if err != nil {
		return err
	}
	if parsed.String("project") != "" {
		root = parsed.String("project")
	}
	if parsed.String("top") != "" {
		parsedTop, err := strconv.Atoi(parsed.String("top"))
		if err != nil || parsedTop < 0 {
			return errors.New("--top must be a non-negative integer")
		}
		topN = parsedTop
	}
	report, err := perfscan.AnalyzeProject(perfscan.Options{
		ProjectRoot: root,
		TracePath:   parsed.String("trace"),
		TopN:        topN,
	})
	if err != nil {
		return err
	}
	if parsed.Bool("json") {
		return perfscan.WriteJSON(w, report)
	}
	return perfscan.WriteMarkdown(w, report)
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `glade performance plugin.

Usage:
  glade performance scan [--project <root>] [--trace <path>] [--json] [--top <n>]
  glade-plugin-performance manifest --json
`)
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help"
}
