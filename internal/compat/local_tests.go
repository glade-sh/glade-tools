package compat

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/glade-sh/glade/internal/apextest"
	"github.com/glade-sh/glade/internal/diagnostic"
	"github.com/glade-sh/glade/internal/project"
	"github.com/glade-sh/glade/internal/schema"
	"github.com/glade-sh/glade/internal/sema"
	"github.com/glade-sh/glade/internal/storage"
	"github.com/glade-sh/glade/internal/testreport"
	"github.com/glade-sh/glade/internal/typesys"
	"github.com/glade-sh/glade/internal/watch"
)

type LocalTestOptions struct {
	Project             string
	Class               string
	ClassList           []string
	ClassFile           string
	StartClass          string
	Method              string
	BlockersOnly        bool
	TraceBlocked        bool
	SlowTestThresholdMS int64
	TimeoutMS           int64
	TopFailures         int
	ProfileOnTimeout    bool
	Parallelism         int
	ProgressWriter      io.Writer
	ForceAnalysis       bool
	MaxFailureGroups    int
	ChangedSince        string
	ParallelMethods     bool
	CPUProfilePath      string
	MemProfilePath      string
	PerfJSONPath        string
	ShardCount          int
	ShardIndex          int
	WriteClassShards    string
	DurationHistoryPath string
	AutoTune            bool
	AutoShardCount      bool
	AutoShardIndex      bool
	NoDiskCache         bool
}

type LocalTestComparisonTargetManifest struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Targets       []LocalTestComparisonTarget `json:"targets"`
}

type LocalTestComparisonTarget struct {
	ID         string `json:"id"`
	Class      string `json:"class,omitempty"`
	Method     string `json:"method,omitempty"`
	CPUProfile bool   `json:"cpuProfile"`
}

type LocalTestComparisonOptions struct {
	BaseBin      string
	CandidateBin string
	Project      string
	Out          string
	Workers      int
	Runs         int
	Manifest     string
}

type LocalTestComparisonInvocationOptions struct {
	Snapshot      LocalTestComparisonBinarySnapshot
	SourceProject string
	Target        LocalTestComparisonTarget
	Workers       int
	ArtifactDir   string
	testHooks     *localTestComparisonInvocationTestHooks
}

type localTestComparisonInvocationTestHooks struct {
	beforeProjectCopy            func() error
	afterProjectFileCopy         func(relative string) error
	afterArtifactDirectoryCreate func() error
}

type LocalTestComparisonInvocationResult struct {
	ArtifactDir    string
	ResultPath     string
	PerfPath       string
	StderrPath     string
	MetricsPath    string
	CPUProfilePath string
	Metrics        LocalTestComparisonInvocationMetrics
}

type LocalTestComparisonInvocationMetrics struct {
	WallTimeNS          int64                               `json:"wallTimeNs"`
	UserTimeNS          int64                               `json:"userTimeNs"`
	SystemTimeNS        int64                               `json:"systemTimeNs"`
	MaxRSSBytes         uint64                              `json:"maxRssBytes"`
	ExitCode            int                                 `json:"exitCode"`
	PhysicalProjectRoot string                              `json:"physicalProjectRoot"`
	LogicalSourceRoot   string                              `json:"logicalSourceRoot"`
	Artifacts           []LocalTestComparisonArtifactMetric `json:"artifacts"`
}

type LocalTestComparisonArtifactMetric struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type LocalTestSafetyContractArtifact struct {
	ResultPath   string
	IsolatedRoot string
}

type LocalTestSafetyComparison struct {
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	ContractSHA256 string `json:"contractSha256,omitempty"`
}

type localTestSafetyContract struct {
	CasesDiscovered int                      `json:"casesDiscovered"`
	CasesRun        int                      `json:"casesRun"`
	Summary         LocalTestSummary         `json:"summary"`
	Ready           bool                     `json:"ready"`
	Outcomes        []localTestSafetyOutcome `json:"outcomes"`
}

type localTestSafetyOutcome struct {
	Class        string `json:"class"`
	Method       string `json:"method"`
	Outcome      string `json:"outcome"`
	CapabilityID string `json:"capabilityId,omitempty"`
}

type localTestSafetyOutcomeKey struct {
	Class  string
	Method string
}

type LocalTestComparisonEnvironmentOptions struct {
	Snapshot LocalTestComparisonBinarySnapshot
	Workers  int
	Runs     int
}

type LocalTestComparisonBinarySnapshot struct {
	directory     string
	directoryInfo os.FileInfo
	binary        LocalTestComparisonBinaryFact
	binaryInfo    os.FileInfo
	manifest      json.RawMessage
}

type LocalTestComparisonEnvironment struct {
	GOOS        string                               `json:"goos"`
	GOARCH      string                               `json:"goarch"`
	GoVersion   string                               `json:"goVersion"`
	LogicalCPUs int                                  `json:"logicalCpus"`
	Workers     int                                  `json:"workers"`
	Runs        int                                  `json:"runs"`
	Tuning      LocalTestComparisonEnvironmentTuning `json:"tuning"`
	Binary      LocalTestComparisonBinaryFact        `json:"binary"`
	Manifest    json.RawMessage                      `json:"manifest"`
}

type LocalTestComparisonEnvironmentTuning struct {
	GOMAXPROCS string `json:"GOMAXPROCS"`
	GOGC       string `json:"GOGC"`
	GOMEMLIMIT string `json:"GOMEMLIMIT"`
}

type LocalTestComparisonBinaryFact struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

const localTestComparisonTargetIDPattern = `^[a-z0-9][a-z0-9._-]{0,63}$`

var localTestComparisonTargetID = regexp.MustCompile(localTestComparisonTargetIDPattern)

func LoadLocalTestComparisonTargetManifest(path string) (LocalTestComparisonTargetManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return LocalTestComparisonTargetManifest{}, fmt.Errorf("open local test comparison target manifest: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest LocalTestComparisonTargetManifest
	if err := decoder.Decode(&manifest); err != nil {
		return LocalTestComparisonTargetManifest{}, fmt.Errorf("decode local test comparison target manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return LocalTestComparisonTargetManifest{}, errors.New("decode local test comparison target manifest: trailing JSON data")
		}
		return LocalTestComparisonTargetManifest{}, fmt.Errorf("decode local test comparison target manifest: %w", err)
	}
	if err := validateLocalTestComparisonTargetManifest(manifest); err != nil {
		return LocalTestComparisonTargetManifest{}, err
	}
	return manifest, nil
}

func validateLocalTestComparisonTargetManifest(manifest LocalTestComparisonTargetManifest) error {
	if manifest.SchemaVersion != 1 {
		return errors.New("local test comparison target manifest schemaVersion must be 1")
	}
	if len(manifest.Targets) == 0 {
		return errors.New("local test comparison target manifest requires at least one target")
	}
	seen := make(map[string]struct{}, len(manifest.Targets))
	for _, target := range manifest.Targets {
		if !localTestComparisonTargetID.MatchString(target.ID) {
			return fmt.Errorf("target id %q must match %s", target.ID, localTestComparisonTargetIDPattern)
		}
		if _, ok := seen[target.ID]; ok {
			return fmt.Errorf("duplicate target id %q", target.ID)
		}
		seen[target.ID] = struct{}{}
		class := strings.TrimSpace(target.Class)
		method := strings.TrimSpace(target.Method)
		if target.Class != "" && class == "" {
			return fmt.Errorf("target %q class must not be blank", target.ID)
		}
		if target.Method != "" && method == "" {
			return fmt.Errorf("target %q method must not be blank", target.ID)
		}
		if method != "" && class == "" {
			return fmt.Errorf("target %q method requires class", target.ID)
		}
	}
	return nil
}

func ValidateLocalTestComparisonOptions(options LocalTestComparisonOptions) error {
	if strings.TrimSpace(options.BaseBin) == "" {
		return errors.New("base binary path is required")
	}
	if strings.TrimSpace(options.CandidateBin) == "" {
		return errors.New("candidate binary path is required")
	}
	if strings.TrimSpace(options.Project) == "" {
		return errors.New("project path is required")
	}
	if strings.TrimSpace(options.Out) == "" {
		return errors.New("output path is required")
	}
	if options.Workers < 1 {
		return errors.New("workers must be at least 1")
	}
	if options.Runs != 5 {
		return errors.New("runs must be exactly 5")
	}
	if strings.TrimSpace(options.Manifest) == "" {
		return errors.New("manifest path is required")
	}
	return nil
}

const (
	localTestSafetyLogicalRoot = "<isolated-local-test-root>"
	localTestSafetyTarget      = "local Apex test execution readiness"
)

func CompareLocalTestSafetyContracts(baseArtifact, candidateArtifact LocalTestSafetyContractArtifact) LocalTestSafetyComparison {
	base, reason := readLocalTestSafetyContract("base", baseArtifact)
	if reason != "" {
		return refusedLocalTestSafetyComparison(reason)
	}
	candidate, reason := readLocalTestSafetyContract("candidate", candidateArtifact)
	if reason != "" {
		return refusedLocalTestSafetyComparison(reason)
	}
	if base.CasesDiscovered != candidate.CasesDiscovered {
		return refusedLocalTestSafetyComparison(fmt.Sprintf("casesDiscovered mismatch: base=%d candidate=%d", base.CasesDiscovered, candidate.CasesDiscovered))
	}
	if base.CasesRun != candidate.CasesRun {
		return refusedLocalTestSafetyComparison(fmt.Sprintf("casesRun mismatch: base=%d candidate=%d", base.CasesRun, candidate.CasesRun))
	}
	if base.Summary != candidate.Summary {
		return refusedLocalTestSafetyComparison("summary mismatch")
	}
	if base.Ready != candidate.Ready {
		return refusedLocalTestSafetyComparison(fmt.Sprintf("ready mismatch: base=%t candidate=%t", base.Ready, candidate.Ready))
	}

	candidateByIdentity := make(map[localTestSafetyOutcomeKey]localTestSafetyOutcome, len(candidate.Outcomes))
	for _, outcome := range candidate.Outcomes {
		candidateByIdentity[localTestSafetyOutcomeIdentity(outcome)] = outcome
	}
	baseByIdentity := make(map[localTestSafetyOutcomeKey]localTestSafetyOutcome, len(base.Outcomes))
	for _, outcome := range base.Outcomes {
		identity := localTestSafetyOutcomeIdentity(outcome)
		baseByIdentity[identity] = outcome
		candidateOutcome, ok := candidateByIdentity[identity]
		if !ok {
			return refusedLocalTestSafetyComparison(fmt.Sprintf("candidate result is missing outcome identity %q", formatLocalTestSafetyOutcomeIdentity(identity)))
		}
		if outcome.Outcome != candidateOutcome.Outcome {
			return refusedLocalTestSafetyComparison(fmt.Sprintf("outcome mismatch for outcome identity %q: base=%q candidate=%q", formatLocalTestSafetyOutcomeIdentity(identity), outcome.Outcome, candidateOutcome.Outcome))
		}
		if outcome.CapabilityID != candidateOutcome.CapabilityID {
			return refusedLocalTestSafetyComparison(fmt.Sprintf("capabilityID mismatch for outcome identity %q: base=%q candidate=%q", formatLocalTestSafetyOutcomeIdentity(identity), outcome.CapabilityID, candidateOutcome.CapabilityID))
		}
	}
	for _, outcome := range candidate.Outcomes {
		identity := localTestSafetyOutcomeIdentity(outcome)
		if _, ok := baseByIdentity[identity]; !ok {
			return refusedLocalTestSafetyComparison(fmt.Sprintf("candidate result has extra outcome identity %q", formatLocalTestSafetyOutcomeIdentity(identity)))
		}
	}

	canonical, err := json.Marshal(base)
	if err != nil {
		return refusedLocalTestSafetyComparison("matched safety contract could not be recorded")
	}
	digest := sha256.Sum256(canonical)
	return LocalTestSafetyComparison{Status: "matched", ContractSHA256: fmt.Sprintf("%x", digest)}
}

func readLocalTestSafetyContract(label string, artifact LocalTestSafetyContractArtifact) (localTestSafetyContract, string) {
	if strings.TrimSpace(artifact.IsolatedRoot) == "" {
		return localTestSafetyContract{}, fmt.Sprintf("%s isolated root is required", label)
	}
	raw, err := os.ReadFile(artifact.ResultPath)
	if err != nil {
		return localTestSafetyContract{}, fmt.Sprintf("%s result artifact could not be read", label)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var encodedReport json.RawMessage
	if err := decoder.Decode(&encodedReport); err != nil {
		return localTestSafetyContract{}, fmt.Sprintf("%s result JSON is invalid", label)
	}
	trimmed := bytes.TrimSpace(encodedReport)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return localTestSafetyContract{}, fmt.Sprintf("%s result JSON is invalid", label)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return localTestSafetyContract{}, fmt.Sprintf("%s result JSON is invalid", label)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encodedReport, &fields); err != nil {
		return localTestSafetyContract{}, fmt.Sprintf("%s result JSON is invalid", label)
	}
	for _, field := range []string{"target", "ready", "project", "casesDiscovered", "casesRun", "summary", "outcomes"} {
		value, ok := fields[field]
		if !ok {
			return localTestSafetyContract{}, fmt.Sprintf("%s result is missing required field %q", label, field)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return localTestSafetyContract{}, fmt.Sprintf("%s result field %q must not be null", label, field)
		}
	}
	var summaryFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["summary"], &summaryFields); err != nil {
		return localTestSafetyContract{}, fmt.Sprintf("%s result JSON is invalid", label)
	}
	for _, field := range []string{"total", "pass", "fail", "unsupported", "loadError", "compileError", "internalError"} {
		value, ok := summaryFields[field]
		if !ok {
			return localTestSafetyContract{}, fmt.Sprintf("%s result summary is missing required field %q", label, field)
		}
		var number json.Number
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &number) != nil {
			return localTestSafetyContract{}, fmt.Sprintf("%s result summary field %q must be a number", label, field)
		}
	}
	var report LocalTestReport
	if err := json.Unmarshal(encodedReport, &report); err != nil {
		return localTestSafetyContract{}, fmt.Sprintf("%s result JSON is invalid", label)
	}
	if report.Target != localTestSafetyTarget {
		return localTestSafetyContract{}, fmt.Sprintf("%s result target must be %q", label, localTestSafetyTarget)
	}
	if strings.TrimSpace(report.Project) == "" {
		return localTestSafetyContract{}, fmt.Sprintf("%s result project is required", label)
	}
	if report.CasesDiscovered < 0 {
		return localTestSafetyContract{}, fmt.Sprintf("%s result casesDiscovered must not be negative", label)
	}
	if report.CasesRun < 0 {
		return localTestSafetyContract{}, fmt.Sprintf("%s result casesRun must not be negative", label)
	}
	if report.CasesRun > report.CasesDiscovered {
		return localTestSafetyContract{}, fmt.Sprintf("%s result casesRun must not exceed casesDiscovered", label)
	}

	outcomes := make([]localTestSafetyOutcome, 0, len(report.Outcomes))
	seen := make(map[localTestSafetyOutcomeKey]struct{}, len(report.Outcomes))
	var derivedSummary LocalTestSummary
	for index, outcome := range report.Outcomes {
		if strings.TrimSpace(outcome.Class) == "" || strings.TrimSpace(outcome.Method) == "" {
			return localTestSafetyContract{}, fmt.Sprintf("%s result has invalid outcome identity at index %d", label, index)
		}
		derivedSummary.Total++
		switch outcome.Outcome {
		case "pass":
			derivedSummary.Pass++
		case "fail":
			derivedSummary.Fail++
		case "unsupported":
			derivedSummary.Unsupported++
		case "load_error":
			derivedSummary.LoadErrors++
		case "compile_error":
			derivedSummary.CompileErrors++
		case "internal_error":
			derivedSummary.InternalErrors++
		case "assert_fail":
			derivedSummary.AssertFailures++
		case "runtime_gap":
			derivedSummary.RuntimeGaps++
		case "compile_gap":
			derivedSummary.CompileGaps++
		case "timeout":
			derivedSummary.Timeouts++
		default:
			return localTestSafetyContract{}, fmt.Sprintf("%s result has invalid outcome at index %d", label, index)
		}
		safetyOutcome := localTestSafetyOutcome{
			Class:        outcome.Class,
			Method:       outcome.Method,
			Outcome:      outcome.Outcome,
			CapabilityID: normalizeLocalTestSafetyPath(outcome.CapabilityID, artifact.IsolatedRoot),
		}
		identity := localTestSafetyOutcomeIdentity(safetyOutcome)
		if _, ok := seen[identity]; ok {
			return localTestSafetyContract{}, fmt.Sprintf("%s result has duplicate outcome identity %q", label, formatLocalTestSafetyOutcomeIdentity(identity))
		}
		seen[identity] = struct{}{}
		outcomes = append(outcomes, safetyOutcome)
	}
	if report.Summary != derivedSummary {
		return localTestSafetyContract{}, fmt.Sprintf("%s result summary does not match outcomes", label)
	}
	if report.Ready != localTestSafetySummaryReady(report.Summary) {
		return localTestSafetyContract{}, fmt.Sprintf("%s result ready does not match summary", label)
	}
	sort.Slice(outcomes, func(i, j int) bool {
		if outcomes[i].Class != outcomes[j].Class {
			return outcomes[i].Class < outcomes[j].Class
		}
		return outcomes[i].Method < outcomes[j].Method
	})
	return localTestSafetyContract{
		CasesDiscovered: report.CasesDiscovered,
		CasesRun:        report.CasesRun,
		Summary:         report.Summary,
		Ready:           report.Ready,
		Outcomes:        outcomes,
	}, ""
}

