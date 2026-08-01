package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type supportProfileArtifact struct {
	Total         int            `json:"total"`
	ByDisposition map[string]int `json:"byDisposition"`
	ByGapClass    map[string]int `json:"byGapClass"`
	Rows          []struct {
		SurfaceID string `json:"surfaceId"`
	} `json:"rows"`
}

type surfaceStatusArtifact struct {
	Total         int            `json:"total"`
	ByDisposition map[string]int `json:"byDisposition"`
	ByGapClass    map[string]int `json:"byGapClass"`
	Rows          []struct {
		SurfaceID string `json:"surfaceId"`
	} `json:"rows"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/verify-apex-surface-status.go <profile.json> <status.html>")
		os.Exit(2)
	}
	profileData, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail(err)
	}
	var profile supportProfileArtifact
	if err := json.Unmarshal(profileData, &profile); err != nil {
		fail(fmt.Errorf("decode profile JSON: %w", err))
	}
	htmlData, err := os.ReadFile(os.Args[2])
	if err != nil {
		fail(err)
	}
	payload, err := extractPageData(string(htmlData))
	if err != nil {
		fail(err)
	}
	var page surfaceStatusArtifact
	if err := json.Unmarshal([]byte(payload), &page); err != nil {
		fail(fmt.Errorf("decode embedded page data: %w", err))
	}
	if err := reconcile(profile, page); err != nil {
		fail(err)
	}
	fmt.Printf("verified Apex surface status: total=%d rows=%d dispositions=%d gaps=%d\n", page.Total, len(page.Rows), len(page.ByDisposition), len(page.ByGapClass))
}

func extractPageData(html string) (string, error) {
	const open = `<script id="page-data" type="application/json">`
	start := strings.Index(html, open)
	if start < 0 {
		return "", fmt.Errorf("missing page-data script")
	}
	start += len(open)
	end := strings.Index(html[start:], "</script>")
	if end < 0 {
		return "", fmt.Errorf("missing page-data closing tag")
	}
	payload := html[start : start+end]
	if strings.Contains(payload, "</script>") {
		return "", fmt.Errorf("embedded JSON contains an unescaped script close")
	}
	return payload, nil
}

func reconcile(profile supportProfileArtifact, page surfaceStatusArtifact) error {
	if page.Total != profile.Total {
		return fmt.Errorf("total mismatch: profile=%d page=%d", profile.Total, page.Total)
	}
	if len(profile.Rows) != profile.Total || len(page.Rows) != page.Total {
		return fmt.Errorf("row count mismatch: profile total/rows=%d/%d page total/rows=%d/%d", profile.Total, len(profile.Rows), page.Total, len(page.Rows))
	}
	for _, disposition := range []string{
		"local-runtime-required",
		"deterministic-mock-required",
		"compile-shape-required",
		"hosted-deferred",
	} {
		if page.ByDisposition[disposition] != profile.ByDisposition[disposition] {
			return fmt.Errorf("disposition mismatch for %q: profile=%d page=%d", disposition, profile.ByDisposition[disposition], page.ByDisposition[disposition])
		}
	}
	if len(page.ByDisposition) != 4 {
		return fmt.Errorf("page disposition key count=%d, want 4", len(page.ByDisposition))
	}
	if len(page.ByGapClass) != len(profile.ByGapClass) {
		return fmt.Errorf("gap key count mismatch: profile=%d page=%d", len(profile.ByGapClass), len(page.ByGapClass))
	}
	for gap, want := range profile.ByGapClass {
		if page.ByGapClass[gap] != want {
			return fmt.Errorf("gap mismatch for %q: profile=%d page=%d", gap, want, page.ByGapClass[gap])
		}
	}
	profileIDs, err := uniqueSortedIDs(profile.Rows)
	if err != nil {
		return fmt.Errorf("profile rows: %w", err)
	}
	pageIDs, err := uniqueSortedIDs(page.Rows)
	if err != nil {
		return fmt.Errorf("page rows: %w", err)
	}
	if len(profileIDs) != len(pageIDs) {
		return fmt.Errorf("unique row count mismatch: profile=%d page=%d", len(profileIDs), len(pageIDs))
	}
	for i := range profileIDs {
		if profileIDs[i] != pageIDs[i] {
			return fmt.Errorf("row ID mismatch at index %d: profile=%q page=%q", i, profileIDs[i], pageIDs[i])
		}
	}
	return nil
}

func uniqueSortedIDs(rows []struct {
	SurfaceID string `json:"surfaceId"`
}) ([]string, error) {
	ids := make([]string, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.SurfaceID == "" {
			return nil, fmt.Errorf("empty surface ID")
		}
		if seen[row.SurfaceID] {
			return nil, fmt.Errorf("duplicate surface ID %q", row.SurfaceID)
		}
		seen[row.SurfaceID] = true
		ids = append(ids, row.SurfaceID)
	}
	sort.Strings(ids)
	return ids, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
