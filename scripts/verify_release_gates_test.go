package scripts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const releaseGateSHA = "0123456789abcdef0123456789abcdef01234567"

type releaseGateFixture struct {
	ciRuns, ciJobs string
}

func validReleaseGateFixture() releaseGateFixture {
	return releaseGateFixture{
		ciRuns: `{"total_count":1,"workflow_runs":[{"id":101,"path":".github/workflows/ci.yml","head_sha":"` + releaseGateSHA + `","status":"completed","conclusion":"success","event":"push","created_at":"2026-01-02T00:00:00Z","html_url":"https://example/run/101"}]}`,
		ciJobs: `{"total_count":1,"jobs":[{"id":501,"name":"test","head_sha":"` + releaseGateSHA + `","status":"completed","conclusion":"success","html_url":"https://example/job/501"}]}`,
	}
}

func TestVerifyReleaseGatesUsesNewestSuccessfulRun(t *testing.T) {
	fixture := validReleaseGateFixture()
	fixture.ciRuns = `{"total_count":2,"workflow_runs":[{"id":101,"path":".github/workflows/ci.yml","head_sha":"` + releaseGateSHA + `","status":"completed","conclusion":"success","event":"push","created_at":"2026-01-01T00:00:00Z","html_url":"https://example/run/101"},{"id":102,"path":".github/workflows/ci.yml","head_sha":"` + releaseGateSHA + `","status":"completed","conclusion":"success","event":"push","created_at":"2026-01-02T00:00:00Z","html_url":"https://example/run/102"}]}`
	out, _, err := runReleaseGateFixture(t, fixture, "glade-sh/glade-tools", releaseGateSHA)
	if err != nil {
		t.Fatalf("newest successful run rejected: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"run_id": 102`) {
		t.Fatalf("newest successful run not selected:\n%s", out)
	}
}

func TestVerifyReleaseGatesRequiresOnlyExactPushCI(t *testing.T) {
	out, calls, err := runReleaseGateFixture(t, validReleaseGateFixture(), "glade-sh/glade-tools", releaseGateSHA)
	if err != nil {
		t.Fatalf("verification failed: %v\n%s", err, out)
	}
	var got struct {
		SchemaVersion int `json:"schema_version"`
		FullFixtures  struct {
			Evaluated bool   `json:"evaluated"`
			Lane      string `json:"lane"`
		} `json:"full_fixtures"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid verification JSON: %v\n%s", err, out)
	}
	if got.SchemaVersion != 2 || got.FullFixtures.Evaluated || got.FullFixtures.Lane != "diagnostic" {
		t.Fatalf("release result must disclose unevaluated diagnostic full fixtures: %#v\n%s", got, out)
	}
	if strings.Contains(calls, "full-fixtures.yml") {
		t.Fatalf("release gate queried diagnostic full-fixtures workflow:\n%s", calls)
	}
}

func runReleaseGateFixture(t *testing.T, fixture releaseGateFixture, args ...string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture := func(name, contents string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	gh := `#!/usr/bin/env bash
printf '%s\n' "$*" >>"$VERIFY_RELEASE_GATE_CALLS"
case "$*" in
  *"/actions/workflows/ci.yml/runs"*) cat "$VERIFY_RELEASE_GATE_CI_RUNS" ;;
  *"/actions/runs/101/jobs"*) cat "$VERIFY_RELEASE_GATE_CI_JOBS" ;;
  *"/actions/runs/102/jobs"*) cat "$VERIFY_RELEASE_GATE_CI_JOBS" ;;
  *) echo "unexpected gh call: $*" >&2; exit 97 ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(gh), 0o700); err != nil {
		t.Fatal(err)
	}
	callsPath := filepath.Join(dir, "calls")
	cmd := exec.Command("bash", append([]string{"verify-release-gates.sh"}, args...)...)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"VERIFY_RELEASE_GATE_CALLS="+callsPath,
		"VERIFY_RELEASE_GATE_CI_RUNS="+writeFixture("ci-runs.json", fixture.ciRuns),
		"VERIFY_RELEASE_GATE_CI_JOBS="+writeFixture("ci-jobs.json", fixture.ciJobs),
	)
	out, err := cmd.CombinedOutput()
	calls, readErr := os.ReadFile(callsPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	return string(out), string(calls), err
}

