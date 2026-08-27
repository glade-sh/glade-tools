package corpusassurance

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Orchestrator struct {
	db *sql.DB
}

// OrchestratorHubObservation is the sanitized coordinator-local health
// snapshot used before allocating a scratch org. Quota pointers distinguish a
// reported zero from an unavailable limit.
type OrchestratorHubObservation struct {
	HubAlias                   string    `json:"hubAlias"`
	ObservedAt                 time.Time `json:"observedAt"`
	Healthy                    bool      `json:"healthy"`
	Quarantined                bool      `json:"quarantined"`
	DailyScratchOrgsRemaining  *int      `json:"dailyScratchOrgsRemaining"`
	ActiveScratchOrgsRemaining *int      `json:"activeScratchOrgsRemaining"`
}

const orchestratorHubObservationMaxAge = 2 * time.Minute

type OrchestratorArtifact struct {
	Commit string `json:"commit"`
	SHA256 string `json:"sha256"`
}

const OrchestratorToolsAMD64Input = "tools-darwin-amd64"

type OrchestratorCampaignDefinition struct {
	Candidate             OrchestratorArtifact `json:"candidate"`
	Tools                 OrchestratorArtifact `json:"tools"`
	ScopePath             string               `json:"scopePath"`
	ScopeSHA256           string               `json:"scopeSha256"`
	ControlledInputSHA256 map[string]string    `json:"controlledInputSha256"`
	Shards                [][]string           `json:"shards"`
}

type OrchestratorJobKind string

const OrchestratorJobSurfaceRuntimeShard OrchestratorJobKind = "surface-runtime-shard"

type OrchestratorJob struct {
	ID         string              `json:"id"`
	Kind       OrchestratorJobKind `json:"kind"`
	ShardIndex int                 `json:"shardIndex"`
	SurfaceIDs []string            `json:"surfaceIds"`
}

type OrchestratorCampaignPlan struct {
	CampaignID        string                         `json:"campaignId"`
	SpecSHA256        string                         `json:"specSha256"`
	MaxAttemptsPerJob int                            `json:"maxAttemptsPerJob,omitempty"`
	Definition        OrchestratorCampaignDefinition `json:"definition"`
	Jobs              []OrchestratorJob              `json:"jobs"`
}

const DefaultOrchestratorMaxAttemptsPerJob = 3

func normalizedOrchestratorMaxAttemptsPerJob(value int) (int, error) {
	if value == 0 {
		return DefaultOrchestratorMaxAttemptsPerJob, nil
	}
	if value < 1 {
		return 0, fmt.Errorf("max attempts per job must be positive")
	}
	return value, nil
}

type OrchestratorLease struct {
	CampaignID string              `json:"campaignId"`
	JobID      string              `json:"jobId"`
	Kind       OrchestratorJobKind `json:"kind"`
	ShardIndex int                 `json:"shardIndex"`
	SurfaceIDs []string            `json:"surfaceIds"`
	Generation int                 `json:"generation"`
	Worker     string              `json:"worker"`
	LeaseUntil time.Time           `json:"leaseUntil"`
	DurationMS int64               `json:"durationMs"`
}

type OrchestratorCleanupClaim struct {
	CampaignID      string    `json:"campaignId"`
	JobID           string    `json:"jobId"`
	Generation      int       `json:"generation"`
	AllocationAlias string    `json:"allocationAlias"`
	HubAlias        string    `json:"hubAlias"`
	Worker          string    `json:"worker"`
	ClaimUntil      time.Time `json:"claimUntil"`
}

type OrchestratorReceiptRequest struct {
	Lease     OrchestratorLease `json:"lease"`
	BatchRoot string            `json:"batchRoot"`
}

type OrchestratorReceipt struct {
	ID             string `json:"id"`
	CampaignID     string `json:"campaignId"`
	JobID          string `json:"jobId"`
	Generation     int    `json:"generation"`
	BatchRoot      string `json:"batchRoot"`
	ManifestSHA256 string `json:"manifestSha256"`
	BindingSHA256  string `json:"bindingSha256"`
	AcceptedCredit int    `json:"acceptedCredit"`
	RejectedCredit int    `json:"rejectedCredit"`
}

type OrchestratorCampaignStatus struct {
	CampaignID  string               `json:"campaignId"`
	Candidate   OrchestratorArtifact `json:"candidate"`
	Queued      int                  `json:"queued"`
	Running     int                  `json:"running"`
	Retryable   int                  `json:"retryable"`
	Failed      int                  `json:"failed"`
	Closed      int                  `json:"closed"`
	Accepted    int                  `json:"accepted"`
	Rejected    int                  `json:"rejected"`
	Unseen      int                  `json:"unseen"`
	CleanupOpen int                  `json:"cleanupOpen"`
}

type OrchestratorStatusCredit struct {
	Candidate string `json:"candidate"`
	Credit    int    `json:"credit"`
	Total     int    `json:"total"`
}

type OrchestratorStatusSnapshot struct {
	Current           OrchestratorStatusCredit `json:"current"`
	Historical        OrchestratorStatusCredit `json:"historical"`
	Accounted         int                      `json:"accounted"`
	DirectLocal       int                      `json:"directLocal"`
	TerminalLocalOnly int                      `json:"terminalLocalOnly"`
}

func ValidateOrchestratorStatus(status OrchestratorStatusSnapshot, currentCandidate string, acceptedCredit int, expectedHistorical OrchestratorStatusCredit) error {
	if status.Current.Candidate != currentCandidate || status.Current.Total <= 0 || status.Current.Credit < 0 || status.Current.Credit > status.Current.Total || status.Current.Credit != acceptedCredit {
		return fmt.Errorf("current candidate credit does not match orchestrator state")
	}
	if status.Historical != expectedHistorical || expectedHistorical.Candidate == "" || expectedHistorical.Candidate == currentCandidate || expectedHistorical.Total != status.Current.Total || expectedHistorical.Credit <= 0 || expectedHistorical.Credit > expectedHistorical.Total {
		return fmt.Errorf("historical evidence requires a distinct candidate binding")
	}
	if status.Accounted != status.Current.Total || status.DirectLocal < 0 || status.TerminalLocalOnly < 0 || status.DirectLocal+status.TerminalLocalOnly != status.Accounted {
		return fmt.Errorf("status denominators do not reconcile")
	}
	return nil
}

type OrchestratorBatchBinding struct {
	SchemaVersion         int                  `json:"schemaVersion"`
	CampaignID            string               `json:"campaignId"`
	SpecSHA256            string               `json:"specSha256"`
	Candidate             OrchestratorArtifact `json:"candidate"`
	Tools                 OrchestratorArtifact `json:"tools"`
	ScopeSHA256           string               `json:"scopeSha256"`
	ControlledInputSHA256 map[string]string    `json:"controlledInputSha256"`
	JobID                 string               `json:"jobId"`
	JobKind               OrchestratorJobKind  `json:"jobKind"`
	Generation            int                  `json:"generation"`
	ShardIndex            int                  `json:"shardIndex"`
	SurfaceIDs            []string             `json:"surfaceIds"`
}

func OpenOrchestrator(path string) (*Orchestrator, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("absolute orchestrator database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	orchestrator := &Orchestrator{db: db}
	for _, statement := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		orchestratorSchema,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize orchestrator: %w", err)
		}
	}
	if err := migrateOrchestratorJobs(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate orchestrator schema: %w", err)
	}
	if err := ensureHubObservationColumns(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize hub observations: %w", err)
	}
	if err := ensureCampaignAttemptCapColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize campaign attempt cap: %w", err)
	}
	return orchestrator, nil
}

func (o *Orchestrator) Close() error { return o.db.Close() }

func PlanOrchestratorCampaign(definition OrchestratorCampaignDefinition) (OrchestratorCampaignPlan, error) {
	return planOrchestratorCampaignAtScope(definition, definition.ScopePath)
}

