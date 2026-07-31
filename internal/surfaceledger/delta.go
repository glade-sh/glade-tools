package surfaceledger

import (
	"fmt"
	"sort"
)

// DeltaKind classifies a row's movement between two ledger snapshots.
type DeltaKind string

const (
	DeltaAdded     DeltaKind = "added"
	DeltaRemoved   DeltaKind = "removed"
	DeltaChanged   DeltaKind = "changed"
	DeltaUnchanged DeltaKind = "unchanged"
)

// DeltaEntry describes one row's delta with its old and new state.
type DeltaEntry struct {
	SurfaceID string
	Kind      DeltaKind
	Old       *SurfaceLedgerRow // nil for added
	New       *SurfaceLedgerRow // nil for removed
}

// ReleaseScope is the investigation tier for a surface.
type ReleaseScope string

const (
	ScopeT0           ReleaseScope = "t0"
	ScopeT1           ReleaseScope = "t1"
	ScopeT2           ReleaseScope = "t2"
	ScopeOutsideClaim ReleaseScope = "outside-claim"
)

// ReleaseDisposition is the required action for a surface.
type ReleaseDisposition string

const (
	DispoExistingCase        ReleaseDisposition = "existing-case"
	DispoNewCase             ReleaseDisposition = "new-case"
	DispoDeterministicMock   ReleaseDisposition = "deterministic-mock"
	DispoExplicitUnsupported ReleaseDisposition = "explicit-unsupported"
)

// ReleaseClassification records an explicit decision for one surface.
type ReleaseClassification struct {
	SurfaceID   string
	Scope       ReleaseScope
	Disposition ReleaseDisposition
	CaseID      string // required for existing-case, new-case
	ReasonRef   string // required for deterministic-mock, explicit-unsupported
}

// ComputeReleaseDelta produces added, removed, changed, and unchanged lists
// by joining previous and current ledger rows on their canonical SurfaceID.
// It fails on duplicate IDs, missing required classifications, stale
// classifications, and invalid scope or disposition values.
func ComputeReleaseDelta(prev, current []SurfaceLedgerRow, classifications []ReleaseClassification) (
	added, removed, changed, unchanged []DeltaEntry, err error,
) {
	// Build prev index; fail on empty IDs or duplicate canonical keys.
	prevByKey := make(map[string]SurfaceLedgerRow, len(prev))
	for _, row := range prev {
		key := surfaceIDKey(row.SurfaceID)
		if key == "" {
			return nil, nil, nil, nil, fmt.Errorf("empty canonical SurfaceID in prev: %q", row.SurfaceID)
		}
		if _, exists := prevByKey[key]; exists {
			return nil, nil, nil, nil, fmt.Errorf("duplicate canonical SurfaceID in prev: %s", key)
		}
		prevByKey[key] = row
	}

	// Build current index; fail on empty IDs or duplicate canonical keys.
	currByKey := make(map[string]SurfaceLedgerRow, len(current))
	for _, row := range current {
		key := surfaceIDKey(row.SurfaceID)
		if key == "" {
			return nil, nil, nil, nil, fmt.Errorf("empty canonical SurfaceID in current: %q", row.SurfaceID)
		}
		if _, exists := currByKey[key]; exists {
			return nil, nil, nil, nil, fmt.Errorf("duplicate canonical SurfaceID in current: %s", key)
		}
		currByKey[key] = row
	}

	// Index classifications by canonical key; fail on empty IDs or duplicates.
	classByKey := make(map[string]ReleaseClassification, len(classifications))
	for _, c := range classifications {
		key := surfaceIDKey(c.SurfaceID)
		if key == "" {
			return nil, nil, nil, nil, fmt.Errorf("empty canonical SurfaceID in classification: %q", c.SurfaceID)
		}
		if _, exists := classByKey[key]; exists {
			return nil, nil, nil, nil, fmt.Errorf("duplicate classification for: %s", key)
		}
		classByKey[key] = c
	}

	// Classify each canonical key into one of the four lists.
	var addedKeys, removedKeys, changedKeys, unchangedKeys []string

	for key, currRow := range currByKey {
		prevRow, inPrev := prevByKey[key]
		if inPrev {
			if contractEqual(prevRow, currRow) {
				unchangedKeys = append(unchangedKeys, key)
			} else {
				changedKeys = append(changedKeys, key)
			}
		} else {
			addedKeys = append(addedKeys, key)
		}
	}

	for key := range prevByKey {
		if _, inCurr := currByKey[key]; !inCurr {
			removedKeys = append(removedKeys, key)
		}
	}

	// Deterministic sort on canonical SurfaceID.
	sort.Strings(addedKeys)
	sort.Strings(removedKeys)
	sort.Strings(changedKeys)
	sort.Strings(unchangedKeys)

	// Build result slices.
	added = buildEntries(addedKeys, nil, currByKey, DeltaAdded)
	removed = buildEntries(removedKeys, prevByKey, nil, DeltaRemoved)
	changed = buildEntries(changedKeys, prevByKey, currByKey, DeltaChanged)
	unchanged = buildEntries(unchangedKeys, prevByKey, currByKey, DeltaUnchanged)

	// Validate: every added/changed row must have a valid classification.
	for _, key := range addedKeys {
		c, ok := classByKey[key]
		if !ok {
			return nil, nil, nil, nil, fmt.Errorf("missing classification for added row: %s", key)
		}
		if err := validateClassification(c); err != nil {
			return nil, nil, nil, nil, err
		}
	}
	for _, key := range changedKeys {
		c, ok := classByKey[key]
		if !ok {
			return nil, nil, nil, nil, fmt.Errorf("missing classification for changed row: %s", key)
		}
		if err := validateClassification(c); err != nil {
			return nil, nil, nil, nil, err
		}
	}

	// Validate: no stale classifications (must match added or changed row).
	for key := range classByKey {
		if !stringSliceContains(addedKeys, key) && !stringSliceContains(changedKeys, key) {
			return nil, nil, nil, nil, fmt.Errorf("classification for row not in added or changed: %s", key)
		}
	}

	return added, removed, changed, unchanged, nil
}

