package toolcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/glade-sh/glade/tools/internal/compat"
	"github.com/glade-sh/glade/tools/internal/editorfindings"
)

func runCompatVisualforce(ctx context.Context, args []string, w io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printCompatVisualforceHelp(w)
		return nil
	}
	switch args[0] {
	case "capture":
		return runCompatVisualforceCapture(ctx, args[1:], w)
	case "diff":
		return runCompatVisualforceDiff(args[1:], w)
	case "summary":
		return runCompatVisualforceSummary(args[1:], w)
	default:
		return errors.New(compatVisualforceUsage())
	}
}

func runCompatVisualforceCapture(ctx context.Context, args []string, w io.Writer) error {
	options := compat.VisualforceCaptureOptions{Project: "."}
	localOptions := compat.LocalVisualforceCaptureOptions{Project: "."}
	localCapture := false
	jsonOut := false
	editorFindings := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--local":
			localCapture = true
		case "--glade-bin":
			if i+1 >= len(args) {
				return errors.New("--glade-bin requires a value")
			}
			localOptions.GladeBin = args[i+1]
			i++
		case "--target-org":
			if i+1 >= len(args) {
				return errors.New("--target-org requires a value")
			}
			options.TargetOrg = args[i+1]
			i++
		case "--project":
			if i+1 >= len(args) {
				return errors.New("--project requires a value")
			}
			options.Project = args[i+1]
			localOptions.Project = args[i+1]
			i++
		case "--pages":
			if i+1 >= len(args) {
				return errors.New("--pages requires a value")
			}
			options.Pages = append(options.Pages, strings.Split(args[i+1], ",")...)
			localOptions.Pages = append(localOptions.Pages, strings.Split(args[i+1], ",")...)
			i++
		case "--phase":
			if i+1 >= len(args) {
				return errors.New("--phase requires a value")
			}
			options.Phase = args[i+1]
			localOptions.Phase = args[i+1]
			i++
		case "--out":
			if i+1 >= len(args) {
				return errors.New("--out requires a value")
			}
			options.Out = args[i+1]
			localOptions.Out = args[i+1]
			i++
		case "--skip-deploy":
			options.SkipDeploy = true
		case "--batch-size":
			if i+1 >= len(args) {
				return errors.New("--batch-size requires a value")
			}
			batchSize, err := strconv.Atoi(args[i+1])
			if err != nil || batchSize <= 0 {
				return errors.New("--batch-size must be a positive integer")
			}
			options.BatchSize = batchSize
			i++
		case "--json":
			jsonOut = true
		case "--editor-findings":
			editorFindings = true
		default:
			return fmt.Errorf("unknown visualforce capture flag %q", args[i])
		}
	}
	var report compat.VisualforceCaptureReport
	var err error
	if localCapture {
		report, err = compat.RunLocalVisualforceCapture(ctx, localOptions)
	} else {
		report, err = compat.RunVisualforceCapture(ctx, options)
	}
	if jsonOut {
		var encodeErr error
		if editorFindings {
			outPath := options.Out
			if localCapture {
				outPath = localOptions.Out
			}
			encodeErr = editorfindings.Write(w, visualforceCaptureEditorFindings(report, outPath))
		} else {
			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			encodeErr = enc.Encode(report)
		}
		if encodeErr != nil && err == nil {
			err = encodeErr
		}
		return visualforceCaptureError(report, err)
	}
	if report.Username != "" || report.OrgID != "" {
		fmt.Fprintf(w, "confirmed target org %s", report.TargetOrg)
		if report.Username != "" {
			fmt.Fprintf(w, " as %s", report.Username)
		}
		if report.OrgID != "" {
			fmt.Fprintf(w, " (%s)", report.OrgID)
		}
		fmt.Fprintln(w)
	}
	if report.Deploy.Ran && report.Deploy.OK {
		fmt.Fprintf(w, "deployed %s\n", report.SourceDir)
	}
	if report.Probe.Ran && report.Probe.OK {
		if localCapture {
			fmt.Fprintf(w, "captured %d local Visualforce pages: html pass=%d fail=%d notCaptured=%d, pdf pass=%d fail=%d notCaptured=%d\n", report.Counts.Pages, report.Counts.HTMLPass, report.Counts.HTMLFail, report.Counts.HTMLNotCaptured, report.Counts.PDFPass, report.Counts.PDFFail, report.Counts.PDFNotCaptured)
		} else {
			fmt.Fprintf(w, "captured %d Visualforce pages: html pass=%d fail=%d notCaptured=%d, pdf pass=%d fail=%d notCaptured=%d\n", report.Counts.Pages, report.Counts.HTMLPass, report.Counts.HTMLFail, report.Counts.HTMLNotCaptured, report.Counts.PDFPass, report.Counts.PDFFail, report.Counts.PDFNotCaptured)
		}
	}
	if options.Out != "" {
		outPath := options.Out
		if localCapture {
			outPath = localOptions.Out
		}
		fmt.Fprintf(w, "wrote %s\n", outPath)
	}
	return visualforceCaptureError(report, err)
}

