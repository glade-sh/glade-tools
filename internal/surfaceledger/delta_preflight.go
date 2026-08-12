package surfaceledger

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// DeltaPreflightResult is the compact, deterministic result of applying a
// bounded evidence delta to an existing merged ledger. It intentionally does
// not include the resulting rows; callers that need the full ledger can keep
// the rows returned by ComputeDeltaPreflight and write them with
// WriteLedgerJSON.
type DeltaPreflightResult struct {
	BaseRows       int           `json:"baseRows"`
	ResultRows     int           `json:"resultRows"`
	AddedIDs       []string      `json:"addedIds"`
	RemovedIDs     []string      `json:"removedIds"`
	ChangedIDs     []string      `json:"changedIds"`
	TombstoneIDs   []string      `json:"tombstoneIds"`
	NonDeferredIDs []string      `json:"nonDeferredIds"`
	Summary        LedgerSummary `json:"summary"`
}

// ComputeDeltaPreflight applies additions and removals to a base ledger using
// the same Merge and Classify paths as a full surface refresh. Tombstones are
// removals with separate provenance in the compact result; they always win
// over additions for the same canonical surface ID.
//
// If policy is non-nil, NonDeferredIDs contains the exact IDs in the resulting
// support profile's non-deferred gap set. A nil policy leaves that set empty,
// allowing callers to use this helper for fast ledger-only reconciliation.
func ComputeDeltaPreflight(base, additions []SurfaceLedgerRow, removals, tombstones []string, policy *SupportPolicy) (SurfaceLedger, DeltaPreflightResult, error) {
	removalKeys, err := preflightIDs(removals, "removal")
	if err != nil {
		return SurfaceLedger{}, DeltaPreflightResult{}, err
	}
	tombstoneKeys, err := preflightIDs(tombstones, "tombstone")
	if err != nil {
		return SurfaceLedger{}, DeltaPreflightResult{}, err
	}
	for key := range tombstoneKeys {
		removalKeys[key] = tombstoneKeys[key]
	}

	baseByKey := rowsByCanonicalID(base)
	mergedInput := make([]SurfaceLedgerRow, 0, len(base)+len(additions))
	mergedInput = append(mergedInput, base...)
	mergedInput = append(mergedInput, additions...)
	merged := Merge(nil, nil, nil, mergedInput)

	rows := make([]SurfaceLedgerRow, 0, len(merged.Rows))
	for _, row := range merged.Rows {
		if _, remove := removalKeys[surfaceIDKey(row.SurfaceID)]; remove {
			continue
		}
		Classify(&row)
		rows = append(rows, row)
	}
	// Merge returns canonical sorted rows; filtering preserves that order. The
	// explicit summary recomputation keeps this helper correct if that ordering
	// or merge implementation changes later.
	ledger := SurfaceLedger{SchemaVersion: SchemaVersion, Rows: rows}
	ledger.Summary = Summarize(rows)

	resultByKey := rowsByCanonicalID(rows)
	result := DeltaPreflightResult{
		BaseRows:     len(baseByKey),
		ResultRows:   len(resultByKey),
		AddedIDs:     preflightDiffIDs(resultByKey, baseByKey),
		RemovedIDs:   preflightDiffIDs(baseByKey, resultByKey),
		ChangedIDs:   preflightChangedIDs(baseByKey, resultByKey),
		TombstoneIDs: preflightIDValues(tombstoneKeys),
		Summary:      ledger.Summary,
	}
	if policy != nil {
		profile := ComputeSupportProfile(rows, *policy, nil)
		if len(profile.ValidationErrors) > 0 {
			return ledger, result, fmt.Errorf("delta preflight support policy validation failed with %d error(s): %s", len(profile.ValidationErrors), strings.Join(profile.ValidationErrors, "; "))
		}
		result.NonDeferredIDs = make([]string, 0, len(profile.NonDeferredGaps))
		for _, row := range profile.NonDeferredGaps {
			result.NonDeferredIDs = append(result.NonDeferredIDs, row.SurfaceID)
		}
		sort.Slice(result.NonDeferredIDs, func(i, j int) bool {
			return surfaceIDKey(result.NonDeferredIDs[i]) < surfaceIDKey(result.NonDeferredIDs[j])
		})
	}
	return ledger, result, nil
}

// WriteDeltaPreflightJSON writes the compact result in the same deterministic
// pretty-JSON format as the other surface-ledger reports.
func WriteDeltaPreflightJSON(w io.Writer, result DeltaPreflightResult) error {
	data, err := marshalPretty(result)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func rowsByCanonicalID(rows []SurfaceLedgerRow) map[string]SurfaceLedgerRow {
	byKey := make(map[string]SurfaceLedgerRow, len(rows))
	for _, row := range rows {
		if key := surfaceIDKey(row.SurfaceID); key != "" {
			byKey[key] = row
		}
	}
	return byKey
}

func preflightIDs(ids []string, label string) (map[string]string, error) {
	result := make(map[string]string, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, fmt.Errorf("empty %s ID", label)
		}
		key := surfaceIDKey(id)
		if key == "" {
			return nil, fmt.Errorf("invalid %s ID %q", label, raw)
		}
		if _, exists := result[key]; !exists {
			result[key] = id
		}
	}
	return result, nil
}

func preflightIDValues(byKey map[string]string) []string {
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, byKey[key])
	}
	return ids
}

// preflightDiffIDs reports IDs present in left but not right.
func preflightDiffIDs(left, right map[string]SurfaceLedgerRow) []string {
	keys := make([]string, 0)
	for key := range left {
		if _, exists := right[key]; !exists {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, left[key].SurfaceID)
	}
	return ids
}

func preflightChangedIDs(base, result map[string]SurfaceLedgerRow) []string {
	keys := make([]string, 0)
	for key, baseRow := range base {
		resultRow, exists := result[key]
		if exists && !contractEqual(baseRow, resultRow) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, result[key].SurfaceID)
	}
	return ids
}
