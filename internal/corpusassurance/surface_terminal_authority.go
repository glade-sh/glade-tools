package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

const (
	terminalVersionCurrentAPI = "version-current-api-exclusion"
	terminalHostedContext     = "hosted-context-boundary"
)

type SurfaceTerminalAuthorityRequest struct {
	ScopePath          string
	CoveragePath       string
	LedgerPath         string
	SupportPolicyPath  string
	FixtureRoot        string
	ClassificationPath string
	OutputPath         string
}

type SurfaceTerminalAuthority struct {
	SchemaVersion          int                           `json:"schemaVersion"`
	ScopeSHA256            string                        `json:"scopeSha256"`
	SourceProfileSHA256    string                        `json:"sourceProfileSha256"`
	SourceCoverageSHA256   string                        `json:"sourceCoverageSha256"`
	DirectCoverageSHA256   string                        `json:"directCoverageSha256"`
	LedgerSHA256           string                        `json:"ledgerSha256"`
	SupportPolicySHA256    string                        `json:"supportPolicySha256"`
	ClassificationSHA256   string                        `json:"classificationSha256"`
	FixtureSetSHA256       string                        `json:"fixtureSetSha256"`
	RowsSHA256             string                        `json:"rowsSha256"`
	Count                  int                           `json:"count"`
	ByClass                map[string]int                `json:"byClass"`
	LocalRuntimeCredit     int                           `json:"localRuntimeCredit"`
	SalesforceParityCredit int                           `json:"salesforceParityCredit"`
	Rows                   []SurfaceTerminalAuthorityRow `json:"rows"`
}

type SurfaceTerminalAuthorityRow struct {
	SurfaceID string                             `json:"surfaceId"`
	Class     string                             `json:"class"`
	Reason    string                             `json:"reason"`
	Policy    SurfaceTerminalPolicyProvenance    `json:"policy"`
	Ledger    SurfaceTerminalLedgerProvenance    `json:"ledger"`
	Fixtures  []SurfaceTerminalFixtureProvenance `json:"fixtures"`
}

type SurfaceTerminalPolicyProvenance struct {
	Disposition string `json:"disposition"`
	MatchRule   string `json:"matchRule"`
	Reason      string `json:"reason"`
}

type SurfaceTerminalLedgerProvenance struct {
	SHA256  string   `json:"sha256"`
	Sources []string `json:"sources"`
}

type SurfaceTerminalFixtureProvenance struct {
	ID                        string `json:"id"`
	File                      string `json:"file"`
	SHA256                    string `json:"sha256"`
	SalesforceEligible        *bool  `json:"salesforceEligible,omitempty"`
	SalesforceExclusionClass  string `json:"salesforceExclusionClass,omitempty"`
	SalesforceExclusionReason string `json:"salesforceExclusionReason,omitempty"`
}

type SurfaceTerminalAccounting struct {
	AuthoritySHA256        string                  `json:"authoritySha256"`
	DirectLocalProof       int                     `json:"directLocalProof"`
	TerminalAccounted      int                     `json:"terminalAccounted"`
	Accounted              int                     `json:"accounted"`
	Required               int                     `json:"required"`
	Remaining              int                     `json:"remaining"`
	ByClass                map[string]int          `json:"byClass"`
	LocalRuntimeCredit     int                     `json:"localRuntimeCredit"`
	SalesforceParityCredit int                     `json:"salesforceParityCredit"`
	ActionableMissing      []SurfaceOracleScopeRow `json:"actionableMissing"`
}

