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
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/glade-sh/glade/internal/testreport"
)

const fullLocalTestFixturesEnv = "GLADE_TOOLS_RUN_FULL_LOCAL_TEST_FIXTURES"

var localTestRunSlots = make(chan struct{}, 1)

type localTestReadyFixture struct {
	name  string
	total int
}

func TestLoadLocalTestComparisonTargetManifestAcceptsFullAndFocusedTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	writeLocalTestFile(t, path, `{
  "schemaVersion": 1,
  "targets": [
    {"id":"full","cpuProfile":false},
    {"id":"focused","class":"ExampleTest","method":"runs","cpuProfile":true}
  ]
}`)

	manifest, err := LoadLocalTestComparisonTargetManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 1 || len(manifest.Targets) != 2 {
		t.Fatalf("manifest = %#v", manifest)
	}
	if got := manifest.Targets[0]; got.ID != "full" || got.Class != "" || got.Method != "" || got.CPUProfile {
		t.Fatalf("full target = %#v", got)
	}
	if got := manifest.Targets[1]; got.ID != "focused" || got.Class != "ExampleTest" || got.Method != "runs" || !got.CPUProfile {
		t.Fatalf("focused target = %#v", got)
	}
}

func TestLoadLocalTestComparisonTargetManifestRejectsUnknownFields(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
	}{
		{
			name: "manifest field",
			data: `{"schemaVersion":1,"targets":[{"id":"full","cpuProfile":false}],"unknown":true}`,
		},
		{
			name: "target field",
			data: `{"schemaVersion":1,"targets":[{"id":"focused","cpuProfile":false,"unknown":true}]}`,
		},
		{
			name: "trailing JSON",
			data: `{"schemaVersion":1,"targets":[{"id":"full","cpuProfile":false}]} {}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "targets.json")
			writeLocalTestFile(t, path, tt.data)
			if _, err := LoadLocalTestComparisonTargetManifest(path); err == nil {
				t.Fatal("LoadLocalTestComparisonTargetManifest error = nil")
			}
		})
	}
}

func TestLoadLocalTestComparisonTargetManifestRejectsInvalidSchema(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		want string
	}{
		{
			name: "missing schema version",
			data: `{"targets":[{"id":"full","cpuProfile":false}]}`,
			want: "schemaVersion must be 1",
		},
		{
			name: "future schema version",
			data: `{"schemaVersion":2,"targets":[{"id":"full","cpuProfile":false}]}`,
			want: "schemaVersion must be 1",
		},
		{
			name: "missing targets",
			data: `{"schemaVersion":1,"targets":[]}`,
			want: "requires at least one target",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "targets.json")
			writeLocalTestFile(t, path, tt.data)
			_, err := LoadLocalTestComparisonTargetManifest(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadLocalTestComparisonTargetManifest error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadLocalTestComparisonTargetManifestRejectsDuplicateOrUnsafeTargetID(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		want string
	}{
		{
			name: "duplicate",
			data: `{"schemaVersion":1,"targets":[{"id":"full","cpuProfile":false},{"id":"full","cpuProfile":true}]}`,
			want: `duplicate target id "full"`,
		},
		{
			name: "unsafe",
			data: `{"schemaVersion":1,"targets":[{"id":"focused!","cpuProfile":false}]}`,
			want: `target id "focused!" must match`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "targets.json")
			writeLocalTestFile(t, path, tt.data)
			_, err := LoadLocalTestComparisonTargetManifest(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadLocalTestComparisonTargetManifest error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadLocalTestComparisonTargetManifestRejectsMethodWithoutClass(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		want string
	}{
		{
			name: "missing class",
			data: `{"schemaVersion":1,"targets":[{"id":"focused","method":"runs","cpuProfile":true}]}`,
			want: `target "focused" method requires class`,
		},
		{
			name: "whitespace class",
			data: `{"schemaVersion":1,"targets":[{"id":"focused","class":"   ","method":"runs","cpuProfile":true}]}`,
			want: `target "focused" class must not be blank`,
		},
		{
			name: "whitespace method",
			data: `{"schemaVersion":1,"targets":[{"id":"focused","class":"ExampleTest","method":"   ","cpuProfile":true}]}`,
			want: `target "focused" method must not be blank`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "targets.json")
			writeLocalTestFile(t, path, tt.data)
			_, err := LoadLocalTestComparisonTargetManifest(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadLocalTestComparisonTargetManifest error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateLocalTestComparisonOptionsRequiresEveryExplicitInput(t *testing.T) {
	valid := LocalTestComparisonOptions{
		BaseBin:      "/base/glade",
		CandidateBin: "/candidate/glade",
		Project:      "/source/project",
		Out:          "/output",
		Workers:      1,
		Runs:         5,
		Manifest:     "/targets.json",
	}
	if err := ValidateLocalTestComparisonOptions(valid); err != nil {
		t.Fatalf("ValidateLocalTestComparisonOptions(valid) error = %v", err)
	}

	for _, tt := range []struct {
		name string
		edit func(*LocalTestComparisonOptions)
		want string
	}{
		{name: "base binary", edit: func(options *LocalTestComparisonOptions) { options.BaseBin = " " }, want: "base binary path is required"},
		{name: "candidate binary", edit: func(options *LocalTestComparisonOptions) { options.CandidateBin = " " }, want: "candidate binary path is required"},
		{name: "project", edit: func(options *LocalTestComparisonOptions) { options.Project = " " }, want: "project path is required"},
		{name: "output", edit: func(options *LocalTestComparisonOptions) { options.Out = " " }, want: "output path is required"},
		{name: "manifest", edit: func(options *LocalTestComparisonOptions) { options.Manifest = " " }, want: "manifest path is required"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			options := valid
			tt.edit(&options)
			if err := ValidateLocalTestComparisonOptions(options); err == nil || err.Error() != tt.want {
				t.Fatalf("ValidateLocalTestComparisonOptions error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateLocalTestComparisonOptionsRequiresFiveRunsAndPositiveWorkers(t *testing.T) {
	valid := LocalTestComparisonOptions{
		BaseBin:      "/base/glade",
		CandidateBin: "/candidate/glade",
		Project:      "/source/project",
		Out:          "/output",
		Workers:      1,
		Runs:         5,
		Manifest:     "/targets.json",
	}
	for _, tt := range []struct {
		name string
		edit func(*LocalTestComparisonOptions)
		want string
	}{
		{name: "zero workers", edit: func(options *LocalTestComparisonOptions) { options.Workers = 0 }, want: "workers must be at least 1"},
		{name: "negative workers", edit: func(options *LocalTestComparisonOptions) { options.Workers = -1 }, want: "workers must be at least 1"},
		{name: "too few runs", edit: func(options *LocalTestComparisonOptions) { options.Runs = 4 }, want: "runs must be exactly 5"},
		{name: "too many runs", edit: func(options *LocalTestComparisonOptions) { options.Runs = 6 }, want: "runs must be exactly 5"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			options := valid
			tt.edit(&options)
			if err := ValidateLocalTestComparisonOptions(options); err == nil || err.Error() != tt.want {
				t.Fatalf("ValidateLocalTestComparisonOptions error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRunLocalTestComparisonInvocationUsesUniqueColdProjectCopies(t *testing.T) {
	project := newLocalTestComparisonProject(t)
	snapshot := newLocalTestComparisonSnapshot(t)

	var copiedProjects []string
	for i := 0; i < 2; i++ {
		result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
			Snapshot:      snapshot,
			SourceProject: project,
			Target:        LocalTestComparisonTarget{ID: "full"},
			Workers:       2,
			ArtifactDir:   newLocalTestComparisonArtifactDir(t),
		})
		if err != nil {
			t.Fatal(err)
		}
		var output struct {
			Project        string `json:"project"`
			ModePreserved  bool   `json:"modePreserved"`
			ExcludedAbsent bool   `json:"excludedAbsent"`
			GOMAXPROCS     string `json:"gomaxprocs"`
		}
		decodeLocalTestComparisonJSON(t, result.ResultPath, &output)
		if output.Project == project || filepath.Base(output.Project) != "project" {
			t.Fatalf("child project = %q, source = %q", output.Project, project)
		}
		if !output.ModePreserved || !output.ExcludedAbsent || output.GOMAXPROCS != "2" {
			t.Fatalf("child output = %#v", output)
		}
		if _, err := os.Stat(output.Project); !os.IsNotExist(err) {
			t.Fatalf("copied project still exists: %v", err)
		}
		copiedProjects = append(copiedProjects, output.Project)
	}
	if copiedProjects[0] == copiedProjects[1] {
		t.Fatalf("project copies were reused: %q", copiedProjects[0])
	}
}

func TestRunLocalTestComparisonInvocationDoesNotCopyOrMutateSourceGladeState(t *testing.T) {
	project := newLocalTestComparisonProject(t)
	sentinel := filepath.Join(project, ".glade", "sentinel")
	writeLocalTestFile(t, sentinel, "source-state\n")
	snapshot := newLocalTestComparisonSnapshot(t)

	result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      snapshot,
		SourceProject: project,
		Target:        LocalTestComparisonTarget{ID: "full"},
		Workers:       1,
		ArtifactDir:   newLocalTestComparisonArtifactDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(sentinel)
	if err != nil || string(contents) != "source-state\n" {
		t.Fatalf("source sentinel = %q, %v", contents, err)
	}
	var output struct {
		SourceStateAbsent bool `json:"sourceStateAbsent"`
	}
	decodeLocalTestComparisonJSON(t, result.ResultPath, &output)
	if !output.SourceStateAbsent {
		t.Fatal("source .glade state was present in child project")
	}
}

func TestRunLocalTestComparisonInvocationRejectsSymlinks(t *testing.T) {
	project := newLocalTestComparisonProject(t)
	if err := os.Symlink("project.txt", filepath.Join(project, "linked.txt")); err != nil {
		t.Fatal(err)
	}
	artifactDir := newLocalTestComparisonArtifactDir(t)
	_, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      newLocalTestComparisonSnapshot(t),
		SourceProject: project,
		Target:        LocalTestComparisonTarget{ID: "full"},
		Workers:       1,
		ArtifactDir:   artifactDir,
	})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("RunLocalTestComparisonInvocation error = %v, want symlink rejection", err)
	}
	if _, err := os.Stat(artifactDir); !os.IsNotExist(err) {
		t.Fatalf("artifact directory exists after pre-launch copy rejection: %v", err)
	}
}

func TestRunLocalTestComparisonInvocationPreCanceledCreatesNoArtifacts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	artifactDir := newLocalTestComparisonArtifactDir(t)
	_, err := RunLocalTestComparisonInvocation(ctx, LocalTestComparisonInvocationOptions{
		Snapshot:      newLocalTestComparisonSnapshot(t),
		SourceProject: newLocalTestComparisonProject(t),
		Target:        LocalTestComparisonTarget{ID: "full"},
		Workers:       1,
		ArtifactDir:   artifactDir,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunLocalTestComparisonInvocation error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(artifactDir); !os.IsNotExist(err) {
		t.Fatalf("artifact directory exists for pre-canceled invocation: %v", err)
	}
}

func TestRunLocalTestComparisonInvocationRejectsSameLengthPostCopyMutation(t *testing.T) {
	project := newLocalTestComparisonProject(t)
	writeLocalTestFile(t, filepath.Join(project, "project.txt"), "AAAA")
	artifactDir := newLocalTestComparisonArtifactDir(t)
	_, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      newLocalTestComparisonSnapshot(t),
		SourceProject: project,
		Target:        LocalTestComparisonTarget{ID: "full"},
		Workers:       1,
		ArtifactDir:   artifactDir,
		testHooks: &localTestComparisonInvocationTestHooks{
			afterProjectFileCopy: func(relative string) error {
				if relative != "project.txt" {
					return nil
				}
				return os.WriteFile(filepath.Join(project, relative), []byte("BBBB"), 0o644)
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "source project changed") {
		t.Fatalf("RunLocalTestComparisonInvocation error = %v, want stable-content rejection", err)
	}
	if _, err := os.Stat(artifactDir); !os.IsNotExist(err) {
		t.Fatalf("artifact directory exists after source drift: %v", err)
	}
}

func TestRunLocalTestComparisonInvocationCopiesCanonicalProjectAfterAliasRetarget(t *testing.T) {
	firstParent := t.TempDir()
	secondParent := t.TempDir()
	firstProject := filepath.Join(firstParent, "project")
	secondProject := filepath.Join(secondParent, "project")
	writeLocalTestFile(t, filepath.Join(firstProject, "project.txt"), "canonical-first")
	writeLocalTestFile(t, filepath.Join(secondProject, "project.txt"), "retargeted-second")
	alias := filepath.Join(t.TempDir(), "source-alias")
	if err := os.Symlink(firstParent, alias); err != nil {
		t.Fatal(err)
	}

	result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      newLocalTestComparisonSnapshot(t),
		SourceProject: filepath.Join(alias, "project"),
		Target:        LocalTestComparisonTarget{ID: "full"},
		Workers:       1,
		ArtifactDir:   newLocalTestComparisonArtifactDir(t),
		testHooks: &localTestComparisonInvocationTestHooks{
			beforeProjectCopy: func() error {
				if err := os.Remove(alias); err != nil {
					return err
				}
				return os.Symlink(secondParent, alias)
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		ProjectContent string `json:"projectContent"`
	}
	decodeLocalTestComparisonJSON(t, result.ResultPath, &output)
	if output.ProjectContent != "canonical-first" {
		t.Fatalf("project content = %q, want canonical source", output.ProjectContent)
	}
}

func TestRunLocalTestComparisonInvocationSkipsWorktreeGitPointerFile(t *testing.T) {
	project := newLocalTestComparisonProject(t)
	if err := os.RemoveAll(filepath.Join(project, ".git")); err != nil {
		t.Fatal(err)
	}
	writeLocalTestFile(t, filepath.Join(project, ".git"), "gitdir: /generic/worktree\n")

	result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      newLocalTestComparisonSnapshot(t),
		SourceProject: project,
		Target:        LocalTestComparisonTarget{ID: "full"},
		Workers:       1,
		ArtifactDir:   newLocalTestComparisonArtifactDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		ExcludedAbsent bool `json:"excludedAbsent"`
	}
	decodeLocalTestComparisonJSON(t, result.ResultPath, &output)
	if !output.ExcludedAbsent {
		t.Fatal("worktree .git pointer was copied into the cold project")
	}
}

func TestRunLocalTestComparisonInvocationRejectsArtifactPathResolvingInsideSource(t *testing.T) {
	project := newLocalTestComparisonProject(t)
	alias := filepath.Join(t.TempDir(), "project-alias")
	if err := os.Symlink(project, alias); err != nil {
		t.Fatal(err)
	}
	artifactDir := filepath.Join(alias, "artifacts", "invocation")

	_, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      newLocalTestComparisonSnapshot(t),
		SourceProject: project,
		Target:        LocalTestComparisonTarget{ID: "full"},
		Workers:       1,
		ArtifactDir:   artifactDir,
	})
	if err == nil || !strings.Contains(err.Error(), "outside the source project") {
		t.Fatalf("RunLocalTestComparisonInvocation error = %v, want resolved-path rejection", err)
	}
	if _, err := os.Stat(filepath.Join(project, "artifacts")); !os.IsNotExist(err) {
		t.Fatalf("source project was mutated through artifact symlink: %v", err)
	}
}

func TestRunLocalTestComparisonInvocationUsesCanonicalArtifactPath(t *testing.T) {
	canonicalParent := t.TempDir()
	if err := os.Chmod(canonicalParent, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "artifact-alias")
	if err := os.Symlink(canonicalParent, alias); err != nil {
		t.Fatal(err)
	}

	result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      newLocalTestComparisonSnapshot(t),
		SourceProject: newLocalTestComparisonProject(t),
		Target:        LocalTestComparisonTarget{ID: "full"},
		Workers:       1,
		ArtifactDir:   filepath.Join(alias, "invocation"),
	})
	if err != nil {
		t.Fatal(err)
	}
	resolvedParent, err := filepath.EvalSymlinks(canonicalParent)
	if err != nil {
		t.Fatal(err)
	}
	wantArtifactDir := filepath.Join(resolvedParent, "invocation")
	if result.ArtifactDir != wantArtifactDir {
		t.Fatalf("artifact directory = %q, want canonical %q", result.ArtifactDir, wantArtifactDir)
	}
	for _, path := range []string{result.ResultPath, result.PerfPath, result.StderrPath, result.MetricsPath} {
		if filepath.Dir(path) != wantArtifactDir {
			t.Fatalf("artifact path = %q, want canonical parent %q", path, wantArtifactDir)
		}
	}
}

func TestRunLocalTestComparisonInvocationRejectsArtifactParentSwapBeforeFileCreation(t *testing.T) {
	trustedParent := t.TempDir()
	if err := os.Chmod(trustedParent, 0o700); err != nil {
		t.Fatal(err)
	}
	replacementParent := t.TempDir()
	if err := os.Chmod(replacementParent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(replacementParent, "invocation"), 0o700); err != nil {
		t.Fatal(err)
	}
	movedTrustedParent := trustedParent + "-moved"
	artifactDir := filepath.Join(trustedParent, "invocation")

	_, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      newLocalTestComparisonSnapshot(t),
		SourceProject: newLocalTestComparisonProject(t),
		Target:        LocalTestComparisonTarget{ID: "full"},
		Workers:       1,
		ArtifactDir:   artifactDir,
		testHooks: &localTestComparisonInvocationTestHooks{
			afterArtifactDirectoryCreate: func() error {
				if err := os.Rename(trustedParent, movedTrustedParent); err != nil {
					return err
				}
				return os.Rename(replacementParent, trustedParent)
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "artifact parent changed") {
		t.Fatalf("RunLocalTestComparisonInvocation error = %v, want artifact parent drift", err)
	}
	for _, path := range []string{
		filepath.Join(trustedParent, "invocation", "result.json"),
		filepath.Join(movedTrustedParent, "invocation", "result.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("redirected artifact exists at %q: %v", path, err)
		}
	}
}

func TestRunLocalTestComparisonInvocationCancellationTerminatesDescendants(t *testing.T) {
	stateDir := t.TempDir()
	startedPath := filepath.Join(stateDir, "started")
	latePath := filepath.Join(stateDir, "late")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snapshot := prepareLocalTestComparisonSnapshot(t, newCancelingLocalTestComparisonExecutable(t, startedPath, latePath))
	project := newLocalTestComparisonProject(t)
	artifactDir := newLocalTestComparisonArtifactDir(t)
	type invocationOutcome struct {
		result LocalTestComparisonInvocationResult
		err    error
	}
	outcomes := make(chan invocationOutcome, 1)
	go func() {
		result, err := RunLocalTestComparisonInvocation(ctx, LocalTestComparisonInvocationOptions{
			Snapshot:      snapshot,
			SourceProject: project,
			Target:        LocalTestComparisonTarget{ID: "full"},
			Workers:       1,
			ArtifactDir:   artifactDir,
		})
		outcomes <- invocationOutcome{result: result, err: err}
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case early := <-outcomes:
			t.Fatalf("invocation returned before descendant started: %v", early.err)
		default:
		}
		if _, err := os.Stat(startedPath); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("descendant did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	var outcome invocationOutcome
	select {
	case outcome = <-outcomes:
	case <-time.After(5 * time.Second):
		t.Fatal("canceled invocation did not return")
	}
	if !errors.Is(outcome.err, context.Canceled) {
		t.Fatalf("RunLocalTestComparisonInvocation error = %v, want context.Canceled", outcome.err)
	}
	paths := []string{outcome.result.ResultPath, outcome.result.PerfPath, outcome.result.StderrPath, outcome.result.MetricsPath}
	checksums := make(map[string]string, len(paths))
	for _, path := range paths {
		checksum, err := localTestComparisonFileSHA256(path)
		if err != nil {
			t.Fatal(err)
		}
		checksums[path] = checksum
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(latePath); !os.IsNotExist(err) {
		t.Fatalf("descendant performed late write: %v", err)
	}
	for path, want := range checksums {
		got, err := localTestComparisonFileSHA256(path)
		if err != nil || got != want {
			t.Fatalf("artifact checksum changed for %q: %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestRunLocalTestComparisonInvocationCleansDescendantsAfterTerminalExit(t *testing.T) {
	for _, exitCode := range []int{0, 7} {
		t.Run(fmt.Sprintf("exit-%d", exitCode), func(t *testing.T) {
			stateDir := t.TempDir()
			latePath := filepath.Join(stateDir, "late")
			result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
				Snapshot:      prepareLocalTestComparisonSnapshot(t, newBackgroundLocalTestComparisonExecutable(t, exitCode, latePath)),
				SourceProject: newLocalTestComparisonProject(t),
				Target:        LocalTestComparisonTarget{ID: "full"},
				Workers:       1,
				ArtifactDir:   newLocalTestComparisonArtifactDir(t),
			})
			if exitCode == 0 && err != nil {
				t.Fatal(err)
			}
			if exitCode != 0 && (err == nil || !strings.Contains(err.Error(), "exited with code 7")) {
				t.Fatalf("RunLocalTestComparisonInvocation error = %v, want exit code 7", err)
			}
			paths := []string{result.ResultPath, result.PerfPath, result.StderrPath, result.MetricsPath}
			checksums := make(map[string]string, len(paths))
			for _, path := range paths {
				checksum, checksumErr := localTestComparisonFileSHA256(path)
				if checksumErr != nil {
					t.Fatal(checksumErr)
				}
				checksums[path] = checksum
			}
			time.Sleep(1500 * time.Millisecond)
			if _, statErr := os.Stat(latePath); !os.IsNotExist(statErr) {
				t.Fatalf("descendant performed late write: %v", statErr)
			}
			for path, want := range checksums {
				got, checksumErr := localTestComparisonFileSHA256(path)
				if checksumErr != nil || got != want {
					t.Fatalf("artifact checksum changed for %q: %q, %v; want %q", path, got, checksumErr, want)
				}
			}
		})
	}
}

func TestRunLocalTestComparisonInvocationWritesResultPerfStderrAndMetrics(t *testing.T) {
	t.Setenv("SALESFORCE_ACCESS_TOKEN", "must-not-leak")
	artifactDir := newLocalTestComparisonArtifactDir(t)
	project := newLocalTestComparisonProject(t)
	result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      newLocalTestComparisonSnapshot(t),
		SourceProject: project,
		Target:        LocalTestComparisonTarget{ID: "full"},
		Workers:       2,
		ArtifactDir:   artifactDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{result.ResultPath, result.PerfPath, result.StderrPath, result.MetricsPath} {
		if filepath.Dir(path) != result.ArtifactDir {
			t.Fatalf("artifact path = %q, want directory %q", path, result.ArtifactDir)
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("artifact %q: %v", path, err)
		}
	}
	stderr, err := os.ReadFile(result.StderrPath)
	if err != nil || string(stderr) != "generic stderr\n" {
		t.Fatalf("stderr = %q, %v", stderr, err)
	}
	var metrics LocalTestComparisonInvocationMetrics
	decodeLocalTestComparisonJSON(t, result.MetricsPath, &metrics)
	if metrics.ExitCode != 0 || metrics.WallTimeNS <= 0 || metrics.MaxRSSBytes == 0 {
		t.Fatalf("metrics = %#v", metrics)
	}
	var output struct {
		Project     string `json:"project"`
		Secret      string `json:"secret"`
		HomePrivate bool   `json:"homePrivate"`
	}
	decodeLocalTestComparisonJSON(t, result.ResultPath, &output)
	resolvedProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if output.Secret != "" || !output.HomePrivate {
		t.Fatalf("child environment output = %#v", output)
	}
	if metrics.PhysicalProjectRoot != output.Project || metrics.LogicalSourceRoot != resolvedProject {
		t.Fatalf("project root mapping = %#v, child project = %q", metrics, output.Project)
	}
	if len(metrics.Artifacts) != 3 {
		t.Fatalf("metric artifacts = %#v", metrics.Artifacts)
	}
	for _, artifact := range metrics.Artifacts {
		data, err := os.ReadFile(artifact.Path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != artifact.SHA256 {
			t.Fatalf("checksum for %q = %q, want %q", artifact.Path, artifact.SHA256, got)
		}
	}
	if err := os.Mkdir(artifactDir, 0o755); !os.IsExist(err) {
		t.Fatalf("artifact directory was not created exclusively: %v", err)
	}
	if info, err := os.Stat(result.ArtifactDir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("artifact directory mode = %v, %v", info, err)
	}
	for _, path := range []string{result.ResultPath, result.PerfPath, result.StderrPath, result.MetricsPath} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("artifact mode for %q = %v, %v", path, info, err)
		}
	}
}

func TestRunLocalTestComparisonInvocationRejectsInvalidGeneratedArtifacts(t *testing.T) {
	for _, tt := range []struct {
		name       string
		behavior   string
		cpuProfile bool
		wantError  string
	}{
		{name: "empty perf", behavior: "empty-perf", wantError: "perf artifact is empty"},
		{name: "invalid perf", behavior: "invalid-perf", wantError: "perf artifact is not valid JSON"},
		{name: "replaced perf inode", behavior: "replace-perf", wantError: "perf artifact inode changed"},
		{name: "empty CPU profile", behavior: "empty-cpu", cpuProfile: true, wantError: "CPU profile artifact is empty"},
		{name: "invalid CPU profile", behavior: "invalid-cpu", cpuProfile: true, wantError: "CPU profile artifact is not a valid gzip profile"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
				Snapshot:      prepareLocalTestComparisonSnapshot(t, newInvalidArtifactLocalTestComparisonExecutable(t, tt.behavior)),
				SourceProject: newLocalTestComparisonProject(t),
				Target:        LocalTestComparisonTarget{ID: "artifact-validation", CPUProfile: tt.cpuProfile},
				Workers:       1,
				ArtifactDir:   newLocalTestComparisonArtifactDir(t),
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("RunLocalTestComparisonInvocation error = %v, want %q", err, tt.wantError)
			}
			if data, readErr := os.ReadFile(result.ResultPath); readErr != nil || string(data) != "{\"completed\":true}\n" {
				t.Fatalf("successful child result = %q, %v", data, readErr)
			}
		})
	}
}

func TestRunLocalTestComparisonInvocationPreservesArtifactsOnNonzeroExit(t *testing.T) {
	result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      prepareLocalTestComparisonSnapshot(t, newFailingLocalTestComparisonExecutable(t)),
		SourceProject: newLocalTestComparisonProject(t),
		Target:        LocalTestComparisonTarget{ID: "full"},
		Workers:       1,
		ArtifactDir:   newLocalTestComparisonArtifactDir(t),
	})
	if err == nil || !strings.Contains(err.Error(), "exited with code 7") {
		t.Fatalf("RunLocalTestComparisonInvocation error = %v, want exit code 7", err)
	}
	for path, want := range map[string]string{
		result.ResultPath: `{"failed":true}` + "\n",
		result.StderrPath: "failure stderr\n",
		result.PerfPath:   `{"perf":true}` + "\n",
	} {
		data, readErr := os.ReadFile(path)
		if readErr != nil || string(data) != want {
			t.Fatalf("artifact %q = %q, %v; want %q", path, data, readErr, want)
		}
	}
	var metrics LocalTestComparisonInvocationMetrics
	decodeLocalTestComparisonJSON(t, result.MetricsPath, &metrics)
	if metrics.ExitCode != 7 || len(metrics.Artifacts) != 3 {
		t.Fatalf("metrics = %#v", metrics)
	}
}

func TestRunLocalTestComparisonInvocationPassesOnlyManifestSelectors(t *testing.T) {
	result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      newLocalTestComparisonSnapshot(t),
		SourceProject: newLocalTestComparisonProject(t),
		Target: LocalTestComparisonTarget{
			ID:     "focused",
			Class:  "GenericTest",
			Method: "runs",
		},
		Workers:     3,
		ArtifactDir: newLocalTestComparisonArtifactDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Argv []string `json:"argv"`
	}
	decodeLocalTestComparisonJSON(t, result.ResultPath, &output)
	want := []string{
		"local-tests",
		"--project", output.Argv[2],
		"--parallel", "3",
		"--parallel-methods",
		"--perf-json", result.PerfPath,
		"--json",
		"--class", "GenericTest",
		"--method", "runs",
	}
	if fmt.Sprint(output.Argv) != fmt.Sprint(want) {
		t.Fatalf("argv = %#v, want %#v", output.Argv, want)
	}
}

func TestRunLocalTestComparisonInvocationCapturesCPUProfileWhenRequested(t *testing.T) {
	result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      newLocalTestComparisonSnapshot(t),
		SourceProject: newLocalTestComparisonProject(t),
		Target:        LocalTestComparisonTarget{ID: "profiled", CPUProfile: true},
		Workers:       1,
		ArtifactDir:   newLocalTestComparisonArtifactDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result.CPUProfilePath) != "cpu.pprof" {
		t.Fatalf("CPU profile path = %q", result.CPUProfilePath)
	}
	data, err := os.ReadFile(result.CPUProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	decompressed, err := io.ReadAll(profile)
	closeErr := profile.Close()
	if err != nil || closeErr != nil || string(decompressed) != "generic cpu profile\n" {
		t.Fatalf("CPU profile = %q, %v, close = %v", decompressed, err, closeErr)
	}
	if info, err := os.Stat(result.CPUProfilePath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("CPU profile mode = %v, %v", info, err)
	}
}

func TestRunLocalTestComparisonInvocationDoesNotCaptureCPUProfileWhenTargetDisablesIt(t *testing.T) {
	result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      newLocalTestComparisonSnapshot(t),
		SourceProject: newLocalTestComparisonProject(t),
		Target:        LocalTestComparisonTarget{ID: "unprofiled", CPUProfile: false},
		Workers:       1,
		ArtifactDir:   newLocalTestComparisonArtifactDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CPUProfilePath != "" {
		t.Fatalf("CPU profile path = %q, want empty", result.CPUProfilePath)
	}
	var output struct {
		Argv []string `json:"argv"`
	}
	decodeLocalTestComparisonJSON(t, result.ResultPath, &output)
	if strings.Contains(strings.Join(output.Argv, " "), "--cpu-profile") {
		t.Fatalf("argv unexpectedly enables CPU profile: %#v", output.Argv)
	}
}

func TestWriteLocalTestComparisonEnvironmentOmitsSensitiveEnvironment(t *testing.T) {
	snapshot := newLocalTestComparisonSnapshot(t)
	t.Setenv("GOMAXPROCS", "99")
	t.Setenv("GOGC", "75")
	t.Setenv("GOMEMLIMIT", "512MiB")
	t.Setenv("HOME", "/sensitive/home")
	t.Setenv("USER", "sensitive-user")
	t.Setenv("UNRELATED_SECRET", "sensitive-value")

	first := filepath.Join(t.TempDir(), "environment.json")
	second := filepath.Join(t.TempDir(), "environment.json")
	options := LocalTestComparisonEnvironmentOptions{Snapshot: snapshot, Workers: 2, Runs: 5}
	if err := WriteLocalTestComparisonEnvironment(context.Background(), first, options); err != nil {
		t.Fatal(err)
	}
	if err := WriteLocalTestComparisonEnvironment(context.Background(), second, options); err != nil {
		t.Fatal(err)
	}
	firstData, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondData, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstData, secondData) {
		t.Fatalf("environment JSON is not deterministic:\n%s\n%s", firstData, secondData)
	}
	for _, sensitive := range []string{"/sensitive/home", "sensitive-user", "sensitive-value", "UNRELATED_SECRET", "HOME", "USER"} {
		if bytes.Contains(firstData, []byte(sensitive)) {
			t.Fatalf("environment JSON contains %q: %s", sensitive, firstData)
		}
	}
	var environment LocalTestComparisonEnvironment
	if err := json.Unmarshal(firstData, &environment); err != nil {
		t.Fatal(err)
	}
	if environment.GOOS != runtime.GOOS || environment.GOARCH != runtime.GOARCH || environment.Binary.Path != snapshot.binary.Path {
		t.Fatalf("environment = %#v", environment)
	}
	if environment.Workers != 2 || environment.Runs != 5 || environment.LogicalCPUs != runtime.NumCPU() {
		t.Fatalf("environment = %#v", environment)
	}
	if environment.Tuning.GOMAXPROCS != "2" || environment.Tuning.GOGC != "75" || environment.Tuning.GOMEMLIMIT != "512MiB" {
		t.Fatalf("environment tuning = %#v", environment.Tuning)
	}
	if string(environment.Manifest) != `{"schemaVersion":1,"name":"generic"}` {
		t.Fatalf("manifest = %s", environment.Manifest)
	}
	if info, err := os.Stat(first); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("environment mode = %v, %v", info, err)
	}
}

func TestLocalTestComparisonBinarySnapshotSurvivesOriginalReplacement(t *testing.T) {
	original := newLocalTestComparisonExecutable(t)
	snapshot, err := PrepareLocalTestComparisonBinarySnapshot(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := snapshot.Remove(); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	})
	writeLocalTestFile(t, original, `#!/bin/sh
set -eu
if [ "${1:-}" = "manifest" ]; then
  printf '{"schemaVersion":1,"name":"replacement"}\n'
  exit 0
fi
exit 91
`)
	if err := os.Chmod(original, 0o755); err != nil {
		t.Fatal(err)
	}

	environmentPath := filepath.Join(t.TempDir(), "environment.json")
	if err := WriteLocalTestComparisonEnvironment(context.Background(), environmentPath, LocalTestComparisonEnvironmentOptions{
		Snapshot: snapshot,
		Workers:  2,
		Runs:     5,
	}); err != nil {
		t.Fatal(err)
	}
	var environment LocalTestComparisonEnvironment
	decodeLocalTestComparisonJSON(t, environmentPath, &environment)
	if environment.Binary.Path == original || string(environment.Manifest) != `{"schemaVersion":1,"name":"generic"}` {
		t.Fatalf("environment = %#v", environment)
	}
	checksum, err := localTestComparisonFileSHA256(environment.Binary.Path)
	if err != nil || checksum != environment.Binary.SHA256 {
		t.Fatalf("snapshot checksum = %q, %v; environment = %#v", checksum, err, environment.Binary)
	}
	binaryInfo, err := os.Stat(environment.Binary.Path)
	if err != nil || binaryInfo.Mode().Perm() != 0o500 {
		t.Fatalf("snapshot binary mode = %v, %v", binaryInfo, err)
	}
	directoryInfo, err := os.Stat(filepath.Dir(environment.Binary.Path))
	if err != nil || directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("snapshot directory mode = %v, %v", directoryInfo, err)
	}

	result, err := RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
		Snapshot:      snapshot,
		SourceProject: newLocalTestComparisonProject(t),
		Target:        LocalTestComparisonTarget{ID: "full"},
		Workers:       2,
		ArtifactDir:   newLocalTestComparisonArtifactDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		GOMAXPROCS string `json:"gomaxprocs"`
	}
	decodeLocalTestComparisonJSON(t, result.ResultPath, &output)
	if output.GOMAXPROCS != "2" {
		t.Fatalf("snapshot invocation output = %#v", output)
	}
}

func TestLocalTestComparisonBinarySnapshotRejectsDriftBeforeEveryUse(t *testing.T) {
	for _, mutation := range []string{"remove", "mutate", "replace"} {
		t.Run(mutation, func(t *testing.T) {
			snapshot := newLocalTestComparisonSnapshot(t)
			originalSnapshot, err := os.ReadFile(snapshot.binary.Path)
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "remove":
				if err := os.Remove(snapshot.binary.Path); err != nil {
					t.Fatal(err)
				}
			case "mutate":
				if err := os.Chmod(snapshot.binary.Path, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(snapshot.binary.Path, append(originalSnapshot, '\n'), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(snapshot.binary.Path, 0o500); err != nil {
					t.Fatal(err)
				}
			case "replace":
				if err := os.Remove(snapshot.binary.Path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(snapshot.binary.Path, originalSnapshot, 0o500); err != nil {
					t.Fatal(err)
				}
			}

			environmentPath := filepath.Join(t.TempDir(), "environment.json")
			err = WriteLocalTestComparisonEnvironment(context.Background(), environmentPath, LocalTestComparisonEnvironmentOptions{
				Snapshot: snapshot,
				Workers:  1,
				Runs:     5,
			})
			if err == nil || !strings.Contains(err.Error(), "snapshot drift") {
				t.Fatalf("WriteLocalTestComparisonEnvironment error = %v, want snapshot drift", err)
			}
			if _, err := os.Stat(environmentPath); !os.IsNotExist(err) {
				t.Fatalf("environment created after snapshot drift: %v", err)
			}

			artifactDir := newLocalTestComparisonArtifactDir(t)
			_, err = RunLocalTestComparisonInvocation(context.Background(), LocalTestComparisonInvocationOptions{
				Snapshot:      snapshot,
				SourceProject: newLocalTestComparisonProject(t),
				Target:        LocalTestComparisonTarget{ID: "full"},
				Workers:       1,
				ArtifactDir:   artifactDir,
			})
			if err == nil || !strings.Contains(err.Error(), "snapshot drift") {
				t.Fatalf("RunLocalTestComparisonInvocation error = %v, want snapshot drift", err)
			}
			if _, err := os.Stat(artifactDir); !os.IsNotExist(err) {
				t.Fatalf("artifact directory created after snapshot drift: %v", err)
			}
		})
	}
}

func TestLocalTestFixtureExecutionSelection(t *testing.T) {
	t.Setenv(fullLocalTestFixturesEnv, "")
	if shouldRunLocalTestFixture("platform-apis") {
		t.Fatal("large local-test fixture should not run without the opt-in environment variable")
	}
	if shouldRunLocalTestFixture("enterprise-composed") {
		t.Fatal("full local-test fixture should not run without the opt-in environment variable")
	}

	t.Setenv(fullLocalTestFixturesEnv, "1")
	if !shouldRunLocalTestFixture("platform-apis") {
		t.Fatal("large local-test fixture should run when the opt-in environment variable is set")
	}
	if !shouldRunLocalTestFixture("enterprise-composed") {
		t.Fatal("full local-test fixture should run when the opt-in environment variable is set")
	}
}

func TestRunLocalTestsNoDiskCacheDoesNotWriteStartupCache(t *testing.T) {
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}]}`)
	writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/NoDiskCacheTest.cls"), `
@isTest
private class NoDiskCacheTest {
  @isTest static void runs() {
    System.assertEquals(1, 1);
  }
}
`)
	report, err := RunLocalTests(LocalTestOptions{Project: root, NoDiskCache: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready || report.Summary.Pass != 1 {
		t.Fatalf("report = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(root, ".glade", "test", "startup.gob")); !os.IsNotExist(err) {
		t.Fatalf("startup cache stat err = %v, want not exist", err)
	}
}

func shouldRunLocalTestFixture(string) bool {
	return os.Getenv(fullLocalTestFixturesEnv) != ""
}

func requireFullLocalTestFixtures(t *testing.T) {
	t.Helper()
	if os.Getenv(fullLocalTestFixturesEnv) == "" {
		t.Skipf("local-test corpus fixture skipped; set %s=1 to run the full sweep", fullLocalTestFixturesEnv)
	}
}

func runLocalTestReadyFixture(t *testing.T, fixture localTestReadyFixture) {
	t.Helper()
	if !shouldRunLocalTestFixture(fixture.name) {
		t.Skipf("local-test fixture skipped; set %s=1 to run the full sweep", fullLocalTestFixturesEnv)
	}
	report, err := runLocalTestsForTest(t, LocalTestOptions{Project: filepath.Join("..", "..", "testdata", "local-tests", fixture.name)})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready {
		t.Fatalf("ready = false, summary = %#v outcomes = %#v", report.Summary, report.Outcomes)
	}
	if report.Summary.Total != fixture.total || report.Summary.Pass != fixture.total || report.Summary.CompileErrors != 0 || report.Summary.Unsupported != 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
}

func runLocalTestsForTest(t *testing.T, options LocalTestOptions) (LocalTestReport, error) {
	t.Helper()
	localTestRunSlots <- struct{}{}
	t.Cleanup(func() {
		<-localTestRunSlots
	})
	options.NoDiskCache = true
	return RunLocalTests(options)
}

func TestRunLocalTestsClassifiesBasicFixture(t *testing.T) {
	t.Parallel()
	report, err := runLocalTestsForTest(t, LocalTestOptions{Project: filepath.Join("..", "..", "testdata", "local-tests", "basic"), TraceBlocked: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 3 || report.Summary.Pass != 1 || report.Summary.AssertFailures != 1 || report.Summary.Unsupported != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if report.Ready {
		t.Fatalf("ready = true, want false")
	}
	var failing LocalTestOutcome
	for _, outcome := range report.Outcomes {
		if outcome.Class == "FailingTest" {
			failing = outcome
			break
		}
	}
	if failing.TraceEvents == 0 || failing.ProfileEvents == 0 || len(failing.ProfileCategories) == 0 {
		t.Fatalf("failing outcome missing trace/profile summary: %#v", failing)
	}
}

func TestRunLocalTestsProgressShowsCountsElapsedAndETA(t *testing.T) {
	t.Parallel()
	var progress bytes.Buffer
	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:        filepath.Join("..", "..", "testdata", "local-tests", "basic"),
		ProgressWriter: &progress,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CasesRun != 3 {
		t.Fatalf("casesRun = %d, want 3", report.CasesRun)
	}
	out := progress.String()
	for _, want := range []string{
		"Phase: load_start elapsed=",
		"Phase: run_start elapsed=",
		"Progress: 3/3",
		"elapsed=",
		"eta=",
		"pass=1",
		"fail=2",
		"error=0",
		"running=UnsupportedTest.unsupported",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("progress missing %q:\n%s", want, out)
		}
	}
}

func TestRunLocalTestsPerfJSONIncludesCloneAndAllocationCounters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "perf.json")
	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:      filepath.Join("..", "..", "testdata", "local-tests", "basic"),
		PerfJSONPath: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CasesRun == 0 {
		t.Fatalf("expected local tests to run: %#v", report)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var perf LocalTestPerfSummary
	if err := json.Unmarshal(data, &perf); err != nil {
		t.Fatal(err)
	}
	if perf.CloneStats.CloneRuntimeOrgCalls == 0 {
		t.Fatalf("cloneRuntimeOrg calls were not counted: %#v", perf.CloneStats)
	}
	if perf.CloneStats.CloneRuntimeCalls == 0 {
		t.Fatalf("runtime clone calls were not counted: %#v", perf.CloneStats)
	}
	if len(perf.TopCloneClasses) > 0 && perf.TopCloneClasses[0].TestClones == 0 && perf.TopCloneClasses[0].SetupClones == 0 {
		t.Fatalf("topCloneClasses missing clone counts: %#v", perf.TopCloneClasses)
	}
	if len(perf.Phases) == 0 || perf.Phases[0].TotalAllocBytes == 0 {
		t.Fatalf("phase allocation counters missing: %#v", perf.Phases)
	}
}

func TestRunLocalTestsSkipsTraceByDefault(t *testing.T) {
	t.Parallel()
	report, err := runLocalTestsForTest(t, LocalTestOptions{Project: filepath.Join("..", "..", "testdata", "local-tests", "basic")})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range report.Outcomes {
		if outcome.TraceEvents != 0 || outcome.ProfileEvents != 0 || len(outcome.ProfileCategories) != 0 {
			t.Fatalf("default outcome should not include trace/profile: %#v", outcome)
		}
	}
}

func TestRunLocalTestsReportsTopFailures(t *testing.T) {
	t.Parallel()
	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:     filepath.Join("..", "..", "testdata", "local-tests", "basic"),
		TopFailures: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.TopFailures) != 2 {
		t.Fatalf("topFailures = %#v", report.TopFailures)
	}
	if report.TopFailures[0].Count == 0 || report.TopFailures[0].Outcome == "pass" {
		t.Fatalf("topFailures[0] = %#v", report.TopFailures[0])
	}
	if len(report.TopFailures[0].Samples) == 0 {
		t.Fatalf("topFailures[0] missing samples: %#v", report.TopFailures[0])
	}
}

func TestRunLocalTestsFiltersClassList(t *testing.T) {
	t.Parallel()
	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:   filepath.Join("..", "..", "testdata", "local-tests", "basic"),
		ClassList: []string{"PassingTest"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CasesDiscovered == 0 {
		t.Fatalf("expected filtered cases: %#v", report)
	}
	for _, outcome := range report.Outcomes {
		if outcome.Class != "PassingTest" {
			t.Fatalf("unexpected class %q in %#v", outcome.Class, report.Outcomes)
		}
	}
}

func TestRunLocalTestsStartsAtClass(t *testing.T) {
	t.Parallel()
	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:    filepath.Join("..", "..", "testdata", "local-tests", "basic"),
		StartClass: "PassingTest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CasesDiscovered != 2 {
		t.Fatalf("casesDiscovered = %d, want 2: %#v", report.CasesDiscovered, report.Outcomes)
	}
	for _, outcome := range report.Outcomes {
		if outcome.Class == "FailingTest" {
			t.Fatalf("start class included earlier class: %#v", report.Outcomes)
		}
	}
}

func TestRunLocalTestsStopsAfterMaxFailureGroups(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}]}`)
	writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/AFailingTest.cls"), `