func planOrchestratorCampaignAtScope(definition OrchestratorCampaignDefinition, scopePath string) (OrchestratorCampaignPlan, error) {
	if err := validateOrchestratorDefinitionAtScope(definition, scopePath); err != nil {
		return OrchestratorCampaignPlan{}, err
	}
	definition.ControlledInputSHA256 = maps.Clone(definition.ControlledInputSHA256)
	definition.Shards = append([][]string(nil), definition.Shards...)
	for i := range definition.Shards {
		definition.Shards[i] = append([]string(nil), definition.Shards[i]...)
		sort.Strings(definition.Shards[i])
	}
	data, _ := json.Marshal(definition)
	specSHA := replayBytesSHA256(data)
	campaignID := "campaign-" + specSHA[:16]
	jobs := make([]OrchestratorJob, len(definition.Shards))
	for i := range jobs {
		jobs[i] = OrchestratorJob{ID: fmt.Sprintf("%s:%s:%d", campaignID, OrchestratorJobSurfaceRuntimeShard, i), Kind: OrchestratorJobSurfaceRuntimeShard, ShardIndex: i, SurfaceIDs: append([]string(nil), definition.Shards[i]...)}
	}
	return OrchestratorCampaignPlan{CampaignID: campaignID, SpecSHA256: specSHA, MaxAttemptsPerJob: DefaultOrchestratorMaxAttemptsPerJob, Definition: definition, Jobs: jobs}, nil
}

func WriteOrchestratorBatchBinding(path string, plan OrchestratorCampaignPlan, lease OrchestratorLease) (OrchestratorBatchBinding, error) {
	return writeOrchestratorBatchBindingAtScope(path, plan, lease, plan.Definition.ScopePath)
}

func writeOrchestratorBatchBindingAtScope(path string, plan OrchestratorCampaignPlan, lease OrchestratorLease, scopePath string) (OrchestratorBatchBinding, error) {
	if !filepath.IsAbs(path) {
		return OrchestratorBatchBinding{}, fmt.Errorf("absolute orchestrator batch binding path is required")
	}
	if err := validateOrchestratorPlanAtScope(plan, scopePath); err != nil {
		return OrchestratorBatchBinding{}, err
	}
	var job *OrchestratorJob
	for i := range plan.Jobs {
		if plan.Jobs[i].ID == lease.JobID {
			job = &plan.Jobs[i]
			break
		}
	}
	if job == nil || lease.CampaignID != plan.CampaignID || lease.Generation < 1 || lease.Kind != job.Kind || lease.ShardIndex != job.ShardIndex || !reflect.DeepEqual(lease.SurfaceIDs, job.SurfaceIDs) {
		return OrchestratorBatchBinding{}, fmt.Errorf("lease does not match immutable campaign job")
	}
	binding := OrchestratorBatchBinding{
		SchemaVersion: 1, CampaignID: plan.CampaignID, SpecSHA256: plan.SpecSHA256,
		Candidate: plan.Definition.Candidate, Tools: plan.Definition.Tools,
		ScopeSHA256: plan.Definition.ScopeSHA256, ControlledInputSHA256: maps.Clone(plan.Definition.ControlledInputSHA256),
		JobID: job.ID, JobKind: job.Kind, Generation: lease.Generation, ShardIndex: job.ShardIndex, SurfaceIDs: append([]string(nil), job.SurfaceIDs...),
	}
	if err := WriteNewJSON(path, binding); err != nil {
		return OrchestratorBatchBinding{}, err
	}
	if err := os.Chmod(path, 0o400); err != nil {
		_ = os.Remove(path)
		return OrchestratorBatchBinding{}, err
	}
	return binding, nil
}

func (o *Orchestrator) InitCampaign(plan OrchestratorCampaignPlan) error {
	if err := validateOrchestratorPlan(plan); err != nil {
		return err
	}
	maxAttempts, err := normalizedOrchestratorMaxAttemptsPerJob(plan.MaxAttemptsPerJob)
	if err != nil {
		return err
	}
	inputs, _ := json.Marshal(plan.Definition.ControlledInputSHA256)
	tx, err := o.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var storedSpec, candidateCommit, candidateSHA, toolsCommit, toolsSHA, scopePath, scopeSHA, storedInputs string
	var storedMaxAttempts int
	err = tx.QueryRow(`SELECT spec_sha256, candidate_commit, candidate_sha256, tools_commit, tools_sha256, scope_path, scope_sha256, controlled_inputs_json, max_attempts_per_job FROM campaigns WHERE id = ?`, plan.CampaignID).Scan(&storedSpec, &candidateCommit, &candidateSHA, &toolsCommit, &toolsSHA, &scopePath, &scopeSHA, &storedInputs, &storedMaxAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.Exec(`INSERT INTO campaigns (id, spec_sha256, candidate_commit, candidate_sha256, tools_commit, tools_sha256, scope_path, scope_sha256, controlled_inputs_json, max_attempts_per_job, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, plan.CampaignID, plan.SpecSHA256, plan.Definition.Candidate.Commit, plan.Definition.Candidate.SHA256, plan.Definition.Tools.Commit, plan.Definition.Tools.SHA256, plan.Definition.ScopePath, plan.Definition.ScopeSHA256, string(inputs), maxAttempts, time.Now().UTC().UnixMilli()); err != nil {
			return fmt.Errorf("initialize immutable campaign: %w", err)
		}
	} else if err != nil || storedSpec != plan.SpecSHA256 || candidateCommit != plan.Definition.Candidate.Commit || candidateSHA != plan.Definition.Candidate.SHA256 || toolsCommit != plan.Definition.Tools.Commit || toolsSHA != plan.Definition.Tools.SHA256 || scopePath != plan.Definition.ScopePath || scopeSHA != plan.Definition.ScopeSHA256 || storedInputs != string(inputs) || storedMaxAttempts != maxAttempts {
		return fmt.Errorf("campaign binding drift")
	}
	for _, job := range plan.Jobs {
		surfaces, _ := json.Marshal(job.SurfaceIDs)
		var kind string
		var shard int
		var storedSurfaces string
		err := tx.QueryRow(`SELECT kind, shard_index, surface_ids_json FROM jobs WHERE campaign_id = ? AND id = ?`, plan.CampaignID, job.ID).Scan(&kind, &shard, &storedSurfaces)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := tx.Exec(`INSERT INTO jobs (campaign_id, id, kind, shard_index, surface_ids_json, status, generation) VALUES (?, ?, ?, ?, ?, 'queued', 0)`, plan.CampaignID, job.ID, job.Kind, job.ShardIndex, string(surfaces)); err != nil {
				return fmt.Errorf("initialize immutable job: %w", err)
			}
		} else if err != nil || kind != string(job.Kind) || shard != job.ShardIndex || storedSurfaces != string(surfaces) {
			return fmt.Errorf("campaign job binding drift")
		}
	}
	return tx.Commit()
}

func (o *Orchestrator) Enqueue(plan OrchestratorCampaignPlan) error {
	return o.InitCampaign(plan)
}

func (o *Orchestrator) Lease(campaignID, worker string, now time.Time, duration time.Duration) (OrchestratorLease, error) {
	if campaignID == "" || !safeOrchestratorToken(worker) || duration <= 0 {
		return OrchestratorLease{}, fmt.Errorf("campaign, safe worker, and positive lease duration are required")
	}
	tx, err := o.db.Begin()
	if err != nil {
		return OrchestratorLease{}, err
	}
	defer tx.Rollback()
	var maxAttempts int
	if err := tx.QueryRow(`SELECT max_attempts_per_job FROM campaigns WHERE id = ?`, campaignID).Scan(&maxAttempts); err != nil {
		return OrchestratorLease{}, fmt.Errorf("read campaign attempt cap: %w", err)
	}
	if maxAttempts, err = normalizedOrchestratorMaxAttemptsPerJob(maxAttempts); err != nil {
		return OrchestratorLease{}, err
	}
	result, err := tx.Exec(`UPDATE jobs SET status = 'failed' WHERE campaign_id = ? AND status = 'running' AND generation >= ? AND lease_until <= ?`, campaignID, maxAttempts, now.UTC().UnixMilli())
	if err != nil {
		return OrchestratorLease{}, fmt.Errorf("close capped orchestrator jobs: %w", err)
	}
	cappedJobs, _ := result.RowsAffected()
	if _, err := tx.Exec(`UPDATE attempts SET status = 'failed' WHERE campaign_id = ? AND status = 'running' AND generation >= ? AND EXISTS (SELECT 1 FROM jobs WHERE jobs.campaign_id = attempts.campaign_id AND jobs.id = attempts.job_id AND jobs.generation = attempts.generation AND jobs.status = 'failed')`, campaignID, maxAttempts); err != nil {
		return OrchestratorLease{}, fmt.Errorf("close capped orchestrator attempts: %w", err)
	}
	var lease OrchestratorLease
	var kind string
	var surfaces string
	var previousStatus string
	if err := tx.QueryRow(`SELECT id, kind, shard_index, surface_ids_json, generation, status FROM jobs WHERE campaign_id = ? AND (status = 'queued' OR (status = 'running' AND lease_until <= ? AND generation < ?)) ORDER BY CASE status WHEN 'running' THEN 0 ELSE 1 END, shard_index LIMIT 1`, campaignID, now.UTC().UnixMilli(), maxAttempts).Scan(&lease.JobID, &kind, &lease.ShardIndex, &surfaces, &lease.Generation, &previousStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if cappedJobs > 0 {
				if err := tx.Commit(); err != nil {
					return OrchestratorLease{}, err
				}
				return OrchestratorLease{}, fmt.Errorf("no leaseable orchestrator job: attempt cap exhausted")
			}
			return OrchestratorLease{}, fmt.Errorf("no leaseable orchestrator job")
		}
		return OrchestratorLease{}, err
	}
	lease.CampaignID, lease.Kind, lease.Worker = campaignID, OrchestratorJobKind(kind), worker
	lease.Generation++
	lease.LeaseUntil = now.UTC().Add(duration)
	lease.DurationMS = duration.Milliseconds()
	if err := json.Unmarshal([]byte(surfaces), &lease.SurfaceIDs); err != nil {
		return OrchestratorLease{}, err
	}
	if previousStatus == "running" {
		if _, err := tx.Exec(`UPDATE attempts SET status = 'retryable' WHERE campaign_id = ? AND job_id = ? AND generation = ? AND status = 'running'`, campaignID, lease.JobID, lease.Generation-1); err != nil {
			return OrchestratorLease{}, err
		}
	}
	result, err = tx.Exec(`UPDATE jobs SET status = 'running', generation = ?, leased_by = ?, lease_until = ?, heartbeat_at = ? WHERE campaign_id = ? AND id = ? AND generation = ?`, lease.Generation, worker, lease.LeaseUntil.UnixMilli(), now.UTC().UnixMilli(), campaignID, lease.JobID, lease.Generation-1)
	if err != nil {
		return OrchestratorLease{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return OrchestratorLease{}, fmt.Errorf("job lease changed concurrently")
	}
	if _, err := tx.Exec(`INSERT INTO attempts (campaign_id, job_id, generation, worker, status, leased_at, lease_until, heartbeat_at) VALUES (?, ?, ?, ?, 'running', ?, ?, ?)`, campaignID, lease.JobID, lease.Generation, worker, now.UTC().UnixMilli(), lease.LeaseUntil.UnixMilli(), now.UTC().UnixMilli()); err != nil {
		return OrchestratorLease{}, fmt.Errorf("record campaign-bound attempt: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO lease_terms (campaign_id, job_id, generation, duration_ms) VALUES (?, ?, ?, ?)`, campaignID, lease.JobID, lease.Generation, lease.DurationMS); err != nil {
		return OrchestratorLease{}, fmt.Errorf("record campaign-bound lease term: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return OrchestratorLease{}, err
	}
	return lease, nil
}

func (o *Orchestrator) issuedLeaseDuration(lease OrchestratorLease) (time.Duration, error) {
	var milliseconds int64
	if err := o.db.QueryRow(`SELECT duration_ms FROM lease_terms WHERE campaign_id = ? AND job_id = ? AND generation = ?`, lease.CampaignID, lease.JobID, lease.Generation).Scan(&milliseconds); err != nil || milliseconds < 1 {
		return 0, fmt.Errorf("issued orchestrator lease term not found")
	}
	return time.Duration(milliseconds) * time.Millisecond, nil
}

func (o *Orchestrator) Heartbeat(lease OrchestratorLease, now time.Time, duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("positive heartbeat duration is required")
	}
	tx, err := o.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	until := now.UTC().Add(duration).UnixMilli()
	result, err := tx.Exec(`UPDATE jobs SET heartbeat_at = ?, lease_until = ? WHERE campaign_id = ? AND id = ? AND generation = ? AND leased_by = ? AND status = 'running' AND lease_until > ?`, now.UTC().UnixMilli(), until, lease.CampaignID, lease.JobID, lease.Generation, lease.Worker, now.UTC().UnixMilli())
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("stale or expired orchestrator lease")
	}
	if _, err := tx.Exec(`UPDATE attempts SET heartbeat_at = ?, lease_until = ? WHERE campaign_id = ? AND job_id = ? AND generation = ? AND worker = ? AND status = 'running'`, now.UTC().UnixMilli(), until, lease.CampaignID, lease.JobID, lease.Generation, lease.Worker); err != nil {
		return err
	}
	return tx.Commit()
}

