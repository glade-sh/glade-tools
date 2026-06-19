package lwcparity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type oracleCaptureFile struct {
	CapturedAt string              `json:"capturedAt"`
	Rows       []oracleCaptureRow  `json:"rows"`
	Cases      []oracleCaptureCase `json:"cases"`
}

type oracleCaptureRow struct {
	ID           string `json:"id"`
	Category     string `json:"category"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	OracleTest   string `json:"oracleTest"`
	Test         string `json:"test"`
	OracleStatus string `json:"oracleStatus"`
	Status       string `json:"status"`
	LastVerified string `json:"lastVerified"`
}

type oracleCaptureCase struct {
	Name     string `json:"name"`
	Feature  string `json:"feature"`
	Status   string `json:"status"`
	Metadata struct {
		Route string `json:"route"`
	} `json:"metadata"`
}

func ReconcileOracleCapture(report Report, captureJSON []byte) (Report, int, error) {
	var capture oracleCaptureFile
	if err := json.Unmarshal(captureJSON, &capture); err != nil {
		return Report{}, 0, err
	}
	rows := map[string]oracleCaptureRow{}
	for _, row := range capture.Rows {
		key := oracleCaptureKey(firstNonEmpty(row.ID, rowID(firstNonEmpty(row.Category, row.Type), row.Name)))
		if key == "" {
			continue
		}
		rows[key] = row
	}
	for _, c := range capture.Cases {
		key := oracleCaptureKey(c.Name)
		if key == "" {
			continue
		}
		rows[key] = oracleCaptureRow{
			ID:           c.Name,
			OracleTest:   c.Metadata.Route,
			OracleStatus: c.Status,
		}
	}

	out := report
	out.Rows = append([]Row(nil), report.Rows...)
	count := 0
	for i := range out.Rows {
		row := &out.Rows[i]
		key := oracleCaptureKey(firstNonEmpty(row.ID, rowID(row.Category, row.Name)))
		captured, ok := rows[key]
		if !ok {
			finalizeRowContract(row)
			continue
		}
		row.OracleTest = firstNonEmpty(captured.OracleTest, captured.Test, row.OracleTest)
		row.OracleStatus = firstNonEmpty(captured.OracleStatus, captured.Status, row.OracleStatus, StatusOracleMissing)
		row.LastVerified = firstNonEmpty(captured.LastVerified, capture.CapturedAt, row.LastVerified)
		finalizeRowContract(row)
		count++
	}
	out.Summary = summarize(out.Rows)
	return out, count, nil
}

func CheckRequiredInventory(report Report) error {
	categories := map[string]int{}
	gaps := 0
	for _, row := range report.Rows {
		categories[row.Category]++
		if row.Status == StatusLocalOnly || row.ParityTier == parityTierForStatus(StatusLocalOnly) {
			gaps++
		}
	}
	var problems []string
	for _, category := range []string{CategoryAPIModule, CategorySalesforceModule, CategoryPageReference} {
		if categories[category] == 0 {
			problems = append(problems, "missing "+category+" docs inventory")
		}
	}
	if gaps > 0 {
		problems = append(problems, fmt.Sprintf("%d local-only row(s)", gaps))
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("LWC docs inventory gaps: %s; pass --allow-inventory-gaps to write the ledger anyway", strings.Join(problems, ", "))
}

func CheckReportGates(report Report, failOn []string, requireOracle bool) error {
	failSet := map[string]bool{}
	for _, status := range failOn {
		status = strings.TrimSpace(status)
		if status != "" {
			failSet[status] = true
		}
	}
	var problems []string
	if len(failSet) > 0 {
		counts := map[string]int{}
		for _, row := range report.Rows {
			if failSet[row.Status] {
				counts[row.Status]++
			}
		}
		for _, status := range sortedKeys(counts) {
			problems = append(problems, fmt.Sprintf("%s=%d", status, counts[status]))
		}
	}
	if requireOracle {
		missing := 0
		for _, row := range report.Rows {
			if strings.TrimSpace(row.OracleStatus) == "" || row.OracleStatus == StatusOracleMissing {
				missing++
			}
		}
		if missing > 0 {
			problems = append(problems, fmt.Sprintf("oracle-missing=%d", missing))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("LWC parity gate failed: %s", strings.Join(problems, ", "))
}

func CheckOracleFixtures(report Report, fixtureDir string) error {
	tempDir, err := os.MkdirTemp("", "glade-lwc-oracle-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	if _, err := WriteOracleFixtures(report, tempDir); err != nil {
		return err
	}
	expected, err := snapshotFiles(tempDir)
	if err != nil {
		return err
	}
	actual, err := snapshotFiles(fixtureDir)
	if err != nil {
		return err
	}
	if len(expected) != len(actual) {
		return fmt.Errorf("LWC oracle fixture drift: expected %d files, found %d", len(expected), len(actual))
	}
	for _, path := range sortedFileKeys(expected) {
		got, ok := actual[path]
		if !ok {
			return fmt.Errorf("LWC oracle fixture drift: missing %s", path)
		}
		if !bytes.Equal(expected[path], got) {
			return fmt.Errorf("LWC oracle fixture drift: %s differs", path)
		}
	}
	for _, path := range sortedFileKeys(actual) {
		if _, ok := expected[path]; !ok {
			return fmt.Errorf("LWC oracle fixture drift: unexpected %s", path)
		}
	}
	return nil
}

func oracleCaptureKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func snapshotFiles(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".glade" {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	return files, err
}

func sortedFileKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
