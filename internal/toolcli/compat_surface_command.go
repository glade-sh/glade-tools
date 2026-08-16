package toolcli

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func runCompatSurface(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New(surfaceUsage())
	}
	if isHelpArg(args[0]) {
		fmt.Fprintln(w, surfaceUsage())
		return nil
	}
	switch args[0] {
	case "refresh":
		return runCompatSurfaceRefresh(args[1:], w)
	case "sources":
		return runCompatSurfaceSources(args[1:], w)
	case "docs":
		return runCompatSurfaceDocs(args[1:], w)
	case "org":
		return runCompatSurfaceOrg(args[1:], w)
	case "glade":
		return runCompatSurfaceGlade(args[1:], w)
	case "evidence":
		return runCompatSurfaceEvidence(args[1:], w)
	case "ledger":
		return runCompatSurfaceLedger(args[1:], w)
	case "packet":
		return runCompatSurfacePacket(args[1:], w)
	case "progress":
		return runCompatSurfaceProgress(args[1:], w)
	case "gaps":
		return runCompatSurfaceGaps(args[1:], w)
	case "explain":
		return runCompatSurfaceExplain(args[1:], w)
	case "check":
		return runCompatSurfaceCheck(args[1:], w)
	case "strict-current-base":
		return runCompatSurfaceStrictCurrentBase(args[1:], w)
	case "support-profile":
		return runCompatSurfaceSupportProfile(args[1:], w)
	case "corpus-usage":
		return runCompatSurfaceCorpusUsage(args[1:], w)
	case "delta-preflight":
		return runCompatSurfaceDeltaPreflight(args[1:], w)
	default:
		return errors.New(surfaceUsage())
	}
}

func surfaceUsage() string {
	return "usage: glade-tools surface refresh|sources|docs|org|glade|evidence|ledger|packet|progress|gaps|explain|check|strict-current-base|support-profile|corpus-usage|delta-preflight [flags]"
}

func runCompatSurfaceSources(args []string, w io.Writer) error {
	docs := ""
	output := ""
	check := ""
	jsonOut := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--docs":
			i++
			var err error
			docs, err = argValue(args, i, "--docs")
			if err != nil {
				return err
			}
		case "--output":
			i++
			var err error
			output, err = argValue(args, i, "--output")
			if err != nil {
				return err
			}
		case "--check":
			i++
			var err error
			check, err = argValue(args, i, "--check")
			if err != nil {
				return err
			}
		case "--json":
			jsonOut = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	requested := 0
	for _, set := range []bool{jsonOut, output != "", check != ""} {
		if set {
			requested++
		}
	}
	if requested > 1 {
		return errors.New("use only one of --json, --output, or --check")
	}
	if docs == "" {
		docs = os.Getenv("GLADE_SALESFORCE_DOCS_SOURCE")
	}
	report, err := surfaceledger.AuditSourceUniverse(docs)
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	markdown := surfaceledger.SourceAuditMarkdown(report)
	if output != "" {
		if err := os.WriteFile(output, []byte(markdown), 0o644); err != nil {
			return err
		}
	}
	if check != "" {
		existing, err := os.ReadFile(check)
		if err != nil {
			return err
		}
		if string(existing) != markdown {
			return fmt.Errorf("surface sources drift: run `glade-tools surface sources --docs %s --output %s`", docs, check)
		}
		if !surfaceledger.SourceAuditComplete(report) {
			return fmt.Errorf("surface sources check failed: missing source families: %d, partial source families: %d, missingLocalMarkdown: %d, unmanifestedMarkdown: %d", report.MissingRequired, report.PartialRequired, report.MissingLocalMarkdown, report.UnmanifestedMarkdown)
		}
		fmt.Fprintf(w, "%s: up to date\n", check)
	}
	fmt.Fprintln(w, "surface sources: ok")
	fmt.Fprintf(w, "atlas: pinned=%d missing=%d partial=%d\n", report.AtlasPinned, report.AtlasMissing, report.AtlasPartial)
	fmt.Fprintf(w, "nonAtlas: lwc=%s siteReferences=%s\n", report.LWCStatus, report.SiteReferencesStatus)
	fmt.Fprintf(w, "files: manifest=%s searchIndex=%s missingLocalMarkdown=%d\n", presentWord(report.ManifestPresent), presentWord(report.SearchIndexPresent), report.MissingLocalMarkdown)
	if check != "" {
		fmt.Fprintf(w, "check: %s\n", check)
	}
	return nil
}

func presentWord(ok bool) string {
	if ok {
		return "present"
	}
	return "missing"
}