func (o *Orchestrator) SetHubCapacity(hubAlias string, capacity int) error {
	if !safeOrchestratorToken(hubAlias) || capacity < 1 {
		return fmt.Errorf("safe hub alias and positive capacity are required")
	}
	_, err := o.db.Exec(`INSERT INTO hub_capacity (hub_alias, capacity, reserved) VALUES (?, ?, 0) ON CONFLICT(hub_alias) DO NOTHING`, hubAlias, capacity)
	if err != nil {
		return err
	}
	var stored int
	if err := o.db.QueryRow(`SELECT capacity FROM hub_capacity WHERE hub_alias = ?`, hubAlias).Scan(&stored); err != nil || stored != capacity {
		return fmt.Errorf("hub capacity is immutable")
	}
	return nil
}

func (o *Orchestrator) ObserveHub(observation OrchestratorHubObservation) error {
	if err := validateHubObservation(observation); err != nil {
		return err
	}
	_, err := o.db.Exec(`INSERT INTO hub_observations (hub_alias, observed_at, healthy, quarantined, daily_scratch_orgs_remaining, active_scratch_orgs_remaining) VALUES (?, ?, ?, ?, ?, ?)`, observation.HubAlias, observation.ObservedAt.UTC().UnixMilli(), boolInt(observation.Healthy), boolInt(observation.Quarantined), nullableInt(observation.DailyScratchOrgsRemaining), nullableInt(observation.ActiveScratchOrgsRemaining))
	return err
}

func ReadOrchestratorHubObservation(path string) (OrchestratorHubObservation, error) {
	var observation OrchestratorHubObservation
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return observation, fmt.Errorf("absolute clean hub observation path is required")
	}
	snapshot, err := readRegularFileSnapshot(path)
	if err != nil || snapshot.Mode.Perm() != 0o600 {
		return observation, fmt.Errorf("hub observation must be a mode 0600 regular file")
	}
	if err := decodeExactJSON(snapshot.Data, &observation); err != nil {
		return observation, fmt.Errorf("decode hub observation: %w", err)
	}
	if err := validateHubObservation(observation); err != nil {
		return observation, err
	}
	return observation, nil
}

func validateHubObservation(observation OrchestratorHubObservation) error {
	if !safeOrchestratorToken(observation.HubAlias) || observation.ObservedAt.IsZero() {
		return fmt.Errorf("safe hub alias and observation time are required")
	}
	for name, remaining := range map[string]*int{
		"DailyScratchOrgs":  observation.DailyScratchOrgsRemaining,
		"ActiveScratchOrgs": observation.ActiveScratchOrgsRemaining,
	} {
		if remaining != nil && *remaining < 0 {
			return fmt.Errorf("%s remaining cannot be negative", name)
		}
	}
	return nil
}

