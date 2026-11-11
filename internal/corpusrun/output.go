package corpusrun

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const maxMessage = 300

// RepoResult is one repo's run and verdict.
type RepoResult struct {
	Repo string `json:"repo"`
	// Complete is true when both test and check produced results.
	Complete bool     `json:"complete"`
	Observed Observed `json:"observed"`
	Diff     Diff     `json:"diff"`
}

// RunReport is a whole run.
type RunReport struct {
	Tier     string       `json:"tier"`
	First    string       `json:"first"`
	ExitCode int          `json:"exitCode"`
	Results  []RepoResult `json:"results"`
}

// Summarize gives the first line and exit code for a run.
func Summarize(results []RepoResult) (string, int) {
	newCount, newRepos, infra := 0, 0, 0
	for _, r := range results {
		if n := r.Diff.New.Count(); n > 0 {
			newCount += n
			newRepos++
		}
		if r.Diff.Infra != "" {
			infra++
		}
	}
	switch {
	case newCount > 0:
		return fmt.Sprintf("CORPUS-NEW %d in %d repos", newCount, newRepos), ExitNew
	case infra > 0 || len(results) == 0:
		return fmt.Sprintf("CORPUS-INFRA %d repos", infra), ExitInfra
	}
	return "CORPUS-CLEAN", ExitClean
}

func NewRunReport(tier string, results []RepoResult) RunReport {
	first, exit := Summarize(results)
	return RunReport{Tier: tier, First: first, ExitCode: exit, Results: results}
}

// WriteText prints the verdict line, then one line per repo.
func (r RunReport) WriteText(w io.Writer) {
	fmt.Fprintln(w, r.First)
	for _, res := range r.Results {
		fmt.Fprintf(w, "%s %s\n", res.Repo, res.Diff.Line())
	}
}

// TestOutput is the parsed `glade test --json` envelope.
type TestOutput struct {
	Tests    int
	Passed   int
	Failing  []TestEntry
	Timeouts []TestEntry
}

// CheckOutput is the parsed `glade check --format json` envelope.
type CheckOutput struct {
	SourceAPI   string
	Classes     int
	Diagnostics []DiagnosticEntry
}

type problem struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type testRow struct {
	Name       string   `json:"name"`
	ClassName  string   `json:"className"`
	MethodName string   `json:"methodName"`
	Status     string   `json:"status"`
	Reason     string   `json:"reason"`
	Problem    *problem `json:"problem"`
}

func oneLine(s, root string) string {
	if root != "" {
		s = strings.ReplaceAll(s, root, "<root>")
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxMessage {
		s = s[:maxMessage] + "..."
	}
	return s
}

func isTimeout(row testRow) bool {
	switch strings.ToLower(row.Reason) {
	case "timeout", "cancelled", "canceled":
		return true
	}
	if row.Problem != nil {
		switch strings.ToLower(row.Problem.Type) {
		case "canceled", "cancelled", "timeout":
			return true
		}
		m := strings.ToLower(row.Problem.Message)
		return strings.Contains(m, "context canceled") || strings.Contains(m, "deadline exceeded")
	}
	return false
}

func parseTestOutput(data []byte, root string) (TestOutput, error) {
	var env struct {
		Summary *struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
		} `json:"summary"`
		Tests json.RawMessage `json:"tests"`
		Data  struct {
			ExcludedClasses []struct {
				ClassName string `json:"className"`
				Message   string `json:"message"`
			} `json:"excludedClasses"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return TestOutput{}, fmt.Errorf("test output: %w", err)
	}
	if env.Summary == nil && env.Tests == nil {
		return TestOutput{}, errors.New("test output has no summary or tests")
	}
	var rows []testRow
	if env.Tests != nil {
		if err := json.Unmarshal(env.Tests, &rows); err != nil {
			return TestOutput{}, fmt.Errorf("test output tests: %w", err)
		}
	}
	var out TestOutput
	if env.Summary != nil {
		out.Tests, out.Passed = env.Summary.Total, env.Summary.Passed
	}
	seen := map[string]bool{}
	add := func(list *[]TestEntry, name, msg string) {
		if seen[name] {
			return
		}
		seen[name] = true
		*list = append(*list, TestEntry{Test: name, Message: msg})
	}
	for _, row := range rows {
		if row.Status == "pass" || row.Status == "skipped" {
			continue
		}
		name := row.Name
		if name == "" {
			name = row.ClassName + "." + row.MethodName
		}
		msg := row.Reason
		if row.Problem != nil {
			msg = row.Problem.Message
			if row.Problem.Type != "" {
				msg = row.Problem.Type + ": " + msg
			}
		}
		msg = oneLine(msg, root)
		if isTimeout(row) {
			add(&out.Timeouts, name, msg)
		} else {
			add(&out.Failing, name, msg)
		}
	}
	for _, ex := range env.Data.ExcludedClasses {
		covered := false
		for _, f := range out.Failing {
			if strings.HasPrefix(f.Test, ex.ClassName+".") {
				covered = true
			}
		}
		if !covered {
			add(&out.Failing, ex.ClassName, oneLine(ex.Message, root))
		}
	}
	sort.Slice(out.Failing, func(i, j int) bool { return out.Failing[i].Test < out.Failing[j].Test })
	sort.Slice(out.Timeouts, func(i, j int) bool { return out.Timeouts[i].Test < out.Timeouts[j].Test })
	return out, nil
}

func parseCheckOutput(data []byte, root string) (CheckOutput, error) {
	var env struct {
		Project struct {
			Root      string `json:"root"`
			SourceAPI string `json:"sourceApiVersion"`
		} `json:"project"`
		Summary *struct {
			Types int `json:"types"`
		} `json:"summary"`
		Diagnostics json.RawMessage `json:"diagnostics"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return CheckOutput{}, fmt.Errorf("check output: %w", err)
	}
	if env.Summary == nil && env.Diagnostics == nil {
		return CheckOutput{}, errors.New("check output has no summary or diagnostics")
	}
	var rows []struct {
		Severity string `json:"severity"`
		Code     string `json:"code"`
		Message  string `json:"message"`
		File     string `json:"file"`
		Range    struct {
			Start struct {
				Line int `json:"line"`
			} `json:"start"`
		} `json:"range"`
	}
	if env.Diagnostics != nil {
		if err := json.Unmarshal(env.Diagnostics, &rows); err != nil {
			return CheckOutput{}, fmt.Errorf("check output diagnostics: %w", err)
		}
	}
	if env.Project.Root != "" {
		root = env.Project.Root
	}
	out := CheckOutput{SourceAPI: env.Project.SourceAPI}
	if env.Summary != nil {
		out.Classes = env.Summary.Types
	}
	for _, r := range rows {
		file := r.File
		if root != "" {
			file = strings.TrimPrefix(file, strings.TrimSuffix(root, "/")+"/")
		}
		out.Diagnostics = append(out.Diagnostics, DiagnosticEntry{
			Code: r.Code, File: file, Line: r.Range.Start.Line, Severity: r.Severity, Message: oneLine(r.Message, root),
		})
	}
	sortDiagnostics(out.Diagnostics)
	return out, nil
}

func sortDiagnostics(d []DiagnosticEntry) {
	sort.SliceStable(d, func(i, j int) bool {
		if d[i].File != d[j].File {
			return d[i].File < d[j].File
		}
		if d[i].Line != d[j].Line {
			return d[i].Line < d[j].Line
		}
		return d[i].Code < d[j].Code
	})
}
