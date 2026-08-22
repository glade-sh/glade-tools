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
	SurfaceID    string             `json:"surfaceId"`
	Scope        ReleaseScope       `json:"scope"`
	Disposition  ReleaseDisposition `json:"disposition"`
	CaseID       string             `json:"caseId,omitempty"`    // existing-case/new-case require this or ProductTests
	ReasonRef    string             `json:"reasonRef,omitempty"` // required for deterministic-mock, explicit-unsupported
	ProductTests []string           `json:"productTests,omitempty"`
}

// ComputeReleaseDelta produces added, removed, changed, and unchanged lists
// by joining previous and current ledger rows on their canonical SurfaceID.
// It fails on duplicate IDs, missing required classifications, stale
// classifications, and invalid scope or disposition values.
func ComputeReleaseDelta(prev, current []SurfaceLedgerRow, classifications []ReleaseClassification) (
	added, removed, changed, unchanged []DeltaEntry, err error,
) {
	added, removed, changed, unchanged, err = DiffReleaseRows(prev, current)
	if err != nil {
		return nil, nil, nil, nil, err
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

	// Validate every added, removed, and changed row against its classification.
	classifiedKeys := make(map[string]struct{}, len(added)+len(removed)+len(changed))
	for _, group := range []struct {
		entries []DeltaEntry
		name    string
	}{
		{entries: added, name: "added"},
		{entries: removed, name: "removed"},
		{entries: changed, name: "changed"},
	} {
		for _, entry := range group.entries {
			key := surfaceIDKey(entry.SurfaceID)
			c, ok := classByKey[key]
			if !ok {
				return added, removed, changed, unchanged, fmt.Errorf("missing classification for %s row: %s", group.name, key)
			}
			if err := ValidateReleaseClassification(c); err != nil {
				return added, removed, changed, unchanged, err
			}
			classifiedKeys[key] = struct{}{}
		}
	}

	var staleKeys []string
	for key := range classByKey {
		if _, ok := classifiedKeys[key]; !ok {
			staleKeys = append(staleKeys, key)
		}
	}
	if len(staleKeys) > 0 {
		sort.Strings(staleKeys)
		return added, removed, changed, unchanged, fmt.Errorf("classification for row not in added, removed, or changed: %s", staleKeys[0])
	}

	return added, removed, changed, unchanged, nil
}

// DiffReleaseRows joins previous and current ledger rows on canonical
// SurfaceID and returns all four deterministic delta lists.
func DiffReleaseRows(prev, current []SurfaceLedgerRow) (
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
	return added, removed, changed, unchanged, nil
}

// contractEqual compares two rows on their stable, contract-bearing fields.
// Display/order noise fields are ignored.
func contractEqual(a, b SurfaceLedgerRow) bool {
	return a.Product == b.Product &&
		a.APIVersion == b.APIVersion &&
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

// ValidateReleaseClassification checks scope, disposition, and required fields.
func ValidateReleaseClassification(c ReleaseClassification) error {
	switch c.Scope {
	case ScopeT0, ScopeT1, ScopeT2, ScopeOutsideClaim:
		// valid
	default:
		return fmt.Errorf("unknown release scope %q for %s", c.Scope, c.SurfaceID)
	}

	switch c.Disposition {
	case DispoExistingCase, DispoNewCase:
		if c.CaseID == "" && len(c.ProductTests) == 0 {
			return fmt.Errorf("%s requires a case ID or product test for %s", c.Disposition, c.SurfaceID)
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