func visualforceCaptureError(report compat.VisualforceCaptureReport, err error) error {
	if err != nil {
		return err
	}
	if !report.OK {
		return fmt.Errorf("Visualforce capture failed: html fail=%d notCaptured=%d, pdf fail=%d notCaptured=%d", report.Counts.HTMLFail, report.Counts.HTMLNotCaptured, report.Counts.PDFFail, report.Counts.PDFNotCaptured)
	}
	return nil
}

func runCompatVisualforceDiff(args []string, w io.Writer) error {
	salesforcePath := ""
	localPath := ""
	project := ""
	phase := ""
	outPath := ""
	jsonOut := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--salesforce":
			if i+1 >= len(args) {
				return errors.New("--salesforce requires a value")
			}
			salesforcePath = args[i+1]
			i++
		case "--local":
			if i+1 >= len(args) {
				return errors.New("--local requires a value")
			}
			localPath = args[i+1]
			i++
		case "--project":
			if i+1 >= len(args) {
				return errors.New("--project requires a value")
			}
			project = args[i+1]
			i++
		case "--phase":
			if i+1 >= len(args) {
				return errors.New("--phase requires a value")
			}
			phase = args[i+1]
			i++
		case "--out":
			if i+1 >= len(args) {
				return errors.New("--out requires a value")
			}
			outPath = args[i+1]
			i++
		case "--json":
			jsonOut = true
		default:
			return fmt.Errorf("unknown visualforce diff flag %q", args[i])
		}
	}
	if strings.TrimSpace(salesforcePath) == "" || strings.TrimSpace(localPath) == "" {
		return errors.New("usage: glade-tools visualforce diff --salesforce <json> --local <json> [--project <root>] [--phase <n>] [--out <path>] [--json]")
	}
	report, err := compat.DiffVisualforceCaptureFilesWithProjectPhase(salesforcePath, localPath, project, phase)
	if err != nil {
		return err
	}
	if strings.TrimSpace(outPath) != "" {
		if err := writeVisualforceDiffReportJSON(outPath, report); err != nil {
			return err
		}
	}
	if jsonOut {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		printVisualforceDiffReport(w, report)
	}
	if !report.OK {
		return fmt.Errorf("Visualforce capture diff found: %d differences", report.DiffCount)
	}
	return nil
}