func normalizeLocalTestSafetyPath(value, isolatedRoot string) string {
	if value == isolatedRoot {
		return localTestSafetyLogicalRoot
	}
	if !strings.HasPrefix(value, isolatedRoot) || len(value) == len(isolatedRoot) {
		return value
	}
	separator := value[len(isolatedRoot)]
	if separator != '/' && separator != '\\' {
		return value
	}
	return localTestSafetyLogicalRoot + value[len(isolatedRoot):]
}

func localTestSafetySummaryReady(summary LocalTestSummary) bool {
	return summary.Fail == 0 &&
		summary.Unsupported == 0 &&
		summary.LoadErrors == 0 &&
		summary.CompileErrors == 0 &&
		summary.InternalErrors == 0 &&
		summary.AssertFailures == 0 &&
		summary.RuntimeGaps == 0 &&
		summary.CompileGaps == 0 &&
		summary.Timeouts == 0
}

func localTestSafetyOutcomeIdentity(outcome localTestSafetyOutcome) localTestSafetyOutcomeKey {
	return localTestSafetyOutcomeKey{Class: outcome.Class, Method: outcome.Method}
}

func formatLocalTestSafetyOutcomeIdentity(identity localTestSafetyOutcomeKey) string {
	return identity.Class + "." + identity.Method
}

func refusedLocalTestSafetyComparison(reason string) LocalTestSafetyComparison {
	return LocalTestSafetyComparison{Status: "refused", Reason: reason}
}

func PrepareLocalTestComparisonBinarySnapshot(ctx context.Context, binaryPath string) (LocalTestComparisonBinarySnapshot, error) {
	var snapshot LocalTestComparisonBinarySnapshot
	if ctx == nil {
		return snapshot, errors.New("local test comparison binary snapshot context is required")
	}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	if strings.TrimSpace(binaryPath) == "" {
		return snapshot, errors.New("local test comparison binary snapshot path is required")
	}
	originalPath, err := filepath.Abs(binaryPath)
	if err != nil {
		return snapshot, fmt.Errorf("resolve local test comparison binary snapshot source: %w", err)
	}
	directory, err := os.MkdirTemp("", "glade-local-test-binary-*")
	if err != nil {
		return snapshot, fmt.Errorf("create local test comparison binary snapshot directory: %w", err)
	}
	removeOnError := true
	defer func() {
		if removeOnError {
			os.RemoveAll(directory)
		}
	}()
	if err := os.Chmod(directory, 0o700); err != nil {
		return snapshot, fmt.Errorf("set local test comparison binary snapshot directory mode: %w", err)
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return snapshot, fmt.Errorf("resolve local test comparison binary snapshot directory: %w", err)
	}
	snapshotPath := filepath.Join(directory, "binary")
	if err := copyLocalTestComparisonFile(ctx, originalPath, snapshotPath, 0o500); err != nil {
		return snapshot, fmt.Errorf("copy stable local test comparison binary snapshot: %w", err)
	}
	binary, err := localTestComparisonBinaryFact(snapshotPath)
	if err != nil {
		return snapshot, err
	}
	directoryInfo, err := os.Lstat(directory)
	if err != nil {
		return snapshot, fmt.Errorf("stat local test comparison binary snapshot directory: %w", err)
	}
	binaryInfo, err := os.Lstat(snapshotPath)
	if err != nil {
		return snapshot, fmt.Errorf("stat local test comparison binary snapshot: %w", err)
	}
	manifestOutput, err := executeLocalTestComparisonManifest(ctx, snapshotPath)
	if err != nil {
		return snapshot, fmt.Errorf("read local test comparison binary snapshot manifest: %w", err)
	}
	if !json.Valid(manifestOutput) {
		return snapshot, errors.New("local test comparison binary snapshot manifest output is not valid JSON")
	}
	snapshot = LocalTestComparisonBinarySnapshot{
		directory:     directory,
		directoryInfo: directoryInfo,
		binary:        binary,
		binaryInfo:    binaryInfo,
		manifest:      append(json.RawMessage(nil), manifestOutput...),
	}
	removeOnError = false
	return snapshot, nil
}

func (snapshot LocalTestComparisonBinarySnapshot) Remove() error {
	if snapshot.directory == "" {
		return nil
	}
	return os.RemoveAll(snapshot.directory)
}

func validateLocalTestComparisonBinarySnapshot(snapshot LocalTestComparisonBinarySnapshot) error {
	if snapshot.directory == "" || snapshot.directoryInfo == nil || snapshot.binary.Path == "" || snapshot.binaryInfo == nil || snapshot.binary.Size < 0 || snapshot.binary.SHA256 == "" || !json.Valid(snapshot.manifest) {
		return errors.New("prepared local test comparison binary snapshot is required")
	}
	return nil
}

func revalidateLocalTestComparisonBinarySnapshot(ctx context.Context, snapshot LocalTestComparisonBinarySnapshot) error {
	if err := validateLocalTestComparisonBinarySnapshot(snapshot); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if filepath.Clean(filepath.Dir(snapshot.binary.Path)) != filepath.Clean(snapshot.directory) {
		return errors.New("local test comparison binary snapshot drift: binary escaped private directory")
	}
	directoryInfo, err := os.Lstat(snapshot.directory)
	if err != nil {
		return fmt.Errorf("local test comparison binary snapshot drift: %w", err)
	}
	if !directoryInfo.IsDir() || directoryInfo.Mode().Perm() != 0o700 || !os.SameFile(snapshot.directoryInfo, directoryInfo) {
		return errors.New("local test comparison binary snapshot drift: directory identity or mode changed")
	}
	if err := validateLocalTestComparisonOwner(directoryInfo); err != nil {
		return fmt.Errorf("local test comparison binary snapshot drift: %w", err)
	}
	binaryInfo, err := os.Lstat(snapshot.binary.Path)
	if err != nil {
		return fmt.Errorf("local test comparison binary snapshot drift: %w", err)
	}
	if !binaryInfo.Mode().IsRegular() || binaryInfo.Mode().Perm() != 0o500 || !os.SameFile(snapshot.binaryInfo, binaryInfo) || binaryInfo.Size() != snapshot.binary.Size {
		return errors.New("local test comparison binary snapshot drift: binary identity, mode, or size changed")
	}
	if err := validateLocalTestComparisonOwner(binaryInfo); err != nil {
		return fmt.Errorf("local test comparison binary snapshot drift: %w", err)
	}
	digest, size, err := hashStableLocalTestComparisonSource(ctx, snapshot.binary.Path, snapshot.binaryInfo)
	if err != nil {
		return fmt.Errorf("local test comparison binary snapshot drift: %w", err)
	}
	if size != snapshot.binary.Size || fmt.Sprintf("%x", digest) != snapshot.binary.SHA256 {
		return errors.New("local test comparison binary snapshot drift: checksum changed")
	}
	manifest, err := executeLocalTestComparisonManifest(ctx, snapshot.binary.Path)
	if err != nil {
		return fmt.Errorf("local test comparison binary snapshot drift: manifest execution failed: %w", err)
	}
	if !bytes.Equal(manifest, snapshot.manifest) {
		return errors.New("local test comparison binary snapshot drift: manifest changed")
	}
	return nil
}

func executeLocalTestComparisonManifest(ctx context.Context, binaryPath string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	homeDir, err := os.MkdirTemp("", "glade-local-test-manifest-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(homeDir)
	if err := os.Chmod(homeDir, 0o700); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binaryPath, "manifest", "--json")
	if err := configureLocalTestComparisonProcess(cmd); err != nil {
		return nil, err
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = localTestComparisonChildEnvironment(1, homeDir)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	waitErr := cmd.Wait()
	cleanupErr := cleanupLocalTestComparisonProcess(cmd)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, errors.Join(contextErr, waitErr, cleanupErr)
	}
	if waitErr != nil || cleanupErr != nil {
		return nil, errors.Join(waitErr, cleanupErr)
	}
	return stdout.Bytes(), nil
}

