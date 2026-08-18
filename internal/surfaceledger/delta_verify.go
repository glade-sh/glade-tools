package surfaceledger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
)

type ExactLedgerDeltaInput struct {
	Path   string `json:"path,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Rows   int    `json:"rows"`
}

type ExactLedgerDeltaInputs struct {
	Base     ExactLedgerDeltaInput `json:"base"`
	Current  ExactLedgerDeltaInput `json:"current"`
	Expected ExactLedgerDeltaInput `json:"expected"`
}

type ExactLedgerDeltaCounts struct {
	Expected        int `json:"expected"`
	FullChanged     int `json:"fullChanged"`
	Added           int `json:"added"`
	Changed         int `json:"changed"`
	Removed         int `json:"removed"`
	Unexpected      int `json:"unexpected"`
	MissingExpected int `json:"missingExpected"`
}

type ExactLedgerDeltaRow struct {
	SurfaceID string    `json:"surfaceId"`
	Change    DeltaKind `json:"change"`
}

type ExactLedgerDeltaReport struct {
	SchemaVersion        int                    `json:"schemaVersion"`
	Status               string                 `json:"status"`
	Inputs               ExactLedgerDeltaInputs `json:"inputs"`
	ExpectedSurfaceIDs   []string               `json:"expectedSurfaceIds"`
	ChangedSurfaceIDs    []string               `json:"changedSurfaceIds"`
	UnexpectedSurfaceIDs []string               `json:"unexpectedSurfaceIds"`
	MissingExpectedIDs   []string               `json:"missingExpectedSurfaceIds"`
	Counts               ExactLedgerDeltaCounts `json:"counts"`
	Rows                 []ExactLedgerDeltaRow  `json:"rows"`
}

func VerifyExactLedgerDelta(base, current []SurfaceLedgerRow, expectedIDs []string) (ExactLedgerDeltaReport, error) {
	baseByID, err := exactRowsByID(base, "base")
	if err != nil {
		return ExactLedgerDeltaReport{}, err
	}
	currentByID, err := exactRowsByID(current, "current")
	if err != nil {
		return ExactLedgerDeltaReport{}, err
	}
	baseIDs := make(map[string]struct{}, len(baseByID))
	for id := range baseByID {
		baseIDs[id] = struct{}{}
	}
	currentIDs := make(map[string]struct{}, len(currentByID))
	for id := range currentByID {
		currentIDs[id] = struct{}{}
	}
	return verifyExactLedgerDelta(baseIDs, currentIDs, expectedIDs, func(id string) bool {
		return reflect.DeepEqual(baseByID[id], currentByID[id])
	})
}

// VerifyExactLedgerDeltaJSON compares the complete stored JSON object for
// every row. It deliberately does not classify or decode rows into the typed
// model, so stored classification fields and fields added by newer producers
// cannot disappear at this trust boundary.
func VerifyExactLedgerDeltaJSON(baseData, currentData []byte, expectedIDs []string) (ExactLedgerDeltaReport, error) {
	baseByID, err := exactRawRowsByID(baseData, "base")
	if err != nil {
		return ExactLedgerDeltaReport{}, err
	}
	currentByID, err := exactRawRowsByID(currentData, "current")
	if err != nil {
		return ExactLedgerDeltaReport{}, err
	}
	baseIDs := make(map[string]struct{}, len(baseByID))
	for id := range baseByID {
		baseIDs[id] = struct{}{}
	}
	currentIDs := make(map[string]struct{}, len(currentByID))
	for id := range currentByID {
		currentIDs[id] = struct{}{}
	}
	return verifyExactLedgerDelta(baseIDs, currentIDs, expectedIDs, func(id string) bool {
		return bytes.Equal(baseByID[id], currentByID[id])
	})
}

func verifyExactLedgerDelta(baseByID, currentByID map[string]struct{}, expectedIDs []string, rowsEqual func(string) bool) (ExactLedgerDeltaReport, error) {
	if len(expectedIDs) == 0 {
		return ExactLedgerDeltaReport{}, fmt.Errorf("expected SurfaceID set is empty")
	}
	expected := make(map[string]struct{}, len(expectedIDs))
	for _, id := range expectedIDs {
		if id == "" {
			return ExactLedgerDeltaReport{}, fmt.Errorf("empty expected SurfaceID")
		}
		if _, exists := expected[id]; exists {
			return ExactLedgerDeltaReport{}, fmt.Errorf("duplicate expected SurfaceID %q", id)
		}
		expected[id] = struct{}{}
	}

	rows := make([]ExactLedgerDeltaRow, 0)
	for id := range currentByID {
		_, exists := baseByID[id]
		switch {
		case !exists:
			rows = append(rows, ExactLedgerDeltaRow{SurfaceID: id, Change: DeltaAdded})
		case !rowsEqual(id):
			rows = append(rows, ExactLedgerDeltaRow{SurfaceID: id, Change: DeltaChanged})
		}
	}
	for id := range baseByID {
		if _, exists := currentByID[id]; !exists {
			rows = append(rows, ExactLedgerDeltaRow{SurfaceID: id, Change: DeltaRemoved})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].SurfaceID < rows[j].SurfaceID })

	report := ExactLedgerDeltaReport{
		SchemaVersion: 1,
		Status:        "pass",
		Inputs: ExactLedgerDeltaInputs{
			Base:     ExactLedgerDeltaInput{Rows: len(baseByID)},
			Current:  ExactLedgerDeltaInput{Rows: len(currentByID)},
			Expected: ExactLedgerDeltaInput{Rows: len(expectedIDs)},
		},
		ExpectedSurfaceIDs:   append([]string{}, expectedIDs...),
		ChangedSurfaceIDs:    make([]string, 0, len(rows)),
		UnexpectedSurfaceIDs: make([]string, 0),
		MissingExpectedIDs:   make([]string, 0),
		Rows:                 rows,
	}
	sort.Strings(report.ExpectedSurfaceIDs)
	for _, row := range rows {
		report.ChangedSurfaceIDs = append(report.ChangedSurfaceIDs, row.SurfaceID)
		switch row.Change {
		case DeltaAdded:
			report.Counts.Added++
		case DeltaChanged:
			report.Counts.Changed++
		case DeltaRemoved:
			report.Counts.Removed++
		}
		if _, ok := expected[row.SurfaceID]; !ok {
			report.UnexpectedSurfaceIDs = append(report.UnexpectedSurfaceIDs, row.SurfaceID)
		}
	}
	changed := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		changed[row.SurfaceID] = struct{}{}
	}
	for _, id := range report.ExpectedSurfaceIDs {
		if _, ok := changed[id]; !ok {
			report.MissingExpectedIDs = append(report.MissingExpectedIDs, id)
		}
	}
	report.Counts.Expected = len(expectedIDs)
	report.Counts.FullChanged = len(rows)
	report.Counts.Unexpected = len(report.UnexpectedSurfaceIDs)
	report.Counts.MissingExpected = len(report.MissingExpectedIDs)
	if report.Counts.Unexpected != 0 || report.Counts.MissingExpected != 0 {
		report.Status = "fail"
	}
	return report, nil
}

func exactRawRowsByID(data []byte, label string) (map[string][]byte, error) {
	var ledger struct {
		Rows []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(data, &ledger); err != nil {
		return nil, fmt.Errorf("parse %s ledger: %w", label, err)
	}
	byID := make(map[string][]byte, len(ledger.Rows))
	for _, raw := range ledger.Rows {
		var row map[string]any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&row); err != nil {
			return nil, fmt.Errorf("parse row in %s ledger: %w", label, err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			if err == nil {
				return nil, fmt.Errorf("parse row in %s ledger: trailing JSON value", label)
			}
			return nil, fmt.Errorf("parse row in %s ledger: %w", label, err)
		}
		id, ok := row["surfaceId"].(string)
		if !ok || id == "" {
			return nil, fmt.Errorf("empty SurfaceID in %s ledger", label)
		}
		if _, exists := byID[id]; exists {
			return nil, fmt.Errorf("duplicate SurfaceID %q in %s ledger", id, label)
		}
		canonical, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("canonicalize SurfaceID %q in %s ledger: %w", id, label, err)
		}
		byID[id] = canonical
	}
	return byID, nil
}

func exactRowsByID(rows []SurfaceLedgerRow, label string) (map[string]SurfaceLedgerRow, error) {
	byID := make(map[string]SurfaceLedgerRow, len(rows))
	for _, row := range rows {
		if row.SurfaceID == "" {
			return nil, fmt.Errorf("empty SurfaceID in %s ledger", label)
		}
		if _, exists := byID[row.SurfaceID]; exists {
			return nil, fmt.Errorf("duplicate SurfaceID %q in %s ledger", row.SurfaceID, label)
		}
		byID[row.SurfaceID] = row
	}
	return byID, nil
}