func runCompatVisualforceSummary(args []string, w io.Writer) error {
	project := "."
	phase := ""
	jsonOut := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project":
			if i+1 >= len(args) {
				return errors.New("--project requires a value")
			}
			project = args[i+1]
			i++
		case "--phase":
			if i+1 >= len(args) {
				return errors.New("--phase requires a value")
			}
			phase = args[i+1]
			i++
		case "--json":
			jsonOut = true
		default:
			return fmt.Errorf("unknown visualforce summary flag %q", args[i])
		}
	}
	summary, err := compat.SummarizeVisualforceProbeIndexWithPhase(project, phase)
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}
	fmt.Fprintf(w, "visualforce probe fixture: %d pages across %d groups\n", summary.PageCount, summary.GroupCount)
	printVisualforceSummaryCounts(w, "owners", summary.OwnerCounts)
	printVisualforceSummaryCounts(w, "categories", summary.CategoryCounts)
	printVisualforceSummaryCounts(w, "phases", summary.PhaseCounts)
	printVisualforceSummaryCounts(w, "families", summary.FamilyCounts)
	printVisualforceSummaryCounts(w, "claims", summary.ClaimCounts)
	printVisualforceSummaryCounts(w, "statuses", summary.StatusCounts)
	for _, group := range summary.Groups {
		fmt.Fprintf(w, "- %s", group.Name)
		if group.Category != "" || group.Owner != "" {
			fmt.Fprint(w, " (")
			var parts []string
			if group.Category != "" {
				parts = append(parts, group.Category)
			}
			if group.Owner != "" {
				parts = append(parts, group.Owner)
			}
			fmt.Fprint(w, strings.Join(parts, ", "))
			fmt.Fprint(w, ")")
		}
		fmt.Fprintf(w, ": %d pages\n", group.PageCount)
	}
	return nil
}