@isTest
private class AFailingTest {
  @isTest static void fails() {
    System.assertEquals(3, 1 + 1);
  }
}
`)
	for i := 0; i < 9; i++ {
		className := fmt.Sprintf("PassingTriage%02dTest", i)
		writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/"+className+".cls"), fmt.Sprintf(`
@isTest
private class %s {
  @isTest static void passes() {
    System.assertEquals(2, 1 + 1);
  }
}
`, className))
	}

	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:          root,
		BlockersOnly:     true,
		TopFailures:      1,
		MaxFailureGroups: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.TriageStopped || report.CasesDiscovered != 10 || report.CasesRun >= report.CasesDiscovered {
		t.Fatalf("triage fields = stopped %v discovered %d run %d", report.TriageStopped, report.CasesDiscovered, report.CasesRun)
	}
	if report.Summary.Total != 1 || report.Summary.AssertFailures != 1 || len(report.TopFailures) != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestRunLocalTestsStopsAfterMaxFailureGroupsWithParallelism(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}]}`)
	writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/AFailingTest.cls"), `
@isTest
private class AFailingTest {
  @isTest static void fails() {
    System.assertEquals(3, 1 + 1);
  }
}
`)
	for i := 0; i < 9; i++ {
		className := fmt.Sprintf("PassingParallelTriage%02dTest", i)
		writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/"+className+".cls"), fmt.Sprintf(`