func (o *Orchestrator) requireReadyHub(tx *sql.Tx, hubAlias string, now time.Time) (int64, int64, error) {
	var observationID int64
	var observedAt int64
	var healthy, quarantined, consumed int
	var daily, active sql.NullInt64
	err := tx.QueryRow(`SELECT id, observed_at, healthy, quarantined, consumed, daily_scratch_orgs_remaining, active_scratch_orgs_remaining FROM hub_observations WHERE hub_alias = ? ORDER BY id DESC LIMIT 1`, hubAlias).Scan(&observationID, &observedAt, &healthy, &quarantined, &consumed, &daily, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, fmt.Errorf("hub readiness observation is missing")
	}
	if err != nil {
		return 0, 0, err
	}
	age := now.UTC().UnixMilli() - observedAt
	if age < 0 || age > orchestratorHubObservationMaxAge.Milliseconds() {
		return 0, 0, fmt.Errorf("hub readiness observation is stale")
	}
	if healthy != 1 {
		return 0, 0, fmt.Errorf("hub is unhealthy")
	}
	if quarantined != 0 {
		return 0, 0, fmt.Errorf("hub is quarantined")
	}
	if consumed != 0 {
		return 0, 0, fmt.Errorf("hub readiness observation is already consumed")
	}
	if !daily.Valid || !active.Valid {
		return 0, 0, fmt.Errorf("hub scratch-org quotas are unavailable")
	}
	var reserved int64
	if err := tx.QueryRow(`SELECT reserved FROM hub_capacity WHERE hub_alias = ?`, hubAlias).Scan(&reserved); err != nil {
		return 0, 0, fmt.Errorf("hub capacity is unavailable")
	}
	if daily.Int64 <= reserved || active.Int64 <= reserved {
		return 0, 0, fmt.Errorf("hub capacity or scratch-org quota is exhausted")
	}
	result, err := tx.Exec(`UPDATE hub_observations SET consumed = 1 WHERE id = ? AND consumed = 0`, observationID)
	if err != nil {
		return 0, 0, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return 0, 0, fmt.Errorf("hub readiness observation changed concurrently")
	}
	return daily.Int64, active.Int64, nil
}

func (o *Orchestrator) Reserve(lease OrchestratorLease, hubAlias, allocationAlias string, now time.Time) error {
	if !safeOrchestratorToken(hubAlias) || !safeOrchestratorToken(allocationAlias) {
		return fmt.Errorf("safe hub and allocation aliases are required")
	}
	tx, err := o.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int
	if err := tx.QueryRow(`SELECT count(*) FROM jobs WHERE campaign_id = ? AND id = ? AND generation = ? AND leased_by = ? AND status = 'running' AND lease_until > ?`, lease.CampaignID, lease.JobID, lease.Generation, lease.Worker, now.UTC().UnixMilli()).Scan(&current); err != nil || current != 1 {
		return fmt.Errorf("reservation requires current campaign lease")
	}
	dailyRemaining, activeRemaining, err := o.requireReadyHub(tx, hubAlias, now)
	if err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE hub_capacity SET reserved = reserved + 1 WHERE hub_alias = ? AND reserved < capacity AND reserved < ? AND reserved < ?`, hubAlias, dailyRemaining, activeRemaining)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("hub capacity or observed scratch-org quota is exhausted")
	}
	if _, err := tx.Exec(`INSERT INTO scratch_allocations (allocation_alias, hub_alias, campaign_id, job_id, generation, state, reserved_at) VALUES (?, ?, ?, ?, ?, 'reserved', ?)`, allocationAlias, hubAlias, lease.CampaignID, lease.JobID, lease.Generation, now.UTC().UnixMilli()); err != nil {
		return fmt.Errorf("reserve global allocation alias: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO cleanup_journal (allocation_alias, campaign_id, job_id, generation, state) VALUES (?, ?, ?, ?, 'pending')`, allocationAlias, lease.CampaignID, lease.JobID, lease.Generation); err != nil {
		return err
	}
	return tx.Commit()
}

func (o *Orchestrator) ClaimCleanup(campaignID, worker string, now time.Time, duration time.Duration) (OrchestratorCleanupClaim, error) {
	if campaignID == "" || !safeOrchestratorToken(worker) || duration <= 0 {
		return OrchestratorCleanupClaim{}, fmt.Errorf("campaign, safe worker, and positive cleanup lease are required")
	}
	tx, err := o.db.Begin()
	if err != nil {
		return OrchestratorCleanupClaim{}, err
	}
	defer tx.Rollback()
	var claim OrchestratorCleanupClaim
	if err := tx.QueryRow(`SELECT c.allocation_alias, c.job_id, c.generation, a.hub_alias FROM cleanup_journal c JOIN scratch_allocations a ON a.allocation_alias = c.allocation_alias JOIN jobs j ON j.campaign_id = c.campaign_id AND j.id = c.job_id WHERE c.campaign_id = ? AND (c.state = 'pending' OR (c.state = 'running' AND c.claim_until <= ?)) AND (j.generation != c.generation OR j.lease_until <= ? OR j.leased_by = ?) ORDER BY a.reserved_at LIMIT 1`, campaignID, now.UTC().UnixMilli(), now.UTC().UnixMilli(), worker).Scan(&claim.AllocationAlias, &claim.JobID, &claim.Generation, &claim.HubAlias); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OrchestratorCleanupClaim{}, fmt.Errorf("no cleanup journal is claimable")
		}
		return OrchestratorCleanupClaim{}, err
	}
	claim.CampaignID, claim.Worker, claim.ClaimUntil = campaignID, worker, now.UTC().Add(duration)
	result, err := tx.Exec(`UPDATE cleanup_journal SET state = 'running', claimed_by = ?, claim_until = ? WHERE allocation_alias = ? AND campaign_id = ? AND generation = ? AND (state = 'pending' OR (state = 'running' AND claim_until <= ?))`, worker, claim.ClaimUntil.UnixMilli(), claim.AllocationAlias, campaignID, claim.Generation, now.UTC().UnixMilli())
	if err != nil {
		return OrchestratorCleanupClaim{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return OrchestratorCleanupClaim{}, fmt.Errorf("cleanup journal changed concurrently")
	}
	if err := tx.Commit(); err != nil {
		return OrchestratorCleanupClaim{}, err
	}
	return claim, nil
}

// ClaimCleanupForLease claims only the allocation reserved by the current
// lease. Campaign-wide ClaimCleanup remains the expired-lease takeover path.
func (o *Orchestrator) ClaimCleanupForLease(lease OrchestratorLease, allocationAlias string, now time.Time, duration time.Duration) (OrchestratorCleanupClaim, error) {
	if !safeOrchestratorToken(lease.Worker) || !safeOrchestratorToken(allocationAlias) || lease.DurationMS <= 0 || lease.LeaseUntil.IsZero() || duration <= 0 {
		return OrchestratorCleanupClaim{}, fmt.Errorf("current lease, safe allocation, and positive cleanup lease are required")
	}
	surfaces, err := json.Marshal(lease.SurfaceIDs)
	if err != nil {
		return OrchestratorCleanupClaim{}, err
	}
	tx, err := o.db.Begin()
	if err != nil {
		return OrchestratorCleanupClaim{}, err
	}
	defer tx.Rollback()
	claim := OrchestratorCleanupClaim{
		CampaignID: lease.CampaignID, JobID: lease.JobID, Generation: lease.Generation,
		AllocationAlias: allocationAlias, Worker: lease.Worker, ClaimUntil: now.UTC().Add(duration),
	}
	if err := tx.QueryRow(`SELECT a.hub_alias FROM cleanup_journal c JOIN scratch_allocations a ON a.allocation_alias = c.allocation_alias JOIN jobs j ON j.campaign_id = c.campaign_id AND j.id = c.job_id JOIN attempts x ON x.campaign_id = c.campaign_id AND x.job_id = c.job_id AND x.generation = c.generation JOIN lease_terms l ON l.campaign_id = c.campaign_id AND l.job_id = c.job_id AND l.generation = c.generation WHERE c.allocation_alias = ? AND c.campaign_id = ? AND c.job_id = ? AND c.generation = ? AND (c.state = 'pending' OR (c.state = 'running' AND c.claim_until <= ?)) AND j.kind = ? AND j.shard_index = ? AND j.surface_ids_json = ? AND j.generation = c.generation AND j.leased_by = ? AND j.status = 'running' AND j.lease_until = ? AND j.lease_until > ? AND x.worker = ? AND x.status = 'running' AND x.lease_until = ? AND l.duration_ms = ?`, allocationAlias, lease.CampaignID, lease.JobID, lease.Generation, now.UTC().UnixMilli(), lease.Kind, lease.ShardIndex, string(surfaces), lease.Worker, lease.LeaseUntil.UTC().UnixMilli(), now.UTC().UnixMilli(), lease.Worker, lease.LeaseUntil.UTC().UnixMilli(), lease.DurationMS).Scan(&claim.HubAlias); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OrchestratorCleanupClaim{}, fmt.Errorf("current lease cleanup journal is not claimable")
		}
		return OrchestratorCleanupClaim{}, err
	}
	result, err := tx.Exec(`UPDATE cleanup_journal SET state = 'running', claimed_by = ?, claim_until = ? WHERE allocation_alias = ? AND campaign_id = ? AND job_id = ? AND generation = ? AND (state = 'pending' OR (state = 'running' AND claim_until <= ?))`, lease.Worker, claim.ClaimUntil.UnixMilli(), allocationAlias, lease.CampaignID, lease.JobID, lease.Generation, now.UTC().UnixMilli())
	if err != nil {
		return OrchestratorCleanupClaim{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return OrchestratorCleanupClaim{}, fmt.Errorf("current lease cleanup journal changed concurrently")
	}
	if err := tx.Commit(); err != nil {
		return OrchestratorCleanupClaim{}, err
	}
	return claim, nil
}