func RunLocalTestComparisonInvocation(ctx context.Context, options LocalTestComparisonInvocationOptions) (LocalTestComparisonInvocationResult, error) {
	var result LocalTestComparisonInvocationResult
	if ctx == nil {
		return result, errors.New("local test comparison invocation context is required")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := revalidateLocalTestComparisonBinarySnapshot(ctx, options.Snapshot); err != nil {
		return result, err
	}
	if strings.TrimSpace(options.SourceProject) == "" {
		return result, errors.New("local test comparison invocation source project is required")
	}
	if strings.TrimSpace(options.ArtifactDir) == "" {
		return result, errors.New("local test comparison invocation artifact directory is required")
	}
	if options.Workers < 1 {
		return result, errors.New("local test comparison invocation workers must be at least 1")
	}
	if err := validateLocalTestComparisonTargetManifest(LocalTestComparisonTargetManifest{
		SchemaVersion: 1,
		Targets:       []LocalTestComparisonTarget{options.Target},
	}); err != nil {
		return result, fmt.Errorf("validate local test comparison invocation target: %w", err)
	}

	binaryPath := options.Snapshot.binary.Path
	projectPath, err := filepath.Abs(options.SourceProject)
	if err != nil {
		return result, fmt.Errorf("resolve local test comparison invocation source project: %w", err)
	}
	artifactDir, err := filepath.Abs(options.ArtifactDir)
	if err != nil {
		return result, fmt.Errorf("resolve local test comparison invocation artifact directory: %w", err)
	}
	resolvedProjectPath, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		return result, fmt.Errorf("resolve local test comparison invocation source project symlinks: %w", err)
	}
	resolvedArtifactDir, err := resolveLocalTestComparisonPath(artifactDir)
	if err != nil {
		return result, fmt.Errorf("resolve local test comparison invocation artifact symlinks: %w", err)
	}
	insideProject, err := pathIsWithin(resolvedArtifactDir, resolvedProjectPath)
	if err != nil {
		return result, fmt.Errorf("compare local test invocation paths: %w", err)
	}
	if insideProject {
		return result, errors.New("local test comparison invocation artifact directory must be outside the source project")
	}
	artifactDir = resolvedArtifactDir
	artifactParentPath := filepath.Dir(artifactDir)
	artifactParent, err := os.Lstat(artifactParentPath)
	if err != nil {
		return result, fmt.Errorf("stat local test comparison invocation canonical artifact parent: %w", err)
	}
	if err := validateLocalTestComparisonArtifactParent(artifactParent); err != nil {
		return result, fmt.Errorf("validate local test comparison invocation canonical artifact parent: %w", err)
	}
	artifactParentFile, err := os.Open(artifactParentPath)
	if err != nil {
		return result, fmt.Errorf("open local test comparison invocation canonical artifact parent: %w", err)
	}
	defer artifactParentFile.Close()
	heldArtifactParent, err := artifactParentFile.Stat()
	if err != nil || !os.SameFile(artifactParent, heldArtifactParent) {
		return result, errors.New("local test comparison invocation canonical artifact parent changed while opening")
	}

	result = LocalTestComparisonInvocationResult{
		ArtifactDir: artifactDir,
		ResultPath:  filepath.Join(artifactDir, "result.json"),
		PerfPath:    filepath.Join(artifactDir, "perf.json"),
		StderrPath:  filepath.Join(artifactDir, "stderr.txt"),
		MetricsPath: filepath.Join(artifactDir, "metrics.json"),
	}
	if options.Target.CPUProfile {
		result.CPUProfilePath = filepath.Join(artifactDir, "cpu.pprof")
	}

	tempRoot, err := os.MkdirTemp("", "glade-local-test-compare-*")
	if err != nil {
		return result, fmt.Errorf("create local test comparison invocation temporary root: %w", err)
	}
	defer os.RemoveAll(tempRoot)
	copiedProject := filepath.Join(tempRoot, "project")
	if options.testHooks != nil && options.testHooks.beforeProjectCopy != nil {
		if err := options.testHooks.beforeProjectCopy(); err != nil {
			return result, err
		}
	}
	if err := copyLocalTestComparisonProject(ctx, resolvedProjectPath, copiedProject, options.testHooks); err != nil {
		return result, fmt.Errorf("copy local test comparison invocation project: %w", err)
	}
	if err := revalidateLocalTestComparisonBinarySnapshot(ctx, options.Snapshot); err != nil {
		return result, err
	}
	homeDir := filepath.Join(tempRoot, "home")
	if err := os.Mkdir(homeDir, 0o700); err != nil {
		return result, fmt.Errorf("create local test comparison invocation private home: %w", err)
	}

	args := []string{
		"local-tests",
		"--project", copiedProject,
		"--parallel", strconv.Itoa(options.Workers),
		"--parallel-methods",
		"--perf-json", result.PerfPath,
		"--json",
	}
	if options.Target.Class != "" {
		args = append(args, "--class", options.Target.Class)
	}
	if options.Target.Method != "" {
		args = append(args, "--method", options.Target.Method)
	}
	if options.Target.CPUProfile {
		args = append(args, "--cpu-profile", result.CPUProfilePath)
	}
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	if err := configureLocalTestComparisonProcess(cmd); err != nil {
		return result, err
	}
	cmd.Env = localTestComparisonChildEnvironment(options.Workers, homeDir)

	if err := ctx.Err(); err != nil {
		return result, err
	}
	beforeMkdir, err := os.Lstat(artifactParentPath)
	if err != nil || !os.SameFile(heldArtifactParent, beforeMkdir) {
		return result, errors.New("local test comparison invocation canonical artifact parent changed before artifact creation")
	}
	if err := validateLocalTestComparisonArtifactParent(beforeMkdir); err != nil {
		return result, err
	}
	artifactName := filepath.Base(artifactDir)
	artifactDirectory, err := createLocalTestComparisonArtifactDirectory(artifactParentFile, artifactName)
	if err != nil {
		return result, fmt.Errorf("create local test comparison invocation artifact directory exclusively: %w", err)
	}
	launched := false
	artifactDirectoryOpen := true
	artifactFileNames := []string{"result.json", "stderr.txt", "perf.json", "metrics.json"}
	if options.Target.CPUProfile {
		artifactFileNames = append(artifactFileNames, "cpu.pprof")
	}
	defer func() {
		if !launched {
			_ = removeLocalTestComparisonArtifactDirectory(artifactParentFile, artifactDirectory, artifactName, artifactFileNames)
			artifactDirectoryOpen = false
		}
		if artifactDirectoryOpen {
			_ = artifactDirectory.Close()
		}
	}()
	artifactDirectoryInfo, err := artifactDirectory.Stat()
	if err != nil {
		return result, err
	}
	if err := validateLocalTestComparisonArtifactParent(artifactDirectoryInfo); err != nil {
		return result, err
	}
	if err := validateLocalTestComparisonArtifactDirectory(artifactParentFile, artifactDirectory, artifactName); err != nil {
		return result, err
	}
	afterMkdirHeld, heldErr := artifactParentFile.Stat()
	afterMkdirPath, pathErr := os.Lstat(artifactParentPath)
	if heldErr != nil || pathErr != nil || !os.SameFile(heldArtifactParent, afterMkdirHeld) || !os.SameFile(heldArtifactParent, afterMkdirPath) {
		return result, errors.New("local test comparison invocation canonical artifact parent changed during artifact creation")
	}
	if err := validateLocalTestComparisonArtifactParent(afterMkdirHeld); err != nil {
		return result, err
	}
	if err := validateLocalTestComparisonArtifactParent(afterMkdirPath); err != nil {
		return result, err
	}
	if options.testHooks != nil && options.testHooks.afterArtifactDirectoryCreate != nil {
		if err := options.testHooks.afterArtifactDirectoryCreate(); err != nil {
			return result, err
		}
	}
	afterHookPath, err := os.Lstat(artifactParentPath)
	if err != nil || !os.SameFile(heldArtifactParent, afterHookPath) {
		return result, errors.New("local test comparison invocation artifact parent changed before file creation")
	}
	if err := validateLocalTestComparisonArtifactDirectory(artifactParentFile, artifactDirectory, artifactName); err != nil {
		return result, err
	}
	perfFile, err := createLocalTestComparisonArtifactFile(artifactDirectory, "perf.json")
	if err != nil {
		return result, fmt.Errorf("create local test comparison invocation perf artifact: %w", err)
	}
	perfArtifactInfo, perfStatErr := perfFile.Stat()
	if perfStatErr != nil {
		perfFile.Close()
		return result, fmt.Errorf("stat local test comparison invocation perf artifact: %w", perfStatErr)
	}
	if err := perfFile.Close(); err != nil {
		return result, err
	}
	var cpuArtifactInfo os.FileInfo
	if options.Target.CPUProfile {
		cpuFile, err := createLocalTestComparisonArtifactFile(artifactDirectory, "cpu.pprof")
		if err != nil {
			return result, fmt.Errorf("create local test comparison invocation CPU profile artifact: %w", err)
		}
		cpuArtifactInfo, err = cpuFile.Stat()
		if err != nil {
			cpuFile.Close()
			return result, fmt.Errorf("stat local test comparison invocation CPU profile artifact: %w", err)
		}
		if err := cpuFile.Close(); err != nil {
			return result, err
		}
	}

	stdout, err := createLocalTestComparisonArtifactFile(artifactDirectory, "result.json")
	if err != nil {
		return result, fmt.Errorf("create local test comparison invocation result artifact: %w", err)
	}
	stderr, err := createLocalTestComparisonArtifactFile(artifactDirectory, "stderr.txt")
	if err != nil {
		stdout.Close()
		return result, fmt.Errorf("create local test comparison invocation stderr artifact: %w", err)
	}

	cmd.Stdout = stdout
	cmd.Stderr = stderr
	startedAt := time.Now()
	startErr := cmd.Start()
	if startErr == nil {
		launched = true
	}
	var waitErr error
	var cleanupErr error
	if startErr == nil {
		waitErr = cmd.Wait()
		cleanupErr = cleanupLocalTestComparisonProcess(cmd)
	}
	wallTime := time.Since(startedAt)
	stdoutCloseErr := stdout.Close()
	stderrCloseErr := stderr.Close()
	artifactBindingErr := validateLocalTestComparisonArtifactDirectory(artifactParentFile, artifactDirectory, artifactName)
	currentArtifactParent, currentParentErr := os.Lstat(artifactParentPath)
	if currentParentErr != nil || !os.SameFile(heldArtifactParent, currentArtifactParent) {
		artifactBindingErr = errors.Join(artifactBindingErr, errors.New("local test comparison invocation artifact parent changed after child exit"))
	}
	modeErr := setLocalTestComparisonArtifactModes(artifactDirectory, result)

	metrics := LocalTestComparisonInvocationMetrics{
		WallTimeNS:          wallTime.Nanoseconds(),
		ExitCode:            -1,
		PhysicalProjectRoot: copiedProject,
		LogicalSourceRoot:   resolvedProjectPath,
	}
	var rssErr error
	if cmd.ProcessState != nil {
		metrics.ExitCode = cmd.ProcessState.ExitCode()
		metrics.UserTimeNS = cmd.ProcessState.UserTime().Nanoseconds()
		metrics.SystemTimeNS = cmd.ProcessState.SystemTime().Nanoseconds()
		maxRSSBytes, err := localTestComparisonMaxRSSBytes(cmd.ProcessState)
		if err != nil {
			rssErr = err
		} else {
			metrics.MaxRSSBytes = maxRSSBytes
		}
	}

	artifactSpecs := []struct {
		name         string
		displayName  string
		file         string
		path         string
		required     bool
		expectedInfo os.FileInfo
		validate     func(io.Reader) error
	}{
		{name: "result", file: "result.json", path: result.ResultPath, required: true},
		{name: "stderr", file: "stderr.txt", path: result.StderrPath, required: true},
		{
			name:         "perf",
			displayName:  "perf",
			file:         "perf.json",
			path:         result.PerfPath,
			required:     startErr == nil && waitErr == nil,
			expectedInfo: perfArtifactInfo,
			validate:     validateLocalTestComparisonPerfArtifact,
		},
	}
	if options.Target.CPUProfile {
		artifactSpecs = append(artifactSpecs, struct {
			name         string
			displayName  string
			file         string
			path         string
			required     bool
			expectedInfo os.FileInfo
			validate     func(io.Reader) error
		}{
			name:         "cpuProfile",
			displayName:  "CPU profile",
			file:         "cpu.pprof",
			path:         result.CPUProfilePath,
			required:     startErr == nil && waitErr == nil,
			expectedInfo: cpuArtifactInfo,
			validate:     validateLocalTestComparisonCPUProfileArtifact,
		})
	}
	var artifactErr error
	for _, spec := range artifactSpecs {
		artifact, metricErr := localTestComparisonArtifactMetric(
			artifactDirectory,
			spec.name,
			spec.displayName,
			spec.file,
			spec.path,
			spec.expectedInfo,
			spec.validate,
		)
		if metricErr != nil {
			if spec.required && artifactErr == nil {
				artifactErr = metricErr
			}
			continue
		}
		metrics.Artifacts = append(metrics.Artifacts, artifact)
	}
	result.Metrics = metrics
	if err := writeLocalTestComparisonJSONExclusiveArtifact(artifactDirectory, "metrics.json", metrics); err != nil {
		return result, errors.Join(ctx.Err(), cleanupErr, artifactBindingErr, modeErr, rssErr, fmt.Errorf("write local test comparison invocation metrics: %w", err))
	}

	if startErr != nil {
		return result, fmt.Errorf("start local test comparison invocation: %w", startErr)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return result, errors.Join(fmt.Errorf("local test comparison invocation canceled: %w", contextErr), cleanupErr, artifactBindingErr, modeErr, rssErr, stdoutCloseErr, stderrCloseErr, artifactErr)
	}
	if waitErr != nil {
		return result, errors.Join(fmt.Errorf("local test comparison invocation exited with code %d: %w", metrics.ExitCode, waitErr), cleanupErr, artifactBindingErr, modeErr, rssErr, stdoutCloseErr, stderrCloseErr, artifactErr)
	}
	if cleanupErr != nil {
		return result, cleanupErr
	}
	if artifactBindingErr != nil {
		return result, artifactBindingErr
	}
	if modeErr != nil {
		return result, modeErr
	}
	if rssErr != nil {
		return result, rssErr
	}
	if stdoutCloseErr != nil {
		return result, fmt.Errorf("close local test comparison invocation result artifact: %w", stdoutCloseErr)
	}
	if stderrCloseErr != nil {
		return result, fmt.Errorf("close local test comparison invocation stderr artifact: %w", stderrCloseErr)
	}
	if artifactErr != nil {
		return result, artifactErr
	}
	return result, nil
}