@isTest
private class %s {
  @isTest static void passes() {
    System.assertEquals(2, 1 + 1);
  }
}
`, className))
	}

	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:          root,
		BlockersOnly:     true,
		TopFailures:      1,
		MaxFailureGroups: 1,
		Parallelism:      4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.TriageStopped || report.CasesDiscovered != 10 || report.CasesRun != 4 {
		t.Fatalf("triage fields = stopped %v discovered %d run %d", report.TriageStopped, report.CasesDiscovered, report.CasesRun)
	}
	if report.Summary.Total != 1 || report.Summary.AssertFailures != 1 || len(report.TopFailures) != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestLocalTestRunOutcomeClassifiesDeadlineRuntimeErrorAsTimeout(t *testing.T) {
	outcome := localTestRunOutcome("fixture", testreport.Case{
		ClassName:  "SlowTest",
		MethodName: "timesOut",
		Status:     testreport.StatusRuntimeError,
		Problem:    &testreport.Problem{Type: "RuntimeError", Message: "context deadline exceeded"},
	})
	if outcome.Outcome != "timeout" || outcome.Phase != "timeout" || outcome.CapabilityID != "apex.test.timeout" {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func TestShouldAnalyzeLocalTestsSkipsFocusedRuns(t *testing.T) {
	if !shouldAnalyzeLocalTests(LocalTestOptions{}, 12) {
		t.Fatalf("unfiltered local test run should analyze the full project")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{Class: "CartItemTest"}, 12) {
		t.Fatalf("class-filtered local test run should skip full-project semantic analysis")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{Method: "runsFast"}, 12) {
		t.Fatalf("method-filtered local test run should skip full-project semantic analysis")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{}, largeLocalTestAnalysisThreshold+1) {
		t.Fatalf("large unfiltered local test run should skip full-project semantic analysis by default")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{BlockersOnly: true}, 12) {
		t.Fatalf("blocker-only local test run should skip full-project semantic analysis")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{TopFailures: 10}, 12) {
		t.Fatalf("top-failures local test run should skip full-project semantic analysis")
	}
	if !shouldAnalyzeLocalTests(LocalTestOptions{ForceAnalysis: true}, largeLocalTestAnalysisThreshold+1) {
		t.Fatalf("large unfiltered local test run should allow forced full-project semantic analysis")
	}
	if !shouldAnalyzeLocalTests(LocalTestOptions{BlockersOnly: true, ForceAnalysis: true}, largeLocalTestAnalysisThreshold+1) {
		t.Fatalf("forced blocker-only local test run should allow full-project semantic analysis")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{Parallelism: 8}, 100) {
		t.Fatalf("explicit parallel local test run should skip full-project semantic analysis")
	}
	if shouldAnalyzeLocalTests(LocalTestOptions{ProgressWriter: io.Discard}, 100) {
		t.Fatalf("progress local test run should skip full-project semantic analysis")
	}
}

func TestLocalTestParallelismCapsFocusedClassRuns(t *testing.T) {
	if got := localTestParallelism(LocalTestOptions{}); got < 1 || got > 8 {
		t.Fatalf("full-project default parallelism = %d, want 1..8", got)
	}
	if got := localTestParallelism(LocalTestOptions{Class: "CartSubmitterTest"}); got < 1 || got > 8 {
		t.Fatalf("focused class parallelism = %d, want 1..8", got)
	}
	if got := localTestParallelism(LocalTestOptions{Class: "CartSubmitterTest", Parallelism: 4}); got != 4 {
		t.Fatalf("explicit focused class parallelism = %d, want 4", got)
	}
	if got := localTestParallelism(LocalTestOptions{Class: "CartSubmitterTest", Method: "runs"}); got != 1 {
		t.Fatalf("focused method parallelism = %d, want 1", got)
	}
}

func TestAutoParallelismForCases(t *testing.T) {
	if got := autoParallelismForCases(10); got < 1 || got > 2 {
		t.Fatalf("auto parallelism for tiny suite = %d, want 1..2", got)
	}
	if got := autoParallelismForCases(100); got < 1 || got > 4 {
		t.Fatalf("auto parallelism for small suite = %d, want 1..4", got)
	}
	if got := autoParallelismForCases(800); got < 1 || got > 8 {
		t.Fatalf("auto parallelism for medium suite = %d, want 1..8", got)
	}
	if got := autoParallelismForCases(5000); got < 1 || got > 4 {
		t.Fatalf("auto parallelism for large suite = %d, want 1..4", got)
	}
}

func TestAutoTuneLocalTestOptionsUsesShardEnv(t *testing.T) {
	t.Setenv("GLADE_SHARD_COUNT", "6")
	t.Setenv("GLADE_SHARD_INDEX", "2")
	options, parallelism := autoTuneLocalTestOptions(LocalTestOptions{
		AutoTune:       true,
		AutoShardCount: true,
		AutoShardIndex: true,
	}, 2000, 1)
	if options.ShardCount != 6 {
		t.Fatalf("ShardCount = %d, want 6", options.ShardCount)
	}
	if options.ShardIndex != 2 {
		t.Fatalf("ShardIndex = %d, want 2", options.ShardIndex)
	}
	if parallelism < 1 {
		t.Fatalf("parallelism = %d, want >= 1", parallelism)
	}
}

func TestShouldParallelizeMethodsForLargeFocusedClasses(t *testing.T) {
	if shouldParallelizeMethods(LocalTestOptions{Class: "CartSubmitterTest"}, 4, 12) {
		t.Fatalf("large focused class run should keep methods serial by default")
	}
	if shouldParallelizeMethods(LocalTestOptions{Class: "SmallTest"}, 4, 3) {
		t.Fatalf("small focused class run should keep methods serial")
	}
	if shouldParallelizeMethods(LocalTestOptions{Class: "CartSubmitterTest", Method: "runs"}, 4, 12) {
		t.Fatalf("focused method run should keep methods serial")
	}
	if shouldParallelizeMethods(LocalTestOptions{Class: "CartSubmitterTest"}, 1, 12) {
		t.Fatalf("explicit serial focused class run should keep methods serial")
	}
	if !shouldParallelizeMethods(LocalTestOptions{Class: "CartSubmitterTest", ParallelMethods: true}, 4, 12) {
		t.Fatalf("focused class run should allow explicit method parallelism")
	}
}

func TestFocusedLocalTestsSkipTraceByDefault(t *testing.T) {
	t.Parallel()
	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project: filepath.Join("..", "..", "testdata", "local-tests", "basic"),
		Class:   "FailingTest",
		Method:  "fails",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 1 || report.Summary.AssertFailures != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	outcome := report.Outcomes[0]
	if outcome.TraceEvents != 0 || outcome.ProfileEvents != 0 || len(outcome.ProfileCategories) != 0 {
		t.Fatalf("focused outcome should not include trace/profile by default: %#v", outcome)
	}
}

func TestRunLocalTestsClassFilterIsExact(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}]}`)
	writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/CartSubmitterTest.cls"), `
