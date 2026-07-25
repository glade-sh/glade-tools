package apexrules

import "sort"

// Compare evaluates accept/reject parity only. Compiler wording is deliberately
// excluded because the catalog tracks the behavioral contract, not diagnostics.
func Compare(rules []Rule, glade map[string]Outcome) []Result {
	results := make([]Result, 0, len(rules))
	for _, rule := range rules {
		outcome := glade[rule.ID]
		results = append(results, Result{ID: rule.ID, Oracle: rule.Oracle, Glade: outcome, Matched: rule.Oracle == outcome, Status: rule.Status})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results
}
