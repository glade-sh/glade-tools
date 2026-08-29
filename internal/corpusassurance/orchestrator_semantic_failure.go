package corpusassurance

import (
	"database/sql"
	"fmt"
	"reflect"
	"time"
)

const (
	OrchestratorSemanticMismatchFailureCode  = "salesforce-semantic-mismatch"
	orchestratorSemanticMismatchKind         = "semantic-mismatch"
	orchestratorSemanticMismatchStatus       = "validated-semantic-mismatch"
	orchestratorSemanticMismatchTerminalized = "terminalized-semantic-mismatch"
)

// OrchestratorSemanticMismatchAuthority is a typed, zero-credit authority for
// a worker dispatch whose Salesforce result is known to be semantically
// mismatched. It binds the failed dispatch to one exact leased attempt.
type OrchestratorSemanticMismatchAuthority struct {
	SchemaVersion   int                                  `json:"schemaVersion"`
	Kind            string                               `json:"kind"`
	Status          string                               `json:"status"`
	FailureCode     string                               `json:"failureCode"`
	CampaignID      string                               `json:"campaignId"`
	JobID           string                               `json:"jobId"`
	ShardIndex      int                                  `json:"shardIndex"`
	Generation      int                                  `json:"generation"`
	Worker          string                               `json:"worker"`
	LeaseUntil      time.Time                            `json:"leaseUntil"`
	DurationMS      int64                                `json:"durationMs"`
	SpecSHA256      string                               `json:"specSha256"`
	PlanSHA256      string                               `json:"planSha256"`
	LeaseSHA256     string                               `json:"leaseSha256"`
	DispatchSHA256  string                               `json:"dispatchSha256"`
	Candidate       OrchestratorArtifact                 `json:"candidate"`
	Tools           OrchestratorArtifact                 `json:"tools"`
	SurfaceIDs      []string                             `json:"surfaceIds"`
	AllocationAlias string                               `json:"allocationAlias"`
	Evidence        OrchestratorSemanticMismatchEvidence `json:"evidence"`
	EvidenceSHA256  string                               `json:"evidenceSha256"`
	Dispatch        OrchestratorSSHDispatchReceipt       `json:"dispatch"`
}

// OrchestratorSemanticMismatchEvidence is the typed runtime assertion that
// distinguishes a semantic result from an infrastructure dispatch failure.
type OrchestratorSemanticMismatchEvidence struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Status        string               `json:"status"`
	FailureCode   string               `json:"failureCode"`
	CampaignID    string               `json:"campaignId"`
	JobID         string               `json:"jobId"`
	ShardIndex    int                  `json:"shardIndex"`
	Generation    int                  `json:"generation"`
	SpecSHA256    string               `json:"specSha256"`
	PlanSHA256    string               `json:"planSha256"`
	LeaseSHA256   string               `json:"leaseSha256"`
	ResultSHA256  string               `json:"resultSha256"`
	Candidate     OrchestratorArtifact `json:"candidate"`
	Tools         OrchestratorArtifact `json:"tools"`
	SurfaceIDs    []string             `json:"surfaceIds"`
	Expected      string               `json:"expected"`
	Actual        string               `json:"actual"`
	Assertion     string               `json:"assertion"`
	CompilePassed bool                 `json:"compilePassed"`
	ResidueAbsent bool                 `json:"residueAbsent"`
	CleanupClosed bool                 `json:"cleanupClosed"`
}

type OrchestratorSemanticMismatchReceipt struct {
	SchemaVersion      int    `json:"schemaVersion"`
	Status             string `json:"status"`
	FailureCode        string `json:"failureCode"`
	CampaignID         string `json:"campaignId"`
	JobID              string `json:"jobId"`
	Generation         int    `json:"generation"`
	AllocationAlias    string `json:"allocationAlias"`
	AuthoritySHA256    string `json:"authoritySha256"`
	ProofCredit        int    `json:"proofCredit"`
	CleanupCreditBlock int    `json:"cleanupCreditBlock"`
}