func writeVisualforceDiffReportJSON(path string, report compat.VisualforceCaptureDiffReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func printVisualforceDiffReport(w io.Writer, report compat.VisualforceCaptureDiffReport) {
	if report.OK {
		fmt.Fprintln(w, "visualforce diff: no differences")
		printVisualforceDiffScoreboard(w, report.Summary.Scoreboard)
		return
	}
	fmt.Fprintf(w, "visualforce diff: %d differences\n", report.DiffCount)
	for _, diff := range report.Diffs {
		if diff.Variant != "" {
			fmt.Fprintf(w, "- %s %s %s: salesforce=%s local=%s\n", diff.Page, diff.Variant, diff.Field, diff.Salesforce, diff.Local)
			continue
		}
		fmt.Fprintf(w, "- %s %s: salesforce=%s local=%s\n", diff.Page, diff.Field, diff.Salesforce, diff.Local)
	}
	printVisualforceDiffScoreboard(w, report.Summary.Scoreboard)
}

func printVisualforceDiffScoreboard(w io.Writer, scoreboard compat.VisualforceCaptureDiffScoreboard) {
	printVisualforceDiffScoreboardRows(w, "scoreboard by group", scoreboard.Groups)
	printVisualforceDiffScoreboardRows(w, "scoreboard by owner", scoreboard.Owners)
	printVisualforceDiffScoreboardRows(w, "scoreboard by category", scoreboard.Categories)
}

func printVisualforceDiffScoreboardRows(w io.Writer, label string, rows []compat.VisualforceCaptureDiffScoreboardRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(w, "%s:\n", label)
	for _, row := range rows {
		fmt.Fprintf(w, "- %s", row.Name)
		if row.Category != "" || row.Owner != "" {
			var parts []string
			if row.Category != "" {
				parts = append(parts, row.Category)
			}
			if row.Owner != "" {
				parts = append(parts, row.Owner)
			}
			fmt.Fprintf(w, " (%s)", strings.Join(parts, ", "))
		}
		fmt.Fprintf(w, ": pass=%d fail=%d missing=%d diffs=%d pages=%d\n", row.PassCount, row.FailCount, row.MissingCount, row.DiffCount, row.PageCount)
	}
}

func printVisualforceSummaryCounts(w io.Writer, label string, counts map[string]int) {
	if len(counts) == 0 {
		return
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	fmt.Fprintf(w, "%s: %s\n", label, strings.Join(parts, ", "))
}

func printCompatVisualforceHelp(w io.Writer) {
	fmt.Fprint(w, strings.TrimSpace(`
Capture and diff Visualforce rendering evidence.

Usage:
  glade-tools visualforce capture --local --glade-bin <path> --project <root> [--pages <a,b>] [--phase <n>] [--out <path>] [--json] [--editor-findings]
  glade-tools visualforce capture --target-org <alias> [--project <root>] [--pages <a,b>] [--phase <n>] [--out <path>] [--skip-deploy] [--batch-size <n>] [--json] [--editor-findings]
  glade-tools visualforce diff --salesforce <json> --local <json> [--project <root>] [--phase <n>] [--out <path>] [--json]
  glade-tools visualforce summary [--project <root>] [--phase <n>] [--json]
  glade compat visualforce capture --local --glade-bin <path> --project <root> [--pages <a,b>] [--phase <n>] [--out <path>] [--json] [--editor-findings]
  glade compat visualforce capture --target-org <alias> [--project <root>] [--pages <a,b>] [--phase <n>] [--out <path>] [--skip-deploy] [--batch-size <n>] [--json] [--editor-findings]
  glade compat visualforce diff --salesforce <json> --local <json> [--project <root>] [--phase <n>] [--out <path>] [--json]
  glade compat visualforce summary [--project <root>] [--phase <n>] [--json]

Common flags:
  --local                Capture from a local glade dev vf subprocess.
  --glade-bin <path>     Glade binary to run for local capture.
  --target-org <alias>   Scratch org alias or username.
  --project <root>       Salesforce project root. Defaults to current directory.
  --pages <a,b>          Comma-separated Visualforce page API names. Defaults to discovered .page files.
  --phase <n>            Filter capture, diff, or summary to pages in a probe-index phase.
  --out <path>           Write the capture or diff report JSON.
  --skip-deploy          Capture against already-deployed metadata.
  --batch-size <n>       Salesforce probe pages per Apex run. Defaults to 5.
  --salesforce <json>    Salesforce capture report for diff.
  --local <json>         Local capture report for diff.
  --project <root>       Project root containing visualforce-probe-index.json for scoreboard lanes.
  --json                 Write the report JSON to stdout.
  --editor-findings      With --json, write glade.findings.v1 to stdout.

Examples:
  glade-tools visualforce capture --local --glade-bin /path/to/glade --project /tmp/vf-parity-project --pages Core,Fields --phase 1 --out /tmp/glade-vf-local-capture.json
  glade-tools visualforce capture --target-org oaer-probe-max --project /tmp/vf-parity-project --pages Core,Fields --phase 1 --out /tmp/glade-vf-parity-capture.json
  glade-tools visualforce diff --salesforce /tmp/salesforce-vf.json --local /tmp/local-vf.json --project docs/fixtures/visualforce/probe-project --phase 1 --out /tmp/glade-vf-diff.json
  glade-tools visualforce summary --project docs/fixtures/visualforce/probe-project --phase 1 --json
  glade compat visualforce capture --local --glade-bin /path/to/glade --project /tmp/vf-parity-project --out /tmp/glade-vf-local-capture.json
  glade compat visualforce capture --target-org oaer-probe-max --project /tmp/vf-parity-project --out /tmp/glade-vf-parity-capture.json
  glade compat visualforce diff --salesforce /tmp/salesforce-vf.json --local /tmp/local-vf.json --project docs/fixtures/visualforce/probe-project --out /tmp/glade-vf-diff.json
`)+"\n")
}

func compatVisualforceUsage() string {
	return "usage: glade-tools visualforce capture --local --glade-bin <path> --project <root> [--pages <a,b>] [--phase <n>] [--out <path>] [--json] [--editor-findings]\n       glade-tools visualforce capture --target-org <alias> [--project <root>] [--pages <a,b>] [--phase <n>] [--out <path>] [--skip-deploy] [--batch-size <n>] [--json] [--editor-findings]\n       glade-tools visualforce diff --salesforce <json> --local <json> [--project <root>] [--phase <n>] [--out <path>] [--json]\n       glade-tools visualforce summary [--project <root>] [--phase <n>] [--json]"
}