@isTest
private class CartSubmitterTest {
  @isTest static void runs() {
    System.assertEquals(1, 1);
  }
}
`)
	writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/ScheduleWithCartSubmitterTest.cls"), `
@isTest
private class ScheduleWithCartSubmitterTest {
  @isTest static void shouldNotRun() {
    System.assert(false, 'wrong class');
  }
}
`)

	report, err := runLocalTestsForTest(t, LocalTestOptions{Project: root, Class: "CartSubmitterTest"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 1 || report.Summary.Pass != 1 {
		t.Fatalf("summary = %#v outcomes = %#v", report.Summary, report.Outcomes)
	}
	if report.Outcomes[0].Class != "CartSubmitterTest" {
		t.Fatalf("outcome = %#v", report.Outcomes[0])
	}
}

func TestRunLocalTestsChangedSinceNoneDoesNotTurnFocusedClassIntoLoadError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true}]}`)
	writeLocalTestFile(t, filepath.Join(root, "force-app/main/default/classes/SampleTest.cls"), `
@isTest
private class SampleTest {
  @isTest static void runs() {
    System.assertEquals(1, 1);
  }
}
`)
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init")
	runGit("config", "user.email", "glade@example.test")
	runGit("config", "user.name", "Glade Test")
	runGit("add", ".")
	runGit("commit", "-m", "initial")

	report, err := runLocalTestsForTest(t, LocalTestOptions{
		Project:      root,
		Class:        "SampleTest",
		ChangedSince: "HEAD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready || report.Summary.Total != 0 || report.Summary.LoadErrors != 0 {
		t.Fatalf("report = %#v, want ready zero-run report", report)
	}
	if report.CasesDiscovered != 0 || report.CasesRun != 0 {
		t.Fatalf("cases = discovered %d run %d, want 0/0", report.CasesDiscovered, report.CasesRun)
	}
	if report.Selection == nil {
		t.Fatalf("selection missing: %#v", report)
	}
}