// contractEqual compares two rows on their stable, contract-bearing fields.
// Display/order noise fields are ignored.
func contractEqual(a, b SurfaceLedgerRow) bool {
	return a.Product == b.Product &&
		a.Area == b.Area &&
		a.Namespace == b.Namespace &&
		a.TypeName == b.TypeName &&
		a.MemberName == b.MemberName &&
		a.Resource == b.Resource &&
		a.FieldName == b.FieldName &&
		a.Kind == b.Kind &&
		a.Signature == b.Signature &&
		a.ReturnType == b.ReturnType &&
		stringSlicesEqual(a.Parameters, b.Parameters) &&
		a.Docs == b.Docs &&
		a.Org == b.Org &&
		a.DocsReturnType == b.DocsReturnType &&
		a.OrgReturnType == b.OrgReturnType &&
		a.GladeReturnType == b.GladeReturnType &&
		stringSlicesEqual(a.DocsParameters, b.DocsParameters) &&
		stringSlicesEqual(a.OrgParameters, b.OrgParameters) &&
		stringSlicesEqual(a.GladeParameters, b.GladeParameters) &&
		a.GladeShape == b.GladeShape &&
		a.GladeBehavior == b.GladeBehavior &&
		a.Evidence == b.Evidence
}

// validateClassification checks scope, disposition, and required fields.
func validateClassification(c ReleaseClassification) error {
	switch c.Scope {
	case ScopeT0, ScopeT1, ScopeT2, ScopeOutsideClaim:
		// valid
	default:
		return fmt.Errorf("unknown release scope %q for %s", c.Scope, c.SurfaceID)
	}

	switch c.Disposition {
	case DispoExistingCase, DispoNewCase:
		if c.CaseID == "" {
			return fmt.Errorf("%s requires non-empty case ID for %s", c.Disposition, c.SurfaceID)
		}
	case DispoDeterministicMock, DispoExplicitUnsupported:
		if c.ReasonRef == "" {
			return fmt.Errorf("%s requires non-empty reason/reference for %s", c.Disposition, c.SurfaceID)
		}
	default:
		return fmt.Errorf("unknown release disposition %q for %s", c.Disposition, c.SurfaceID)
	}

	return nil
}

// buildEntries creates DeltaEntry slices from canonical keys and row maps.
func buildEntries(keys []string, oldByKey, newByKey map[string]SurfaceLedgerRow, kind DeltaKind) []DeltaEntry {
	entries := make([]DeltaEntry, len(keys))
	for i, key := range keys {
		e := DeltaEntry{Kind: kind}
		if oldByKey != nil {
			row := oldByKey[key]
			e.Old = copyRowPtr(row)
		}
		if newByKey != nil {
			row := newByKey[key]
			e.SurfaceID = row.SurfaceID
			e.New = copyRowPtr(row)
		} else if oldByKey != nil {
			// Only old (removed): SurfaceID from old row.
			e.SurfaceID = oldByKey[key].SurfaceID
		}
		entries[i] = e
	}
	return entries
}

func copyRowPtr(row SurfaceLedgerRow) *SurfaceLedgerRow {
	r := row
	return &r
}

func stringSliceContains(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