// closeRawAcceptanceCleanup atomically releases the exact allocation bound to
// a fully validated raw worker result, even after its SSH lease has expired.
func (o *Orchestrator) closeRawAcceptanceCleanup(lease OrchestratorLease, allocationAlias string, now time.Time, proofEligible bool) error {
	if !safeOrchestratorToken(lease.Worker) || !safeOrchestratorToken(allocationAlias) {
		return fmt.Errorf("validated raw cleanup close is invalid")
	}
	tx, err := o.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hubAlias string
	if err := tx.QueryRow(`SELECT s.hub_alias FROM cleanup_journal c JOIN scratch_allocations s ON s.allocation_alias = c.allocation_alias JOIN jobs j ON j.campaign_id = c.campaign_id AND j.id = c.job_id JOIN attempts a ON a.campaign_id = c.campaign_id AND a.job_id = c.job_id AND a.generation = c.generation WHERE c.allocation_alias = ? AND c.campaign_id = ? AND c.job_id = ? AND c.generation = ? AND (c.state = 'pending' OR (c.state = 'running' AND c.claim_until <= ?)) AND s.state = 'reserved' AND j.generation = c.generation AND j.leased_by = ? AND j.status = 'running' AND j.lease_until = ? AND a.worker = ? AND a.status = 'running' AND a.lease_until = ?`, allocationAlias, lease.CampaignID, lease.JobID, lease.Generation, now.UTC().UnixMilli(), lease.Worker, lease.LeaseUntil.UTC().UnixMilli(), lease.Worker, lease.LeaseUntil.UTC().UnixMilli()).Scan(&hubAlias); err != nil {
		return fmt.Errorf("validated raw cleanup journal is not closable")
	}
	if !proofEligible {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO cleanup_credit_blocks (allocation_alias) VALUES (?)`, allocationAlias); err != nil {
			return err
		}
	}
	result, err := tx.Exec(`UPDATE cleanup_journal SET state = 'closed', closed_at = ? WHERE allocation_alias = ? AND campaign_id = ? AND job_id = ? AND generation = ? AND (state = 'pending' OR (state = 'running' AND claim_until <= ?))`, now.UTC().UnixMilli(), allocationAlias, lease.CampaignID, lease.JobID, lease.Generation, now.UTC().UnixMilli())
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("validated raw cleanup journal changed concurrently")
	}
	result, err = tx.Exec(`UPDATE scratch_allocations SET state = 'closed' WHERE allocation_alias = ? AND state = 'reserved'`, allocationAlias)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("validated raw cleanup allocation is not reserved")
	}
	result, err = tx.Exec(`UPDATE hub_capacity SET reserved = reserved - 1 WHERE hub_alias = ? AND reserved > 0`, hubAlias)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("validated raw cleanup capacity is unavailable")
	}
	actionPrefix := fmt.Sprintf("job %s generation %d: scratch cleanup action required; journal ", lease.JobID, lease.Generation)
	if _, err := tx.Exec(`UPDATE actions SET state = 'closed' WHERE campaign_id = ? AND state = 'open' AND substr(detail, 1, ?) = ?`, lease.CampaignID, len(actionPrefix), actionPrefix); err != nil {
		return err
	}
	return tx.Commit()
}

func (o *Orchestrator) CloseCleanup(claim OrchestratorCleanupClaim, now time.Time) error {
	return o.closeCleanup(claim, now, true)
}

func (o *Orchestrator) closeCleanup(claim OrchestratorCleanupClaim, now time.Time, proofEligible bool) error {
	tx, err := o.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hubAlias string
	if err := tx.QueryRow(`SELECT a.hub_alias FROM cleanup_journal c JOIN scratch_allocations a ON a.allocation_alias = c.allocation_alias WHERE c.allocation_alias = ? AND c.campaign_id = ? AND c.job_id = ? AND c.generation = ? AND c.state = 'running' AND c.claimed_by = ? AND c.claim_until = ? AND c.claim_until > ?`, claim.AllocationAlias, claim.CampaignID, claim.JobID, claim.Generation, claim.Worker, claim.ClaimUntil.UTC().UnixMilli(), now.UTC().UnixMilli()).Scan(&hubAlias); err != nil {
		return fmt.Errorf("cleanup close requires the current unexpired claim")
	}
	if !proofEligible {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO cleanup_credit_blocks (allocation_alias) VALUES (?)`, claim.AllocationAlias); err != nil {
			return err
		}
	}
	result, err := tx.Exec(`UPDATE cleanup_journal SET state = 'closed', closed_at = ? WHERE allocation_alias = ? AND campaign_id = ? AND job_id = ? AND generation = ? AND state = 'running' AND claimed_by = ? AND claim_until = ?`, now.UTC().UnixMilli(), claim.AllocationAlias, claim.CampaignID, claim.JobID, claim.Generation, claim.Worker, claim.ClaimUntil.UTC().UnixMilli())
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("cleanup close requires the current claim")
	}
	result, err = tx.Exec(`UPDATE scratch_allocations SET state = 'closed' WHERE allocation_alias = ? AND state = 'reserved'`, claim.AllocationAlias)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("cleanup allocation is not reserved")
	}
	result, err = tx.Exec(`UPDATE hub_capacity SET reserved = reserved - 1 WHERE hub_alias = ? AND reserved > 0`, hubAlias)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("cleanup hub reservation is unavailable")
	}
	actionPrefix := fmt.Sprintf("job %s generation %d: scratch cleanup action required; journal ", claim.JobID, claim.Generation)
	if _, err := tx.Exec(`UPDATE actions SET state = 'closed' WHERE campaign_id = ? AND state = 'open' AND substr(detail, 1, ?) = ?`, claim.CampaignID, len(actionPrefix), actionPrefix); err != nil {
		return err
	}
	return tx.Commit()
}

func (o *Orchestrator) RecordReceipt(request OrchestratorReceiptRequest, now time.Time) (OrchestratorReceipt, error) {
	return o.recordReceipt(request, now, nil)
}

func (o *Orchestrator) recordValidatedReceipt(request OrchestratorReceiptRequest, now time.Time, batch validatedOrchestratorBatch) (OrchestratorReceipt, error) {
	return o.recordReceipt(request, now, &batch)
}

