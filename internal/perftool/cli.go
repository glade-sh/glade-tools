package perftool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/glade-sh/glade/internal/flagparse"
	"github.com/glade-sh/glade/tools/internal/editorfindings"
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
		var exit exitError
		if errors.As(err, &exit) {
			if exit.err != nil {
				fmt.Fprintf(stderr, "glade-plugin-performance: %v\n", exit.err)
			}
			return exit.code
		}
		fmt.Fprintf(stderr, "glade-plugin-performance: %v\n", err)
		return 1
	}
	return 0
}

type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func run(ctx context.Context, args []string, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) < 1 || args[0] != "performance" {
		return errors.New("usage: glade performance [scan] [--project <root>] [--trace <path>] [--org-facts <path>] [--format markdown|json|sarif] [--min-confidence static|measured|combined] [--fail-on none|high|measured] [--top <n>] [--editor-findings]")
	}
	scanArgs := args[1:]
	if len(scanArgs) > 0 && scanArgs[0] == "scan" {
		scanArgs = scanArgs[1:]
	}
	return runScan(scanArgs, w)
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
		String("org-facts", "").
		String("format", "f").
		String("top", "").
		String("min-confidence", "").
		String("fail-on", "").
		Bool("json", "j").
		Bool("editor-findings", "").
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
	format, err := scanFormat(parsed.String("format"), parsed.Bool("json"))
	if err != nil {
		return err
	}
	minConfidence, err := scanMinConfidence(parsed.String("min-confidence"))
	if err != nil {
		return err
	}
	failOn, err := scanFailOn(parsed.String("fail-on"))
	if err != nil {
		return err
	}
	report, err := perfscan.AnalyzeProject(perfscan.Options{
		ProjectRoot:  root,
		TracePath:    parsed.String("trace"),
		OrgFactsPath: parsed.String("org-facts"),
	})
	if err != nil {
		return err
	}
	report = filterReport(report, minConfidence, topN)
	if parsed.Bool("editor-findings") {
		if err := editorfindings.Write(w, performanceEditorFindings(report)); err != nil {
			return err
		}
	} else if err := writeReport(w, format, report); err != nil {
		return err
	}
	if failOnReport(report, failOn) {
		return exitError{code: 2, err: fmt.Errorf("performance findings meet --fail-on %s", failOn)}
	}
	return nil
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `glade performance plugin.

Usage:
  glade performance [scan] [--project <root>] [--trace <path>] [--org-facts <path>] [--format markdown|json|sarif] [--min-confidence static|measured|combined] [--fail-on none|high|measured] [--top <n>] [--editor-findings]
  glade-plugin-performance manifest --json
`)
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help"
}

type outputFormat string

const (
	outputMarkdown outputFormat = "markdown"
	outputJSON     outputFormat = "json"
	outputSARIF    outputFormat = "sarif"
)

type failOnMode string

const (
	failOnNone     failOnMode = "none"
	failOnHigh     failOnMode = "high"
	failOnMeasured failOnMode = "measured"
)

func scanFormat(value string, jsonFlag bool) (outputFormat, error) {
	if jsonFlag {
		if value != "" && !strings.EqualFold(value, string(outputJSON)) {
			return "", errors.New("--json cannot be combined with non-json --format")
		}
		return outputJSON, nil
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "md", "markdown":
		return outputMarkdown, nil
	case "json":
		return outputJSON, nil
	case "sarif":
		return outputSARIF, nil
	default:
		return "", errors.New("--format must be markdown, json, or sarif")
	}
}

func scanMinConfidence(value string) (perfscan.Confidence, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(perfscan.ConfidenceStatic):
		return perfscan.ConfidenceStatic, nil
	case string(perfscan.ConfidenceMeasured):
		return perfscan.ConfidenceMeasured, nil
	case string(perfscan.ConfidenceCombined):
		return perfscan.ConfidenceCombined, nil
	default:
		return "", errors.New("--min-confidence must be static, measured, or combined")
	}
}

func scanFailOn(value string) (failOnMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(failOnNone):
		return failOnNone, nil
	case string(failOnHigh):
		return failOnHigh, nil
	case string(failOnMeasured):
		return failOnMeasured, nil
	default:
		return "", errors.New("--fail-on must be none, high, or measured")
	}
}

func writeReport(w io.Writer, format outputFormat, report perfscan.Report) error {
	switch format {
	case outputJSON:
		return perfscan.WriteJSON(w, report)
	case outputSARIF:
		return perfscan.WriteSARIF(w, report)
	default:
		return perfscan.WriteMarkdown(w, report)
	}
}

func filterReport(report perfscan.Report, minConfidence perfscan.Confidence, topN int) perfscan.Report {
	filtered := report
	filtered.Findings = make([]perfscan.Finding, 0, len(report.Findings))
	for _, finding := range report.Findings {
		if confidenceRank(finding.Confidence) >= confidenceRank(minConfidence) {
			filtered.Findings = append(filtered.Findings, finding)
		}
	}
	if topN > 0 && len(filtered.Findings) > topN {
		filtered.Findings = filtered.Findings[:topN]
	}
	filtered.Finalize()
	return filtered
}

func failOnReport(report perfscan.Report, mode failOnMode) bool {
	switch mode {
	case failOnHigh:
		for _, finding := range report.Findings {
			if finding.Severity == perfscan.SeverityHigh {
				return true
			}
		}
	case failOnMeasured:
		for _, finding := range report.Findings {
			if finding.Confidence == perfscan.ConfidenceMeasured || finding.Confidence == perfscan.ConfidenceCombined {
				return true
			}
		}
	}
	return false
}

func confidenceRank(confidence perfscan.Confidence) int {
	switch confidence {
	case perfscan.ConfidenceCombined:
		return 3
	case perfscan.ConfidenceMeasured:
		return 2
	default:
		return 1
	}
}