func TestRunLocalTestsFocusedSelectionReportsNoMatches(t *testing.T) {
	t.Parallel()
	missingRoot := filepath.Join(t.TempDir(), "missing")
	for _, tt := range []struct {
		name    string
		options LocalTestOptions
		want    string
	}{
		{
			name: "class",
			options: LocalTestOptions{
				Project: filepath.Join("..", "..", "testdata", "local-tests", "basic"),
				Class:   "MissingTest",
			},
			want: `no Apex test methods matched --class "MissingTest"`,
		},
		{
			name: "method without class",
			options: LocalTestOptions{
				Project: filepath.Join("..", "..", "testdata", "local-tests", "basic"),
				Method:  "passes",
			},
			want: "--method requires --class",
		},
		{
			name: "missing project",
			options: LocalTestOptions{
				Project: missingRoot,
			},
			want: "project root does not exist",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			report, err := runLocalTestsForTest(t, tt.options)
			if err != nil {
				t.Fatal(err)
			}
			if report.Ready {
				t.Fatalf("ready = true, want false: %#v", report)
			}
			if report.Summary.Total != 1 || report.Summary.LoadErrors != 1 {
				t.Fatalf("summary = %#v, want one load error", report.Summary)
			}
			if len(report.Outcomes) != 1 || report.Outcomes[0].Outcome != "load_error" || !strings.Contains(report.Outcomes[0].Error, tt.want) {
				t.Fatalf("outcomes = %#v, want %q", report.Outcomes, tt.want)
			}
		})
	}
}

