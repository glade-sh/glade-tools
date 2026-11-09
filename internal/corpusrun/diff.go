package corpusrun

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Statuses an expected entry can carry.
const (
	StatusKnownGap     = "known-gap"
	StatusIntended     = "intended"
	StatusEnv          = "env"
	StatusFlaky        = "flaky"
	StatusUnclassified = "unclassified"
)

// Exit codes of a run.
const (
	ExitClean = 0
	ExitNew   = 1
	ExitInfra = 2
)

const slowFactor = 1.5

type TestEntry struct {
	Test    string `json:"test"`
	Status  string `json:"status,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Ticket  string `json:"ticket,omitempty"`
	Message string `json:"message,omitempty"`
}

type DiagnosticEntry struct {
	Code     string `json:"code"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message,omitempty"`
	Status   string `json:"status,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Ticket   string `json:"ticket,omitempty"`
}

type Counts struct {
	Classes int `json:"classes"`
	Tests   int `json:"tests"`
	Passed  int `json:"passed"`
}

type GladeInfo struct {
	SourceAPI string `json:"sourceApi"`
}

// Observed is what one run produced for one repo.
type Observed struct {
	Pin         string            `json:"pin"`
	Overlay     Overlay           `json:"overlay"`
	Glade       GladeInfo         `json:"glade"`
	Counts      Counts            `json:"counts"`
	Failing     []TestEntry       `json:"failing,omitempty"`
	Diagnostics []DiagnosticEntry `json:"diagnostics,omitempty"`
	Timeouts    []TestEntry       `json:"timeouts,omitempty"`
	WallSeconds float64           `json:"wallSeconds"`
}

// Delta is a set of differences.
type Delta struct {
	Failing     []TestEntry       `json:"failing,omitempty"`
	Timeouts    []TestEntry       `json:"timeouts,omitempty"`
	Diagnostics []DiagnosticEntry `json:"diagnostics,omitempty"`
}

func (d Delta) Count() int { return len(d.Failing) + len(d.Timeouts) + len(d.Diagnostics) }

// Diff is the comparison of one run with its golden.
type Diff struct {
	New   Delta   `json:"new"`
	Gone  Delta   `json:"gone"`
	Slow  float64 `json:"slow,omitempty"`
	Infra string  `json:"infra,omitempty"`
}

// Line is the one-line verdict for a repo.
func (d Diff) Line() string {
	var parts []string
	if n := d.New.Count(); n > 0 {
		parts = append(parts, fmt.Sprintf("NEW %d", n))
	}
	if n := d.Gone.Count(); n > 0 {
		parts = append(parts, fmt.Sprintf("GONE %d", n))
	}
	if d.Slow > 0 {
		parts = append(parts, fmt.Sprintf("SLOW %.1f", d.Slow))
	}
	if d.Infra != "" {
		parts = append(parts, "INFRA "+d.Infra)
	}
	if len(parts) == 0 {
		return "OK"
	}
	return strings.Join(parts, " ")
}

func normOverlay(o Overlay) Overlay {
	norm := func(in []string) []string {
		if len(in) == 0 {
			return nil
		}
		out := append([]string(nil), in...)
		sort.Strings(out)
		return out
	}
	return Overlay{Deps: norm(o.Deps), Config: OverlayConfig{Features: norm(o.Config.Features), PermissionSets: norm(o.Config.PermissionSets)}}
}

func short(pin string) string {
	if len(pin) > 12 {
		return pin[:12]
	}
	return pin
}

type diagKey struct {
	code, file string
	line       int
}

func keyOf(d DiagnosticEntry) diagKey { return diagKey{d.Code, d.File, d.Line} }

func testSet(entries []TestEntry) map[string]bool {
	set := map[string]bool{}
	for _, e := range entries {
		set[e.Test] = true
	}
	return set
}

// Compare applies the diff rules. Everything the golden lists is expected;
// anything else the run produced is NEW. An expected entry the run no longer
// produces is GONE, except flaky ones.
func Compare(golden Golden, observed Observed) Diff {
	var diff Diff
	for _, entries := range [][]TestEntry{golden.Failing, golden.Timeouts} {
		for _, entry := range entries {
			if entry.Status == StatusUnclassified {
				return Diff{Infra: "golden contains unclassified entries"}
			}
		}
	}
	for _, entry := range golden.Diagnostics {
		if entry.Status == StatusUnclassified {
			return Diff{Infra: "golden contains unclassified entries"}
		}
	}
	switch {
	case golden.Pin != observed.Pin:
		diff.Infra = fmt.Sprintf("golden pin %s differs from run pin %s", short(golden.Pin), short(observed.Pin))
		return diff
	case !reflect.DeepEqual(normOverlay(golden.Overlay), normOverlay(observed.Overlay)):
		diff.Infra = "golden overlay differs from config"
		return diff
	case golden.Glade.SourceAPI != observed.Glade.SourceAPI:
		diff.Infra = fmt.Sprintf("golden source API %s differs from run %s", golden.Glade.SourceAPI, observed.Glade.SourceAPI)
		return diff
	}
	dropped := golden.Counts.Tests > 0 && observed.Counts.Tests < golden.Counts.Tests
	if dropped {
		diff.Infra = fmt.Sprintf("tests dropped from %d to %d", golden.Counts.Tests, observed.Counts.Tests)
	}

	goldenFailing, goldenTimeouts := testSet(golden.Failing), testSet(golden.Timeouts)
	obsFailing, obsTimeouts := testSet(observed.Failing), testSet(observed.Timeouts)

	for _, e := range observed.Failing {
		if !goldenFailing[e.Test] && !goldenTimeouts[e.Test] {
			diff.New.Failing = append(diff.New.Failing, e)
		}
	}
	// Timeout rule: from none to some is NEW; otherwise only more than twice
	// the golden count is, and then every timeout the golden lacks counts.
	if len(golden.Timeouts) == 0 || len(observed.Timeouts) > 2*len(golden.Timeouts) {
		for _, e := range observed.Timeouts {
			if !goldenTimeouts[e.Test] && !goldenFailing[e.Test] {
				diff.New.Timeouts = append(diff.New.Timeouts, e)
			}
		}
	}
	if !dropped {
		for _, e := range golden.Failing {
			if e.Status != StatusFlaky && !obsFailing[e.Test] && !obsTimeouts[e.Test] {
				diff.Gone.Failing = append(diff.Gone.Failing, e)
			}
		}
		for _, e := range golden.Timeouts {
			if e.Status != StatusFlaky && !obsTimeouts[e.Test] && !obsFailing[e.Test] {
				diff.Gone.Timeouts = append(diff.Gone.Timeouts, e)
			}
		}
	}

	// Diagnostics are a multiset: each copy in the golden covers one copy.
	goldenCount := map[diagKey]int{}
	for _, d := range golden.Diagnostics {
		goldenCount[keyOf(d)]++
	}
	obsCount := map[diagKey]int{}
	for _, d := range observed.Diagnostics {
		k := keyOf(d)
		obsCount[k]++
		if obsCount[k] > goldenCount[k] {
			diff.New.Diagnostics = append(diff.New.Diagnostics, d)
		}
	}
	seen := map[diagKey]int{}
	for _, d := range golden.Diagnostics {
		k := keyOf(d)
		seen[k]++
		if seen[k] > obsCount[k] && d.Status != StatusFlaky && !dropped {
			diff.Gone.Diagnostics = append(diff.Gone.Diagnostics, d)
		}
	}

	if golden.WallSeconds > 0 && observed.WallSeconds > slowFactor*golden.WallSeconds {
		diff.Slow = observed.WallSeconds / golden.WallSeconds
	}
	return diff
}
