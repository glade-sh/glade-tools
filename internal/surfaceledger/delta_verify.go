package surfaceledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
)

var exactSnapshotFiles = []string{
	"DOCS_SNAPSHOT.json",
	"ORG_SNAPSHOT.json",
	"GLADE_SNAPSHOT.json",
	"EVIDENCE_SNAPSHOT.json",
}

type ExactLedgerDeltaInput struct {
	Path   string `json:"path,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Rows   int    `json:"rows"`
}

type ExactLedgerDeltaInputs struct {
	Base      ExactLedgerDeltaInput `json:"base"`
	Current   ExactLedgerDeltaInput `json:"current"`
	Expected  ExactLedgerDeltaInput `json:"expected"`
	Authority ExactLedgerDeltaInput `json:"authority"`
	Attempt   ExactLedgerDeltaInput `json:"attempt"`
}

type LedgerSnapshotAuthority struct {
	LedgerSHA256   string            `json:"ledgerSha256"`
	SnapshotSHA256 map[string]string `json:"snapshotSha256"`
	SourceIdentity *SourceIdentity   `json:"sourceIdentity"`
}

type ExactLedgerDeltaAuthority struct {
	SchemaVersion     int                     `json:"schemaVersion"`
	Status            string                  `json:"status"`
	Candidate         ExactCandidateAuthority `json:"candidate"`
	Base              LedgerSnapshotAuthority `json:"base"`
	Current           LedgerSnapshotAuthority `json:"current"`
	ExpectedIDsSHA256 string                  `json:"expectedIDsSha256"`
}

type ExactCandidateAuthority struct {
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
	SHA256 string `json:"sha256"`
}

type ExactCandidateVerification struct {
	Root       string `json:"root"`
	Commit     string `json:"commit"`
	Tree       string `json:"tree"`
	BinaryPath string `json:"binaryPath"`
	SHA256     string `json:"sha256"`
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
	SchemaVersion        int                        `json:"schemaVersion"`
	Status               string                     `json:"status"`
	Inputs               ExactLedgerDeltaInputs     `json:"inputs"`
	ExpectedSurfaceIDs   []string                   `json:"expectedSurfaceIds"`
	ChangedSurfaceIDs    []string                   `json:"changedSurfaceIds"`
	UnexpectedSurfaceIDs []string                   `json:"unexpectedSurfaceIds"`
	MissingExpectedIDs   []string                   `json:"missingExpectedSurfaceIds"`
	AuthorityCandidate   ExactCandidateAuthority    `json:"authorityCandidate"`
	ActualCandidate      ExactCandidateVerification `json:"actualCandidate"`
	AuthorityToolsCommit string                     `json:"authorityToolsCommit"`
	RunningToolsSHA256   string                     `json:"runningToolsSha256"`
	Counts               ExactLedgerDeltaCounts     `json:"counts"`
	Rows                 []ExactLedgerDeltaRow      `json:"rows"`
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

// VerifyLedgerSnapshotClosure proves that ledgerData is the complete ledger
// produced by its four retained source snapshots. Hashing a caller-projected
// ledger is insufficient: the projection must fail this rederivation step.
func VerifyLedgerSnapshotClosure(ledgerData []byte, snapshotDir string, authority LedgerSnapshotAuthority) error {
	var stored SurfaceLedger
	if err := json.Unmarshal(ledgerData, &stored); err != nil {
		return fmt.Errorf("parse ledger for snapshot closure: %w", err)
	}
	if stored.SourceSnapshotBindings == nil || len(stored.SourceSnapshotBindings.Files) != len(exactSnapshotFiles) {
		return fmt.Errorf("snapshot closure requires exactly %d source bindings", len(exactSnapshotFiles))
	}
	if len(authority.SnapshotSHA256) != len(exactSnapshotFiles) || !reflect.DeepEqual(stored.SourceSnapshotBindings.Files, authority.SnapshotSHA256) {
		return fmt.Errorf("snapshot closure does not match external authority")
	}
	if !reflect.DeepEqual(stored.SourceIdentity, authority.SourceIdentity) {
		return fmt.Errorf("snapshot closure source identity does not match external authority")
	}
	groups := make(map[string][]SurfaceLedgerRow, len(exactSnapshotFiles))
	bindings := SourceSnapshotBindings{Files: make(map[string]string, len(exactSnapshotFiles))}
	for _, name := range exactSnapshotFiles {
		want, ok := authority.SnapshotSHA256[name]
		if !ok || want == "" {
			return fmt.Errorf("snapshot closure missing binding for %s", name)
		}
		data, err := os.ReadFile(filepath.Join(snapshotDir, name))
		if err != nil {
			return fmt.Errorf("snapshot closure read %s: %w", name, err)
		}
		got := fmt.Sprintf("%x", sha256.Sum256(data))
		if got != want {
			return fmt.Errorf("snapshot closure hash mismatch for %s: got %s want %s", name, got, want)
		}
		rows, err := decodeExactSnapshotRows(data, name)
		if err != nil {
			return err
		}
		groups[name] = rows
		bindings.Files[name] = got
	}
	rebuilt := Merge(
		groups["DOCS_SNAPSHOT.json"],
		groups["ORG_SNAPSHOT.json"],
		groups["GLADE_SNAPSHOT.json"],
		groups["EVIDENCE_SNAPSHOT.json"],
	)
	if stored.SourceIdentity != nil {
		ApplySourceIdentity(&rebuilt, *stored.SourceIdentity)
	}
	AssignPriorities(rebuilt.Rows)
	rebuilt.Summary = Summarize(rebuilt.Rows)
	rebuilt.SourceSnapshotBindings = &bindings
	rebuiltData, err := json.Marshal(rebuilt)
	if err != nil {
		return fmt.Errorf("encode rebuilt snapshot closure: %w", err)
	}
	storedCanonical, err := canonicalJSON(ledgerData)
	if err != nil {
		return fmt.Errorf("canonicalize stored snapshot closure: %w", err)
	}
	rebuiltCanonical, err := canonicalJSON(rebuiltData)
	if err != nil {
		return fmt.Errorf("canonicalize rebuilt snapshot closure: %w", err)
	}
	if !bytes.Equal(storedCanonical, rebuiltCanonical) {
		return fmt.Errorf("ledger does not equal complete snapshot closure")
	}
	return nil
}

func decodeExactSnapshotRows(data []byte, name string) ([]SurfaceLedgerRow, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var rows []SurfaceLedgerRow
	if err := decoder.Decode(&rows); err != nil {
		return nil, fmt.Errorf("snapshot closure parse %s: %w", name, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("snapshot closure parse %s: trailing JSON value", name)
		}
		return nil, fmt.Errorf("snapshot closure parse %s: %w", name, err)
	}
	return rows, nil
}

func canonicalJSON(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("trailing JSON value")
		}
		return nil, err
	}
	return json.Marshal(value)
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