func (o *Orchestrator) recordReceipt(request OrchestratorReceiptRequest, now time.Time, carrier *validatedOrchestratorBatch) (OrchestratorReceipt, error) {
	if !filepath.IsAbs(request.BatchRoot) {
		return OrchestratorReceipt{}, fmt.Errorf("absolute coordinator-local batch root is required")
	}
	lease := request.Lease
	var scopePath, scopeSHA, specSHA, candidateCommit, candidateSHA, toolsCommit, toolsSHA, inputsJSON, surfacesJSON, jobStatus, jobKind, attemptStatus string
	var maxAttempts int
	var shardIndex int
	var jobLeaseUntil, attemptLeaseUntil int64
	err := o.db.QueryRow(`SELECT c.scope_path, c.scope_sha256, c.spec_sha256, c.candidate_commit, c.candidate_sha256, c.tools_commit, c.tools_sha256, c.controlled_inputs_json, c.max_attempts_per_job, j.surface_ids_json, j.status, j.kind, j.shard_index, j.lease_until, a.status, a.lease_until FROM campaigns c JOIN jobs j ON j.campaign_id = c.id JOIN attempts a ON a.campaign_id = j.campaign_id AND a.job_id = j.id AND a.generation = j.generation WHERE c.id = ? AND j.id = ? AND j.generation = ? AND a.worker = ?`, lease.CampaignID, lease.JobID, lease.Generation, lease.Worker).Scan(&scopePath, &scopeSHA, &specSHA, &candidateCommit, &candidateSHA, &toolsCommit, &toolsSHA, &inputsJSON, &maxAttempts, &surfacesJSON, &jobStatus, &jobKind, &shardIndex, &jobLeaseUntil, &attemptStatus, &attemptLeaseUntil)
	if err != nil {
		return OrchestratorReceipt{}, fmt.Errorf("receipt requires current campaign attempt")
	}
	var cleanupClosed int
	if err := o.db.QueryRow(`SELECT count(*) FROM cleanup_journal WHERE campaign_id = ? AND job_id = ? AND generation = ? AND state = 'closed'`, lease.CampaignID, lease.JobID, lease.Generation).Scan(&cleanupClosed); err != nil || cleanupClosed != 1 {
		return OrchestratorReceipt{}, fmt.Errorf("receipt requires closed cleanup")
	}
	var proofBlocked int
	if err := o.db.QueryRow(`SELECT count(*) FROM cleanup_credit_blocks b JOIN cleanup_journal c ON c.allocation_alias = b.allocation_alias WHERE c.campaign_id = ? AND c.job_id = ? AND c.generation = ?`, lease.CampaignID, lease.JobID, lease.Generation).Scan(&proofBlocked); err != nil || proofBlocked != 0 {
		return OrchestratorReceipt{}, fmt.Errorf("cleanup is proof-ineligible")
	}
	scope, scopeBytes, err := readExactJSONBytes[SurfaceOracleScope](scopePath)
	if err != nil || replayBytesSHA256(scopeBytes) != scopeSHA || validateSurfaceOracleScope(scope) != nil {
		return OrchestratorReceipt{}, fmt.Errorf("campaign scope binding drift")
	}
	// Rehydrate the immutable plan from stored inputs and shard rows before the
	// shared validator runs; callers never supply a second production authority.
	var jobSurfaces []string
	var controlledInputs map[string]string
	if err := json.Unmarshal([]byte(surfacesJSON), &jobSurfaces); err != nil {
		return OrchestratorReceipt{}, err
	}
	if err := json.Unmarshal([]byte(inputsJSON), &controlledInputs); err != nil {
		return OrchestratorReceipt{}, err
	}
	storedPlan := OrchestratorCampaignPlan{
		CampaignID: lease.CampaignID, SpecSHA256: specSHA, MaxAttemptsPerJob: maxAttempts,
		Definition: OrchestratorCampaignDefinition{
			Candidate: OrchestratorArtifact{Commit: candidateCommit, SHA256: candidateSHA}, Tools: OrchestratorArtifact{Commit: toolsCommit, SHA256: toolsSHA},
			ScopePath: scopePath, ScopeSHA256: scopeSHA, ControlledInputSHA256: controlledInputs,
		},
		Jobs: []OrchestratorJob{{ID: lease.JobID, Kind: OrchestratorJobKind(jobKind), ShardIndex: shardIndex, SurfaceIDs: jobSurfaces}},
	}
	// Plan validation needs every immutable job, not only the current row.
	rows, queryErr := o.db.Query(`SELECT id, kind, shard_index, surface_ids_json FROM jobs WHERE campaign_id = ? ORDER BY shard_index`, lease.CampaignID)
	if queryErr != nil {
		return OrchestratorReceipt{}, queryErr
	}
	storedPlan.Jobs = nil
	for rows.Next() {
		var job OrchestratorJob
		var surfaces string
		if err := rows.Scan(&job.ID, &job.Kind, &job.ShardIndex, &surfaces); err != nil || json.Unmarshal([]byte(surfaces), &job.SurfaceIDs) != nil {
			rows.Close()
			return OrchestratorReceipt{}, fmt.Errorf("invalid stored shard")
		}
		storedPlan.Jobs = append(storedPlan.Jobs, job)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return OrchestratorReceipt{}, err
	}
	if err := rows.Close(); err != nil {
		return OrchestratorReceipt{}, err
	}
	storedPlan.Definition.Shards = make([][]string, len(storedPlan.Jobs))
	for index := range storedPlan.Jobs {
		storedPlan.Definition.Shards[index] = append([]string(nil), storedPlan.Jobs[index].SurfaceIDs...)
	}
	var batch validatedOrchestratorBatch
	if carrier == nil {
		batch, err = validateOrchestratorBatch(request.BatchRoot, storedPlan, lease, false)
		if err != nil {
			return OrchestratorReceipt{}, fmt.Errorf("validate final batch: %w", err)
		}
		batch.BatchRoot = request.BatchRoot
	} else {
		batch = *carrier
		if batch.BatchRoot != request.BatchRoot {
			return OrchestratorReceipt{}, fmt.Errorf("validated batch root drift")
		}
	}
	if batch.Candidate.Commit != candidateCommit || batch.Candidate.SHA256 != candidateSHA || batch.Tools.Commit != toolsCommit || batch.Tools.SHA256 != toolsSHA {
		return OrchestratorReceipt{}, fmt.Errorf("final batch campaign binding drift")
	}
	if len(batch.ProofStates) != len(jobSurfaces) {
		return OrchestratorReceipt{}, fmt.Errorf("validated batch must adjudicate the exact shard in v1")
	}
	for _, id := range jobSurfaces {
		if batch.ProofStates[id] == "inconclusive" {
			return OrchestratorReceipt{}, fmt.Errorf("validated batch contains inconclusive evidence")
		}
		if batch.ProofStates[id] != "accepted" && batch.ProofStates[id] != "rejected" {
			return OrchestratorReceipt{}, fmt.Errorf("validated batch must adjudicate the exact shard in v1")
		}
	}
	proofStates := batch.ProofStates
	bindingSHA := batch.AuthoritySHA256
	receiptIdentity := lease.CampaignID + "\x00" + lease.JobID + "\x00" + fmt.Sprint(lease.Generation) + "\x00" + batch.ManifestSHA256 + "\x00" + bindingSHA
	if batch.SchemaVersion == orchestratorProductionBatchSchema {
		receiptIdentity = "orchestrator-production-receipt/v3\x00" + receiptIdentity
	}
	receiptID := "receipt-" + replayBytesSHA256([]byte(receiptIdentity))[:16]
	receipt := OrchestratorReceipt{ID: receiptID, CampaignID: lease.CampaignID, JobID: lease.JobID, Generation: lease.Generation, BatchRoot: request.BatchRoot, ManifestSHA256: batch.ManifestSHA256, BindingSHA256: bindingSHA}
	for _, state := range proofStates {
		if state == "accepted" {
			receipt.AcceptedCredit++
		} else {
			receipt.RejectedCredit++
		}
	}
	if batch.SchemaVersion == orchestratorProductionBatchSchema {
		if err := verifyProductionBatchIdentity(batch.BatchRoot, batch.AuthoritySHA256, batch.ProductionFiles); err != nil {
			return OrchestratorReceipt{}, fmt.Errorf("verify production batch identity: %w", err)
		}
	}
	existing, existingStates, found, err := loadOrchestratorReceipt(o.db, lease.CampaignID, lease.JobID, lease.Generation)
	if err != nil {
		return OrchestratorReceipt{}, err
	}
	if found {
		if existing != receipt || !maps.Equal(existingStates, proofStates) {
			return OrchestratorReceipt{}, fmt.Errorf("receipt replay differs from recorded receipt")
		}
		return existing, nil
	}
	if jobStatus != "running" || attemptStatus != "running" || jobLeaseUntil <= now.UTC().UnixMilli() || attemptLeaseUntil <= now.UTC().UnixMilli() {
		return OrchestratorReceipt{}, fmt.Errorf("new receipt requires current running campaign attempt")
	}
	tx, err := o.db.Begin()
	if err != nil {
		return OrchestratorReceipt{}, err
	}
	defer tx.Rollback()
	if batch.SchemaVersion == orchestratorProductionBatchSchema {
		if err := verifyProductionBatchIdentity(batch.BatchRoot, batch.AuthoritySHA256, batch.ProductionFiles); err != nil {
			return OrchestratorReceipt{}, fmt.Errorf("verify production batch identity at receipt transaction: %w", err)
		}
	}
	result, err := tx.Exec(`INSERT INTO receipts (id, campaign_id, job_id, generation, batch_root, manifest_sha256, binding_sha256, validated, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?) ON CONFLICT(campaign_id, job_id, generation) DO NOTHING`, receipt.ID, receipt.CampaignID, receipt.JobID, receipt.Generation, receipt.BatchRoot, receipt.ManifestSHA256, receipt.BindingSHA256, now.UTC().UnixMilli())
	if err != nil {
		return OrchestratorReceipt{}, fmt.Errorf("record validated receipt: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		existing, existingStates, found, err := loadOrchestratorReceipt(tx, lease.CampaignID, lease.JobID, lease.Generation)
		if err != nil || !found || existing != receipt || !maps.Equal(existingStates, proofStates) {
			return OrchestratorReceipt{}, fmt.Errorf("concurrent receipt replay differs from recorded receipt")
		}
		return existing, nil
	}
	for _, id := range jobSurfaces {
		if _, err := tx.Exec(`INSERT INTO proof_credits (campaign_id, surface_id, receipt_id, state, accepted_at) VALUES (?, ?, ?, ?, ?)`, receipt.CampaignID, id, receipt.ID, proofStates[id], now.UTC().UnixMilli()); err != nil {
			return OrchestratorReceipt{}, fmt.Errorf("record unique campaign credit: %w", err)
		}
	}
	result, err = tx.Exec(`UPDATE jobs SET status = 'closed' WHERE campaign_id = ? AND id = ? AND generation = ? AND leased_by = ? AND status = 'running' AND lease_until > ?`, lease.CampaignID, lease.JobID, lease.Generation, lease.Worker, now.UTC().UnixMilli())
	if err != nil {
		return OrchestratorReceipt{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return OrchestratorReceipt{}, fmt.Errorf("receipt attempt changed concurrently")
	}
	result, err = tx.Exec(`UPDATE attempts SET status = 'closed' WHERE campaign_id = ? AND job_id = ? AND generation = ? AND worker = ? AND status = 'running' AND lease_until > ?`, lease.CampaignID, lease.JobID, lease.Generation, lease.Worker, now.UTC().UnixMilli())
	if err != nil {
		return OrchestratorReceipt{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return OrchestratorReceipt{}, fmt.Errorf("receipt attempt is not current and running")
	}
	if err := tx.Commit(); err != nil {
		return OrchestratorReceipt{}, err
	}
	return receipt, nil
}

type orchestratorQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func loadOrchestratorReceipt(query orchestratorQuerier, campaignID, jobID string, generation int) (OrchestratorReceipt, map[string]string, bool, error) {
	var receipt OrchestratorReceipt
	err := query.QueryRow(`SELECT id, campaign_id, job_id, generation, batch_root, manifest_sha256, binding_sha256 FROM receipts WHERE campaign_id = ? AND job_id = ? AND generation = ?`, campaignID, jobID, generation).Scan(&receipt.ID, &receipt.CampaignID, &receipt.JobID, &receipt.Generation, &receipt.BatchRoot, &receipt.ManifestSHA256, &receipt.BindingSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return OrchestratorReceipt{}, nil, false, nil
	}
	if err != nil {
		return OrchestratorReceipt{}, nil, false, err
	}
	rows, err := query.Query(`SELECT surface_id, state FROM proof_credits WHERE receipt_id = ? ORDER BY surface_id`, receipt.ID)
	if err != nil {
		return OrchestratorReceipt{}, nil, false, err
	}
	defer rows.Close()
	states := map[string]string{}
	for rows.Next() {
		var id, state string
		if err := rows.Scan(&id, &state); err != nil {
			return OrchestratorReceipt{}, nil, false, err
		}
		states[id] = state
		if state == "accepted" {
			receipt.AcceptedCredit++
		} else {
			receipt.RejectedCredit++
		}
	}
	if err := rows.Err(); err != nil {
		return OrchestratorReceipt{}, nil, false, err
	}
	return receipt, states, true, nil
}

func (o *Orchestrator) Status(campaignID string) (OrchestratorCampaignStatus, error) {
	status := OrchestratorCampaignStatus{CampaignID: campaignID}
	if err := o.db.QueryRow(`SELECT candidate_commit, candidate_sha256 FROM campaigns WHERE id = ?`, campaignID).Scan(&status.Candidate.Commit, &status.Candidate.SHA256); err != nil {
		return OrchestratorCampaignStatus{}, err
	}
	if err := o.db.QueryRow(`SELECT count(*) FILTER (WHERE status = 'queued'), count(*) FILTER (WHERE status = 'running'), count(*) FILTER (WHERE status = 'closed') FROM jobs WHERE campaign_id = ?`, campaignID).Scan(&status.Queued, &status.Running, &status.Closed); err != nil {
		return OrchestratorCampaignStatus{}, err
	}
	if err := o.db.QueryRow(`SELECT count(*) FILTER (WHERE status = 'retryable'), count(*) FILTER (WHERE status = 'failed') FROM attempts WHERE campaign_id = ?`, campaignID).Scan(&status.Retryable, &status.Failed); err != nil {
		return OrchestratorCampaignStatus{}, err
	}
	if err := o.db.QueryRow(`SELECT count(*) FILTER (WHERE state = 'accepted'), count(*) FILTER (WHERE state = 'rejected') FROM proof_credits WHERE campaign_id = ?`, campaignID).Scan(&status.Accepted, &status.Rejected); err != nil {
		return OrchestratorCampaignStatus{}, err
	}
	rows, err := o.db.Query(`SELECT surface_ids_json FROM jobs WHERE campaign_id = ?`, campaignID)
	if err != nil {
		return OrchestratorCampaignStatus{}, err
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var data string
		var ids []string
		if err := rows.Scan(&data); err != nil || json.Unmarshal([]byte(data), &ids) != nil {
			return OrchestratorCampaignStatus{}, fmt.Errorf("invalid stored shard")
		}
		total += len(ids)
	}
	if err := rows.Err(); err != nil {
		return OrchestratorCampaignStatus{}, err
	}
	status.Unseen = total - status.Accepted - status.Rejected
	if err := o.db.QueryRow(`SELECT count(*) FROM cleanup_journal WHERE campaign_id = ? AND state != 'closed'`, campaignID).Scan(&status.CleanupOpen); err != nil {
		return OrchestratorCampaignStatus{}, err
	}
	return status, nil
}

func validateOrchestratorDefinition(definition OrchestratorCampaignDefinition) error {
	return validateOrchestratorDefinitionAtScope(definition, definition.ScopePath)
}

func validateOrchestratorDefinitionAtScope(definition OrchestratorCampaignDefinition, scopePath string) error {
	if !commitPattern.MatchString(definition.Candidate.Commit) || !sha256Pattern.MatchString(definition.Candidate.SHA256) || !commitPattern.MatchString(definition.Tools.Commit) || !sha256Pattern.MatchString(definition.Tools.SHA256) {
		return fmt.Errorf("invalid candidate or Tools bindings")
	}
	if !filepath.IsAbs(definition.ScopePath) || !filepath.IsAbs(scopePath) || filepath.Clean(scopePath) != scopePath || !sha256Pattern.MatchString(definition.ScopeSHA256) {
		return fmt.Errorf("absolute exact scope binding is required")
	}
	scope, scopeBytes, err := readExactJSONBytes[SurfaceOracleScope](scopePath)
	if err != nil || validateSurfaceOracleScope(scope) != nil || replayBytesSHA256(scopeBytes) != definition.ScopeSHA256 {
		return fmt.Errorf("exact scope binding does not validate")
	}
	if len(definition.ControlledInputSHA256) == 0 {
		return fmt.Errorf("controlled input hashes are required")
	}
	if len(definition.Shards) == 0 {
		return fmt.Errorf("at least one non-empty shard is required")
	}
	for name, hash := range definition.ControlledInputSHA256 {
		if strings.TrimSpace(name) == "" || !sha256Pattern.MatchString(hash) {
			return fmt.Errorf("invalid controlled input hash")
		}
	}
	if scope.Kind == "oracle-plan" && definition.ControlledInputSHA256["oracle-plan"] != scope.OraclePlanSHA256 {
		return fmt.Errorf("campaign scope does not bind the controlled Oracle plan")
	}
	scopeIDs := make(map[string]bool, len(scope.Rows))
	for _, row := range scope.Rows {
		scopeIDs[row.SurfaceID] = true
	}
	seen := map[string]bool{}
	for _, shard := range definition.Shards {
		if len(shard) == 0 {
			return fmt.Errorf("non-empty shards are required")
		}
		for _, id := range shard {
			if seen[id] {
				return fmt.Errorf("shards must be disjoint")
			}
			if !scopeIDs[id] {
				return fmt.Errorf("shard surface is outside exact scope")
			}
			seen[id] = true
		}
	}
	if len(seen) != len(scopeIDs) {
		return fmt.Errorf("shards must partition the exact scope")
	}
	return nil
}

func validateOrchestratorPlan(plan OrchestratorCampaignPlan) error {
	return validateOrchestratorPlanAtScope(plan, plan.Definition.ScopePath)
}

func validateOrchestratorPlanAtScope(plan OrchestratorCampaignPlan, scopePath string) error {
	want, err := planOrchestratorCampaignAtScope(plan.Definition, scopePath)
	if err != nil {
		return err
	}
	for _, job := range plan.Jobs {
		if job.Kind != OrchestratorJobSurfaceRuntimeShard {
			return fmt.Errorf("unknown orchestrator job kind %q", job.Kind)
		}
	}
	maxAttempts, err := normalizedOrchestratorMaxAttemptsPerJob(plan.MaxAttemptsPerJob)
	if err != nil {
		return err
	}
	normalized := plan
	normalized.MaxAttemptsPerJob = maxAttempts
	want.MaxAttemptsPerJob = maxAttempts
	if !reflect.DeepEqual(normalized, want) {
		return fmt.Errorf("orchestrator plan drift")
	}
	return nil
}

func migrateOrchestratorJobs(db *sql.DB) error {
	var createSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'jobs'`).Scan(&createSQL); err != nil {
		return err
	}
	if !strings.Contains(createSQL, "shard_index IN (0, 1)") {
		return nil
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	if _, err := db.Exec(`PRAGMA legacy_alter_table = ON`); err != nil {
		return err
	}
	defer db.Exec(`PRAGMA legacy_alter_table = OFF`)
	defer db.Exec(`PRAGMA foreign_keys = ON`)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE jobs_new (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id),
  id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind = 'surface-runtime-shard'),
  shard_index INTEGER NOT NULL CHECK (shard_index >= 0),
  surface_ids_json TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'retryable', 'failed', 'closed')),
  generation INTEGER NOT NULL,
  leased_by TEXT,
  lease_until INTEGER,
  heartbeat_at INTEGER,
  PRIMARY KEY (campaign_id, id),
  UNIQUE (campaign_id, shard_index)
)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO jobs_new SELECT campaign_id, id, kind, shard_index, surface_ids_json, status, generation, leased_by, lease_until, heartbeat_at FROM jobs`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE jobs RENAME TO jobs_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE jobs_new RENAME TO jobs`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE jobs_legacy`); err != nil {
		return err
	}
	return tx.Commit()
}

func safeOrchestratorToken(value string) bool {
	return value != "" && safeAttemptRunID(value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func ensureHubObservationColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(hub_observations)`)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for name, definition := range map[string]string{
		"quarantined":                   "INTEGER NOT NULL DEFAULT 0",
		"consumed":                      "INTEGER NOT NULL DEFAULT 0",
		"daily_scratch_orgs_remaining":  "INTEGER",
		"active_scratch_orgs_remaining": "INTEGER",
	} {
		if !columns[name] {
			if _, err := db.Exec(`ALTER TABLE hub_observations ADD COLUMN ` + name + ` ` + definition); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureCampaignAttemptCapColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(campaigns)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "max_attempts_per_job" {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE campaigns ADD COLUMN max_attempts_per_job INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts_per_job > 0)`)
	return err
}

const orchestratorSchema = `
CREATE TABLE IF NOT EXISTS campaigns (
  id TEXT PRIMARY KEY,
  spec_sha256 TEXT NOT NULL,
  candidate_commit TEXT NOT NULL,
  candidate_sha256 TEXT NOT NULL,
  tools_commit TEXT NOT NULL,
  tools_sha256 TEXT NOT NULL,
  scope_path TEXT NOT NULL,
  scope_sha256 TEXT NOT NULL,
  controlled_inputs_json TEXT NOT NULL,
  max_attempts_per_job INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts_per_job > 0),
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS jobs (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id),
  id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind = 'surface-runtime-shard'),
  shard_index INTEGER NOT NULL CHECK (shard_index >= 0),
  surface_ids_json TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'retryable', 'failed', 'closed')),
  generation INTEGER NOT NULL,
  leased_by TEXT,
  lease_until INTEGER,
  heartbeat_at INTEGER,
  PRIMARY KEY (campaign_id, id),
  UNIQUE (campaign_id, shard_index)
);
CREATE TABLE IF NOT EXISTS attempts (
  campaign_id TEXT NOT NULL,
  job_id TEXT NOT NULL,
  generation INTEGER NOT NULL,
  worker TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('running', 'retryable', 'failed', 'closed')),
  leased_at INTEGER NOT NULL,
  lease_until INTEGER NOT NULL,
  heartbeat_at INTEGER NOT NULL,
  PRIMARY KEY (campaign_id, job_id, generation),
  FOREIGN KEY (campaign_id, job_id) REFERENCES jobs(campaign_id, id)
);
CREATE TABLE IF NOT EXISTS lease_terms (
  campaign_id TEXT NOT NULL,
  job_id TEXT NOT NULL,
  generation INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL CHECK (duration_ms > 0),
  PRIMARY KEY (campaign_id, job_id, generation),
  FOREIGN KEY (campaign_id, job_id, generation) REFERENCES attempts(campaign_id, job_id, generation)
);
CREATE TABLE IF NOT EXISTS hub_observations (
  id INTEGER PRIMARY KEY,
  hub_alias TEXT NOT NULL,
  observed_at INTEGER NOT NULL,
  healthy INTEGER NOT NULL,
  quarantined INTEGER NOT NULL DEFAULT 0,
  consumed INTEGER NOT NULL DEFAULT 0,
  daily_scratch_orgs_remaining INTEGER,
  active_scratch_orgs_remaining INTEGER,
  detail TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS receipts (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL,
  job_id TEXT NOT NULL,
  generation INTEGER NOT NULL,
  batch_root TEXT NOT NULL,
  manifest_sha256 TEXT NOT NULL,
  binding_sha256 TEXT NOT NULL,
  validated INTEGER NOT NULL CHECK (validated = 1),
  recorded_at INTEGER NOT NULL,
  UNIQUE (campaign_id, job_id, generation),
  FOREIGN KEY (campaign_id, job_id, generation) REFERENCES attempts(campaign_id, job_id, generation)
);
CREATE TABLE IF NOT EXISTS proof_credits (
  campaign_id TEXT NOT NULL,
  surface_id TEXT NOT NULL,
  receipt_id TEXT NOT NULL REFERENCES receipts(id),
  state TEXT NOT NULL CHECK (state IN ('accepted', 'rejected')),
  accepted_at INTEGER NOT NULL,
  UNIQUE (campaign_id, surface_id)
);
CREATE TABLE IF NOT EXISTS hub_capacity (
  hub_alias TEXT PRIMARY KEY,
  capacity INTEGER NOT NULL CHECK (capacity > 0),
  reserved INTEGER NOT NULL CHECK (reserved >= 0 AND reserved <= capacity)
);
CREATE TABLE IF NOT EXISTS scratch_allocations (
  allocation_alias TEXT PRIMARY KEY,
  hub_alias TEXT NOT NULL REFERENCES hub_capacity(hub_alias),
  campaign_id TEXT NOT NULL,
  job_id TEXT NOT NULL,
  generation INTEGER NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('reserved', 'closed')),
  reserved_at INTEGER NOT NULL,
  UNIQUE (campaign_id, job_id, generation),
  FOREIGN KEY (campaign_id, job_id, generation) REFERENCES attempts(campaign_id, job_id, generation)
);
CREATE TABLE IF NOT EXISTS cleanup_journal (
  allocation_alias TEXT PRIMARY KEY REFERENCES scratch_allocations(allocation_alias),
  campaign_id TEXT NOT NULL,
  job_id TEXT NOT NULL,
  generation INTEGER NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('pending', 'running', 'closed', 'action_required')),
  claimed_by TEXT,
  claim_until INTEGER,
  closed_at INTEGER,
  UNIQUE (campaign_id, job_id, generation)
);
CREATE TABLE IF NOT EXISTS cleanup_credit_blocks (
  allocation_alias TEXT PRIMARY KEY REFERENCES cleanup_journal(allocation_alias)
);
CREATE TABLE IF NOT EXISTS actions (
  id INTEGER PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id),
  kind TEXT NOT NULL,
  detail TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('open', 'closed')),
  created_at INTEGER NOT NULL
);`