func WriteLocalTestComparisonEnvironment(ctx context.Context, path string, options LocalTestComparisonEnvironmentOptions) error {
	if ctx == nil {
		return errors.New("local test comparison environment context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("local test comparison environment path is required")
	}
	if err := revalidateLocalTestComparisonBinarySnapshot(ctx, options.Snapshot); err != nil {
		return err
	}
	if options.Workers < 1 {
		return errors.New("local test comparison environment workers must be at least 1")
	}
	if options.Runs < 1 {
		return errors.New("local test comparison environment runs must be at least 1")
	}

	environment := LocalTestComparisonEnvironment{
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		GoVersion:   runtime.Version(),
		LogicalCPUs: runtime.NumCPU(),
		Workers:     options.Workers,
		Runs:        options.Runs,
		Tuning: LocalTestComparisonEnvironmentTuning{
			GOMAXPROCS: strconv.Itoa(options.Workers),
			GOGC:       os.Getenv("GOGC"),
			GOMEMLIMIT: os.Getenv("GOMEMLIMIT"),
		},
		Binary:   options.Snapshot.binary,
		Manifest: append(json.RawMessage(nil), options.Snapshot.manifest...),
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeLocalTestComparisonJSONExclusive(path, environment); err != nil {
		return fmt.Errorf("write local test comparison environment: %w", err)
	}
	return nil
}

type localTestComparisonProjectEntry struct {
	relative string
	info     os.FileInfo
	digest   string
}

func copyLocalTestComparisonProject(ctx context.Context, source, destination string, testHooks *localTestComparisonInvocationTestHooks) (err error) {
	manifest, err := scanLocalTestComparisonProject(ctx, source)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			os.RemoveAll(destination)
		}
	}()
	type copiedDirectory struct {
		path string
		mode os.FileMode
	}
	var directories []copiedDirectory
	for _, entry := range manifest {
		if err := ctx.Err(); err != nil {
			return err
		}
		sourcePath := source
		targetPath := destination
		if entry.relative != "." {
			sourcePath = filepath.Join(source, entry.relative)
			targetPath = filepath.Join(destination, entry.relative)
		}
		if entry.info.IsDir() {
			if err := os.Mkdir(targetPath, 0o700); err != nil {
				return err
			}
			directories = append(directories, copiedDirectory{path: targetPath, mode: entry.info.Mode().Perm()})
			continue
		}
		if err := copyLocalTestComparisonFile(ctx, sourcePath, targetPath, entry.info.Mode().Perm(), entry.info); err != nil {
			return err
		}
		if testHooks != nil && testHooks.afterProjectFileCopy != nil {
			if err := testHooks.afterProjectFileCopy(entry.relative); err != nil {
				return err
			}
		}
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := os.Chmod(directories[i].path, directories[i].mode); err != nil {
			return err
		}
	}
	after, err := scanLocalTestComparisonProject(ctx, source)
	if err != nil {
		return err
	}
	if !sameLocalTestComparisonProjectManifest(manifest, after) {
		return errors.New("source project changed during stable copy")
	}
	complete = true
	return nil
}

func scanLocalTestComparisonProject(ctx context.Context, source string) ([]localTestComparisonProjectEntry, error) {
	rootInfo, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("source project root is a symlink")
	}
	if !rootInfo.IsDir() {
		return nil, errors.New("source project root is not a directory")
	}
	var manifest []localTestComparisonProjectEntry
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("reject symlink %q", filepath.Base(path))
		}
		if path != source && localTestComparisonSkippedDirectory(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("reject non-regular project entry %q", filepath.Base(path))
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		manifestEntry := localTestComparisonProjectEntry{relative: relative, info: info}
		if info.Mode().IsRegular() {
			digest, size, err := hashStableLocalTestComparisonSource(ctx, path, info)
			if err != nil {
				return err
			}
			if size != info.Size() {
				return errors.New("source project changed while hashing manifest")
			}
			manifestEntry.digest = fmt.Sprintf("%x", digest)
		}
		manifest = append(manifest, manifestEntry)
		return nil
	})
	return manifest, err
}

func sameLocalTestComparisonProjectManifest(before, after []localTestComparisonProjectEntry) bool {
	if len(before) != len(after) {
		return false
	}
	for i := range before {
		if before[i].relative != after[i].relative || before[i].info.Mode() != after[i].info.Mode() || before[i].info.Size() != after[i].info.Size() || before[i].digest != after[i].digest || !os.SameFile(before[i].info, after[i].info) {
			return false
		}
	}
	return true
}

func localTestComparisonSkippedDirectory(name string) bool {
	switch name {
	case ".glade", ".git", ".sf", ".sfdx", "node_modules":
		return true
	default:
		return false
	}
}

func copyLocalTestComparisonFile(ctx context.Context, source, destination string, mode os.FileMode, expected ...os.FileInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	before, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return errors.New("source changed before stable file copy")
	}
	if len(expected) > 0 && !os.SameFile(expected[0], before) {
		return errors.New("source identity changed before stable file copy")
	}
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	opened, err := sourceFile.Stat()
	if err != nil {
		return err
	}
	after, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || !after.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(before, after) {
		return errors.New("source changed during stable file copy")
	}
	destinationFile, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	hash := sha256.New()
	copied, copyErr := io.Copy(io.MultiWriter(destinationFile, hash), localTestComparisonContextReader{ctx: ctx, reader: sourceFile})
	closeErr := destinationFile.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	secondHash, secondSize, err := hashStableLocalTestComparisonSource(ctx, source, before)
	if err != nil {
		return err
	}
	if copied != secondSize || !bytes.Equal(hash.Sum(nil), secondHash) {
		return errors.New("source content changed during stable file copy")
	}
	return os.Chmod(destination, mode)
}

type localTestComparisonContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader localTestComparisonContextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func hashStableLocalTestComparisonSource(ctx context.Context, path string, expected os.FileInfo) ([]byte, int64, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if !before.Mode().IsRegular() || !os.SameFile(expected, before) {
		return nil, 0, errors.New("source identity changed before stable rehash")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if !opened.Mode().IsRegular() || !after.Mode().IsRegular() || !os.SameFile(expected, opened) || !os.SameFile(expected, after) {
		return nil, 0, errors.New("source identity changed during stable rehash")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, localTestComparisonContextReader{ctx: ctx, reader: file})
	if err != nil {
		return nil, 0, err
	}
	return hash.Sum(nil), size, nil
}

func localTestComparisonChildEnvironment(workers int, homeDir string) []string {
	var environment []string
	for _, key := range []string{
		"PATH", "TMPDIR", "TMP", "TEMP",
		"LANG", "LANGUAGE", "TZ",
		"LC_ALL", "LC_CTYPE", "LC_MESSAGES", "LC_COLLATE", "LC_MONETARY", "LC_NUMERIC", "LC_TIME",
		"LC_PAPER", "LC_NAME", "LC_ADDRESS", "LC_TELEPHONE", "LC_MEASUREMENT", "LC_IDENTIFICATION",
		"GOGC", "GOMEMLIMIT",
	} {
		if value, ok := os.LookupEnv(key); ok {
			environment = append(environment, key+"="+value)
		}
	}
	environment = append(environment, "HOME="+homeDir, "GOMAXPROCS="+strconv.Itoa(workers))
	return environment
}

func setLocalTestComparisonArtifactModes(directory *os.File, result LocalTestComparisonInvocationResult) error {
	files := []string{"result.json", "stderr.txt", "perf.json"}
	if result.CPUProfilePath != "" {
		files = append(files, "cpu.pprof")
	}
	var modeErr error
	for _, name := range files {
		file, err := openLocalTestComparisonArtifactFile(directory, name)
		if err != nil {
			if !os.IsNotExist(err) {
				modeErr = errors.Join(modeErr, fmt.Errorf("open local test comparison artifact for private mode: %w", err))
			}
			continue
		}
		if err := file.Chmod(0o600); err != nil {
			modeErr = errors.Join(modeErr, fmt.Errorf("set private mode on local test comparison artifact: %w", err))
		}
		if err := file.Close(); err != nil {
			modeErr = errors.Join(modeErr, err)
		}
	}
	return modeErr
}

func localTestComparisonMaxRSSBytes(state *os.ProcessState) (uint64, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return 0, fmt.Errorf("max RSS capture is unsupported on %s", runtime.GOOS)
	}
	usage := reflect.ValueOf(state.SysUsage())
	if usage.Kind() == reflect.Pointer {
		if usage.IsNil() {
			return 0, errors.New("max RSS process usage is nil")
		}
		usage = usage.Elem()
	}
	if usage.Kind() != reflect.Struct {
		return 0, errors.New("max RSS process usage is not a struct")
	}
	field := usage.FieldByName("Maxrss")
	if !field.IsValid() || !field.CanInt() {
		return 0, errors.New("max RSS process usage has no integer Maxrss field")
	}
	value := field.Int()
	if value < 0 {
		return 0, errors.New("max RSS process usage is negative")
	}
	bytes := uint64(value)
	if runtime.GOOS == "linux" {
		bytes *= 1024
	}
	return bytes, nil
}

func localTestComparisonArtifactMetric(
	directory *os.File,
	name string,
	displayName string,
	fileName string,
	path string,
	expectedInfo os.FileInfo,
	validate func(io.Reader) error,
) (LocalTestComparisonArtifactMetric, error) {
	file, err := openLocalTestComparisonArtifactFile(directory, fileName)
	if err != nil {
		return LocalTestComparisonArtifactMetric{}, fmt.Errorf("open local test comparison %s artifact: %w", name, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return LocalTestComparisonArtifactMetric{}, fmt.Errorf("stat local test comparison %s artifact: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return LocalTestComparisonArtifactMetric{}, fmt.Errorf("local test comparison %s artifact is not a regular file", name)
	}
	if expectedInfo != nil && !os.SameFile(expectedInfo, info) {
		return LocalTestComparisonArtifactMetric{}, fmt.Errorf("local test comparison %s artifact inode changed", displayName)
	}
	if validate != nil && info.Size() == 0 {
		return LocalTestComparisonArtifactMetric{}, fmt.Errorf("local test comparison %s artifact is empty", displayName)
	}
	hash := sha256.New()
	reader := io.TeeReader(file, hash)
	if validate != nil {
		if err := validate(reader); err != nil {
			return LocalTestComparisonArtifactMetric{}, err
		}
	}
	if _, err := io.Copy(hash, file); err != nil {
		return LocalTestComparisonArtifactMetric{}, fmt.Errorf("checksum local test comparison %s artifact: %w", name, err)
	}
	return LocalTestComparisonArtifactMetric{Name: name, Path: path, Size: info.Size(), SHA256: fmt.Sprintf("%x", hash.Sum(nil))}, nil
}

func validateLocalTestComparisonPerfArtifact(reader io.Reader) error {
	decoder := json.NewDecoder(reader)
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return errors.New("local test comparison perf artifact is not valid JSON")
	}
	if err := decoder.Decode(&value); !errors.Is(err, io.EOF) {
		return errors.New("local test comparison perf artifact is not valid JSON")
	}
	return nil
}

func validateLocalTestComparisonCPUProfileArtifact(reader io.Reader) error {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return errors.New("local test comparison CPU profile artifact is not a valid gzip profile")
	}
	uncompressedSize, readErr := io.Copy(io.Discard, gzipReader)
	closeErr := gzipReader.Close()
	if readErr != nil || closeErr != nil || uncompressedSize == 0 {
		return errors.New("local test comparison CPU profile artifact is not a valid gzip profile")
	}
	return nil
}

func localTestComparisonBinaryFact(path string) (LocalTestComparisonBinaryFact, error) {
	info, err := os.Stat(path)
	if err != nil {
		return LocalTestComparisonBinaryFact{}, fmt.Errorf("stat local test comparison binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return LocalTestComparisonBinaryFact{}, errors.New("local test comparison binary is not a regular file")
	}
	checksum, err := localTestComparisonFileSHA256(path)
	if err != nil {
		return LocalTestComparisonBinaryFact{}, fmt.Errorf("checksum local test comparison binary: %w", err)
	}
	return LocalTestComparisonBinaryFact{Path: path, Size: info.Size(), SHA256: checksum}, nil
}

func localTestComparisonFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func writeLocalTestComparisonJSONExclusive(path string, value any) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encodeErr := encoder.Encode(value)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func writeLocalTestComparisonJSONExclusiveArtifact(directory *os.File, name string, value any) error {
	file, err := createLocalTestComparisonArtifactFile(directory, name)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encodeErr := encoder.Encode(value)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func resolveLocalTestComparisonPath(path string) (string, error) {
	current := filepath.Clean(path)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func validateLocalTestComparisonArtifactParent(info os.FileInfo) error {
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("local test comparison invocation canonical artifact parent must be a private 0700 directory")
	}
	return validateLocalTestComparisonOwner(info)
}

func pathIsWithin(path, directory string) (bool, error) {
	relative, err := filepath.Rel(directory, path)
	if err != nil {
		return false, err
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))), nil
}

type LocalTestPhaseTiming struct {
	Name            string `json:"name"`
	DurationMS      int64  `json:"durationMs"`
	HeapAllocBytes  uint64 `json:"heapAllocBytes,omitempty"`
	TotalAllocBytes uint64 `json:"totalAllocBytes,omitempty"`
	Mallocs         uint64 `json:"mallocs,omitempty"`
	Frees           uint64 `json:"frees,omitempty"`
	NumGC           uint32 `json:"numGC,omitempty"`
}

