package toolcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/glade-sh/glade/tools/internal/apexrules"
)

func runApexRules(ctx context.Context, args []string, w io.Writer) error {
	if len(args) == 3 && args[0] == "validate" && args[1] == "--catalog" {
		if _, err := apexrules.LoadCatalog(args[2]); err != nil {
			return err
		}
		fmt.Fprintln(w, "ok")
		return nil
	}
	if len(args) != 8 || args[0] != "compare" || args[1] != "--catalog" || args[3] != "--target-org" || args[5] != "--glade-bin" || args[7] != "--json" {
		return fmt.Errorf("usage: glade-tools apex-rules compare --catalog <path> --target-org <alias> --glade-bin <path> --json")
	}
	catalog, err := apexrules.LoadCatalog(args[2])
	if err != nil {
		return err
	}
	salesforce, err := apexrules.RunSalesforce(ctx, args[4], catalog.Rules)
	if err != nil {
		return err
	}
	glade, err := apexrules.RunGlade(ctx, args[6], catalog.Rules)
	if err != nil {
		return err
	}
	results := apexrules.CompareObserved(catalog.Rules, salesforce, glade)
	report := struct {
		Results             []apexrules.Result `json:"results"`
		SupportedMismatches int                `json:"supportedMismatches"`
		OracleDrifts        int                `json:"oracleDrifts"`
	}{Results: results}
	for _, result := range results {
		if result.Status == apexrules.StatusSupported && !result.Matched {
			report.SupportedMismatches++
		}
		if !result.OracleMatched {
			report.OracleDrifts++
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode comparison report: %w", err)
	}
	fmt.Fprintln(w, string(encoded))
	if report.SupportedMismatches != 0 {
		return fmt.Errorf("%d supported Apex rule mismatches", report.SupportedMismatches)
	}
	if report.OracleDrifts != 0 {
		return fmt.Errorf("%d Apex rule catalog oracle drifts", report.OracleDrifts)
	}
	return nil
}