func runCompatSurfaceRefresh(args []string, w io.Writer) error {
	options := surfaceledger.RefreshOptions{OutputDir: filepath.Join("docs", "generated", "salesforce")}
	jsonOut := false
	dryRun := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--docs":
			i++
			if i >= len(args) {
				return errors.New("--docs requires a value")
			}
			options.DocsSource = args[i]
		case "--tooling-completions":
			i++
			if i >= len(args) {
				return errors.New("--tooling-completions requires a value")
			}
			options.ToolingCompletions = args[i]
		case "--target-org":
			i++
			if i >= len(args) {
				return errors.New("--target-org requires a value")
			}
			options.TargetOrg = args[i]
		case "--out":
			i++
			if i >= len(args) {
				return errors.New("--out requires a value")
			}
			options.OutputDir = args[i]
		case "--release":
			i++
			if i >= len(args) {
				return errors.New("--release requires a value")
			}
			options.Release = args[i]
		case "--diff-from":
			i++
			if i >= len(args) {
				return errors.New("--diff-from requires a value")
			}
			options.DiffFrom = args[i]
		case "--oracle-evidence":
			i++
			if i >= len(args) {
				return errors.New("--oracle-evidence requires a value")
			}
			options.OracleEvidenceGlob = append(options.OracleEvidenceGlob, args[i])
		case "--source-identity":
			i++
			if i >= len(args) {
				return errors.New("--source-identity requires a value")
			}
			options.SourceIdentityPath = args[i]
		case "--json":
			jsonOut = true
		case "--dry-run":
			dryRun = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if options.DocsSource == "" {
		options.DocsSource = os.Getenv("GLADE_SALESFORCE_DOCS_SOURCE")
	}
	if options.DocsSource == "" {
		return errors.New("--docs is required unless GLADE_SALESFORCE_DOCS_SOURCE is set")
	}
	if options.ToolingCompletions == "" && options.TargetOrg == "" {
		if defaultPath := defaultSalesforceToolingCompletionsSource(); fileExists(defaultPath) {
			options.ToolingCompletions = defaultPath
		}
	}
	if dryRun {
		out, err := os.MkdirTemp("", "glade-surface-ledger-*")
		if err != nil {
			return err
		}
		options.OutputDir = out
	}
	result, err := surfaceledger.Refresh(options)
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	fmt.Fprintln(w, "surface refresh: ok")
	if dryRun {
		fmt.Fprintf(w, "dryRunOut=%s\n", result.OutputDir)
	}
	inputs := fmt.Sprintf("inputs: docs=%s org=%s glade=standard-symbols evidence=fixtures", options.DocsSource, orgInputName(options))
	if len(options.OracleEvidenceGlob) > 0 {
		inputs += fmt.Sprintf(" oracle=%s", oracleInputName(options))
	}
	fmt.Fprintln(w, inputs)
	fmt.Fprintf(w, "implemented=%d partial=%d passive=%d stubNoOp=%d explicitUnsupported=%d\n", result.Summary.Implemented, result.Summary.Partial, result.Summary.Passive, result.Summary.StubNoOp, result.Summary.ExplicitUnsupported)
	fmt.Fprintf(w, "gaps: missingShape=%d missingBehavior=%d missingEvidence=%d\n", result.Summary.Gaps[surfaceledger.GapMissingShape], result.Summary.Gaps[surfaceledger.GapMissingBehavior], result.Summary.Gaps[surfaceledger.GapMissingEvidence])
	fmt.Fprintf(w, "failures: parser=%d docsOrgMismatch=%d staleGlade=%d returnTypeMismatch=%d parameterMismatch=%d passiveServiceRisk=%d\n", result.Summary.Failures["parser"], result.Summary.Failures[surfaceledger.GapDocsOrgMismatch], result.Summary.Failures[surfaceledger.GapStaleGladeShape], result.Summary.Failures[surfaceledger.GapReturnTypeMismatch], result.Summary.Failures[surfaceledger.GapParameterMismatch], result.Summary.Failures[surfaceledger.GapPassiveServiceRisk])
	fmt.Fprintf(w, "reports: %s\n", filepath.Join(result.OutputDir, "SURFACE_DASHBOARD.md"))
	fmt.Fprintf(w, "progress: %s\n", filepath.Join(result.OutputDir, "SURFACE_PROGRESS.html"))
	return nil
}

func orgInputName(options surfaceledger.RefreshOptions) string {
	if options.TargetOrg != "" {
		return options.TargetOrg
	}
	if options.ToolingCompletions != "" {
		return filepath.Base(options.ToolingCompletions)
	}
	return "not-queried"
}

func oracleInputName(options surfaceledger.RefreshOptions) string {
	if len(options.OracleEvidenceGlob) > 0 {
		return strings.Join(options.OracleEvidenceGlob, ",")
	}
	return "not-queried"
}