type LocalTestReport struct {
	Target          string                   `json:"target"`
	Ready           bool                     `json:"ready"`
	Project         string                   `json:"project"`
	DurationMS      int64                    `json:"durationMs,omitempty"`
	CasesDiscovered int                      `json:"casesDiscovered,omitempty"`
	CasesRun        int                      `json:"casesRun,omitempty"`
	TriageStopped   bool                     `json:"triageStopped,omitempty"`
	Dependencies    []typesys.DependencyInfo `json:"dependencies,omitempty"`
	Selection       *watch.TestSelection     `json:"selection,omitempty"`
	Phases          []LocalTestPhaseTiming   `json:"phases,omitempty"`
	Summary         LocalTestSummary         `json:"summary"`
	Outcomes        []LocalTestOutcome       `json:"outcomes"`
	TopFailures     []LocalTestFailureGroup  `json:"topFailures,omitempty"`
	Diagnostics     []diagnostic.Diagnostic  `json:"diagnostics,omitempty"`
	Perf            *LocalTestPerfSummary    `json:"perf,omitempty"`
}

type LocalTestPerfSummary struct {
	GeneratedAt     string                 `json:"generatedAt"`
	Project         string                 `json:"project"`
	DurationMS      int64                  `json:"durationMs"`
	CasesDiscovered int                    `json:"casesDiscovered"`
	CasesRun        int                    `json:"casesRun"`
	Summary         LocalTestSummary       `json:"summary"`
	Phases          []LocalTestPhaseTiming `json:"phases,omitempty"`
	TopSlowClasses  []LocalTestPerfClass   `json:"topSlowClasses,omitempty"`
	TopCloneClasses []LocalTestCloneClass  `json:"topCloneClasses,omitempty"`
	CloneStats      LocalTestCloneStats    `json:"cloneStats"`
	CPUProfilePath  string                 `json:"cpuProfilePath,omitempty"`
	MemProfilePath  string                 `json:"memProfilePath,omitempty"`
}

type LocalTestPerfClass struct {
	Class      string `json:"class"`
	DurationMS int64  `json:"durationMs"`
	Tests      int    `json:"tests"`
}

type LocalTestCloneStats struct {
	CloneRuntimeOrgCalls       uint64 `json:"cloneRuntimeOrgCalls"`
	CloneRuntimeCalls          uint64 `json:"cloneRuntimeCalls"`
	CloneRollbackSnapshotCalls uint64 `json:"cloneRollbackSnapshotCalls"`
	JournalRollbacks           uint64 `json:"journalRollbacks,omitempty"`
	CloneFallbacks             uint64 `json:"cloneFallbacks,omitempty"`
}

type LocalTestCloneClass struct {
	Class       string `json:"class"`
	SetupClones uint64 `json:"setupClones,omitempty"`
	TestClones  uint64 `json:"testClones,omitempty"`
	DurationMS  int64  `json:"durationMs,omitempty"`
}

type LocalTestSummary struct {
	Total          int `json:"total"`
	Pass           int `json:"pass"`
	Fail           int `json:"fail"`
	Unsupported    int `json:"unsupported"`
	LoadErrors     int `json:"loadError"`
	CompileErrors  int `json:"compileError"`
	InternalErrors int `json:"internalError"`
	AssertFailures int `json:"assertFail,omitempty"`
	RuntimeGaps    int `json:"runtimeGap,omitempty"`
	CompileGaps    int `json:"compileGap,omitempty"`
	Timeouts       int `json:"timeout,omitempty"`
}

type LocalTestOutcome struct {
	ProjectLabel        string                 `json:"projectLabel"`
	Class               string                 `json:"class"`
	Method              string                 `json:"method"`
	Outcome             string                 `json:"outcome"`
	Phase               string                 `json:"phase,omitempty"`
	CapabilityID        string                 `json:"capabilityId,omitempty"`
	File                string                 `json:"file,omitempty"`
	Line                int                    `json:"line,omitempty"`
	TopFrame            *testreport.StackFrame `json:"topFrame,omitempty"`
	Error               string                 `json:"error,omitempty"`
	RelatedMetadataFile string                 `json:"relatedMetadataFile,omitempty"`
	DurationMS          int64                  `json:"durationMs,omitempty"`
	TraceEvents         int                    `json:"traceEvents,omitempty"`
	ProfileEvents       int                    `json:"profileEvents,omitempty"`
	ProfileCategories   map[string]int         `json:"profileCategories,omitempty"`
}

type LocalTestFailureGroup struct {
	Outcome      string   `json:"outcome"`
	Phase        string   `json:"phase,omitempty"`
	CapabilityID string   `json:"capabilityId,omitempty"`
	Error        string   `json:"error,omitempty"`
	Count        int      `json:"count"`
	Samples      []string `json:"samples,omitempty"`
}

type LocalTestCorpusBaseline struct {
	Target   string                   `json:"target"`
	Projects []LocalTestCorpusProject `json:"projects"`
}

type LocalTestCorpusProject struct {
	Project  string                     `json:"project"`
	Ready    bool                       `json:"ready"`
	Summary  LocalTestSummary           `json:"summary"`
	Outcomes []LocalTestExpectedOutcome `json:"outcomes"`
}

type LocalTestExpectedOutcome struct {
	Class        string `json:"class"`
	Method       string `json:"method"`
	Outcome      string `json:"outcome"`
	CapabilityID string `json:"capabilityId,omitempty"`
}

type LocalTestCorpusReport struct {
	Target   string                         `json:"target"`
	Ready    bool                           `json:"ready"`
	Baseline string                         `json:"baseline"`
	Projects []LocalTestCorpusProjectResult `json:"projects"`
	Failures []string                       `json:"failures,omitempty"`
}

type LocalTestCorpusProjectResult struct {
	Project  string                     `json:"project"`
	Ready    bool                       `json:"ready"`
	Summary  LocalTestSummary           `json:"summary"`
	Outcomes []LocalTestExpectedOutcome `json:"outcomes"`
}

func RunLocalTests(options LocalTestOptions) (LocalTestReport, error) {
	started := time.Now()
	resetLocalTestPerfCounters(options)
	root := options.Project
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absRoot, _ := filepath.Abs(root)
	report := LocalTestReport{
		Target:  "local Apex test execution readiness",
		Project: absRoot,
	}
	projectLabel := filepath.Base(absRoot)
	if projectLabel == "." || projectLabel == string(filepath.Separator) {
		projectLabel = absRoot
	}
	if _, err := os.Stat(absRoot); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			appendLocalTestLoadError(&report, projectLabel, fmt.Sprintf("project root does not exist: %s", absRoot))
			finalizeLocalTestReport(&report, options, started)
			return report, nil
		}
		return LocalTestReport{}, err
	}
	stopProfile, err := startLocalTestProfiler(options)
	if err != nil {
		return LocalTestReport{}, err
	}
	defer func() {
		if stopProfile != nil {
			_ = stopProfile()
		}
	}()
	if strings.TrimSpace(options.Method) != "" && strings.TrimSpace(options.Class) == "" {
		appendLocalTestLoadError(&report, projectLabel, "--method requires --class")
		finalizeLocalTestReport(&report, options, started)
		return report, nil
	}

	recordLocalTestPhase(&report, options, "load_start", started)
	index, loadDiagnostics, err := loadLocalTestIndex(root)
	recordLocalTestPhase(&report, options, "load_done", started)
	if err != nil {
		outcome := LocalTestOutcome{
			ProjectLabel: projectLabel,
			Outcome:      "load_error",
			Phase:        "load",
			CapabilityID: localTestCapabilityID("load", "", err.Error()),
			Error:        err.Error(),
		}
		report.Diagnostics = loadDiagnostics
		report.Outcomes = append(report.Outcomes, outcome)
		finalizeLocalTestReport(&report, options, started)
		if stopProfile != nil {
			if stopErr := stopProfile(); stopErr != nil {
				return LocalTestReport{}, stopErr
			}
			stopProfile = nil
		}
		if err := maybeWriteLocalTestPerfJSON(report, options); err != nil {
			return LocalTestReport{}, err
		}
		return report, nil
	}
	options, err = loadLocalTestClassFile(options)
	if err != nil {
		outcome := LocalTestOutcome{
			ProjectLabel: projectLabel,
			Outcome:      "load_error",
			Phase:        "load",
			CapabilityID: localTestCapabilityID("load", "", err.Error()),
			Error:        err.Error(),
		}
		report.Outcomes = append(report.Outcomes, outcome)
		finalizeLocalTestReport(&report, options, started)
		if stopProfile != nil {
			if stopErr := stopProfile(); stopErr != nil {
				return LocalTestReport{}, stopErr
			}
			stopProfile = nil
		}
		if err := maybeWriteLocalTestPerfJSON(report, options); err != nil {
			return LocalTestReport{}, err
		}
		return report, nil
	}
	report.Dependencies = append(report.Dependencies, index.Dependencies...)
	report.Diagnostics = append(report.Diagnostics, loadDiagnostics...)

	progress := newLocalTestProgressReporter(options.ProgressWriter)
	recordLocalTestPhase(&report, options, "discover_start", started)
	parallelism := localTestParallelism(options)
	testOpts := apextest.Options{
		Filter:              localTestFilter(options),
		TraceBlocked:        shouldTraceFocusedLocalTests(options),
		SlowTestThresholdMS: options.SlowTestThresholdMS,
		TimeoutMS:           options.TimeoutMS,
		Parallelism:         parallelism,
		Progress:            progress.handle,
		NoDiskCache:         options.NoDiskCache,
	}
	cases := apextest.Discover(index, testOpts)
	cases = filterLocalTestCases(cases, options)
	if err := validateFocusedLocalTestSelection(cases, options); err != nil {
		appendLocalTestLoadError(&report, projectLabel, err.Error())
		finalizeLocalTestReport(&report, options, started)
		if stopProfile != nil {
			if stopErr := stopProfile(); stopErr != nil {
				return LocalTestReport{}, stopErr
			}
			stopProfile = nil
		}
		if err := maybeWriteLocalTestPerfJSON(report, options); err != nil {
			return LocalTestReport{}, err
		}
		return report, nil
	}
	cases = selectChangedLocalTestCases(&report, index, cases, root, options)
	testOpts.ParallelMethods = shouldParallelizeMethods(options, parallelism, len(cases))
	sort.SliceStable(cases, func(i, j int) bool {
		if cases[i].ClassName == cases[j].ClassName {
			return cases[i].MethodName < cases[j].MethodName
		}
		return cases[i].ClassName < cases[j].ClassName
	})
	report.CasesDiscovered = len(cases)
	recordLocalTestPhase(&report, options, fmt.Sprintf("discover_done total=%d", len(cases)), started)
	durations, err := loadLocalTestDurationHistory(options.DurationHistoryPath)
	if err != nil {
		return LocalTestReport{}, err
	}
	options, parallelism = autoTuneLocalTestOptions(options, len(cases), parallelism)
	testOpts.Parallelism = parallelism
	testOpts.ParallelMethods = shouldParallelizeMethods(options, parallelism, len(cases))
	testOpts.ClassDurationMS = durations
	if options.AutoTune {
		recordLocalTestPhase(&report, options, fmt.Sprintf("auto_tune parallel=%d shardIndex=%d shardCount=%d methodParallel=%t", parallelism, options.ShardIndex, options.ShardCount, testOpts.ParallelMethods), started)
	}
	if strings.TrimSpace(options.WriteClassShards) != "" {
		recordLocalTestPhase(&report, options, "shard_write_start", started)
		shardCount := options.ShardCount
		if shardCount <= 0 {
			shardCount = parallelism
		}
		if shardCount <= 0 {
			shardCount = 1
		}
		if err := writeLocalTestClassShardFiles(options.WriteClassShards, planLocalTestClassShards(cases, durations, shardCount)); err != nil {
			return LocalTestReport{}, err
		}
		recordLocalTestPhase(&report, options, fmt.Sprintf("shard_write_done shards=%d", shardCount), started)
		finalizeLocalTestReport(&report, options, started)
		if stopProfile != nil {
			if stopErr := stopProfile(); stopErr != nil {
				return LocalTestReport{}, stopErr
			}
			stopProfile = nil
		}
		if err := maybeWriteLocalTestPerfJSON(report, options); err != nil {
			return LocalTestReport{}, err
		}
		return report, nil
	}
	if options.ShardCount > 0 {
		sharded, err := selectLocalTestShard(cases, durations, options.ShardCount, options.ShardIndex)
		if err != nil {
			return LocalTestReport{}, err
		}
		cases = sharded
		recordLocalTestPhase(&report, options, fmt.Sprintf("shard_select index=%d count=%d cases=%d", options.ShardIndex, options.ShardCount, len(cases)), started)
	}
	progress.setTotal(len(cases))

	if shouldAnalyzeLocalTests(options, len(cases)) {
		recordLocalTestPhase(&report, options, "analyze_start", started)
		semaResult := sema.Analyze(index)
		recordLocalTestPhase(&report, options, fmt.Sprintf("analyze_done diagnostics=%d", len(semaResult.Diagnostics)), started)
		report.Diagnostics = append(report.Diagnostics, semaResult.Diagnostics...)
		if firstError, ok := firstLocalTestError(semaResult.Diagnostics); ok {
			for _, testCase := range cases {
				report.Outcomes = append(report.Outcomes, localTestDiagnosticOutcome(projectLabel, testCase, "compile_gap", "compile", firstError))
			}
			finalizeLocalTestReport(&report, options, started)
			if stopProfile != nil {
				if stopErr := stopProfile(); stopErr != nil {
					return LocalTestReport{}, stopErr
				}
				stopProfile = nil
			}
			if err := maybeWriteLocalTestPerfJSON(report, options); err != nil {
				return LocalTestReport{}, err
			}
			return report, nil
		}
	} else if strings.TrimSpace(options.Class) == "" && strings.TrimSpace(options.Method) == "" {
		recordLocalTestPhase(&report, options, fmt.Sprintf("analyze_skip total=%d", len(cases)), started)
	}

	if options.ProfileOnTimeout {
		testOpts.TraceBlocked = true
	}
	recordLocalTestPhase(&report, options, "run_start", started)
	runOutcomes, casesRun, triageStopped := runLocalTestCases(index, testOpts, cases, projectLabel, options, started)
	progress.finish()
	report.Outcomes = append(report.Outcomes, runOutcomes...)
	report.CasesRun = casesRun
	report.TriageStopped = triageStopped
	recordLocalTestPhase(&report, options, "run_done", started)
	finalizeLocalTestReport(&report, options, started)
	if stopProfile != nil {
		if stopErr := stopProfile(); stopErr != nil {
			return LocalTestReport{}, stopErr
		}
		stopProfile = nil
	}
	if err := maybeWriteLocalTestPerfJSON(report, options); err != nil {
		return LocalTestReport{}, err
	}
	return report, nil
}