func TestLocalTestRunOutcomeSplitsRuntimeAndTimeout(t *testing.T) {
	runtimeGap := localTestRunOutcome("fixture", testreport.Case{
		ClassName:  "RuntimeGapTest",
		MethodName: "fails",
		Status:     testreport.StatusFail,
		Problem:    &testreport.Problem{Type: "RuntimeError", Message: "method dispatch failed"},
	})
	if runtimeGap.Outcome != "runtime_gap" {
		t.Fatalf("runtime outcome = %#v", runtimeGap)
	}

	assertFail := localTestRunOutcome("fixture", testreport.Case{
		ClassName:  "AssertTest",
		MethodName: "fails",
		Status:     testreport.StatusFail,
		Problem:    &testreport.Problem{Type: "AssertException", Message: "Assertion Failed"},
	})
	if assertFail.Outcome != "assert_fail" || assertFail.Phase != "assert" {
		t.Fatalf("assert outcome = %#v", assertFail)
	}

	timeout := localTestRunOutcome("fixture", testreport.Case{
		ClassName:  "TimeoutTest",
		MethodName: "hangs",
		Status:     testreport.StatusUnsupported,
		Problem:    &testreport.Problem{Type: "Canceled", Message: "context deadline exceeded"},
	})
	if timeout.Outcome != "timeout" || timeout.CapabilityID != "apex.test.timeout" {
		t.Fatalf("timeout outcome = %#v", timeout)
	}
}

func TestRunLocalTestsPlatformAPIsFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "platform-apis", total: 4})
}

func TestRunLocalTestsNamedCredentialCalloutsFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "named-credential-callouts", total: 2})
}

func TestRunLocalTestsFilesEmailFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "files-email", total: 2})
}

func TestRunLocalTestsWorkflowFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "workflow", total: 1})
}

func TestRunLocalTestsFlowFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "flow", total: 1})
}

func TestRunLocalTestsResourcesLabelsFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "resources-labels", total: 2})
}

func TestRunLocalTestsUIControllerContractsFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "ui-controller-contracts", total: 2})
}

func TestRunLocalTestsVisualforcePagesFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "visualforce-pages", total: 3})
}

func TestRunLocalTestsOrgLikeRunnerFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "org-like-runner", total: 2})
}

func TestRunLocalTestsVMExceptionDispatchFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "vm-exception-dispatch", total: 1})
}

func TestRunLocalTestsStandardObjectShapeFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "standard-object-shape", total: 2})
}

func TestRunLocalTestsEnterpriseComposedFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "enterprise-composed", total: 2})
}

func TestRunLocalTestsMetadataDeployFixtureReady(t *testing.T) {
	runLocalTestReadyFixture(t, localTestReadyFixture{name: "metadata-deploy", total: 1})
}

func TestCheckLocalTestCorpusFixture(t *testing.T) {
	requireFullLocalTestFixtures(t)
	report, err := CheckLocalTestCorpus(filepath.Join("..", "..", "docs", "fixtures", "local-tests-corpus.json"))
	if err != nil {
		t.Fatalf("CheckLocalTestCorpus error = %v, report = %#v", err, report)
	}
	if !report.Ready || len(report.Projects) != 16 {
		t.Fatalf("report = %#v", report)
	}
}

