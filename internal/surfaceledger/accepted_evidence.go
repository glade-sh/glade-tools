package surfaceledger

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type AcceptedEvidenceManifest struct {
	SchemaVersion int                      `json:"schemaVersion"`
	ManifestSHA   string                   `json:"manifestSHA256"`
	SourceFiles   []AcceptedSourceInput    `json:"sourceFiles"`
	TotalInput    int                      `json:"totalInput"`
	Accepted      int                      `json:"accepted"`
	Rejected      int                      `json:"rejected"`
	AcceptedRows  []AcceptedEvidenceRow    `json:"acceptedRows"`
	RejectedRows  []AcceptedRejectedRow    `json:"rejectedRows"`
	SupportTotal  int                      `json:"supportProfileTotal"`
}

type AcceptedSourceInput struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type AcceptedEvidenceRow struct {
	SurfaceID      string                   `json:"surfaceId"`
	CoverageKind   string                   `json:"coverageKind"`
	PacketID       string                   `json:"packetId,omitempty"`
	CandidatePath  string                   `json:"candidatePath,omitempty"`
	CandidateSHA   string                   `json:"candidateSha256,omitempty"`
	APIVersion     string                   `json:"apiVersion,omitempty"`
	OrgAlias       string                   `json:"orgAlias,omitempty"`
	OrgID          string                   `json:"orgId,omitempty"`
	SourceBatch    string                   `json:"sourceBatch,omitempty"`
	ScenarioIDs    []string                 `json:"scenarioIds,omitempty"`
	EvidencePaths  []string                 `json:"evidencePaths,omitempty"`
	SourceHashes   []string                 `json:"sourceHashes,omitempty"`
	WitnessSides   []string                 `json:"witnessSides,omitempty"`
	EvidenceReason string                   `json:"evidenceReason,omitempty"`
}

type AcceptedRejectedRow struct {
	SurfaceID string `json:"surfaceId"`
	Reason    string `json:"reason"`
}

type rawMap109 struct {
	PacketID    string `json:"packetId"`
	ClosureID   string `json:"closureId"`
	Candidate   struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
	APIVersion  string `json:"apiVersion"`
	SourceBatch string `json:"sourceBatch"`
	Rows        []struct {
		SurfaceID          string `json:"surfaceId"`
		CoverageKind       string `json:"coverageKind"`
		ScenarioIDs        []string `json:"scenarioIds"`
		EvidencePaths      []string `json:"evidencePaths"`
		AttemptedScenarioIDs []string `json:"attemptedScenarioIds"`
		Reason             string `json:"reason"`
		SourceWitnesses    []struct {
			Side         string `json:"side"`
			Kind         string `json:"kind"`
			SourcePath   string `json:"sourcePath"`
			SourceSHA256 string `json:"sourceSha256"`
		} `json:"sourceWitnesses"`
		ComparisonStatuses []struct {
			ScenarioID string `json:"scenarioId"`
			Source     string `json:"source"`
			Comparison string `json:"comparison"`
		} `json:"comparisonStatuses"`
	} `json:"rows"`
}

type rawMap110 struct {
	PacketID     string `json:"packetId"`
	CorrectionID string `json:"correctionId"`
	SourceBatch  string `json:"sourceBatch"`
	EvidenceOnly bool   `json:"evidenceOnly"`
	CandidateSHA string `json:"candidateSha256"`
	OrgAlias     string `json:"orgAlias"`
	APIVersion   string `json:"apiVersion"`
	Rows         []struct {
		SurfaceID     string `json:"surfaceId"`
		CoverageKind  string `json:"coverageKind"`
		ScenarioIDs    []string `json:"scenarioIds"`
		EvidencePaths []string `json:"evidencePaths"`
		AttemptedScenarioIDs []string `json:"attemptedScenarioIds"`
		Reason        string `json:"reason"`
		SourceWitnesses []struct {
			Side         string `json:"side"`
			Kind         string `json:"kind"`
			SourcePath   string `json:"sourcePath"`
			SourceSHA256 string `json:"sourceSha256"`
		} `json:"sourceWitnesses"`
		ComparisonStatuses []struct {
			ScenarioID string `json:"scenarioId"`
			Source     string `json:"source"`
			Comparison string `json:"comparison"`
		} `json:"comparisonStatuses"`
		EvidenceBasis struct {
			HostedBoundary string `json:"hostedBoundary"`
		} `json:"evidenceBasis"`
	} `json:"rows"`
}