func resetLocalTestPerfCounters(options LocalTestOptions) {
	if strings.TrimSpace(options.PerfJSONPath) == "" {
		return
	}
	apextest.ResetPerfCounters()
	storage.ResetCloneStats()
}

func startLocalTestProfiler(options LocalTestOptions) (func() error, error) {
	if strings.TrimSpace(options.CPUProfilePath) == "" && strings.TrimSpace(options.MemProfilePath) == "" {
		return nil, nil
	}
	var cpuFile *os.File
	if path := strings.TrimSpace(options.CPUProfilePath); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		file, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		if err := pprof.StartCPUProfile(file); err != nil {
			_ = file.Close()
			return nil, err
		}
		cpuFile = file
	}
	return func() error {
		if cpuFile != nil {
			pprof.StopCPUProfile()
			if err := cpuFile.Close(); err != nil {
				return err
			}
		}
		if path := strings.TrimSpace(options.MemProfilePath); path != "" {
			runtime.GC()
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			file, err := os.Create(path)
			if err != nil {
				return err
			}
			if err := pprof.WriteHeapProfile(file); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
		return nil
	}, nil
}

func maybeWriteLocalTestPerfJSON(report LocalTestReport, options LocalTestOptions) error {
	if strings.TrimSpace(options.PerfJSONPath) == "" {
		return nil
	}
	perf := localTestPerfSummary(report, options)
	if err := os.MkdirAll(filepath.Dir(options.PerfJSONPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(perf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(options.PerfJSONPath, append(data, '\n'), 0o644)
}

func localTestPerfSummary(report LocalTestReport, options LocalTestOptions) LocalTestPerfSummary {
	apexStats := apextest.SnapshotPerfCounters()
	storageStats := storage.SnapshotCloneStats()
	perf := LocalTestPerfSummary{
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		Project:         report.Project,
		DurationMS:      report.DurationMS,
		CasesDiscovered: report.CasesDiscovered,
		CasesRun:        report.CasesRun,
		Summary:         report.Summary,
		Phases:          append([]LocalTestPhaseTiming(nil), report.Phases...),
		CloneStats: LocalTestCloneStats{
			CloneRuntimeOrgCalls:       apexStats.CloneRuntimeOrgCalls,
			CloneRuntimeCalls:          storageStats.CloneRuntimeCalls,
			CloneRollbackSnapshotCalls: storageStats.CloneRollbackSnapshotCalls,
			JournalRollbacks:           apexStats.JournalRollbacks,
			CloneFallbacks:             apexStats.CloneFallbacks,
		},
		CPUProfilePath: strings.TrimSpace(options.CPUProfilePath),
		MemProfilePath: strings.TrimSpace(options.MemProfilePath),
	}
	perf.TopSlowClasses = localTestTopSlowClasses(report.Outcomes, 15)
	perf.TopCloneClasses = localTestTopCloneClasses(apexStats.CloneClasses, report.Outcomes, 15)
	return perf
}

func localTestTopCloneClasses(classes []apextest.PerfCloneClass, outcomes []LocalTestOutcome, limit int) []LocalTestCloneClass {
	if limit <= 0 || len(classes) == 0 {
		return nil
	}
	durations := make(map[string]int64, len(classes))
	for _, outcome := range outcomes {
		if strings.TrimSpace(outcome.Class) == "" {
			continue
		}
		durations[outcome.Class] += outcome.DurationMS
	}
	out := make([]LocalTestCloneClass, 0, len(classes))
	for _, class := range classes {
		out = append(out, LocalTestCloneClass{
			Class:       class.Class,
			SetupClones: class.SetupClones,
			TestClones:  class.TestClones,
			DurationMS:  durations[class.Class],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i].SetupClones + out[i].TestClones
		right := out[j].SetupClones + out[j].TestClones
		if left == right {
			if out[i].DurationMS == out[j].DurationMS {
				return out[i].Class < out[j].Class
			}
			return out[i].DurationMS > out[j].DurationMS
		}
		return left > right
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func localTestTopSlowClasses(outcomes []LocalTestOutcome, limit int) []LocalTestPerfClass {
	if limit <= 0 {
		return nil
	}
	totals := map[string]*LocalTestPerfClass{}
	for _, outcome := range outcomes {
		if strings.TrimSpace(outcome.Class) == "" {
			continue
		}
		entry := totals[outcome.Class]
		if entry == nil {
			entry = &LocalTestPerfClass{Class: outcome.Class}
			totals[outcome.Class] = entry
		}
		entry.DurationMS += outcome.DurationMS
		entry.Tests++
	}
	if len(totals) == 0 {
		return nil
	}
	classes := make([]LocalTestPerfClass, 0, len(totals))
	for _, entry := range totals {
		classes = append(classes, *entry)
	}
	sort.Slice(classes, func(i, j int) bool {
		if classes[i].DurationMS == classes[j].DurationMS {
			return classes[i].Class < classes[j].Class
		}
		return classes[i].DurationMS > classes[j].DurationMS
	})
	if len(classes) > limit {
		classes = classes[:limit]
	}
	return classes
}

func reportLocalTestPhase(options LocalTestOptions, event string, started time.Time) {
	if options.ProgressWriter != nil {
		elapsed := time.Since(started)
		fmt.Fprintf(options.ProgressWriter, "Phase: %s elapsed=%s\n", event, formatLocalTestProgressDuration(elapsed))
	}
}

func recordLocalTestPhase(report *LocalTestReport, options LocalTestOptions, event string, started time.Time) {
	duration := time.Since(started).Milliseconds()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	report.Phases = append(report.Phases, LocalTestPhaseTiming{
		Name:            event,
		DurationMS:      duration,
		HeapAllocBytes:  mem.HeapAlloc,
		TotalAllocBytes: mem.TotalAlloc,
		Mallocs:         mem.Mallocs,
		Frees:           mem.Frees,
		NumGC:           mem.NumGC,
	})
	if options.ProgressWriter != nil {
		fmt.Fprintf(options.ProgressWriter, "Phase: %s elapsed=%s\n", event, formatLocalTestProgressDuration(time.Duration(duration)*time.Millisecond))
	}
}

const largeLocalTestAnalysisThreshold = 5000
const localTestTriageClassBatchSize = 1

func runLocalTestCases(index typesys.Index, testOpts apextest.Options, cases []apextest.TestCase, projectLabel string, options LocalTestOptions, started time.Time) ([]LocalTestOutcome, int, bool) {
	if options.MaxFailureGroups <= 0 {
		run := apextest.RunCasesContext(context.Background(), index, testOpts, cases)
		outcomes := localTestOutcomesFromRun(projectLabel, run, options)
		outcomes = retryParallelTimeoutOutcomes(index, testOpts, cases, projectLabel, options, outcomes, started)
		return outcomes, len(cases), false
	}
	outcomes := make([]LocalTestOutcome, 0)
	casesRun := 0
	for _, batch := range localTestCaseBatches(cases, localTestTriageBatchClassSize(options)) {
		reportLocalTestPhase(options, fmt.Sprintf("triage_batch_start cases=%d", len(batch)), started)
		run := apextest.RunCasesContext(context.Background(), index, testOpts, batch)
		outcomes = append(outcomes, localTestOutcomesFromRun(projectLabel, run, options)...)
		casesRun += len(batch)
		groups := localTestFailureGroupCount(outcomes)
		reportLocalTestPhase(options, fmt.Sprintf("triage_batch_done casesRun=%d failureGroups=%d", casesRun, groups), started)
		if groups >= options.MaxFailureGroups {
			reportLocalTestPhase(options, fmt.Sprintf("triage_stop casesRun=%d failureGroups=%d", casesRun, groups), started)
			return outcomes, casesRun, true
		}
	}
	return outcomes, casesRun, false
}

func retryParallelTimeoutOutcomes(index typesys.Index, testOpts apextest.Options, cases []apextest.TestCase, projectLabel string, options LocalTestOptions, outcomes []LocalTestOutcome, started time.Time) []LocalTestOutcome {
	if options.Parallelism <= 1 || testOpts.TimeoutMS <= 0 {
		return outcomes
	}
	timeoutCases := localTestTimeoutCases(cases, outcomes)
	if len(timeoutCases) == 0 {
		return outcomes
	}
	reportLocalTestPhase(options, fmt.Sprintf("timeout_retry_start cases=%d", len(timeoutCases)), started)
	retryOpts := testOpts
	retryOpts.Parallelism = 1
	retryOpts.ParallelMethods = false
	retryOpts.Progress = nil
	retryRun := apextest.RunCasesContext(context.Background(), index, retryOpts, timeoutCases)
	retryOutcomes := localTestOutcomesFromRun(projectLabel, retryRun, options)
	reportLocalTestPhase(options, fmt.Sprintf("timeout_retry_done cases=%d", len(retryOutcomes)), started)
	return replaceLocalTestOutcomes(outcomes, retryOutcomes)
}

func localTestTimeoutCases(cases []apextest.TestCase, outcomes []LocalTestOutcome) []apextest.TestCase {
	timeouts := make(map[string]bool)
	for _, outcome := range outcomes {
		if outcome.Outcome != "timeout" {
			continue
		}
		timeouts[localTestCaseKey(outcome.Class, outcome.Method)] = true
	}
	if len(timeouts) == 0 {
		return nil
	}
	retry := make([]apextest.TestCase, 0, len(timeouts))
	for _, testCase := range cases {
		if timeouts[localTestCaseKey(testCase.ClassName, testCase.MethodName)] {
			retry = append(retry, testCase)
		}
	}
	return retry
}

func replaceLocalTestOutcomes(outcomes, replacements []LocalTestOutcome) []LocalTestOutcome {
	if len(replacements) == 0 {
		return outcomes
	}
	byCase := make(map[string]LocalTestOutcome, len(replacements))
	for _, outcome := range replacements {
		byCase[localTestCaseKey(outcome.Class, outcome.Method)] = outcome
	}
	out := append([]LocalTestOutcome(nil), outcomes...)
	for i, outcome := range out {
		if replacement, ok := byCase[localTestCaseKey(outcome.Class, outcome.Method)]; ok {
			out[i] = replacement
		}
	}
	return out
}

func localTestCaseKey(className, methodName string) string {
	return className + "\x00" + methodName
}

func localTestTriageBatchClassSize(options LocalTestOptions) int {
	if options.Parallelism > 1 {
		return options.Parallelism
	}
	return localTestTriageClassBatchSize
}

func localTestOutcomesFromRun(projectLabel string, run testreport.Run, options LocalTestOptions) []LocalTestOutcome {
	outcomes := make([]LocalTestOutcome, 0)
	for _, suite := range run.Suites {
		for _, testCase := range suite.Cases {
			if !matchesLocalTestCase(testCase.ClassName, testCase.MethodName, options) {
				continue
			}
			outcomes = append(outcomes, localTestRunOutcome(projectLabel, testCase))
		}
	}
	return outcomes
}

func localTestCaseBatches(cases []apextest.TestCase, classBatchSize int) [][]apextest.TestCase {
	if classBatchSize <= 0 || len(cases) == 0 {
		return nil
	}
	var batches [][]apextest.TestCase
	var batch []apextest.TestCase
	classesInBatch := 0
	lastClass := ""
	for _, testCase := range cases {
		if testCase.ClassName != lastClass {
			if classesInBatch >= classBatchSize && len(batch) > 0 {
				batches = append(batches, batch)
				batch = nil
				classesInBatch = 0
			}
			classesInBatch++
			lastClass = testCase.ClassName
		}
		batch = append(batch, testCase)
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

func selectChangedLocalTestCases(report *LocalTestReport, index typesys.Index, cases []apextest.TestCase, root string, options LocalTestOptions) []apextest.TestCase {
	if strings.TrimSpace(options.ChangedSince) == "" {
		return cases
	}
	changes, err := watch.GitChangesSince(root, options.ChangedSince)
	if err != nil {
		report.Diagnostics = append(report.Diagnostics, diagnostic.Diagnostic{
			Severity: diagnostic.Error,
			Code:     "changed_since",
			Message:  err.Error(),
		})
		return cases
	}
	selection := watch.SelectAffectedTests(index, changes)
	report.Selection = &selection
	if selection.Mode == watch.SelectionAll {
		return cases
	}
	if selection.Mode == watch.SelectionNone {
		return cases[:0]
	}
	selected := make(map[string]bool, len(selection.TestClasses))
	for _, className := range selection.TestClasses {
		selected[className] = true
	}
	out := cases[:0]
	for _, testCase := range cases {
		if selected[testCase.ClassName] {
			out = append(out, testCase)
		}
	}
	return out
}

func shouldAnalyzeLocalTests(options LocalTestOptions, totalCases int) bool {
	if strings.TrimSpace(options.Class) != "" || strings.TrimSpace(options.Method) != "" {
		return false
	}
	if options.ForceAnalysis {
		return true
	}
	if options.Parallelism > 0 || options.ProgressWriter != nil {
		return false
	}
	if options.BlockersOnly || options.TopFailures > 0 {
		return false
	}
	return totalCases <= largeLocalTestAnalysisThreshold
}

func localTestParallelism(options LocalTestOptions) int {
	if options.Parallelism > 0 {
		return options.Parallelism
	}
	if strings.TrimSpace(options.Method) != "" {
		return 1
	}
	if strings.TrimSpace(options.Class) != "" {
		procs := runtime.GOMAXPROCS(0)
		if procs < 2 {
			return procs
		}
		if procs < 8 {
			return procs
		}
		return 8
	}
	procs := runtime.GOMAXPROCS(0)
	if procs < 1 {
		return 1
	}
	if procs > 8 {
		return 8
	}
	return procs
}

func shouldParallelizeMethods(options LocalTestOptions, parallelism int, totalCases int) bool {
	if options.ParallelMethods {
		return true
	}
	if options.AutoTune && strings.TrimSpace(options.Method) == "" && strings.TrimSpace(options.Class) != "" && parallelism > 1 && totalCases >= 24 {
		return true
	}
	return false
}

func autoTuneLocalTestOptions(options LocalTestOptions, totalCases int, parallelism int) (LocalTestOptions, int) {
	if !options.AutoTune {
		return options, parallelism
	}
	if options.Parallelism == 0 {
		parallelism = autoParallelismForCases(totalCases)
	}
	if options.ShardCount == 0 && options.AutoShardCount {
		if shardCount, ok := localTestEnvInt("GLADE_SHARD_COUNT"); ok && shardCount > 0 {
			options.ShardCount = shardCount
		}
	}
	if options.ShardIndex == 0 && options.AutoShardIndex && options.ShardCount > 0 {
		if shardIndex, ok := localTestEnvInt("GLADE_SHARD_INDEX"); ok && shardIndex >= 0 {
			options.ShardIndex = shardIndex
		}
	}
	if options.ShardCount > 0 && options.ShardIndex >= options.ShardCount {
		options.ShardIndex = 0
	}
	return options, parallelism
}

func autoParallelismForCases(totalCases int) int {
	procs := runtime.GOMAXPROCS(0)
	if procs < 1 {
		procs = 1
	}
	if procs > 12 {
		procs = 12
	}
	switch {
	case totalCases <= 16:
		if procs > 2 {
			return 2
		}
	case totalCases <= 200:
		if procs > 4 {
			return 4
		}
	case totalCases <= 1200:
		if procs > 8 {
			return 8
		}
	default:
		if procs > 4 {
			return 4
		}
	}
	return procs
}

func localTestEnvInt(name string) (int, bool) {
	raw, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	return parsed, true
}

type localTestProgressReporter struct {
	w        io.Writer
	started  time.Time
	last     time.Time
	total    int
	done     int
	passed   int
	failed   int
	errors   int
	active   string
	printed  bool
	finished bool
	mu       sync.Mutex
}

func newLocalTestProgressReporter(w io.Writer) *localTestProgressReporter {
	return &localTestProgressReporter{
		w:       w,
		started: time.Now(),
	}
}

func (r *localTestProgressReporter) setTotal(total int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.total = total
	r.mu.Unlock()
}

func (r *localTestProgressReporter) handle(progress apextest.TestProgress) {
	if r == nil || r.w == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started.IsZero() {
		r.started = time.Now()
	}
	important := false
	completed := false
	name := progress.ClassName
	if progress.MethodName != "" {
		name += "." + progress.MethodName
	}
	switch progress.Event {
	case "compile_start":
		r.active = "compile"
	case "compile_done":
		r.active = "compile"
		important = progress.Status != "" && progress.Status != "pass"
	case "setup_start", "test_start":
		r.active = name
	case "setup_done":
		r.active = name
		if progress.Status != "" && progress.Status != "pass" {
			r.errors++
			important = r.errors <= 10
		}
	case "test_done":
		r.done++
		completed = true
		r.active = name
		switch testreport.Status(progress.Status) {
		case testreport.StatusPass:
			r.passed++
		case testreport.StatusFail:
			r.failed++
			important = r.failed <= 10
		default:
			r.errors++
			important = r.errors <= 10
		}
	}
	now := time.Now()
	if !r.printed || r.done == r.total || important || (completed && r.done > 0 && r.done%25 == 0) || now.Sub(r.last) >= 2*time.Second {
		r.writeLine()
	}
}

func (r *localTestProgressReporter) finish() {
	if r == nil || r.w == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.finished = true
	if !r.printed || r.done < r.total {
		r.writeLine()
	}
}

func (r *localTestProgressReporter) writeLine() {
	line := fmt.Sprintf("Progress: %s elapsed=%s eta=%s pass=%d fail=%d error=%d",
		r.countText(), formatLocalTestProgressDuration(time.Since(r.started)), r.etaText(), r.passed, r.failed, r.errors)
	if r.active != "" {
		line += " running=" + r.active
	}
	fmt.Fprintln(r.w, line)
	r.printed = true
	r.last = time.Now()
}

func (r *localTestProgressReporter) countText() string {
	if r.total > 0 {
		return fmt.Sprintf("%d/%d", r.done, r.total)
	}
	return fmt.Sprintf("%d done", r.done)
}

func (r *localTestProgressReporter) etaText() string {
	if r.total <= 0 || r.done <= 0 || r.done >= r.total {
		return "0s"
	}
	elapsed := time.Since(r.started)
	remaining := time.Duration(int64(elapsed) * int64(r.total-r.done) / int64(r.done))
	return formatLocalTestProgressDuration(remaining)
}

func formatLocalTestProgressDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d/time.Minute), int((d%time.Minute)/time.Second))
	}
	return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int((d%time.Hour)/time.Minute))
}

func shouldTraceFocusedLocalTests(options LocalTestOptions) bool {
	return options.TraceBlocked || options.ProfileOnTimeout
}

func LocalTestReportFromRun(projectRoot string, run testreport.Run) LocalTestReport {
	root := projectRoot
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absRoot, _ := filepath.Abs(root)
	projectLabel := filepath.Base(absRoot)
	if projectLabel == "." || projectLabel == string(filepath.Separator) {
		projectLabel = absRoot
	}
	report := LocalTestReport{
		Target:  "local Apex test execution readiness",
		Project: absRoot,
	}
	for _, suite := range run.Suites {
		for _, testCase := range suite.Cases {
			report.Outcomes = append(report.Outcomes, localTestRunOutcome(projectLabel, testCase))
		}
	}
	finalizeLocalTestReport(&report, LocalTestOptions{}, time.Now())
	report.DurationMS = run.Summary().DurationMS
	return report
}

func CheckLocalTestCorpus(path string) (LocalTestCorpusReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LocalTestCorpusReport{}, err
	}
	var baseline LocalTestCorpusBaseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return LocalTestCorpusReport{}, err
	}
	absPath, _ := filepath.Abs(path)
	report := LocalTestCorpusReport{
		Target:   baseline.Target,
		Ready:    true,
		Baseline: absPath,
	}
	baseDir := filepath.Dir(absPath)
	for _, expected := range baseline.Projects {
		projectPath := expected.Project
		if !filepath.IsAbs(projectPath) {
			projectPath = filepath.Clean(filepath.Join(baseDir, projectPath))
		}
		actual, err := RunLocalTests(LocalTestOptions{Project: projectPath, NoDiskCache: true})
		if err != nil {
			return report, err
		}
		actualProject := LocalTestCorpusProjectResult{
			Project:  expected.Project,
			Ready:    actual.Ready,
			Summary:  actual.Summary,
			Outcomes: stableLocalTestOutcomes(actual.Outcomes),
		}
		report.Projects = append(report.Projects, actualProject)
		if actual.Ready != expected.Ready {
			report.Failures = append(report.Failures, fmt.Sprintf("%s ready = %t, want %t", expected.Project, actual.Ready, expected.Ready))
		}
		if actual.Summary != expected.Summary {
			report.Failures = append(report.Failures, fmt.Sprintf("%s summary = %+v, want %+v", expected.Project, actual.Summary, expected.Summary))
		}
		compareLocalTestOutcomes(expected.Project, actualProject.Outcomes, expected.Outcomes, &report.Failures)
	}
	report.Ready = len(report.Failures) == 0
	if !report.Ready {
		return report, fmt.Errorf("local-tests corpus baseline mismatch: %s", strings.Join(report.Failures, "; "))
	}
	return report, nil
}

func WriteLocalTestJSON(w io.Writer, report LocalTestReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func WriteLocalTestCorpusJSON(w io.Writer, report LocalTestCorpusReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func WriteLocalTestText(w io.Writer, report LocalTestReport) {
	state := "ready"
	if !report.Ready {
		state = "not ready"
	}
	fmt.Fprintf(w, "Local test readiness: %s\n", state)
	fmt.Fprintf(w, "project: %s\n", report.Project)
	if report.CasesDiscovered != 0 || report.CasesRun != 0 || report.TriageStopped {
		fmt.Fprintf(w, "cases: discovered=%d run=%d triage_stopped=%t duration_ms=%d\n", report.CasesDiscovered, report.CasesRun, report.TriageStopped, report.DurationMS)
	}
	fmt.Fprintf(w, "summary: pass=%d fail=%d unsupported=%d load_error=%d compile_error=%d internal_error=%d assert_fail=%d runtime_gap=%d compile_gap=%d timeout=%d total=%d\n",
		report.Summary.Pass,
		report.Summary.Fail,
		report.Summary.Unsupported,
		report.Summary.LoadErrors,
		report.Summary.CompileErrors,
		report.Summary.InternalErrors,
		report.Summary.AssertFailures,
		report.Summary.RuntimeGaps,
		report.Summary.CompileGaps,
		report.Summary.Timeouts,
		report.Summary.Total,
	)
	for _, group := range report.TopFailures {
		fmt.Fprintf(w, "* %s", group.Outcome)
		if group.CapabilityID != "" {
			fmt.Fprintf(w, " [%s]", group.CapabilityID)
		}
		fmt.Fprintf(w, ": %d", group.Count)
		if group.Error != "" {
			fmt.Fprintf(w, " - %s", group.Error)
		}
		if len(group.Samples) != 0 {
			fmt.Fprintf(w, " samples=%s", strings.Join(group.Samples, ","))
		}
		fmt.Fprintln(w)
	}
	for _, outcome := range report.Outcomes {
		if outcome.Outcome == "pass" {
			continue
		}
		fmt.Fprintf(w, "- %s.%s: %s", outcome.Class, outcome.Method, outcome.Outcome)
		if outcome.CapabilityID != "" {
			fmt.Fprintf(w, " [%s]", outcome.CapabilityID)
		}
		if outcome.Error != "" {
			fmt.Fprintf(w, ": %s", outcome.Error)
		}
		fmt.Fprintln(w)
	}
}

func WriteLocalTestCorpusText(w io.Writer, report LocalTestCorpusReport) {
	state := "ready"
	if !report.Ready {
		state = "not ready"
	}
	fmt.Fprintf(w, "Local test corpus: %s\n", state)
	fmt.Fprintf(w, "baseline: %s\n", report.Baseline)
	for _, project := range report.Projects {
		fmt.Fprintf(w, "- %s: ready=%t pass=%d fail=%d unsupported=%d load_error=%d compile_error=%d internal_error=%d assert_fail=%d runtime_gap=%d compile_gap=%d timeout=%d total=%d\n",
			project.Project,
			project.Ready,
			project.Summary.Pass,
			project.Summary.Fail,
			project.Summary.Unsupported,
			project.Summary.LoadErrors,
			project.Summary.CompileErrors,
			project.Summary.InternalErrors,
			project.Summary.AssertFailures,
			project.Summary.RuntimeGaps,
			project.Summary.CompileGaps,
			project.Summary.Timeouts,
			project.Summary.Total,
		)
	}
	for _, failure := range report.Failures {
		fmt.Fprintf(w, "! %s\n", failure)
	}
}

func stableLocalTestOutcomes(outcomes []LocalTestOutcome) []LocalTestExpectedOutcome {
	stable := make([]LocalTestExpectedOutcome, 0, len(outcomes))
	for _, outcome := range outcomes {
		stable = append(stable, LocalTestExpectedOutcome{
			Class:        outcome.Class,
			Method:       outcome.Method,
			Outcome:      outcome.Outcome,
			CapabilityID: outcome.CapabilityID,
		})
	}
	sort.SliceStable(stable, func(i, j int) bool {
		if stable[i].Class == stable[j].Class {
			return stable[i].Method < stable[j].Method
		}
		return stable[i].Class < stable[j].Class
	})
	return stable
}

func compareLocalTestOutcomes(project string, actual, expected []LocalTestExpectedOutcome, failures *[]string) {
	if len(actual) != len(expected) {
		*failures = append(*failures, fmt.Sprintf("%s outcomes length = %d, want %d", project, len(actual), len(expected)))
		return
	}
	for i := range expected {
		if actual[i] != expected[i] {
			*failures = append(*failures, fmt.Sprintf("%s outcome[%d] = %+v, want %+v", project, i, actual[i], expected[i]))
		}
	}
}

func loadLocalTestIndex(root string) (typesys.Index, []diagnostic.Diagnostic, error) {
	p, err := project.Load(root)
	if err != nil {
		return typesys.Index{}, nil, err
	}
	s, err := schema.LoadProject(p)
	if err != nil {
		index := typesys.Build(p, schema.Schema{})
		diag := diagnostic.Diagnostic{
			Severity: diagnostic.Error,
			Code:     "GLADESCHEMA001",
			Message:  fmt.Sprintf("metadata schema load failed: %v", err),
		}
		index.Diagnostics = append(index.Diagnostics, diag)
		return index, []diagnostic.Diagnostic{diag}, nil
	}
	return typesys.Build(p, s), nil, nil
}

func localTestFilter(options LocalTestOptions) string {
	switch {
	case options.Class != "" && options.Method != "":
		return options.Class + "." + options.Method
	case options.Class != "":
		return options.Class
	case options.Method != "":
		return options.Method
	default:
		return ""
	}
}

func loadLocalTestClassFile(options LocalTestOptions) (LocalTestOptions, error) {
	if strings.TrimSpace(options.ClassFile) == "" {
		return options, nil
	}
	data, err := os.ReadFile(options.ClassFile)
	if err != nil {
		return options, fmt.Errorf("read --class-file: %w", err)
	}
	for _, line := range strings.FieldsFunc(string(data), func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';'
	}) {
		if className := strings.TrimSpace(line); className != "" && !strings.HasPrefix(className, "#") {
			options.ClassList = append(options.ClassList, className)
		}
	}
	return options, nil
}

func filterLocalTestCases(cases []apextest.TestCase, options LocalTestOptions) []apextest.TestCase {
	out := make([]apextest.TestCase, 0, len(cases))
	for _, testCase := range cases {
		if matchesLocalTestCase(testCase.ClassName, testCase.MethodName, options) {
			out = append(out, testCase)
		}
	}
	return out
}

func validateFocusedLocalTestSelection(cases []apextest.TestCase, options LocalTestOptions) error {
	if len(cases) != 0 {
		return nil
	}
	class := strings.TrimSpace(options.Class)
	method := strings.TrimSpace(options.Method)
	switch {
	case class != "" && method != "":
		return fmt.Errorf("no Apex test methods matched --class %q --method %q", class, method)
	case class != "":
		return fmt.Errorf("no Apex test methods matched --class %q", class)
	case strings.TrimSpace(options.ClassFile) != "":
		return fmt.Errorf("no Apex test methods matched --class-file %q", options.ClassFile)
	case len(options.ClassList) != 0:
		return fmt.Errorf("no Apex test methods matched --class-list")
	default:
		return nil
	}
}

func matchesLocalTestCase(className, methodName string, options LocalTestOptions) bool {
	if options.Class != "" && !strings.EqualFold(className, options.Class) {
		return false
	}
	if strings.TrimSpace(options.StartClass) != "" && strings.ToLower(className) < strings.ToLower(strings.TrimSpace(options.StartClass)) {
		return false
	}
	if len(options.ClassList) != 0 && !localTestClassListContains(options.ClassList, className) {
		return false
	}
	if options.Method != "" && !strings.EqualFold(methodName, options.Method) {
		return false
	}
	return true
}

func localTestClassListContains(classes []string, className string) bool {
	for _, candidate := range classes {
		if strings.EqualFold(strings.TrimSpace(candidate), className) {
			return true
		}
	}
	return false
}

func firstLocalTestError(diagnostics []diagnostic.Diagnostic) (diagnostic.Diagnostic, bool) {
	for _, diag := range diagnostics {
		if diag.Severity == diagnostic.Error {
			return diag, true
		}
	}
	return diagnostic.Diagnostic{}, false
}

func localTestDiagnosticOutcome(projectLabel string, testCase apextest.TestCase, outcome, phase string, diag diagnostic.Diagnostic) LocalTestOutcome {
	line := testCase.Range.Start.Line
	if diag.Range != nil && diag.Range.Start.Line > 0 {
		line = diag.Range.Start.Line
	}
	file := testCase.File
	if diag.File != "" {
		file = diag.File
	}
	return LocalTestOutcome{
		ProjectLabel: projectLabel,
		Class:        testCase.ClassName,
		Method:       testCase.MethodName,
		Outcome:      outcome,
		Phase:        phase,
		CapabilityID: localTestCapabilityID(phase, diag.Code, diag.Message),
		File:         file,
		Line:         line,
		Error:        diag.Message,
	}
}

func localTestRunOutcome(projectLabel string, testCase testreport.Case) LocalTestOutcome {
	outcome := "pass"
	phase := ""
	if testCase.Status != testreport.StatusPass {
		phase = "execute"
	}
	switch testCase.Status {
	case testreport.StatusPass:
		outcome = "pass"
	case testreport.StatusUnsupported:
		outcome = "unsupported"
	case testreport.StatusCompileError:
		outcome = "compile_gap"
		phase = "compile"
	case testreport.StatusRuntimeError:
		outcome = "internal_error"
	default:
		outcome = "runtime_gap"
	}
	if testCase.Problem != nil && strings.EqualFold(testCase.Problem.Type, "UnsupportedFeature") {
		outcome = "unsupported"
	}
	if testCase.Problem != nil && isLocalTestTimeoutProblem(testCase.Problem.Type, testCase.Problem.Message) {
		outcome = "timeout"
		phase = "timeout"
	}
	if testCase.Problem != nil && strings.Contains(strings.ToLower(testCase.Problem.Type+" "+testCase.Problem.Message), "assert") {
		outcome = "assert_fail"
		phase = "assert"
	}
	out := LocalTestOutcome{
		ProjectLabel: projectLabel,
		Class:        testCase.ClassName,
		Method:       testCase.MethodName,
		Outcome:      outcome,
		Phase:        phase,
		DurationMS:   testCase.DurationMS,
	}
	if len(testCase.Trace) > 0 {
		out.TraceEvents = len(testCase.Trace)
	}
	if testCase.Profile != nil {
		out.ProfileEvents = testCase.Profile.Events
		out.ProfileCategories = testCase.Profile.Categories
	}
	if testCase.Problem != nil {
		out.Error = testCase.Problem.Message
		out.CapabilityID = localTestCapabilityID(phase, testCase.Problem.Type, testCase.Problem.Message)
		if len(testCase.Problem.Stack) > 0 {
			frame := testCase.Problem.Stack[0]
			out.TopFrame = &frame
			out.File = frame.File
			out.Line = frame.Line
		}
	}
	return out
}

func localTestCapabilityID(phase, code, message string) string {
	code = strings.TrimSpace(code)
	if strings.EqualFold(code, "UnsupportedFeature") {
		return "apex.test.unsupported"
	}
	if isLocalTestTimeoutProblem(code, message) {
		return "apex.test.timeout"
	}
	if strings.EqualFold(code, "Canceled") {
		return "apex.test.canceled"
	}
	if code != "" {
		return strings.ToLower(code)
	}
	text := strings.ToLower(message)
	switch {
	case strings.Contains(text, "unsupported"):
		return "apex.test.unsupported"
	case strings.Contains(text, "metadata"):
		return "metadata.load"
	case strings.Contains(text, "parse"):
		return "apex.parse"
	case strings.Contains(text, "panic") || strings.Contains(text, "internal"):
		return "glade.internal"
	case phase != "":
		return "apex.test." + strings.ReplaceAll(phase, "_", "-")
	default:
		return "apex.test.unknown"
	}
}

func appendLocalTestLoadError(report *LocalTestReport, projectLabel, message string) {
	report.Outcomes = append(report.Outcomes, LocalTestOutcome{
		ProjectLabel: projectLabel,
		Outcome:      "load_error",
		Phase:        "load",
		CapabilityID: localTestCapabilityID("load", "", message),
		Error:        message,
	})
}

func isLocalTestTimeoutProblem(code, message string) bool {
	text := strings.ToLower(strings.TrimSpace(code + " " + message))
	return strings.Contains(text, "deadline exceeded") || strings.Contains(text, "context canceled")
}

func finalizeLocalTestReport(report *LocalTestReport, options LocalTestOptions, started time.Time) {
	if options.BlockersOnly {
		outcomes := report.Outcomes[:0]
		for _, outcome := range report.Outcomes {
			if outcome.Outcome != "pass" {
				outcomes = append(outcomes, outcome)
			}
		}
		report.Outcomes = outcomes
	}
	for _, outcome := range report.Outcomes {
		report.Summary.Total++
		switch outcome.Outcome {
		case "pass":
			report.Summary.Pass++
		case "fail":
			report.Summary.Fail++
		case "unsupported":
			report.Summary.Unsupported++
		case "load_error":
			report.Summary.LoadErrors++
		case "compile_error":
			report.Summary.CompileErrors++
		case "internal_error":
			report.Summary.InternalErrors++
		case "assert_fail":
			report.Summary.AssertFailures++
		case "runtime_gap":
			report.Summary.RuntimeGaps++
		case "compile_gap":
			report.Summary.CompileGaps++
		case "timeout":
			report.Summary.Timeouts++
		}
	}
	if options.TopFailures > 0 {
		report.TopFailures = localTestTopFailures(report.Outcomes, options.TopFailures)
	}
	report.Ready = report.Summary.Fail == 0 &&
		report.Summary.Unsupported == 0 &&
		report.Summary.LoadErrors == 0 &&
		report.Summary.CompileErrors == 0 &&
		report.Summary.InternalErrors == 0 &&
		report.Summary.AssertFailures == 0 &&
		report.Summary.RuntimeGaps == 0 &&
		report.Summary.CompileGaps == 0 &&
		report.Summary.Timeouts == 0
	report.DurationMS = time.Since(started).Milliseconds()
}

func localTestTopFailures(outcomes []LocalTestOutcome, limit int) []LocalTestFailureGroup {
	if limit <= 0 {
		return nil
	}
	counts := localTestFailureGroupStats(outcomes)
	groups := make([]LocalTestFailureGroup, 0, len(counts))
	for key, stat := range counts {
		groups = append(groups, LocalTestFailureGroup{
			Outcome:      key.outcome,
			Phase:        key.phase,
			CapabilityID: key.capabilityID,
			Error:        key.error,
			Count:        stat.count,
			Samples:      stat.samples,
		})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		if groups[i].Outcome != groups[j].Outcome {
			return groups[i].Outcome < groups[j].Outcome
		}
		if groups[i].CapabilityID != groups[j].CapabilityID {
			return groups[i].CapabilityID < groups[j].CapabilityID
		}
		return groups[i].Error < groups[j].Error
	})
	if len(groups) > limit {
		groups = groups[:limit]
	}
	return groups
}

type localTestFailureGroupKey struct {
	outcome      string
	phase        string
	capabilityID string
	error        string
}

func localTestFailureGroupCount(outcomes []LocalTestOutcome) int {
	return len(localTestFailureGroups(outcomes))
}

func localTestFailureGroups(outcomes []LocalTestOutcome) map[localTestFailureGroupKey]int {
	stats := localTestFailureGroupStats(outcomes)
	counts := make(map[localTestFailureGroupKey]int)
	for key, stat := range stats {
		counts[key] = stat.count
	}
	return counts
}

type localTestFailureGroupStat struct {
	count   int
	samples []string
}

func localTestFailureGroupStats(outcomes []LocalTestOutcome) map[localTestFailureGroupKey]localTestFailureGroupStat {
	counts := make(map[localTestFailureGroupKey]localTestFailureGroupStat)
	for _, outcome := range outcomes {
		if outcome.Outcome == "pass" {
			continue
		}
		key := localTestFailureGroupKey{
			outcome:      outcome.Outcome,
			phase:        outcome.Phase,
			capabilityID: outcome.CapabilityID,
			error:        normalizeLocalTestFailureError(outcome.Error),
		}
		stat := counts[key]
		stat.count++
		if len(stat.samples) < 3 {
			stat.samples = append(stat.samples, outcome.Class+"."+outcome.Method)
		}
		counts[key] = stat
	}
	return counts
}

func normalizeLocalTestFailureError(errText string) string {
	errText = strings.TrimSpace(errText)
	if errText == "" {
		return ""
	}
	lines := strings.Split(errText, "\n")
	errText = strings.TrimSpace(lines[0])
	if strings.Contains(errText, "context deadline exceeded") {
		return "context deadline exceeded"
	}
	if strings.Contains(errText, "HTTP transport is not available") {
		return "HTTP transport is not available"
	}
	for _, prefix := range []string{
		"System.AssertException:",
		"Assertion Failed:",
		"Database.insert failed:",
		"Database.update failed:",
		"Database.delete failed:",
		"soql:",
	} {
		if strings.HasPrefix(errText, prefix) {
			errText = prefix + " " + strings.TrimSpace(strings.TrimPrefix(errText, prefix))
			break
		}
	}
	errText = normalizeLocalTestDynamicText(errText)
	if len(errText) > 180 {
		errText = errText[:180]
	}
	return errText
}

func normalizeLocalTestDynamicText(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch >= '0' && ch <= '9' {
			j := i + 1
			for j < len(text) && text[j] >= '0' && text[j] <= '9' {
				j++
			}
			out.WriteByte('#')
			i = j - 1
			continue
		}
		out.WriteByte(ch)
	}
	return out.String()
}

func IsLocalTestTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}
