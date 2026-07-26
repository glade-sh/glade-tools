package apexrules

import "sort"

// Compare evaluates accept/reject parity only. Compiler wording is deliberately
// excluded because the catalog tracks the behavioral contract, not diagnostics.
func Compare(rules []Rule, glade map[string]Outcome) []Result {
	results := make([]Result, 0, len(rules))
	for _, rule := range rules {
		outcome := glade[rule.ID]
		results = append(results, Result{ID: rule.ID, Oracle: rule.Oracle, CatalogOracle: rule.Oracle, OracleMatched: true, Glade: outcome, Matched: rule.Oracle == outcome, Status: rule.Status})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results
}

// CompareObserved keeps the checked catalog expectation, live Salesforce
// result, and Glade result distinct. Live Salesforce remains the parity oracle,
// while OracleMatched reports whether the checked evidence has drifted.
func CompareObserved(rules []Rule, salesforce map[string]SalesforceResult, glade map[string]Outcome) []Result {
	results := make([]Result, 0, len(rules))
	for _, rule := range rules {
		observed := salesforce[rule.ID]
		outcome := glade[rule.ID]
		results = append(results, Result{
			ID:            rule.ID,
			Oracle:        observed.Outcome,
			CatalogOracle: rule.Oracle,
			Salesforce:    observed.Outcome,
			OracleMatched: rule.Oracle == observed.Outcome,
			Glade:         outcome,
			Matched:       observed.Outcome == outcome,
			Status:        rule.Status,
			Problems:      observed.Problems,
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results
}