func (o *Orchestrator) TerminalizeSemanticMismatch(authority OrchestratorSemanticMismatchAuthority) (OrchestratorSemanticMismatchReceipt, error) {
	if err := validateSemanticMismatchAuthority(authority); err != nil {
		return OrchestratorSemanticMismatchReceipt{}, err
	}
	authoritySHA, err := canonicalJSONHash(authority)
	if err != nil {
		return OrchestratorSemanticMismatchReceipt{}, err
	}
	tx, err := o.db.Begin()
	if err != nil {
		return OrchestratorSemanticMismatchReceipt{}, err
	}
	defer tx.Rollback()
	var candidate, candidateSHA, tools, toolsSHA, spec string
	if err := tx.QueryRow(`SELECT candidate_commit, candidate_sha256, tools_commit, tools_sha256, spec_sha256 FROM campaigns WHERE id = ?`, authority.CampaignID).Scan(&candidate, &candidateSHA, &tools, &toolsSHA, &spec); err != nil {
		return OrchestratorSemanticMismatchReceipt{}, fmt.Errorf("semantic mismatch campaign is unavailable")
	}
	if authority.Candidate != (OrchestratorArtifact{Commit: candidate, SHA256: candidateSHA}) || authority.Tools != (OrchestratorArtifact{Commit: tools, SHA256: toolsSHA}) || authority.SpecSHA256 != spec {
		return OrchestratorSemanticMismatchReceipt{}, fmt.Errorf("semantic mismatch campaign binding drift")
	}
	var kind, surfacesJSON, jobStatus, leasedBy string
	var shard, generation int
	var jobLeaseUntil int64
	if err := tx.QueryRow(`SELECT kind, shard_index, surface_ids_json, status, generation, leased_by, lease_until FROM jobs WHERE campaign_id = ? AND id = ?`, authority.CampaignID, authority.JobID).Scan(&kind, &shard, &surfacesJSON, &jobStatus, &generation, &leasedBy, &jobLeaseUntil); err != nil {
		return OrchestratorSemanticMismatchReceipt{}, fmt.Errorf("semantic mismatch job is unavailable")
	}
	var surfaces []string
	if err := decodeExactJSON([]byte(surfacesJSON), &surfaces); err != nil || kind != string(OrchestratorJobSurfaceRuntimeShard) || shard != authority.ShardIndex || generation != authority.Generation || leasedBy != authority.Worker || !reflect.DeepEqual(surfaces, authority.SurfaceIDs) || jobLeaseUntil != authority.LeaseUntil.UnixMilli() {
		return OrchestratorSemanticMismatchReceipt{}, fmt.Errorf("semantic mismatch lease is stale or does not match current attempt")
	}
	var attemptStatus string
	var attemptLeaseUntil, durationMS int64
	if err := tx.QueryRow(`SELECT status, lease_until FROM attempts WHERE campaign_id = ? AND job_id = ? AND generation = ? AND worker = ?`, authority.CampaignID, authority.JobID, authority.Generation, authority.Worker).Scan(&attemptStatus, &attemptLeaseUntil); err != nil || attemptLeaseUntil != authority.LeaseUntil.UnixMilli() {
		return OrchestratorSemanticMismatchReceipt{}, fmt.Errorf("semantic mismatch lease is stale or does not match current attempt")
	}
	if err := tx.QueryRow(`SELECT duration_ms FROM lease_terms WHERE campaign_id = ? AND job_id = ? AND generation = ?`, authority.CampaignID, authority.JobID, authority.Generation).Scan(&durationMS); err != nil || durationMS != authority.DurationMS {
		return OrchestratorSemanticMismatchReceipt{}, fmt.Errorf("semantic mismatch lease term does not match current attempt")
	}
	if err := validateSemanticMismatchAccounting(tx, authority); err != nil {
		return OrchestratorSemanticMismatchReceipt{}, err
	}
	detail := semanticMismatchAuthorityDetail(authority, authoritySHA)
	prefix := fmt.Sprintf("job %s generation %d: semantic mismatch authority ", authority.JobID, authority.Generation)
	var terminalAuthorities, exactAuthorities int
	if err := tx.QueryRow(`SELECT count(*), count(CASE WHEN detail = ? THEN 1 END) FROM actions WHERE campaign_id = ? AND kind = ? AND substr(detail, 1, ?) = ?`, detail, authority.CampaignID, OrchestratorSemanticMismatchFailureCode, len(prefix), prefix).Scan(&terminalAuthorities, &exactAuthorities); err != nil {
		return OrchestratorSemanticMismatchReceipt{}, err
	}
	if jobStatus != "running" || attemptStatus != "running" {
		if jobStatus == "failed" && attemptStatus == "failed" && terminalAuthorities == 1 && exactAuthorities == 1 {
			return semanticMismatchReceipt(authority, authoritySHA), nil
		}
		if jobStatus == "failed" && attemptStatus == "failed" {
			return OrchestratorSemanticMismatchReceipt{}, fmt.Errorf("semantic mismatch authority differs from terminal record")
		}
		return OrchestratorSemanticMismatchReceipt{}, fmt.Errorf("semantic mismatch requires a running current attempt")
	}
	if terminalAuthorities != 0 {
		return OrchestratorSemanticMismatchReceipt{}, fmt.Errorf("semantic mismatch authority is already recorded")
	}
	if _, err := tx.Exec(`INSERT INTO actions (campaign_id, kind, detail, state, created_at) VALUES (?, ?, ?, 'closed', ?)`, authority.CampaignID, OrchestratorSemanticMismatchFailureCode, detail, time.Now().UTC().UnixMilli()); err != nil {
		return OrchestratorSemanticMismatchReceipt{}, err
	}
	result, err := tx.Exec(`UPDATE jobs SET status = 'failed' WHERE campaign_id = ? AND id = ? AND generation = ? AND status = 'running'`, authority.CampaignID, authority.JobID, authority.Generation)
	if err != nil {
		return OrchestratorSemanticMismatchReceipt{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return OrchestratorSemanticMismatchReceipt{}, fmt.Errorf("semantic mismatch current job changed concurrently")
	}
	result, err = tx.Exec(`UPDATE attempts SET status = 'failed' WHERE campaign_id = ? AND job_id = ? AND generation = ? AND worker = ? AND status = 'running'`, authority.CampaignID, authority.JobID, authority.Generation, authority.Worker)
	if err != nil {
		return OrchestratorSemanticMismatchReceipt{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return OrchestratorSemanticMismatchReceipt{}, fmt.Errorf("semantic mismatch current attempt changed concurrently")
	}
	if err := tx.Commit(); err != nil {
		return OrchestratorSemanticMismatchReceipt{}, err
	}
	return semanticMismatchReceipt(authority, authoritySHA), nil
}

func validateSemanticMismatchAccounting(tx *sql.Tx, authority OrchestratorSemanticMismatchAuthority) error {
	var allocationState, cleanupState string
	if err := tx.QueryRow(`SELECT a.state, c.state FROM scratch_allocations a JOIN cleanup_journal c ON c.allocation_alias = a.allocation_alias AND c.campaign_id = a.campaign_id AND c.job_id = a.job_id AND c.generation = a.generation WHERE a.allocation_alias = ? AND a.campaign_id = ? AND a.job_id = ? AND a.generation = ?`, authority.AllocationAlias, authority.CampaignID, authority.JobID, authority.Generation).Scan(&allocationState, &cleanupState); err != nil || allocationState != "closed" || cleanupState != "closed" {
		return fmt.Errorf("semantic mismatch requires closed allocation and cleanup")
	}
	var receipts, credits, blocks int
	if err := tx.QueryRow(`SELECT count(*) FROM receipts WHERE campaign_id = ? AND job_id = ?`, authority.CampaignID, authority.JobID).Scan(&receipts); err != nil || receipts != 0 {
		return fmt.Errorf("semantic mismatch cannot follow a recorded receipt")
	}
	if err := tx.QueryRow(`SELECT count(*) FROM proof_credits p JOIN receipts r ON r.id = p.receipt_id WHERE r.campaign_id = ? AND r.job_id = ?`, authority.CampaignID, authority.JobID).Scan(&credits); err != nil || credits != 0 {
		return fmt.Errorf("semantic mismatch cannot follow proof credit")
	}
	if err := tx.QueryRow(`SELECT count(*) FROM cleanup_credit_blocks b JOIN cleanup_journal c ON c.allocation_alias = b.allocation_alias WHERE b.allocation_alias = ? AND c.campaign_id = ? AND c.job_id = ? AND c.generation = ?`, authority.AllocationAlias, authority.CampaignID, authority.JobID, authority.Generation).Scan(&blocks); err != nil || blocks != 1 {
		return fmt.Errorf("semantic mismatch requires exactly one permanent zero-credit block")
	}
	return nil
}

func semanticMismatchAuthorityDetail(authority OrchestratorSemanticMismatchAuthority, authoritySHA string) string {
	return fmt.Sprintf("job %s generation %d: semantic mismatch authority %s evidence %s allocation %s", authority.JobID, authority.Generation, authoritySHA, authority.EvidenceSHA256, authority.AllocationAlias)
}

func semanticMismatchReceipt(authority OrchestratorSemanticMismatchAuthority, authoritySHA string) OrchestratorSemanticMismatchReceipt {
	return OrchestratorSemanticMismatchReceipt{SchemaVersion: 1, Status: orchestratorSemanticMismatchTerminalized, FailureCode: OrchestratorSemanticMismatchFailureCode, CampaignID: authority.CampaignID, JobID: authority.JobID, Generation: authority.Generation, AllocationAlias: authority.AllocationAlias, AuthoritySHA256: authoritySHA, ProofCredit: 0, CleanupCreditBlock: 1}
}

func validateSemanticMismatchAuthority(a OrchestratorSemanticMismatchAuthority) error {
	if a.SchemaVersion != 1 || a.Kind != orchestratorSemanticMismatchKind || a.Status != orchestratorSemanticMismatchStatus || a.FailureCode != OrchestratorSemanticMismatchFailureCode || a.CampaignID == "" || a.JobID == "" || a.Generation < 1 || !safeOrchestratorToken(a.Worker) || a.LeaseUntil.IsZero() || a.DurationMS < 1 || len(a.SurfaceIDs) == 0 || !safeOrchestratorToken(a.AllocationAlias) {
		return fmt.Errorf("semantic mismatch authority is invalid")
	}
	for _, hash := range []string{a.SpecSHA256, a.PlanSHA256, a.LeaseSHA256, a.DispatchSHA256, a.EvidenceSHA256, a.Evidence.SpecSHA256, a.Evidence.PlanSHA256, a.Evidence.LeaseSHA256, a.Evidence.ResultSHA256, a.Candidate.SHA256, a.Tools.SHA256, a.Evidence.Candidate.SHA256, a.Evidence.Tools.SHA256} {
		if !sha256Pattern.MatchString(hash) {
			return fmt.Errorf("semantic mismatch authority contains an invalid hash")
		}
	}
	if !commitPattern.MatchString(a.Candidate.Commit) || !commitPattern.MatchString(a.Tools.Commit) || a.Evidence.SchemaVersion != 1 || a.Evidence.Status != orchestratorSemanticMismatchStatus || a.Evidence.FailureCode != OrchestratorSemanticMismatchFailureCode || a.Evidence.CampaignID != a.CampaignID || a.Evidence.JobID != a.JobID || a.Evidence.ShardIndex != a.ShardIndex || a.Evidence.Generation != a.Generation || a.Evidence.SpecSHA256 != a.SpecSHA256 || a.Evidence.PlanSHA256 != a.PlanSHA256 || a.Evidence.LeaseSHA256 != a.LeaseSHA256 || a.Evidence.Candidate != a.Candidate || a.Evidence.Tools != a.Tools || !reflect.DeepEqual(a.Evidence.SurfaceIDs, a.SurfaceIDs) || a.Evidence.Expected == "" || a.Evidence.Actual == "" || a.Evidence.Actual == a.Evidence.Expected || a.Evidence.Assertion == "" || !a.Evidence.CompilePassed || !a.Evidence.ResidueAbsent || !a.Evidence.CleanupClosed {
		return fmt.Errorf("semantic mismatch evidence is not typed")
	}
	evidenceSHA, err := canonicalJSONHash(a.Evidence)
	if err != nil || evidenceSHA != a.EvidenceSHA256 {
		return fmt.Errorf("semantic mismatch evidence binding drift")
	}
	if a.Dispatch.SchemaVersion != 1 || (a.Dispatch.Status != "failed" && a.Dispatch.Status != "timeout") || (a.Dispatch.FailureCode != orchestratorSSHDispatchFailed && a.Dispatch.FailureCode != orchestratorSSHDispatchTimeout) || a.Dispatch.CampaignID != a.CampaignID || a.Dispatch.JobID != a.JobID || a.Dispatch.ShardIndex != a.ShardIndex || a.Dispatch.Generation != a.Generation || a.Dispatch.SpecSHA256 != a.SpecSHA256 || a.Dispatch.PlanSHA256 != a.PlanSHA256 || a.Dispatch.LeaseSHA256 != a.LeaseSHA256 {
		return fmt.Errorf("semantic mismatch authority dispatch is not typed")
	}
	dispatchSHA, err := canonicalJSONHash(a.Dispatch)
	if err != nil || dispatchSHA != a.DispatchSHA256 {
		return fmt.Errorf("semantic mismatch dispatch binding drift")
	}
	return nil
}
