package corpusrun

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Golden is the committed expected result for one repo at one pin.
type Golden struct {
	Repo        string            `json:"repo"`
	Pin         string            `json:"pin"`
	Overlay     Overlay           `json:"overlay"`
	Glade       GladeInfo         `json:"glade"`
	Counts      Counts            `json:"counts"`
	Failing     []TestEntry       `json:"failing,omitempty"`
	Diagnostics []DiagnosticEntry `json:"diagnostics,omitempty"`
	Timeouts    []TestEntry       `json:"timeouts,omitempty"`
	WallSeconds float64           `json:"wallSeconds"`
}

// BuildStats reports what a refresh left for a human.
type BuildStats struct {
	Unclassified int // entries with no status yet
	Dropped      int // old entries the run no longer produced
}

// GoldenPath is where a repo's golden lives in the config directory.
func GoldenPath(config, repo string) string {
	return filepath.Join(config, "goldens", repo+".json")
}

func validStatus(s string) bool {
	switch s {
	case StatusKnownGap, StatusIntended, StatusEnv, StatusFlaky, StatusUnclassified:
		return true
	}
	return false
}

// LoadGolden reads a golden. A missing file satisfies errors.Is(err, os.ErrNotExist).
func LoadGolden(config, repo string) (Golden, error) {
	path := GoldenPath(config, repo)
	data, err := os.ReadFile(path)
	if err != nil {
		return Golden{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var g Golden
	if err := dec.Decode(&g); err != nil {
		return Golden{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if g.Repo != repo {
		return Golden{}, fmt.Errorf("%s is the golden of %q, not %q", path, g.Repo, repo)
	}
	for _, list := range [][]TestEntry{g.Failing, g.Timeouts} {
		for _, e := range list {
			if !validStatus(e.Status) {
				return Golden{}, fmt.Errorf("%s: test %q has unknown status %q", path, e.Test, e.Status)
			}
		}
	}
	for _, e := range g.Diagnostics {
		if !validStatus(e.Status) {
			return Golden{}, fmt.Errorf("%s: diagnostic %s has unknown status %q", path, e.Code, e.Status)
		}
	}
	return g, nil
}

// WriteGolden writes indented JSON with a final newline.
func WriteGolden(config string, g Golden) error {
	path := GoldenPath(config, g.Repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// BuildGolden makes a golden from a run. Entries that persist keep the status,
// reason and ticket of existing; new ones are unclassified. Existing flaky
// entries stay even when the run did not produce them.
func BuildGolden(existing *Golden, repo string, observed Observed) (Golden, BuildStats) {
	var stats BuildStats
	g := Golden{Repo: repo, Pin: observed.Pin, Overlay: observed.Overlay, Glade: observed.Glade, Counts: observed.Counts, WallSeconds: observed.WallSeconds}

	// Test classifications carry across the failing and timeout lists.
	old := map[string]TestEntry{}
	used := map[string]bool{}
	if existing != nil {
		for _, list := range [][]TestEntry{existing.Failing, existing.Timeouts} {
			for _, e := range list {
				old[e.Test] = e
			}
		}
	}
	carry := func(in []TestEntry) []TestEntry {
		var out []TestEntry
		for _, e := range in {
			entry := TestEntry{Test: e.Test, Message: e.Message, Status: StatusUnclassified}
			if o, ok := old[e.Test]; ok {
				entry.Status, entry.Reason, entry.Ticket = o.Status, o.Reason, o.Ticket
				used[e.Test] = true
			}
			if entry.Status == StatusUnclassified {
				stats.Unclassified++
			}
			out = append(out, entry)
		}
		return out
	}
	g.Failing = carry(observed.Failing)
	g.Timeouts = carry(observed.Timeouts)
	if existing != nil {
		keep := func(list []TestEntry, into *[]TestEntry) {
			for _, e := range list {
				if used[e.Test] {
					continue
				}
				if e.Status == StatusFlaky {
					e.Message = ""
					*into = append(*into, e)
				} else {
					stats.Dropped++
				}
			}
		}
		keep(existing.Failing, &g.Failing)
		keep(existing.Timeouts, &g.Timeouts)
	}
	byName := func(l []TestEntry) {
		sort.SliceStable(l, func(i, j int) bool { return l[i].Test < l[j].Test })
	}
	byName(g.Failing)
	byName(g.Timeouts)

	// Diagnostics: the n-th observed copy of a key takes the n-th golden copy.
	oldDiags := map[diagKey][]DiagnosticEntry{}
	if existing != nil {
		for _, d := range existing.Diagnostics {
			oldDiags[keyOf(d)] = append(oldDiags[keyOf(d)], d)
		}
	}
	taken := map[diagKey]int{}
	for _, d := range observed.Diagnostics {
		k := keyOf(d)
		entry := DiagnosticEntry{Code: d.Code, File: d.File, Line: d.Line, Severity: d.Severity, Message: d.Message, Status: StatusUnclassified}
		if n := taken[k]; n < len(oldDiags[k]) {
			o := oldDiags[k][n]
			entry.Status, entry.Reason, entry.Ticket = o.Status, o.Reason, o.Ticket
		}
		if entry.Status == StatusUnclassified {
			stats.Unclassified++
		}
		taken[k]++
		g.Diagnostics = append(g.Diagnostics, entry)
	}
	for k, list := range oldDiags {
		for _, d := range list[min(taken[k], len(list)):] {
			if d.Status == StatusFlaky {
				d.Message = ""
				g.Diagnostics = append(g.Diagnostics, d)
			} else {
				stats.Dropped++
			}
		}
	}
	sortDiagnostics(g.Diagnostics)
	return g, stats
}

// BuildFromRun refreshes the golden of repo from a saved run report (JSON).
func BuildFromRun(config, repo, runPath string) (BuildStats, error) {
	data, err := os.ReadFile(runPath)
	if err != nil {
		return BuildStats{}, err
	}
	var report RunReport
	if err := json.Unmarshal(data, &report); err != nil {
		return BuildStats{}, fmt.Errorf("parse %s: %w", runPath, err)
	}
	var result *RepoResult
	for i := range report.Results {
		if report.Results[i].Repo == repo {
			result = &report.Results[i]
		}
	}
	if result == nil {
		return BuildStats{}, fmt.Errorf("repo %q is not in the run report", repo)
	}
	cfg, err := LoadConfig(config)
	if err != nil {
		return BuildStats{}, err
	}
	var pin string
	found := false
	for _, r := range cfg.Repos {
		if r.Name == repo {
			pin, found = r.Pin, true
		}
	}
	if !found {
		return BuildStats{}, fmt.Errorf("repo %q is not in %s", repo, TiersFile)
	}
	if !result.Complete {
		return BuildStats{}, fmt.Errorf("run of %q is incomplete: %s", repo, result.Diff.Infra)
	}
	if result.Observed.Pin != pin {
		return BuildStats{}, fmt.Errorf("run of %q is at pin %s, config pin is %s", repo, short(result.Observed.Pin), short(pin))
	}
	var existing *Golden
	if g, err := LoadGolden(config, repo); err == nil {
		existing = &g
	} else if !errors.Is(err, os.ErrNotExist) {
		return BuildStats{}, err
	}
	golden, stats := BuildGolden(existing, repo, result.Observed)
	return stats, WriteGolden(config, golden)
}

// Classification is the status, reason and ticket given to unclassified entries.
type Classification struct {
	Status string
	Reason string
	Ticket string
}

// ClassifyUnclassified gives every unclassified entry of a golden the same
// classification and returns how many entries changed. Classified entries are
// never touched. A golden with nothing unclassified is not rewritten.
func ClassifyUnclassified(config, repo string, c Classification) (int, error) {
	if !validStatus(c.Status) || c.Status == StatusUnclassified {
		return 0, fmt.Errorf("status %q is not a classification", c.Status)
	}
	if c.Reason == "" || c.Ticket == "" {
		return 0, fmt.Errorf("a classification needs a reason and a ticket")
	}
	g, err := LoadGolden(config, repo)
	if err != nil {
		return 0, err
	}
	changed := 0
	set := func(status *string, reason, ticket *string) {
		if *status == StatusUnclassified {
			*status, *reason, *ticket = c.Status, c.Reason, c.Ticket
			changed++
		}
	}
	for _, list := range [][]TestEntry{g.Failing, g.Timeouts} {
		for i := range list {
			set(&list[i].Status, &list[i].Reason, &list[i].Ticket)
		}
	}
	for i := range g.Diagnostics {
		set(&g.Diagnostics[i].Status, &g.Diagnostics[i].Reason, &g.Diagnostics[i].Ticket)
	}
	if changed == 0 {
		return 0, nil
	}
	return changed, WriteGolden(config, g)
}
