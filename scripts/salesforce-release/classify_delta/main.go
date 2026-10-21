// classify_delta exports exact surface deltas and applies an explicit per-item
// review. It never infers product support from a namespace or a documentation tag.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
	"github.com/glade-sh/glade/tools/internal/releasecontract"
	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

type decision struct {
	PreserveAvailability bool                             `json:"preserveAvailability,omitempty"`
	SurfaceID            string                           `json:"surfaceId"`
	Scope                surfaceledger.ReleaseScope       `json:"scope"`
	Disposition          surfaceledger.ReleaseDisposition `json:"disposition"`
	Declaration          string                           `json:"declaration"`
	SourceURL            string                           `json:"sourceURL"`
	ProductEvidence      string                           `json:"productEvidence"`
	Reason               string                           `json:"reason"`
	ProductTests         []string                         `json:"productTests"`
}

type review struct {
	SchemaVersion   int        `json:"schemaVersion"`
	PreviousRelease string     `json:"previousRelease"`
	CurrentRelease  string     `json:"currentRelease"`
	Decisions       []decision `json:"decisions"`
}

func classify(changes []releasecontract.SurfaceChange, decisions []decision) ([]surfaceledger.ReleaseClassification, error) {
	expected := map[string]bool{}
	for _, change := range changes {
		expected[surfaceledger.CanonicalSurfaceIDKey(change.Entry.SurfaceID)] = true
	}
	seen := map[string]bool{}
	result := make([]surfaceledger.ReleaseClassification, 0, len(changes))
	for _, d := range decisions {
		key := surfaceledger.CanonicalSurfaceIDKey(d.SurfaceID)
		if !expected[key] {
			return nil, fmt.Errorf("stale review %s", d.SurfaceID)
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate review %s", d.SurfaceID)
		}
		seen[key] = true
		u, err := url.Parse(d.SourceURL)
		if err != nil || u.Scheme != "https" || (u.Host != "developer.salesforce.com" && u.Host != "help.salesforce.com" && u.Host != "resources.docs.salesforce.com") || u.Path == "" {
			return nil, fmt.Errorf("review %s requires a public Salesforce source URL", d.SurfaceID)
		}
		for _, value := range []string{d.Declaration, d.ProductEvidence, d.Reason} {
			if strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("review %s requires declaration, product evidence and decision reason", d.SurfaceID)
			}
		}
		if len(d.ProductTests) == 0 {
			return nil, fmt.Errorf("review %s requires product test bindings", d.SurfaceID)
		}
		c := surfaceledger.ReleaseClassification{PreserveAvailability: d.PreserveAvailability, SurfaceID: d.SurfaceID, Scope: d.Scope, Disposition: d.Disposition, ProductTests: d.ProductTests, ReasonRef: "Declaration: " + d.Declaration + " (" + d.SourceURL + "). Product evidence: " + d.ProductEvidence + ". Decision: " + d.Reason}
		if err := surfaceledger.ValidateReleaseClassification(c); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	for _, change := range changes {
		if !seen[surfaceledger.CanonicalSurfaceIDKey(change.Entry.SurfaceID)] {
			return nil, fmt.Errorf("missing review %s", change.Entry.SurfaceID)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SurfaceID < result[j].SurfaceID })
	return result, nil
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func write(path string, value any) {
	b, err := json.MarshalIndent(value, "", "  ")
	must(err)
	must(os.WriteFile(path, append(b, '\n'), 0644))
}
func main() {
	contract := flag.String("contract", "", "Analyzed release contract (applies checked surface corrections)")
	release := flag.String("release", "", "Current release name")
	previous := flag.String("previous-inventory", "", "Previous inventory for standalone review")
	current := flag.String("current-inventory", "", "Current inventory for standalone review")
	previousVersion := flag.String("previous-api", "", "Previous snapshot API")
	currentVersion := flag.String("current-api", "", "Current snapshot API")
	previousName := flag.String("previous-release", "", "Previous release name for standalone review")
	reviewPath := flag.String("review", "", "Explicit reviewed decisions; omit only when exporting the delta")
	deltaOutput := flag.String("delta-output", "", "Export exact delta rows for review")
	output := flag.String("output", "", "Generated schema-2 classification file")
	policyReceipt := flag.String("namespace-policy-receipt", "", "Acquisition policy receipt for excluded ConnectApi references")
	policyOutput := flag.String("namespace-policy-output", "", "Enumerate all known ConnectApi declarations as unsupported, separately from unavailable references")
	flag.Parse()
	if *policyOutput != "" || *policyReceipt != "" {
		if *current == "" || *policyOutput == "" || *policyReceipt == "" || *contract != "" || *output != "" || *deltaOutput != "" || *reviewPath != "" {
			must(fmt.Errorf("namespace policy export requires only --current-inventory, --namespace-policy-receipt and --namespace-policy-output"))
		}
		must(exportNamespacePolicy(*current, *policyReceipt, *policyOutput, *previous, *previousVersion))
		return
	}
	var changes []releasecontract.SurfaceChange
	prevName := *previousName
	if *contract != "" {
		analysis, err := releasecontract.Analyze(*contract)
		must(err)
		for _, change := range analysis.SurfaceChanges {
			if change.CurrentRelease == *release {
				changes = append(changes, change)
				prevName = change.PreviousRelease
			}
		}
	} else {
		a, err := apexdocs.ReadInventory(*previous)
		must(err)
		b, err := apexdocs.ReadInventory(*current)
		must(err)
		ar, err := surfaceledger.MergeReleaseSnapshot(surfaceledger.ReleaseRowsFromDocsInventory(a), *previousVersion)
		must(err)
		br, err := surfaceledger.MergeReleaseSnapshot(surfaceledger.ReleaseRowsFromDocsInventory(b), *currentVersion)
		must(err)
		added, removed, changed, _, err := surfaceledger.DiffReleaseRows(ar.Rows, br.Rows)
		must(err)
		for _, entry := range append(append(added, removed...), changed...) {
			changes = append(changes, releasecontract.SurfaceChange{PreviousRelease: prevName, CurrentRelease: *release, Entry: entry})
		}
	}
	if *release == "" || prevName == "" {
		must(fmt.Errorf("both release names are required"))
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Entry.SurfaceID < changes[j].Entry.SurfaceID })
	if *deltaOutput != "" {
		write(*deltaOutput, changes)
	}
	if *reviewPath == "" {
		if *output != "" || *deltaOutput == "" {
			must(fmt.Errorf("classification output requires --review; use --delta-output for unreviewed rows"))
		}
		return
	}
	f, err := os.Open(*reviewPath)
	must(err)
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	var input review
	must(decoder.Decode(&input))
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		must(fmt.Errorf("review must contain one JSON object"))
	}
	if input.SchemaVersion != 1 || input.PreviousRelease != prevName || input.CurrentRelease != *release {
		must(fmt.Errorf("review schema or release identity mismatch"))
	}
	classifications, err := classify(changes, input.Decisions)
	must(err)
	if *output == "" {
		must(fmt.Errorf("--output is required with --review"))
	}
	write(*output, releasecontract.ClassificationFile{SchemaVersion: 2, PreviousRelease: prevName, CurrentRelease: *release, Classifications: classifications})
}