func TestVerifyReleaseGatesFindsExactSuccessfulAuthorities(t *testing.T) {
	out, calls, err := runReleaseGateFixture(t, validReleaseGateFixture(), "glade-sh/glade-tools", releaseGateSHA)
	if err != nil {
		t.Fatalf("verification failed: %v\n%s", err, out)
	}
	var got struct {
		SchemaVersion int    `json:"schema_version"`
		Repository    string `json:"repository"`
		SHA           string `json:"sha"`
		Conclusion    string `json:"conclusion"`
		CI            struct {
			Event  string `json:"event"`
			RunID  int    `json:"run_id"`
			RunURL string `json:"run_url"`
			JobID  int    `json:"job_id"`
			JobURL string `json:"job_url"`
		} `json:"ci"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid verification JSON: %v\n%s", err, out)
	}
	if got.SchemaVersion != 2 || got.Repository != "glade-sh/glade-tools" || got.SHA != releaseGateSHA || got.Conclusion != "success" {
		t.Fatalf("unexpected verification header: %#v", got)
	}
	if got.CI.Event != "push" || got.CI.RunID != 101 || got.CI.RunURL != "https://example/run/101" || got.CI.JobID != 501 || got.CI.JobURL != "https://example/job/501" {
		t.Fatalf("unexpected CI authority: %#v", got.CI)
	}
	for _, marker := range []string{
		"--method GET /repos/glade-sh/glade-tools/actions/workflows/ci.yml/runs -f head_sha=" + releaseGateSHA + " -f status=completed -f per_page=100",
		"--method GET /repos/glade-sh/glade-tools/actions/runs/101/jobs -f per_page=100",
	} {
		if !strings.Contains(calls, marker) {
			t.Errorf("API calls missing %q\n%s", marker, calls)
		}
	}
}

func TestVerifyReleaseGatesRejectsInvalidArguments(t *testing.T) {
	for name, args := range map[string][]string{
		"missing":      nil,
		"extra":        {"glade-sh/glade-tools", releaseGateSHA, "extra"},
		"invalid repo": {"glade-sh/glade-tools/extra", releaseGateSHA},
		"invalid sha":  {"glade-sh/glade-tools", "main"},
	} {
		t.Run(name, func(t *testing.T) {
			out, calls, err := runReleaseGateFixture(t, validReleaseGateFixture(), args...)
			if err == nil || !strings.Contains(out, "usage:") || calls != "" {
				t.Fatalf("invalid arguments accepted: err=%v calls=%q\n%s", err, calls, out)
			}
		})
	}
}

func TestVerifyReleaseGatesRejectsInvalidRuns(t *testing.T) {
	valid := validReleaseGateFixture()
	for name, mutate := range map[string]func(*releaseGateFixture){
		"pull request CI": func(f *releaseGateFixture) {
			f.ciRuns = strings.Replace(f.ciRuns, `"event":"push"`, `"event":"pull_request"`, 1)
		},
		"wrong SHA": func(f *releaseGateFixture) {
			f.ciRuns = strings.Replace(f.ciRuns, releaseGateSHA, strings.Repeat("f", 40), 1)
		},
		"failed run": func(f *releaseGateFixture) {
			f.ciRuns = strings.Replace(f.ciRuns, `"conclusion":"success"`, `"conclusion":"failure"`, 1)
		},
		"malformed response": func(f *releaseGateFixture) { f.ciRuns = `{"total_count":"one","workflow_runs":{}}` },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := valid
			mutate(&fixture)
			out, _, err := runReleaseGateFixture(t, fixture, "glade-sh/glade-tools", releaseGateSHA)
			if err == nil || !strings.Contains(out, "no successful") {
				t.Fatalf("invalid run authority accepted: err=%v\n%s", err, out)
			}
		})
	}
}

func TestVerifyReleaseGatesRejectsInvalidRequiredJobs(t *testing.T) {
	valid := validReleaseGateFixture()
	for name, mutate := range map[string]func(*releaseGateFixture){
		"missing": func(f *releaseGateFixture) { f.ciJobs = `{"total_count":0,"jobs":[]}` },
		"failed": func(f *releaseGateFixture) {
			f.ciJobs = strings.Replace(f.ciJobs, `"conclusion":"success"`, `"conclusion":"failure"`, 1)
		},
		"cancelled": func(f *releaseGateFixture) {
			f.ciJobs = strings.Replace(f.ciJobs, `"conclusion":"success"`, `"conclusion":"cancelled"`, 1)
		},
		"skipped": func(f *releaseGateFixture) {
			f.ciJobs = strings.Replace(f.ciJobs, `"conclusion":"success"`, `"conclusion":"skipped"`, 1)
		},
		"wrong SHA": func(f *releaseGateFixture) {
			f.ciJobs = strings.Replace(f.ciJobs, releaseGateSHA, strings.Repeat("f", 40), 1)
		},
		"duplicate": func(f *releaseGateFixture) {
			f.ciJobs = strings.Replace(f.ciJobs, `"total_count":1`, `"total_count":2`, 1)
			f.ciJobs = strings.Replace(f.ciJobs, `}]}`, `},{"id":502,"name":"test","head_sha":"`+releaseGateSHA+`","status":"completed","conclusion":"success","html_url":"https://example/job/502"}]}`, 1)
		},
		"malformed response": func(f *releaseGateFixture) { f.ciJobs = `{"total_count":1,"jobs":[{"id":"501"}]}` },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := valid
			mutate(&fixture)
			out, _, err := runReleaseGateFixture(t, fixture, "glade-sh/glade-tools", releaseGateSHA)
			if err == nil || !strings.Contains(out, "no unique successful") {
				t.Fatalf("invalid job authority accepted: err=%v\n%s", err, out)
			}
		})
	}
}
