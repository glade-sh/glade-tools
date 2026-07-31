package surfaceledger

import "sort"

// StrictCurrentBase is the derived strict closure view over a merged Surface
// Ledger. It exposes the difference between shape coverage, claimed behavior,
// executable evidence, and strict closure. It does not count passive,
// unsupported, stub/no-op, unevidenced, mismatched, skipped, or inconclusive
// rows as closed.
type StrictCurrentBase struct {
	Total           int             `json:"total"`
	ShapePresent    int             `json:"shapePresent"`
	BehaviorClaimed int             `json:"behaviorClaimed"`
	EvidenceBacked  int             `json:"evidenceBacked"`
	StrictClosed    int             `json:"strictClosed"`
	StrictOpen      int             `json:"strictOpen"`
	OpenRows        []StrictOpenRow `json:"openRows"`
}

// StrictOpenRow is one canonical surface that is not strict-closed, with the
// machine-readable reasons that kept it open.
type StrictOpenRow struct {
	SurfaceID string   `json:"surfaceId"`
	Reasons   []string `json:"reasons"`
}

// ComputeStrictCurrentBase derives the strict current-base result from a set
// of Surface Ledger rows. Rows are reclassified through the existing Classify
// logic so the result is deterministic regardless of the caller's prior
// classification. Open rows are sorted by SurfaceID.
func ComputeStrictCurrentBase(rows []SurfaceLedgerRow) StrictCurrentBase {
	result := StrictCurrentBase{Total: len(rows)}

	classified := make([]SurfaceLedgerRow, len(rows))
	for i, row := range rows {
		r := row
		Classify(&r)
		classified[i] = r
	}
	sort.Slice(classified, func(i, j int) bool {
		return classified[i].SurfaceID < classified[j].SurfaceID
	})

	for _, row := range classified {
		if row.GladeShape != ShapeAbsent {
			result.ShapePresent++
		}
		if row.GladeBehavior != BehaviorNone {
			result.BehaviorClaimed++
		}
		if executableEvidence(row.Evidence) {
			result.EvidenceBacked++
		}
		if isStrictClosed(row) {
			result.StrictClosed++
			continue
		}
		result.StrictOpen++
		result.OpenRows = append(result.OpenRows, StrictOpenRow{
			SurfaceID: row.SurfaceID,
			Reasons:   strictOpenReasons(row),
		})
	}
	return result
}

// isStrictClosed reports whether a row meets the strict closure contract:
// shape present, executable behavior claimed (supported or partial),
// executable evidence backing it, and no signature, return-type, or
// parameter mismatch. Passive, unsupported, and stub/no-op behavior are never
// closed, regardless of evidence.
func isStrictClosed(row SurfaceLedgerRow) bool {
	if row.Bucket == BucketFailure {
		return false
	}
	if row.GladeShape == ShapeAbsent {
		return false
	}
	if row.GladeBehavior != BehaviorSupported && row.GladeBehavior != BehaviorPartial {
		return false
	}
	if !executableEvidence(row.Evidence) {
		return false
	}
	if row.SignatureChanged() {
		return false
	}
	if hasReturnTypeMismatch(row) {
		return false
	}
	if hasParameterMismatch(row) {
		return false
	}
	return true
}

// executableEvidence reports whether the evidence state is sufficient to back
// executable behavior. Docs alone and no evidence are insufficient.
func executableEvidence(e EvidenceState) bool {
	return e != EvidenceNone && e != EvidenceDocs
}

// strictOpenReasons returns the ordered, machine-readable reasons a row is not
// strict-closed. Reasons are ordered by the structural severity of the gap so
// the first reason is the primary cause.
func strictOpenReasons(row SurfaceLedgerRow) []string {
	var reasons []string
	if row.Bucket == BucketFailure {
		reasons = append(reasons, "bucket-failure")
	}
	if row.SignatureChanged() {
		reasons = append(reasons, "signature-changed")
	}
	if hasReturnTypeMismatch(row) {
		reasons = append(reasons, "return-type-mismatch")
	}
	if hasParameterMismatch(row) {
		reasons = append(reasons, "parameter-mismatch")
	}
	if row.GladeShape == ShapeAbsent {
		reasons = append(reasons, "missing-shape")
	}
	switch row.GladeBehavior {
	case BehaviorUnsupported:
		reasons = append(reasons, "behavior-unsupported")
	case BehaviorPassive:
		reasons = append(reasons, "behavior-passive")
	case BehaviorStubNoOp:
		reasons = append(reasons, "behavior-stub-noop")
	case BehaviorNone:
		reasons = append(reasons, "missing-behavior")
	}
	switch row.Evidence {
	case EvidenceNone:
		reasons = append(reasons, "evidence-none")
	case EvidenceDocs:
		reasons = append(reasons, "evidence-docs")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "not-strict-closed")
	}
	return reasons
}