type rawMap112 struct {
	PacketID    string `json:"packetId"`
	SourceBatch string `json:"sourceBatch"`
	Candidate   struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"candidate"`
	SelectedOrg struct {
		Alias      string `json:"alias"`
		OrgID      string `json:"orgId"`
		APIVersion string `json:"apiVersion"`
	} `json:"selectedOrg"`
	Rows []struct {
		SurfaceID          string `json:"surfaceId"`
		CoverageKind       string `json:"coverageKind"`
		ComparisonStatus   string `json:"comparisonStatus"`
		LocalObserved      bool   `json:"localObserved"`
		SalesforceObserved bool   `json:"salesforceObserved"`
		ScenarioIDs         []string `json:"scenarioIds"`
		EvidencePaths      []string `json:"evidencePaths"`
		Reason             string `json:"reason"`
		SourceEvidence     []struct {
			SourceSHA256 string `json:"sourceSha256"`
		} `json:"sourceEvidence"`
		LocalEvidencePaths    []string `json:"localEvidencePaths"`
		SalesforceEvidencePaths []string `json:"salesforceEvidencePaths"`
	} `json:"rows"`
}

type rawMap114 struct {
	Rows []struct {
		RowID           string `json:"rowId"`
		SourceBatch     string `json:"sourceBatch"`
		CoverageKind    string `json:"coverageKind"`
		Passed          bool   `json:"passed"`
		EvidenceReason  string `json:"evidenceReason"`
		ScenarioIDs      []string `json:"scenarioIds"`
	} `json:"rows"`
}

type rawMap115 struct {
	Packet string `json:"packet"`
	Rows   []struct {
		SurfaceID    string `json:"surfaceId"`
		SourceBatch  string `json:"sourceBatch"`
		CoverageKind string `json:"coverageKind"`
		ScenarioIDs   []string `json:"scenarioIds"`
		EvidencePaths []string `json:"evidencePaths"`
		Reason       string `json:"reason"`
		Comparison   []struct {
			ScenarioID       string `json:"scenarioId"`
			Status           string `json:"status"`
			LocalPassed      bool   `json:"localPassed"`
			SalesforcePassed bool   `json:"salesforcePassed"`
		} `json:"comparison"`
		DirectWitnessAudit struct {
			Passed bool `json:"passed"`
		} `json:"directWitnessAudit"`
	} `json:"rows"`
}

type rawSupportProfile struct {
	Total  int `json:"total"`
	ByDisposition map[string]int `json:"byDisposition"`
}

func IngestAcceptedEvidence(mapPaths []string, supportProfilePath string) (*AcceptedEvidenceManifest, error) {
	manifest := &AcceptedEvidenceManifest{SchemaVersion: 1}

	totalInput := 0
	for _, path := range mapPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read map %s: %w", path, err)
		}
		sha := fmt.Sprintf("%x", sha256.Sum256(data))
		manifest.SourceFiles = append(manifest.SourceFiles, AcceptedSourceInput{Path: path, SHA256: sha})

		rows, err := ingestMap(path, data)
		if err != nil {
			return nil, fmt.Errorf("ingest map %s: %w", path, err)
		}
		totalInput += rows.InputCount
		manifest.AcceptedRows = append(manifest.AcceptedRows, rows.Accepted...)
		manifest.RejectedRows = append(manifest.RejectedRows, rows.Rejected...)
	}

	if supportProfilePath != "" {
		spData, err := os.ReadFile(supportProfilePath)
		if err != nil {
			return nil, fmt.Errorf("read support profile: %w", err)
		}
		spSHA := fmt.Sprintf("%x", sha256.Sum256(spData))
		manifest.SourceFiles = append(manifest.SourceFiles, AcceptedSourceInput{Path: supportProfilePath, SHA256: spSHA})
		var sp rawSupportProfile
		if err := json.Unmarshal(spData, &sp); err != nil {
			return nil, fmt.Errorf("parse support profile: %w", err)
		}
		manifest.SupportTotal = sp.Total
	}

	manifest.TotalInput = totalInput
	sort.Slice(manifest.AcceptedRows, func(i, j int) bool {
		return manifest.AcceptedRows[i].SurfaceID < manifest.AcceptedRows[j].SurfaceID
	})
	sort.Slice(manifest.RejectedRows, func(i, j int) bool {
		return manifest.RejectedRows[i].SurfaceID < manifest.RejectedRows[j].SurfaceID
	})

	manifest.Accepted = len(manifest.AcceptedRows)
	manifest.Rejected = len(manifest.RejectedRows)

	h := sha256.New()
	for _, row := range manifest.AcceptedRows {
		fmt.Fprintf(h, "%s|%s|%s|%s\n", row.SurfaceID, row.CoverageKind, row.PacketID, row.CandidateSHA)
	}
	manifest.ManifestSHA = fmt.Sprintf("%x", h.Sum(nil))
	return manifest, nil
}

type ingestResult struct {
	InputCount int
	Accepted   []AcceptedEvidenceRow
	Rejected   []AcceptedRejectedRow
}

func ingestMap(path string, data []byte) (*ingestResult, error) {
	base := filepathBare(filename(path))
	switch {
	case strings.HasPrefix(base, "cb109"):
		return ingestMap109(data)
	case strings.HasPrefix(base, "cb110"):
		return ingestMap110(data)
	case strings.HasPrefix(base, "cb112"):
		return ingestMap112(data)
	case strings.HasPrefix(base, "cb114"):
		return ingestMap114(data)
	case strings.HasPrefix(base, "cb115"):
		return ingestMap115(data)
	default:
		return nil, fmt.Errorf("unknown map type: %s", base)
	}
}

func ingestMap109(data []byte) (*ingestResult, error) {
	var m rawMap109
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	result := &ingestResult{InputCount: len(m.Rows)}
	for _, r := range m.Rows {
		row, accept, reason := evaluate109Row(r, m.PacketID, m.Candidate.Path, m.Candidate.SHA256, m.APIVersion, m.SourceBatch)
		if accept {
			result.Accepted = append(result.Accepted, row)
		} else {
			result.Rejected = append(result.Rejected, AcceptedRejectedRow{SurfaceID: row.SurfaceID, Reason: reason})
		}
	}
	return result, nil
}

func evaluate109Row(r struct {
	SurfaceID          string `json:"surfaceId"`
	CoverageKind       string `json:"coverageKind"`
	ScenarioIDs        []string `json:"scenarioIds"`
	EvidencePaths      []string `json:"evidencePaths"`
	AttemptedScenarioIDs []string `json:"attemptedScenarioIds"`
	Reason             string `json:"reason"`
	SourceWitnesses    []struct {
		Side         string `json:"side"`
		Kind         string `json:"kind"`
		SourcePath   string `json:"sourcePath"`
		SourceSHA256 string `json:"sourceSha256"`
	} `json:"sourceWitnesses"`
	ComparisonStatuses []struct {
		ScenarioID string `json:"scenarioId"`
		Source     string `json:"source"`
		Comparison string `json:"comparison"`
	} `json:"comparisonStatuses"`
}, packetID, candPath, candSHA, apiVer, sourceBatch string) (AcceptedEvidenceRow, bool, string) {

	row := AcceptedEvidenceRow{
		SurfaceID:     r.SurfaceID,
		CoverageKind:  r.CoverageKind,
		PacketID:      packetID,
		CandidatePath: candPath,
		CandidateSHA:  candSHA,
		APIVersion:    apiVer,
		SourceBatch:   sourceBatch,
		ScenarioIDs:    copyStrs(r.ScenarioIDs),
		EvidencePaths: copyStrs(r.EvidencePaths),
	}

	if r.CoverageKind != "exact-runtime" {
		return row, false, "uncovered"
	}

	hasLocal, hasSF := false, false
	for _, w := range r.SourceWitnesses {
		row.SourceHashes = append(row.SourceHashes, w.SourceSHA256)
		row.WitnessSides = append(row.WitnessSides, w.Side)
		switch w.Side {
		case "local":
			hasLocal = true
		case "salesforce":
			hasSF = true
		}
	}
	sort.Strings(row.SourceHashes)

	if !hasLocal {
		return row, false, "one-sided: no local witness"
	}
	if !hasSF {
		return row, false, "one-sided: no salesforce witness"
	}
	return row, true, ""
}

func ingestMap110(data []byte) (*ingestResult, error) {
	var m rawMap110
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	result := &ingestResult{InputCount: len(m.Rows)}
	for _, r := range m.Rows {
		row, accept, reason := evaluate110Row(r, m.PacketID, "", m.CandidateSHA, m.APIVersion, m.SourceBatch, m.OrgAlias)
		if accept {
			result.Accepted = append(result.Accepted, row)
		} else {
			result.Rejected = append(result.Rejected, AcceptedRejectedRow{SurfaceID: row.SurfaceID, Reason: reason})
		}
	}
	return result, nil
}

func evaluate110Row(r struct {
	SurfaceID     string `json:"surfaceId"`
	CoverageKind  string `json:"coverageKind"`
	ScenarioIDs    []string `json:"scenarioIds"`
	EvidencePaths []string `json:"evidencePaths"`
	AttemptedScenarioIDs []string `json:"attemptedScenarioIds"`
	Reason        string `json:"reason"`
	SourceWitnesses []struct {
		Side         string `json:"side"`
		Kind         string `json:"kind"`
		SourcePath   string `json:"sourcePath"`
		SourceSHA256 string `json:"sourceSha256"`
	} `json:"sourceWitnesses"`
	ComparisonStatuses []struct {
		ScenarioID string `json:"scenarioId"`
		Source     string `json:"source"`
		Comparison string `json:"comparison"`
	} `json:"comparisonStatuses"`
	EvidenceBasis struct {
		HostedBoundary string `json:"hostedBoundary"`
	} `json:"evidenceBasis"`
}, packetID, candPath, candSHA, apiVer, sourceBatch, orgAlias string) (AcceptedEvidenceRow, bool, string) {

	row := AcceptedEvidenceRow{
		SurfaceID:     r.SurfaceID,
		CoverageKind:  r.CoverageKind,
		PacketID:      packetID,
		CandidateSHA:  candSHA,
		APIVersion:    apiVer,
		SourceBatch:   sourceBatch,
		OrgAlias:      orgAlias,
		ScenarioIDs:    copyStrs(r.ScenarioIDs),
		EvidencePaths: copyStrs(r.EvidencePaths),
	}

	if r.EvidenceBasis.HostedBoundary != "" {
		return row, false, "hosted-boundary"
	}
	if r.CoverageKind == "mock-contract" {
		return row, false, "dto-only: mock-contract"
	}
	if r.CoverageKind != "exact-runtime" {
		return row, false, "uncovered"
	}

	hasLocal, hasSF, hasPass := false, false, false
	for _, w := range r.SourceWitnesses {
		row.SourceHashes = append(row.SourceHashes, w.SourceSHA256)
		row.WitnessSides = append(row.WitnessSides, w.Side)
		switch w.Side {
		case "local", "local-exact-candidate":
			hasLocal = true
		case "salesforce":
			hasSF = true
		}
	}
	for _, cs := range r.ComparisonStatuses {
		if strings.EqualFold(cs.Comparison, "pass") || strings.EqualFold(cs.Comparison, "pass-or-contract") {
			hasPass = true
		}
	}
	sort.Strings(row.SourceHashes)

	if hasLocal && hasSF {
		return row, true, ""
	}
	if hasPass {
		return row, true, ""
	}
	if hasLocal && !hasSF {
		return row, false, "one-sided: no salesforce witness"
	}
	if !hasLocal && hasSF {
		return row, false, "one-sided: no local witness"
	}
	return row, false, "one-sided: no pair witnesses"
}

func ingestMap112(data []byte) (*ingestResult, error) {
	var m rawMap112
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	result := &ingestResult{InputCount: len(m.Rows)}
	for _, r := range m.Rows {
		row, accept, reason := evaluate112Row(r, m.PacketID, m.Candidate.Path, m.Candidate.SHA256, m.SelectedOrg.APIVersion, m.SourceBatch, m.SelectedOrg.Alias, m.SelectedOrg.OrgID)
		if accept {
			result.Accepted = append(result.Accepted, row)
		} else {
			result.Rejected = append(result.Rejected, AcceptedRejectedRow{SurfaceID: row.SurfaceID, Reason: reason})
		}
	}
	return result, nil
}

func evaluate112Row(r struct {
	SurfaceID          string `json:"surfaceId"`
	CoverageKind       string `json:"coverageKind"`
	ComparisonStatus   string `json:"comparisonStatus"`
	LocalObserved      bool   `json:"localObserved"`
	SalesforceObserved bool   `json:"salesforceObserved"`
	ScenarioIDs         []string `json:"scenarioIds"`
	EvidencePaths      []string `json:"evidencePaths"`
	Reason             string `json:"reason"`
	SourceEvidence     []struct {
		SourceSHA256 string `json:"sourceSha256"`
	} `json:"sourceEvidence"`
	LocalEvidencePaths    []string `json:"localEvidencePaths"`
	SalesforceEvidencePaths []string `json:"salesforceEvidencePaths"`
}, packetID, candPath, candSHA, apiVer, sourceBatch, orgAlias, orgID string) (AcceptedEvidenceRow, bool, string) {

	row := AcceptedEvidenceRow{
		SurfaceID:     r.SurfaceID,
		CoverageKind:  r.CoverageKind,
		PacketID:      packetID,
		CandidatePath: candPath,
		CandidateSHA:  candSHA,
		APIVersion:    apiVer,
		SourceBatch:   sourceBatch,
		OrgAlias:      orgAlias,
		OrgID:         orgID,
		ScenarioIDs:    copyStrs(r.ScenarioIDs),
		EvidencePaths: copyStrs(r.EvidencePaths),
	}

	if r.CoverageKind != "exact-runtime" {
		return row, false, "uncovered"
	}
	for _, se := range r.SourceEvidence {
		row.SourceHashes = append(row.SourceHashes, se.SourceSHA256)
	}
	sort.Strings(row.SourceHashes)

	if r.LocalObserved && r.SalesforceObserved {
		row.WitnessSides = []string{"local", "salesforce"}
		return row, true, ""
	}
	if !r.LocalObserved {
		return row, false, "one-sided: no local observation"
	}
	return row, false, "one-sided: no salesforce observation"
}

func ingestMap114(data []byte) (*ingestResult, error) {
	var m rawMap114
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	result := &ingestResult{InputCount: len(m.Rows)}
	for _, r := range m.Rows {
		row, accept, reason := evaluate114Row(r)
		if accept {
			result.Accepted = append(result.Accepted, row)
		} else {
			result.Rejected = append(result.Rejected, AcceptedRejectedRow{SurfaceID: row.SurfaceID, Reason: reason})
		}
	}
	return result, nil
}

func evaluate114Row(r struct {
	RowID           string `json:"rowId"`
	SourceBatch     string `json:"sourceBatch"`
	CoverageKind    string `json:"coverageKind"`
	Passed          bool   `json:"passed"`
	EvidenceReason  string `json:"evidenceReason"`
	ScenarioIDs      []string `json:"scenarioIds"`
}) (AcceptedEvidenceRow, bool, string) {

	row := AcceptedEvidenceRow{
		SurfaceID:      r.RowID,
		CoverageKind:   r.CoverageKind,
		SourceBatch:    r.SourceBatch,
		ScenarioIDs:     copyStrs(r.ScenarioIDs),
		EvidenceReason: r.EvidenceReason,
	}

	if r.CoverageKind != "exact-runtime" {
		return row, false, "uncovered"
	}
	if r.Passed {
		return row, true, ""
	}
	return row, false, "not passed: no direct evidence"
}

func ingestMap115(data []byte) (*ingestResult, error) {
	var m rawMap115
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	result := &ingestResult{InputCount: len(m.Rows)}
	for _, r := range m.Rows {
		row, accept, reason := evaluate115Row(r, m.Packet)
		if accept {
			result.Accepted = append(result.Accepted, row)
		} else {
			result.Rejected = append(result.Rejected, AcceptedRejectedRow{SurfaceID: row.SurfaceID, Reason: reason})
		}
	}
	return result, nil
}

func evaluate115Row(r struct {
	SurfaceID    string `json:"surfaceId"`
	SourceBatch  string `json:"sourceBatch"`
	CoverageKind string `json:"coverageKind"`
	ScenarioIDs   []string `json:"scenarioIds"`
	EvidencePaths []string `json:"evidencePaths"`
	Reason       string `json:"reason"`
	Comparison   []struct {
		ScenarioID       string `json:"scenarioId"`
		Status           string `json:"status"`
		LocalPassed      bool   `json:"localPassed"`
		SalesforcePassed bool   `json:"salesforcePassed"`
	} `json:"comparison"`
	DirectWitnessAudit struct {
		Passed bool `json:"passed"`
	} `json:"directWitnessAudit"`
}, packetID string) (AcceptedEvidenceRow, bool, string) {

	row := AcceptedEvidenceRow{
		SurfaceID:     r.SurfaceID,
		CoverageKind:  r.CoverageKind,
		PacketID:      packetID,
		SourceBatch:   r.SourceBatch,
		ScenarioIDs:    copyStrs(r.ScenarioIDs),
		EvidencePaths: copyStrs(r.EvidencePaths),
	}

	if r.CoverageKind != "exact-runtime" {
		return row, false, "uncovered"
	}

	if r.DirectWitnessAudit.Passed {
		return row, true, ""
	}

	hasBoth := false
	for _, c := range r.Comparison {
		if c.LocalPassed && c.SalesforcePassed {
			hasBoth = true
			break
		}
	}
	if hasBoth {
		return row, true, ""
	}
	return row, false, "not passed: no direct evidence"
}

func WriteAcceptedEvidenceJSON(w io.Writer, m *AcceptedEvidenceManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

func filename(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

func filepathBare(name string) string {
	name = filename(name)
	if dot := strings.LastIndexByte(name, '.'); dot > 0 {
		name = name[:dot]
	}
	return name
}

func copyStrs(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}