func runCompatSurfaceDocs(args []string, w io.Writer) error {
	source, output, jsonOut, err := parseSourceOutputJSON(args, "--source")
	if err != nil {
		return err
	}
	rows, err := surfaceledger.BuildDocsSnapshot(source)
	if err != nil {
		return err
	}
	return writeRows(rows, output, jsonOut, w)
}

func runCompatSurfaceOrg(args []string, w io.Writer) error {
	path, output, jsonOut, err := parseSourceOutputJSON(args, "--tooling-completions")
	if err != nil {
		return err
	}
	if path == "" {
		path = defaultSalesforceToolingCompletionsSource()
	}
	rows, err := surfaceledger.BuildOrgSnapshotFromToolingCompletions(path)
	if err != nil {
		return err
	}
	return writeRows(rows, output, jsonOut, w)
}

func runCompatSurfaceGlade(args []string, w io.Writer) error {
	output := ""
	jsonOut := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--output":
			i++
			if i >= len(args) {
				return errors.New("--output requires a value")
			}
			output = args[i]
		case "--json":
			jsonOut = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return writeRows(surfaceledger.BuildGladeSnapshot(), output, jsonOut, w)
}

func runCompatSurfaceEvidence(args []string, w io.Writer) error {
	output := ""
	jsonOut := false
	var fixtures []string
	var oracleEvidence []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--output":
			i++
			if i >= len(args) {
				return errors.New("--output requires a value")
			}
			output = args[i]
		case "--oracle-evidence":
			i++
			if i >= len(args) {
				return errors.New("--oracle-evidence requires a value")
			}
			oracleEvidence = append(oracleEvidence, args[i])
		case "--json":
			jsonOut = true
		default:
			fixtures = append(fixtures, args[i])
		}
	}
	rows, err := surfaceledger.BuildEvidenceSnapshot(fixtures)
	if err != nil {
		return err
	}
	if len(oracleEvidence) > 0 {
		expanded := dedupStrings(surfaceledger.ExpandEvidencePaths(oracleEvidence))
		oracleRows, err := surfaceledger.BuildOracleEvidenceSnapshot(expanded)
		if err != nil {
			return fmt.Errorf("oracle-evidence: %w", err)
		}
		rows = append(rows, oracleRows...)
		surfaceledger.SortEvidenceRows(rows)
	}
	return writeRows(rows, output, jsonOut, w)
}

func runCompatSurfaceLedger(args []string, w io.Writer) error {
	var docsPath, orgPath, gladePath, evidencePath, output string
	var err error
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--docs":
			i++
			docsPath, err = argValue(args, i, "--docs")
			if err != nil {
				return err
			}
		case "--org":
			i++
			orgPath, err = argValue(args, i, "--org")
			if err != nil {
				return err
			}
		case "--glade":
			i++
			gladePath, err = argValue(args, i, "--glade")
			if err != nil {
				return err
			}
		case "--evidence":
			i++
			evidencePath, err = argValue(args, i, "--evidence")
			if err != nil {
				return err
			}
		case "--output":
			i++
			output, err = argValue(args, i, "--output")
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	docsRows, err := surfaceledger.ReadRowsJSON(docsPath)
	if err != nil {
		return err
	}
	orgRows, err := surfaceledger.ReadRowsJSON(orgPath)
	if err != nil {
		return err
	}
	gladeRows, err := surfaceledger.ReadRowsJSON(gladePath)
	if err != nil {
		return err
	}
	evidenceRows, err := surfaceledger.ReadRowsJSON(evidencePath)
	if err != nil {
		return err
	}
	ledger := surfaceledger.Merge(docsRows, orgRows, gladeRows, evidenceRows)
	surfaceledger.AssignPriorities(ledger.Rows)
	ledger.Summary = surfaceledger.Summarize(ledger.Rows)
	if output != "" {
		var buf stringsBuilder
		if err := surfaceledger.WriteLedgerJSON(&buf, ledger); err != nil {
			return err
		}
		return os.WriteFile(output, []byte(buf.String()), 0o644)
	}
	return surfaceledger.WriteLedgerJSON(w, ledger)
}

