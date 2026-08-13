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
	ciRuns, ciJobs, fixtureRuns, fixtureJobs string
}

func validReleaseGateFixture() releaseGateFixture {
	return releaseGateFixture{
		ciRuns:      `{"total_count":1,"workflow_runs":[{"id":101,"path":".github/workflows/ci.yml","head_sha":"` + releaseGateSHA + `","status":"completed","conclusion":"success","event":"push","created_at":"2026-01-02T00:00:00Z","html_url":"https://example/run/101"}]}`,
		ciJobs:      `{"total_count":1,"jobs":[{"id":501,"name":"test","head_sha":"` + releaseGateSHA + `","status":"completed","conclusion":"success","html_url":"https://example/job/501"}]}`,
		fixtureRuns: `{"total_count":1,"workflow_runs":[{"id":201,"path":".github/workflows/full-fixtures.yml","head_sha":"` + releaseGateSHA + `","status":"completed","conclusion":"success","event":"workflow_dispatch","created_at":"2026-01-02T00:00:00Z","html_url":"https://example/run/201"}]}`,
		fixtureJobs: `{"total_count":1,"jobs":[{"id":601,"name":"full-fixtures","head_sha":"` + releaseGateSHA + `","status":"completed","conclusion":"success","html_url":"https://example/job/601"}]}`,
	}
}

func TestVerifyReleaseGatesUsesNewestSuccessfulRun(t *testing.T) {
	fixture := validReleaseGateFixture()
	fixture.fixtureRuns = `{"total_count":2,"workflow_runs":[{"id":200,"path":".github/workflows/full-fixtures.yml","head_sha":"` + releaseGateSHA + `","status":"completed","conclusion":"success","event":"workflow_dispatch","created_at":"2026-01-01T00:00:00Z","html_url":"https://example/run/200"},{"id":201,"path":".github/workflows/full-fixtures.yml","head_sha":"` + releaseGateSHA + `","status":"completed","conclusion":"success","event":"workflow_dispatch","created_at":"2026-01-02T00:00:00Z","html_url":"https://example/run/201"}]}`
	out, _, err := runReleaseGateFixture(t, fixture, "glade-sh/glade-tools", releaseGateSHA)
	if err != nil {
		t.Fatalf("newest successful run rejected: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"run_id": 201`) {
		t.Fatalf("newest successful run not selected:\n%s", out)
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
  *"/actions/workflows/full-fixtures.yml/runs"*) cat "$VERIFY_RELEASE_GATE_FIXTURE_RUNS" ;;
  *"/actions/runs/101/jobs"*) cat "$VERIFY_RELEASE_GATE_CI_JOBS" ;;
  *"/actions/runs/201/jobs"*) cat "$VERIFY_RELEASE_GATE_FIXTURE_JOBS" ;;
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
		"VERIFY_RELEASE_GATE_FIXTURE_RUNS="+writeFixture("fixture-runs.json", fixture.fixtureRuns),
		"VERIFY_RELEASE_GATE_FIXTURE_JOBS="+writeFixture("fixture-jobs.json", fixture.fixtureJobs),
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
		FullFixtures struct {
			Event  string `json:"event"`
			RunID  int    `json:"run_id"`
			RunURL string `json:"run_url"`
			JobID  int    `json:"job_id"`
			JobURL string `json:"job_url"`
		} `json:"full_fixtures"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid verification JSON: %v\n%s", err, out)
	}
	if got.SchemaVersion != 1 || got.Repository != "glade-sh/glade-tools" || got.SHA != releaseGateSHA || got.Conclusion != "success" {
		t.Fatalf("unexpected verification header: %#v", got)
	}
	if got.CI.Event != "push" || got.CI.RunID != 101 || got.CI.RunURL != "https://example/run/101" || got.CI.JobID != 501 || got.CI.JobURL != "https://example/job/501" {
		t.Fatalf("unexpected CI authority: %#v", got.CI)
	}
	if got.FullFixtures.Event != "workflow_dispatch" || got.FullFixtures.RunID != 201 || got.FullFixtures.RunURL != "https://example/run/201" || got.FullFixtures.JobID != 601 || got.FullFixtures.JobURL != "https://example/job/601" {
		t.Fatalf("unexpected full-fixtures authority: %#v", got.FullFixtures)
	}
	for _, marker := range []string{
		"--method GET /repos/glade-sh/glade-tools/actions/workflows/ci.yml/runs -f head_sha=" + releaseGateSHA + " -f status=completed -f per_page=100",
		"--method GET /repos/glade-sh/glade-tools/actions/runs/101/jobs -f per_page=100",
		"--method GET /repos/glade-sh/glade-tools/actions/workflows/full-fixtures.yml/runs -f head_sha=" + releaseGateSHA + " -f status=completed -f per_page=100",
		"--method GET /repos/glade-sh/glade-tools/actions/runs/201/jobs -f per_page=100",
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
		"scheduled fixtures": func(f *releaseGateFixture) {
			f.fixtureRuns = strings.Replace(f.fixtureRuns, `"event":"workflow_dispatch"`, `"event":"schedule"`, 1)
		},
		"wrong SHA": func(f *releaseGateFixture) {
			f.ciRuns = strings.Replace(f.ciRuns, releaseGateSHA, strings.Repeat("f", 40), 1)
		},
		"wrong workflow": func(f *releaseGateFixture) {
			f.fixtureRuns = strings.Replace(f.fixtureRuns, "full-fixtures.yml", "ci.yml", 1)
		},
		"failed run": func(f *releaseGateFixture) {
			f.ciRuns = strings.Replace(f.ciRuns, `"conclusion":"success"`, `"conclusion":"failure"`, 1)
		},
		"malformed response": func(f *releaseGateFixture) { f.fixtureRuns = `{"total_count":"one","workflow_runs":{}}` },
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
		"missing": func(f *releaseGateFixture) { f.fixtureJobs = `{"total_count":0,"jobs":[]}` },
		"failed": func(f *releaseGateFixture) {
			f.ciJobs = strings.Replace(f.ciJobs, `"conclusion":"success"`, `"conclusion":"failure"`, 1)
		},
		"cancelled": func(f *releaseGateFixture) {
			f.fixtureJobs = strings.Replace(f.fixtureJobs, `"conclusion":"success"`, `"conclusion":"cancelled"`, 1)
		},
		"skipped": func(f *releaseGateFixture) {
			f.fixtureJobs = strings.Replace(f.fixtureJobs, `"conclusion":"success"`, `"conclusion":"skipped"`, 1)
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