func CreateSurfaceTerminalAuthority(request SurfaceTerminalAuthorityRequest) (SurfaceTerminalAuthority, error) {
	for _, path := range []string{request.ScopePath, request.CoveragePath, request.LedgerPath, request.SupportPolicyPath, request.FixtureRoot, request.ClassificationPath, request.OutputPath} {
		if !filepath.IsAbs(path) {
			return SurfaceTerminalAuthority{}, fmt.Errorf("absolute surface terminal-authority paths are required")
		}
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return SurfaceTerminalAuthority{}, fmt.Errorf("surface terminal-authority output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return SurfaceTerminalAuthority{}, err
	}
	if stat, err := os.Stat(request.FixtureRoot); err != nil || !stat.IsDir() {
		return SurfaceTerminalAuthority{}, fmt.Errorf("fixture root is not a directory: %s", request.FixtureRoot)
	}
	scope, scopeBytes, err := readExactJSONBytes[SurfaceOracleScope](request.ScopePath)
	if err != nil {
		return SurfaceTerminalAuthority{}, err
	}
	coverage, coverageBytes, err := readExactJSONBytes[SurfaceLocalProofCoverage](request.CoveragePath)
	if err != nil {
		return SurfaceTerminalAuthority{}, err
	}
	ledger, ledgerBytes, err := readExactJSONBytes[surfaceledger.SurfaceLedger](request.LedgerPath)
	if err != nil {
		return SurfaceTerminalAuthority{}, err
	}
	policy, policyBytes, err := readExactJSONBytes[surfaceledger.SupportPolicy](request.SupportPolicyPath)
	if err != nil {
		return SurfaceTerminalAuthority{}, err
	}
	classifications, classificationBytes, err := readExactJSONBytes[ExclusionPolicy](request.ClassificationPath)
	if err != nil {
		return SurfaceTerminalAuthority{}, err
	}
	scopeSHA, ledgerSHA, policySHA := replayBytesSHA256(scopeBytes), replayBytesSHA256(ledgerBytes), replayBytesSHA256(policyBytes)
	if coverage.SchemaVersion != 1 || coverage.ScopeSHA256 != scopeSHA || coverage.Total != scope.Total || coverage.Covered+coverage.MissingCount != coverage.Total || coverage.MissingCount != len(coverage.Missing) || scope.LedgerSHA256 != ledgerSHA || scope.PolicySHA256 != policySHA {
		return SurfaceTerminalAuthority{}, fmt.Errorf("surface terminal authority inputs do not reconcile")
	}
	scopeRows := make(map[string]SurfaceOracleScopeRow, len(scope.Rows))
	for _, row := range scope.Rows {
		if row.SurfaceID == "" || scopeRows[row.SurfaceID].SurfaceID != "" {
			return SurfaceTerminalAuthority{}, fmt.Errorf("invalid or duplicate scope surface %q", row.SurfaceID)
		}
		scopeRows[row.SurfaceID] = row
	}
	missing := make(map[string]bool, len(coverage.Missing))
	for _, row := range coverage.Missing {
		if scopeRows[row.SurfaceID] != row || missing[row.SurfaceID] {
			return SurfaceTerminalAuthority{}, fmt.Errorf("invalid or duplicate missing surface %q", row.SurfaceID)
		}
		missing[row.SurfaceID] = true
	}
	if classifications.SchemaVersion != 1 || len(classifications.Rows) == 0 {
		return SurfaceTerminalAuthority{}, fmt.Errorf("invalid terminal classification policy")
	}
	ledgerRows := make(map[string]surfaceledger.SurfaceLedgerRow, len(ledger.Rows))
	for _, row := range ledger.Rows {
		ledgerRows[row.SurfaceID] = row
	}
	profile := surfaceledger.ComputeSupportProfile(ledger.Rows, policy, nil)
	if len(profile.ValidationErrors) != 0 {
		return SurfaceTerminalAuthority{}, fmt.Errorf("support policy does not produce a valid profile")
	}
	policyRows := make(map[string]surfaceledger.SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		policyRows[row.SurfaceID] = row
	}
	targets := make(map[string]bool, len(classifications.Rows))
	for _, row := range classifications.Rows {
		targets[row.SurfaceID] = true
	}
	fixtures, fixtureSetSHA, err := terminalFixtureProvenance(request.FixtureRoot, targets)
	if err != nil {
		return SurfaceTerminalAuthority{}, err
	}
	authority := SurfaceTerminalAuthority{SchemaVersion: 1, ScopeSHA256: scopeSHA, SourceProfileSHA256: scope.SourceProfileSHA256, SourceCoverageSHA256: replayBytesSHA256(coverageBytes), DirectCoverageSHA256: surfaceDirectCoverageSHA256(coverage), LedgerSHA256: ledgerSHA, SupportPolicySHA256: policySHA, ClassificationSHA256: replayBytesSHA256(classificationBytes), FixtureSetSHA256: fixtureSetSHA, ByClass: map[string]int{}, Rows: make([]SurfaceTerminalAuthorityRow, 0, len(classifications.Rows))}
	seen := make(map[string]bool, len(classifications.Rows))
	for _, classification := range classifications.Rows {
		if classification.SurfaceID == "" || classification.Reason == "" || seen[classification.SurfaceID] || !missing[classification.SurfaceID] || (classification.Class != terminalVersionCurrentAPI && classification.Class != terminalHostedContext) {
			return SurfaceTerminalAuthority{}, fmt.Errorf("invalid, duplicate, or not currently missing terminal surface %q", classification.SurfaceID)
		}
		ledgerRow, ledgerOK := ledgerRows[classification.SurfaceID]
		policyRow, policyOK := policyRows[classification.SurfaceID]
		if !ledgerOK || !policyOK || string(policyRow.Disposition) != scopeRows[classification.SurfaceID].Disposition {
			return SurfaceTerminalAuthority{}, fmt.Errorf("terminal surface provenance is incomplete for %q", classification.SurfaceID)
		}
		ledgerRowBytes, err := json.Marshal(ledgerRow)
		if err != nil {
			return SurfaceTerminalAuthority{}, err
		}
		seen[classification.SurfaceID] = true
		authority.ByClass[classification.Class]++
		fixtureRows := fixtures[classification.SurfaceID]
		if fixtureRows == nil {
			fixtureRows = []SurfaceTerminalFixtureProvenance{}
		}
		authority.Rows = append(authority.Rows, SurfaceTerminalAuthorityRow{
			SurfaceID: classification.SurfaceID,
			Class:     classification.Class,
			Reason:    classification.Reason,
			Policy:    SurfaceTerminalPolicyProvenance{Disposition: string(policyRow.Disposition), MatchRule: policyRow.MatchRule, Reason: policyRow.Reason},
			Ledger:    SurfaceTerminalLedgerProvenance{SHA256: replayBytesSHA256(ledgerRowBytes), Sources: append([]string(nil), ledgerRow.Sources...)},
			Fixtures:  fixtureRows,
		})
	}
	sort.Slice(authority.Rows, func(i, j int) bool { return authority.Rows[i].SurfaceID < authority.Rows[j].SurfaceID })
	authority.Count = len(authority.Rows)
	authority.RowsSHA256 = surfaceTerminalRowsSHA256(authority.Rows)
	if err := verifySurfaceLocalProofInputs(map[string]string{request.ScopePath: scopeSHA, request.LedgerPath: ledgerSHA, request.SupportPolicyPath: policySHA, request.ClassificationPath: authority.ClassificationSHA256}); err != nil {
		return SurfaceTerminalAuthority{}, err
	}
	if after, err := proofInputSHA256(request.CoveragePath); err != nil || after != authority.SourceCoverageSHA256 {
		return SurfaceTerminalAuthority{}, fmt.Errorf("source coverage changed during terminal authorization")
	}
	if after, err := terminalFixtureSetSHA256(request.FixtureRoot, authority.Rows); err != nil || after != fixtureSetSHA {
		return SurfaceTerminalAuthority{}, fmt.Errorf("fixture root changed during terminal authorization")
	}
	if err := WriteNewJSON(request.OutputPath, authority); err != nil {
		return SurfaceTerminalAuthority{}, err
	}
	return authority, nil
}

func ApplySurfaceTerminalAuthority(coverage SurfaceLocalProofCoverage, authority SurfaceTerminalAuthority, authoritySHA, fixtureSetSHA string) (SurfaceTerminalAccounting, error) {
	if authority.SchemaVersion != 1 || !sha256Pattern.MatchString(authoritySHA) || authority.ScopeSHA256 != coverage.ScopeSHA256 || authority.DirectCoverageSHA256 != surfaceDirectCoverageSHA256(coverage) || authority.Count != len(authority.Rows) || authority.RowsSHA256 != surfaceTerminalRowsSHA256(authority.Rows) || authority.LocalRuntimeCredit != 0 || authority.SalesforceParityCredit != 0 {
		return SurfaceTerminalAccounting{}, fmt.Errorf("terminal authority does not bind current direct coverage")
	}
	if authority.FixtureSetSHA256 != fixtureSetSHA {
		return SurfaceTerminalAccounting{}, fmt.Errorf("terminal authority does not bind the current fixture set")
	}
	missing := make(map[string]bool, len(coverage.Missing))
	for _, row := range coverage.Missing {
		missing[row.SurfaceID] = true
	}
	terminal := make(map[string]bool, len(authority.Rows))
	counts := make(map[string]int)
	for i, row := range authority.Rows {
		if row.SurfaceID == "" || row.Reason == "" || terminal[row.SurfaceID] || !missing[row.SurfaceID] || (i > 0 && authority.Rows[i-1].SurfaceID >= row.SurfaceID) || (row.Class != terminalVersionCurrentAPI && row.Class != terminalHostedContext) {
			return SurfaceTerminalAccounting{}, fmt.Errorf("terminal surface %q is invalid or not currently missing", row.SurfaceID)
		}
		terminal[row.SurfaceID] = true
		counts[row.Class]++
	}
	if !sameStringCounts(counts, authority.ByClass) {
		return SurfaceTerminalAccounting{}, fmt.Errorf("terminal authority class counts do not reconcile")
	}
	actionable := make([]SurfaceOracleScopeRow, 0, coverage.MissingCount-len(terminal))
	for _, row := range coverage.Missing {
		if !terminal[row.SurfaceID] {
			actionable = append(actionable, row)
		}
	}
	return SurfaceTerminalAccounting{AuthoritySHA256: authoritySHA, DirectLocalProof: coverage.Covered, TerminalAccounted: len(terminal), Accounted: coverage.Covered + len(terminal), Required: coverage.Total, Remaining: len(actionable), ByClass: counts, ActionableMissing: actionable}, nil
}

func surfaceDirectCoverageSHA256(coverage SurfaceLocalProofCoverage) string {
	coverage.TerminalAccounting = nil
	data, _ := json.Marshal(coverage)
	return replayBytesSHA256(data)
}

func surfaceTerminalRowsSHA256(rows []SurfaceTerminalAuthorityRow) string {
	data, _ := json.Marshal(rows)
	return replayBytesSHA256(data)
}

func terminalFixtureProvenance(root string, targets map[string]bool) (map[string][]SurfaceTerminalFixtureProvenance, string, error) {
	items, err := os.ReadDir(root)
	if err != nil {
		return nil, "", err
	}
	result := make(map[string][]SurfaceTerminalFixtureProvenance)
	var seal strings.Builder
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, item.Name()))
		if err != nil {
			return nil, "", err
		}
		sha := replayBytesSHA256(data)
		var fixture struct {
			Name     string `json:"name"`
			Evidence []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
			SalesforceEligible        *bool  `json:"salesforceEligible"`
			SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
			SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		}
		if json.Unmarshal(data, &fixture) != nil || fixture.Name == "" {
			continue
		}
		matched := false
		for _, evidence := range fixture.Evidence {
			if !targets[evidence.SurfaceID] {
				continue
			}
			matched = true
			result[evidence.SurfaceID] = append(result[evidence.SurfaceID], SurfaceTerminalFixtureProvenance{ID: fixture.Name, File: item.Name(), SHA256: sha, SalesforceEligible: fixture.SalesforceEligible, SalesforceExclusionClass: fixture.SalesforceExclusionClass, SalesforceExclusionReason: fixture.SalesforceExclusionReason})
		}
		if matched {
			seal.WriteString(item.Name())
			seal.WriteByte(0)
			seal.WriteString(sha)
			seal.WriteByte('\n')
		}
	}
	for surfaceID := range result {
		sort.Slice(result[surfaceID], func(i, j int) bool {
			if result[surfaceID][i].ID != result[surfaceID][j].ID {
				return result[surfaceID][i].ID < result[surfaceID][j].ID
			}
			return result[surfaceID][i].File < result[surfaceID][j].File
		})
	}
	return result, replayBytesSHA256([]byte(seal.String())), nil
}

func terminalFixtureSetSHA256(root string, rows []SurfaceTerminalAuthorityRow) (string, error) {
	targets := make(map[string]bool, len(rows))
	for _, row := range rows {
		targets[row.SurfaceID] = true
	}
	_, sha, err := terminalFixtureProvenance(root, targets)
	return sha, err
}

func sameStringCounts(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