func runCompatSurfacePacket(args []string, w io.Writer) error {
	ledgerPath := ""
	area := ""
	output := ""
	manifestPath := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ledger":
			i++
			var err error
			ledgerPath, err = argValue(args, i, "--ledger")
			if err != nil {
				return err
			}
		case "--area":
			i++
			var err error
			area, err = argValue(args, i, "--area")
			if err != nil {
				return err
			}
		case "--manifest":
			i++
			var err error
			manifestPath, err = argValue(args, i, "--manifest")
			if err != nil {
				return err
			}
		case "--out", "--output":
			i++
			var err error
			output, err = argValue(args, i, args[i-1])
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	ledger, err := surfaceledger.ReadLedgerJSON(ledgerPath)
	if err != nil {
		return err
	}
	if manifestPath != "" {
		manifest := surfaceledger.BuildPacketManifest(ledger)
		var buf stringsBuilder
		if err := surfaceledger.WritePacketManifestJSON(&buf, manifest); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(manifestPath, []byte(buf.String()), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(w, "surface packet manifest: %s openRows=%d unassigned=%d\n", manifestPath, manifest.TotalOpenRows, len(manifest.UnassignedRows))
		return nil
	}
	packet, ok := surfaceledger.AreaPacketByName(area)
	if !ok {
		return fmt.Errorf("unknown surface area %q", area)
	}
	markdown := surfaceledger.PacketMarkdown(ledger, packet)
	if output != "" {
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(output, []byte(markdown), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(w, "surface packet: %s -> %s\n", packet.Name, output)
		return nil
	}
	fmt.Fprint(w, markdown)
	return nil
}

func runCompatSurfaceProgress(args []string, w io.Writer) error {
	ledgerPath := ""
	output := ""
	htmlOut := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ledger":
			i++
			var err error
			ledgerPath, err = argValue(args, i, "--ledger")
			if err != nil {
				return err
			}
		case "--output":
			i++
			var err error
			output, err = argValue(args, i, "--output")
			if err != nil {
				return err
			}
		case "--html":
			htmlOut = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	ledger, err := surfaceledger.ReadLedgerJSON(ledgerPath)
	if err != nil {
		return err
	}
	report := surfaceledger.ProgressMarkdown(ledger)
	if htmlOut {
		report = surfaceledger.ProgressHTML(ledger)
	}
	if output != "" {
		if err := os.WriteFile(output, []byte(report), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(w, "surface progress: %s\n", output)
		return nil
	}
	fmt.Fprint(w, report)
	return nil
}

func runCompatSurfaceGaps(args []string, w io.Writer) error {
	ledgerPath, output, err := parseLedgerOutput(args)
	if err != nil {
		return err
	}
	ledger, err := surfaceledger.ReadLedgerJSON(ledgerPath)
	if err != nil {
		return err
	}
	md := surfaceledger.GapsMarkdown(ledger)
	if output != "" {
		return os.WriteFile(output, []byte(md), 0o644)
	}
	fmt.Fprint(w, md)
	return nil
}

func runCompatSurfaceExplain(args []string, w io.Writer) error {
	ledgerPath := ""
	id := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ledger":
			i++
			var err error
			ledgerPath, err = argValue(args, i, "--ledger")
			if err != nil {
				return err
			}
		case "--id":
			i++
			var err error
			id, err = argValue(args, i, "--id")
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	ledger, err := surfaceledger.ReadLedgerJSON(ledgerPath)
	if err != nil {
		return err
	}
	md := surfaceledger.ExplainMarkdown(ledger, id)
	if md == "" {
		return fmt.Errorf("surface %q not found", id)
	}
	fmt.Fprint(w, md)
	return nil
}

func runCompatSurfaceCheck(args []string, w io.Writer) error {
	ledgerPath := ""
	options := surfaceledger.CheckOptions{
		MaxReturnTypeMismatch: -1,
		MaxParameterMismatch:  -1,
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ledger":
			i++
			var err error
			ledgerPath, err = argValue(args, i, "--ledger")
			if err != nil {
				return err
			}
		case "--max-missing-shape":
			i++
			value, err := parseIntArg(args, i, "--max-missing-shape")
			if err != nil {
				return err
			}
			options.MaxMissingShape = value
		case "--max-missing-behavior":
			i++
			value, err := parseIntArg(args, i, "--max-missing-behavior")
			if err != nil {
				return err
			}
			options.MaxMissingBehavior = value
		case "--max-parser-failures":
			i++
			value, err := parseIntArg(args, i, "--max-parser-failures")
			if err != nil {
				return err
			}
			options.MaxParserFailures = value
		case "--max-return-type-mismatch":
			i++
			value, err := parseIntArg(args, i, "--max-return-type-mismatch")
			if err != nil {
				return err
			}
			options.MaxReturnTypeMismatch = value
		case "--max-parameter-mismatch":
			i++
			value, err := parseIntArg(args, i, "--max-parameter-mismatch")
			if err != nil {
				return err
			}
			options.MaxParameterMismatch = value
		case "--strict":
			options.Strict = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	ledger, err := surfaceledger.ReadLedgerJSON(ledgerPath)
	if err != nil {
		return err
	}
	if err := surfaceledger.CheckLedger(ledger, options); err != nil {
		return err
	}
	fmt.Fprintln(w, "surface check: ok")
	return nil
}

func runCompatSurfaceStrictCurrentBase(args []string, w io.Writer) error {
	ledgerPath := ""
	output := ""
	jsonOut := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ledger":
			i++
			var err error
			ledgerPath, err = argValue(args, i, "--ledger")
			if err != nil {
				return err
			}
		case "--output":
			i++
			var err error
			output, err = argValue(args, i, "--output")
			if err != nil {
				return err
			}
		case "--json":
			jsonOut = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if ledgerPath == "" {
		return errors.New("--ledger is required")
	}
	if output != "" && jsonOut {
		return errors.New("use only one of --output or --json")
	}
	ledger, err := surfaceledger.ReadLedgerJSON(ledgerPath)
	if err != nil {
		return err
	}
	base := surfaceledger.ComputeStrictCurrentBase(ledger.Rows)
	if output != "" {
		var buf stringsBuilder
		if err := surfaceledger.WriteStrictCurrentBaseJSON(&buf, base); err != nil {
			return err
		}
		if err := atomicWriteFile(output, buf.data); err != nil {
			return err
		}
		fmt.Fprintf(w, "surface strict-current-base: %s\n", output)
		return nil
	}
	return surfaceledger.WriteStrictCurrentBaseJSON(w, base)
}

// runCompatSurfaceDeltaPreflight applies a bounded set of additions and
// removals to a previously materialized ledger. It deliberately emits only a
// compact reconciliation report; a full support profile remains an explicit
// follow-up command at the end of a family wave.
func runCompatSurfaceDeltaPreflight(args []string, w io.Writer) error {
	basePath := ""
	policyPath := ""
	output := ""
	jsonOut := false
	var additionsPaths, removalsPaths, tombstonePaths []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--base-ledger":
			i++
			value, err := argValue(args, i, "--base-ledger")
			if err != nil {
				return err
			}
			basePath = value
		case "--add", "--addition", "--additions":
			i++
			value, err := argValue(args, i, args[i-1])
			if err != nil {
				return err
			}
			additionsPaths = append(additionsPaths, value)
		case "--remove", "--removal", "--removals":
			i++
			value, err := argValue(args, i, args[i-1])
			if err != nil {
				return err
			}
			removalsPaths = append(removalsPaths, value)
		case "--tombstone", "--tombstones":
			i++
			value, err := argValue(args, i, args[i-1])
			if err != nil {
				return err
			}
			tombstonePaths = append(tombstonePaths, value)
		case "--policy":
			i++
			value, err := argValue(args, i, "--policy")
			if err != nil {
				return err
			}
			policyPath = value
		case "--output":
			i++
			value, err := argValue(args, i, "--output")
			if err != nil {
				return err
			}
			output = value
		case "--json":
			jsonOut = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if basePath == "" {
		return errors.New("--base-ledger is required")
	}
	if output != "" && jsonOut {
		return errors.New("use only one of --output or --json")
	}
	base, err := surfaceledger.ReadLedgerJSON(basePath)
	if err != nil {
		// Evidence bundles often retain a raw BASE_LEDGER_ROWS.json alongside
		// the wrapped SURFACE_LEDGER.json. Accept both forms for preflight so
		// callers do not need a no-op conversion step.
		rows, rowsErr := surfaceledger.ReadRowsJSON(basePath)
		if rowsErr != nil {
			return fmt.Errorf("read base ledger: %w", err)
		}
		base = surfaceledger.SurfaceLedger{SchemaVersion: surfaceledger.SchemaVersion, Rows: rows}
	}
	var additions []surfaceledger.SurfaceLedgerRow
	for _, path := range additionsPaths {
		rows, err := surfaceledger.ReadRowsJSON(path)
		if err != nil {
			return fmt.Errorf("read additions %s: %w", path, err)
		}
		additions = append(additions, rows...)
	}
	removals, err := readSurfaceDeltaIDs(removalsPaths, "removal")
	if err != nil {
		return err
	}
	tombstones, err := readSurfaceDeltaIDs(tombstonePaths, "tombstone")
	if err != nil {
		return err
	}
	var policy *surfaceledger.SupportPolicy
	if policyPath != "" {
		loaded, err := surfaceledger.LoadSupportPolicy(policyPath)
		if err != nil {
			return fmt.Errorf("read policy: %w", err)
		}
		policy = &loaded
	}
	_, result, err := surfaceledger.ComputeDeltaPreflight(base.Rows, additions, removals, tombstones, policy)
	if err != nil {
		return err
	}
	if output != "" {
		var buf stringsBuilder
		if err := surfaceledger.WriteDeltaPreflightJSON(&buf, result); err != nil {
			return err
		}
		if err := atomicWriteFile(output, buf.data); err != nil {
			return err
		}
		fmt.Fprintf(w, "surface delta-preflight: %s\n", output)
		return nil
	}
	return surfaceledger.WriteDeltaPreflightJSON(w, result)
}

// readSurfaceDeltaIDs accepts the compact []string form and the removal
// fixture form used by API-version tombstones ({"removals":[{"surfaceId":
// "..."}]}). Accepting both keeps the preflight command useful with existing
// evidence artifacts without introducing a conversion step.
func readSurfaceDeltaIDs(paths []string, label string) ([]string, error) {
	var ids []string
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s IDs %s: %w", label, path, err)
		}
		var stringsValue []string
		if err := json.Unmarshal(data, &stringsValue); err == nil {
			ids = append(ids, stringsValue...)
			continue
		}
		var rows []surfaceledger.SurfaceLedgerRow
		if err := json.Unmarshal(data, &rows); err == nil {
			for _, row := range rows {
				ids = append(ids, row.SurfaceID)
			}
			continue
		}
		var envelope struct {
			IDs        []string `json:"ids"`
			SurfaceIDs []string `json:"surfaceIds"`
			Removals   []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"removals"`
			Tombstones []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"tombstones"`
			SetChecks struct {
				API67NegativeTombstones struct {
					IDs []string `json:"ids"`
				} `json:"api67NegativeTombstones"`
			} `json:"setChecks"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, fmt.Errorf("parse %s IDs %s: %w", label, path, err)
		}
		ids = append(ids, envelope.IDs...)
		ids = append(ids, envelope.SurfaceIDs...)
		for _, row := range envelope.Removals {
			ids = append(ids, row.SurfaceID)
		}
		for _, row := range envelope.Tombstones {
			ids = append(ids, row.SurfaceID)
		}
		ids = append(ids, envelope.SetChecks.API67NegativeTombstones.IDs...)
		if len(envelope.IDs) == 0 && len(envelope.SurfaceIDs) == 0 && len(envelope.Removals) == 0 && len(envelope.Tombstones) == 0 && len(envelope.SetChecks.API67NegativeTombstones.IDs) == 0 {
			return nil, fmt.Errorf("parse %s IDs %s: expected an ID array or removal/tombstone envelope", label, path)
		}
	}
	return ids, nil
}

// atomicWriteFile writes data to outPath via a temp file in the same
// directory and renames it into place, mirroring the atomic write pattern
// used by the verify report writer.
func atomicWriteFile(outPath string, data []byte) error {
	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	tmpFile, err := os.CreateTemp(dir, ".strict-current-base-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return fmt.Errorf("rename temp to output: %w", err)
	}
	return nil
}

func runCompatSurfaceCorpusUsage(args []string, w io.Writer) error {
	ledgerPath := ""
	publicRoot := ""
	publicFailRoot := ""
	privateRoot := ""
	output := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ledger":
			i++
			var err error
			ledgerPath, err = argValue(args, i, "--ledger")
			if err != nil {
				return err
			}
		case "--public-root":
			i++
			var err error
			publicRoot, err = argValue(args, i, "--public-root")
			if err != nil {
				return err
			}
		case "--public-fail-root":
			i++
			var err error
			publicFailRoot, err = argValue(args, i, "--public-fail-root")
			if err != nil {
				return err
			}
		case "--private-root":
			i++
			var err error
			privateRoot, err = argValue(args, i, "--private-root")
			if err != nil {
				return err
			}
		case "--output":
			i++
			var err error
			output, err = argValue(args, i, "--output")
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if ledgerPath == "" {
		return errors.New("--ledger is required")
	}
	if output == "" {
		return errors.New("--output is required")
	}

	ledger, err := surfaceledger.ReadLedgerJSON(ledgerPath)
	if err != nil {
		return err
	}

	cu, err := surfaceledger.BuildCorpusUsage(ledger.Rows, publicRoot, publicFailRoot, privateRoot)
	if err != nil {
		return err
	}

	var buf stringsBuilder
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cu); err != nil {
		return err
	}
	if err := atomicWriteFile(output, buf.data); err != nil {
		return err
	}
	fmt.Fprintf(w, "surface corpus-usage: %s\n", output)
	return nil
}

func runCompatSurfaceSupportProfile(args []string, w io.Writer) error {
	ledgerPath := ""
	policyPath := ""
	corpusUsagePath := ""
	snapshotDir := ""
	output := ""
	htmlOutput := ""
	jsonOut := false
	markdownOut := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ledger":
			i++
			var err error
			ledgerPath, err = argValue(args, i, "--ledger")
			if err != nil {
				return err
			}
		case "--policy":
			i++
			var err error
			policyPath, err = argValue(args, i, "--policy")
			if err != nil {
				return err
			}
		case "--corpus-usage":
			i++
			var err error
			corpusUsagePath, err = argValue(args, i, "--corpus-usage")
			if err != nil {
				return err
			}
		case "--snapshot-dir":
			i++
			var err error
			snapshotDir, err = argValue(args, i, "--snapshot-dir")
			if err != nil {
				return err
			}
		case "--output":
			i++
			var err error
			output, err = argValue(args, i, "--output")
			if err != nil {
				return err
			}
		case "--html-output":
			i++
			var err error
			htmlOutput, err = argValue(args, i, "--html-output")
			if err != nil {
				return err
			}
		case "--json":
			jsonOut = true
		case "--markdown":
			markdownOut = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if ledgerPath == "" {
		return errors.New("--ledger is required")
	}
	if policyPath == "" {
		return errors.New("--policy is required")
	}
	if jsonOut && markdownOut {
		return errors.New("use only one of --json or --markdown")
	}
	if htmlOutput != "" && output == "" {
		return errors.New("--html-output requires --output")
	}
	if (output != "" || htmlOutput != "") && (jsonOut || markdownOut) {
		return errors.New("use only one of --output/--html-output or --json/--markdown")
	}
	if output != "" && htmlOutput != "" && filepath.Clean(output) == filepath.Clean(htmlOutput) {
		return errors.New("--output and --html-output must be different paths")
	}

	ledgerBytes, err := os.ReadFile(ledgerPath)
	if err != nil {
		return err
	}
	ledger, err := surfaceledger.ParseLedgerJSON(ledgerBytes)
	if err != nil {
		return err
	}
	policyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		return err
	}
	policy, err := surfaceledger.ParseSupportPolicyJSON(policyBytes)
	if err != nil {
		return err
	}
	profileInputBytes := map[string][]byte{"ledger": ledgerBytes, "policy": policyBytes}

	var cu *surfaceledger.CorpusUsage
	if corpusUsagePath != "" {
		data, err := os.ReadFile(corpusUsagePath)
		if err != nil {
			return fmt.Errorf("read corpus-usage: %w", err)
		}
		var parsed surfaceledger.CorpusUsage
		if err := json.Unmarshal(data, &parsed); err != nil {
			return fmt.Errorf("parse corpus-usage: %w", err)
		}
		cu = &parsed
		profileInputBytes["corpus-usage"] = data
	}

	profile := surfaceledger.ComputeSupportProfile(ledger.Rows, policy, cu)
	inputs, err := buildSupportProfileInputs(ledgerPath, policyPath, corpusUsagePath, snapshotDir, profileInputBytes)
	if err != nil {
		return err
	}
	profile.Inputs = inputs

	// When file outputs are used, materialize both reports from the one
	// computed profile and write each atomically even when
	// validation errors exist. Then exit nonzero so the evidence is
	// preserved for repair.
	if output != "" {
		var jsonBuf stringsBuilder
		if err := surfaceledger.WriteSupportProfileJSON(&jsonBuf, profile); err != nil {
			return err
		}
		var htmlBuf stringsBuilder
		if htmlOutput != "" {
			if err := surfaceledger.WriteSupportProfileHTML(&htmlBuf, profile, ledger); err != nil {
				return err
			}
		}
		if err := verifySupportProfileInputsBeforeWrite(inputs); err != nil {
			return err
		}
		if err := atomicWriteFile(output, jsonBuf.data); err != nil {
			return err
		}
		fmt.Fprintf(w, "surface support-profile: %s\n", output)
		if htmlOutput != "" {
			if err := verifySupportProfileInputsBeforeWrite(inputs); err != nil {
				return err
			}
			if err := atomicWriteFile(htmlOutput, htmlBuf.data); err != nil {
				return err
			}
			fmt.Fprintf(w, "surface support-profile html: %s\n", htmlOutput)
		}
		if len(profile.ValidationErrors) > 0 {
			return fmt.Errorf("support profile validation failed with %d error(s):\n%s",
				len(profile.ValidationErrors),
				stringsBuilderFromErrors(profile.ValidationErrors))
		}
		return nil
	}

	// Fail if there are validation errors on any output path.
	if len(profile.ValidationErrors) > 0 {
		return fmt.Errorf("support profile validation failed with %d error(s):\n%s",
			len(profile.ValidationErrors),
			stringsBuilderFromErrors(profile.ValidationErrors))
	}

	if markdownOut {
		return surfaceledger.WriteSupportProfileMarkdown(w, profile)
	}

	// Default to JSON output.
	if err := surfaceledger.WriteSupportProfileJSON(w, profile); err != nil {
		return err
	}
	return nil
}

func buildSupportProfileInputs(ledgerPath, policyPath, corpusUsagePath, snapshotDir string, inputBytes map[string][]byte) (*surfaceledger.SupportProfileInputs, error) {
	requested := []struct {
		name string
		path string
	}{
		{name: "ledger", path: ledgerPath},
		{name: "policy", path: policyPath},
	}
	if corpusUsagePath != "" {
		requested = append(requested, struct {
			name string
			path string
		}{name: "corpus-usage", path: corpusUsagePath})
	}
	if snapshotDir != "" {
		for _, name := range []string{"DOCS_SNAPSHOT.json", "ORG_SNAPSHOT.json", "GLADE_SNAPSHOT.json", "EVIDENCE_SNAPSHOT.json"} {
			requested = append(requested, struct {
				name string
				path string
			}{name: name, path: filepath.Join(snapshotDir, name)})
		}
	}

	inputs := &surfaceledger.SupportProfileInputs{Files: make([]surfaceledger.SupportProfileInput, 0, len(requested))}
	for _, input := range requested {
		path, err := filepath.Abs(input.path)
		if err != nil {
			return nil, fmt.Errorf("support-profile input %s: %w", input.name, err)
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return nil, fmt.Errorf("support-profile input %s: %w", input.name, err)
		}
		data, provided := inputBytes[input.name]
		if !provided {
			data, err = os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("support-profile input %s: %w", input.name, err)
			}
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(data))
		inputs.Files = append(inputs.Files, surfaceledger.SupportProfileInput{Name: input.name, Path: path, SHA256: digest})
	}
	return inputs, nil
}

func verifySupportProfileInputsBeforeWrite(inputs *surfaceledger.SupportProfileInputs) error {
	if inputs == nil {
		return fmt.Errorf("support-profile inputs are required before artifact write")
	}
	for _, input := range inputs.Files {
		canonical, err := filepath.EvalSymlinks(input.Path)
		if err != nil {
			return fmt.Errorf("support-profile input %s changed before artifact write: %w", input.Name, err)
		}
		if filepath.Clean(canonical) != filepath.Clean(input.Path) {
			return fmt.Errorf("support-profile input %s changed before artifact write: path identity changed", input.Name)
		}
		data, err := os.ReadFile(input.Path)
		if err != nil {
			return fmt.Errorf("support-profile input %s changed before artifact write: %w", input.Name, err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != input.SHA256 {
			return fmt.Errorf("support-profile input %s changed before artifact write: digest mismatch", input.Name)
		}
	}
	return nil
}

func stringsBuilderFromErrors(errors []string) string {
	var buf stringsBuilder
	for _, e := range errors {
		buf.data = append(buf.data, "  - "...)
		buf.data = append(buf.data, e...)
		buf.data = append(buf.data, '\n')
	}
	return buf.String()
}

func parseSourceOutputJSON(args []string, sourceFlag string) (string, string, bool, error) {
	source := ""
	output := ""
	jsonOut := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case sourceFlag:
			i++
			if i >= len(args) {
				return "", "", false, fmt.Errorf("%s requires a value", sourceFlag)
			}
			source = args[i]
		case "--output":
			i++
			if i >= len(args) {
				return "", "", false, errors.New("--output requires a value")
			}
			output = args[i]
		case "--json":
			jsonOut = true
		default:
			return "", "", false, fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return source, output, jsonOut, nil
}

func parseLedgerOutput(args []string) (string, string, error) {
	ledger := ""
	output := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ledger":
			i++
			var err error
			ledger, err = argValue(args, i, "--ledger")
			if err != nil {
				return "", "", err
			}
		case "--output":
			i++
			var err error
			output, err = argValue(args, i, "--output")
			if err != nil {
				return "", "", err
			}
		default:
			return "", "", fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return ledger, output, nil
}

func writeRows(rows []surfaceledger.SurfaceLedgerRow, output string, jsonOut bool, w io.Writer) error {
	if output != "" {
		var buf stringsBuilder
		if err := surfaceledger.WriteRowsJSON(&buf, rows); err != nil {
			return err
		}
		return os.WriteFile(output, []byte(buf.String()), 0o644)
	}
	if jsonOut {
		return surfaceledger.WriteRowsJSON(w, rows)
	}
	fmt.Fprintf(w, "rows=%d\n", len(rows))
	return nil
}

func argValue(args []string, i int, flag string) (string, error) {
	if i >= len(args) {
		return "", fmt.Errorf("%s requires a value", flag)
	}
	return args[i], nil
}

func dedupStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func parseIntArg(args []string, i int, flag string) (int, error) {
	value, err := argValue(args, i, flag)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s requires an integer", flag)
	}
	return parsed, nil
}

type stringsBuilder struct {
	data []byte
}

func (b *stringsBuilder) Write(data []byte) (int, error) {
	b.data = append(b.data, data...)
	return len(data), nil
}

func (b *stringsBuilder) String() string {
	return string(b.data)
}