func TestCheckPostParityTraceFixture(t *testing.T) {
	report, err := CheckPostParityTraceFixture(filepath.Join("..", "..", "docs", "fixtures", "post-parity-trace-events.json"))
	if err != nil {
		t.Fatalf("CheckPostParityTraceFixture error = %v, report = %#v", err, report)
	}
	if !report.Ready || len(report.Surfaces) != 3 {
		t.Fatalf("report = %#v", report)
	}
}

func TestCheckUIControllerDiscoveryFixture(t *testing.T) {
	report, err := CheckUIControllerDiscovery(filepath.Join("..", "..", "docs", "fixtures", "ui-controller-discovery.json"))
	if err != nil {
		t.Fatalf("CheckUIControllerDiscovery error = %v, report = %#v", err, report)
	}
	if !report.Ready || report.Summary.AuraBundles != 1 || report.Summary.LWCBundles != 1 || report.Summary.UnresolvedApex != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestRunLocalTestsReportsLoadError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "sfdx-project.json"), `{`)
	report, err := runLocalTestsForTest(t, LocalTestOptions{Project: root})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.LoadErrors != 1 || report.Outcomes[0].Outcome != "load_error" {
		t.Fatalf("report = %#v", report)
	}
}

func writeLocalTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newLocalTestComparisonProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeLocalTestFile(t, filepath.Join(root, "project.txt"), "generic project\n")
	executable := filepath.Join(root, "bin", "tool")
	writeLocalTestFile(t, executable, "#!/bin/sh\n")
	if err := os.Chmod(executable, 0o751); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{".git", ".sf", ".sfdx", "node_modules"} {
		writeLocalTestFile(t, filepath.Join(root, directory, "sentinel"), "excluded\n")
	}
	return root
}

func newLocalTestComparisonArtifactDir(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "invocation")
}

func newLocalTestComparisonSnapshot(t *testing.T) LocalTestComparisonBinarySnapshot {
	t.Helper()
	return prepareLocalTestComparisonSnapshot(t, newLocalTestComparisonExecutable(t))
}

func prepareLocalTestComparisonSnapshot(t *testing.T, binaryPath string) LocalTestComparisonBinarySnapshot {
	t.Helper()
	snapshot, err := PrepareLocalTestComparisonBinarySnapshot(context.Background(), binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := snapshot.Remove(); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	})
	return snapshot
}

func newLocalTestComparisonExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "generic-compat")
	writeLocalTestFile(t, path, `#!/bin/sh
set -eu
if [ "${1:-}" = "manifest" ]; then
  [ "${2:-}" = "--json" ]
  printf '{"schemaVersion":1,"name":"generic"}\n'
  exit 0
fi

argv_json=""
for arg in "$@"; do
  if [ -n "$argv_json" ]; then
    argv_json="$argv_json,"
  fi
  argv_json="$argv_json\"$arg\""
done

[ "${1:-}" = "local-tests" ]
shift
project=""
perf=""
cpu=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --project) project=$2; shift 2 ;;
    --parallel) shift 2 ;;
    --parallel-methods|--json) shift ;;
    --perf-json) perf=$2; shift 2 ;;
    --class|--method) shift 2 ;;
    --cpu-profile) cpu=$2; shift 2 ;;
    *) exit 42 ;;
  esac
done

source_state_absent=false
if [ ! -e "$project/.glade" ]; then
  source_state_absent=true
fi
excluded_absent=true
for directory in .git .sf .sfdx node_modules; do
  if [ -e "$project/$directory" ]; then
    excluded_absent=false
  fi
done
mode_preserved=false
if [ -x "$project/bin/tool" ]; then
  mode_preserved=true
fi
home_private=false
if [ "$(ls -ld "$HOME" | cut -c1-10)" = "drwx------" ]; then
  home_private=true
fi
project_content=$(cat "$project/project.txt")

mkdir -p "$project/.glade"
printf 'child-state\n' > "$project/.glade/child"
printf '{"perf":true}\n' > "$perf"
if [ -n "$cpu" ]; then
  printf 'generic cpu profile\n' | gzip -c > "$cpu"
fi
printf 'generic stderr\n' >&2
printf '{"project":"%s","argv":[%s],"modePreserved":%s,"excludedAbsent":%s,"sourceStateAbsent":%s,"gomaxprocs":"%s","secret":"%s","homePrivate":%s,"projectContent":"%s"}\n' \
  "$project" "$argv_json" "$mode_preserved" "$excluded_absent" "$source_state_absent" "${GOMAXPROCS:-}" "${SALESFORCE_ACCESS_TOKEN:-}" "$home_private" "$project_content"
`)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newFailingLocalTestComparisonExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "failing-generic-compat")
	writeLocalTestFile(t, path, `#!/bin/sh
set -eu
if [ "${1:-}" = "manifest" ]; then
  printf '{"schemaVersion":1,"name":"failing-generic"}\n'
  exit 0
fi
[ "${1:-}" = "local-tests" ]
shift
perf=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --project|--parallel|--class|--method) shift 2 ;;
    --parallel-methods|--json) shift ;;
    --perf-json) perf=$2; shift 2 ;;
    --cpu-profile) shift 2 ;;
    *) exit 42 ;;
  esac
done
printf '{"perf":true}\n' > "$perf"
printf '{"failed":true}\n'
printf 'failure stderr\n' >&2
exit 7
`)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newInvalidArtifactLocalTestComparisonExecutable(t *testing.T, behavior string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "invalid-artifact-generic-compat")
	script := strings.Replace(`#!/bin/sh
set -eu
if [ "${1:-}" = "manifest" ]; then
  printf '{"schemaVersion":1,"name":"invalid-artifact-generic"}\n'
  exit 0
fi
[ "${1:-}" = "local-tests" ]
shift
perf=""
cpu=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --project|--parallel|--class|--method) shift 2 ;;
    --parallel-methods|--json) shift ;;
    --perf-json) perf=$2; shift 2 ;;
    --cpu-profile) cpu=$2; shift 2 ;;
    *) exit 42 ;;
  esac
done
case "BEHAVIOR" in
  empty-perf)
    ;;
  invalid-perf)
    printf 'not JSON\n' > "$perf"
    ;;
  replace-perf)
    printf '{"perf":true}\n' > "${perf}.replacement"
    mv "${perf}.replacement" "$perf"
    ;;
  empty-cpu)
    printf '{"perf":true}\n' > "$perf"
    ;;
  invalid-cpu)
    printf '{"perf":true}\n' > "$perf"
    printf 'not a CPU profile\n' > "$cpu"
    ;;
  *)
    exit 43
    ;;
esac
printf '{"completed":true}\n'
`, "BEHAVIOR", behavior, 1)
	writeLocalTestFile(t, path, script)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newCancelingLocalTestComparisonExecutable(t *testing.T, startedPath, latePath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "canceling-generic-compat")
	writeLocalTestFile(t, path, fmt.Sprintf(`#!/bin/sh
set -eu
if [ "${1:-}" = "manifest" ]; then
  printf '{"schemaVersion":1,"name":"canceling-generic"}\n'
  exit 0
fi
[ "${1:-}" = "local-tests" ]
shift
perf=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --project|--parallel|--class|--method) shift 2 ;;
    --parallel-methods|--json) shift ;;
    --perf-json) perf=$2; shift 2 ;;
    --cpu-profile) shift 2 ;;
    *) exit 42 ;;
  esac
done
printf '{"perf":true}\n' > "$perf"
printf '{"running":true}\n'
printf 'canceling stderr\n' >&2
(
  printf 'started\n' > "%s"
  sleep 1
  printf 'late\n' > "%s"
) &
sleep 30
`, startedPath, latePath))
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newBackgroundLocalTestComparisonExecutable(t *testing.T, exitCode int, latePath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "background-generic-compat")
	writeLocalTestFile(t, path, fmt.Sprintf(`#!/bin/sh
set -eu
if [ "${1:-}" = "manifest" ]; then
  printf '{"schemaVersion":1,"name":"background-generic"}\n'
  exit 0
fi
[ "${1:-}" = "local-tests" ]
shift
perf=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --project|--parallel|--class|--method) shift 2 ;;
    --parallel-methods|--json) shift ;;
    --perf-json) perf=$2; shift 2 ;;
    --cpu-profile) shift 2 ;;
    *) exit 42 ;;
  esac
done
printf '{"perf":true}\n' > "$perf"
printf '{"background":true}\n'
printf 'background stderr\n' >&2
(
  sleep 1
  printf 'late\n' >> "$perf"
  printf 'late\n' > "%s"
) &
exit %d
`, latePath, exitCode))
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func decodeLocalTestComparisonJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("decode %q: %v\n%s", path, err, data)
	}
}
