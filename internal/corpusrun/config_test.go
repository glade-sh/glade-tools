package corpusrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sampleTiers = `{
  "schemaVersion": 1,
  "depSources": {
    "sample-dep": {"paths": ["deps/sample-dep"], "dirs": ["src"], "mode": "source", "note": "synthetic"}
  },
  "repos": [
    {"name": "repo-c", "tier": "C", "clone": "clones/repo-c", "pin": "ccc", "sourceApi": "67.0", "license": "MIT"},
    {"name": "repo-a", "tier": "A", "clone": ["missing/first", "clones/repo-a"], "pin": "aaa",
     "overlay": {"deps": ["sample-dep"], "config": {"permissionSets": ["Sample_Perms"]}}},
    {"name": "repo-b", "tier": "B", "clone": "clones/repo-b", "pin": "bbb"}
  ]
}`

func loadSampleConfig(t *testing.T) *Config {
	t.Helper()
	dir := t.TempDir()
	writeTestConfig(t, dir, sampleTiers)
	config, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func repoNames(repos []Repo) []string {
	names := []string{}
	for _, repo := range repos {
		names = append(names, repo.Name)
	}
	return names
}

func TestSelectTiersAreNested(t *testing.T) {
	config := loadSampleConfig(t)
	cases := []struct {
		tier string
		want []string
	}{
		{"A", []string{"repo-a"}},
		{"B", []string{"repo-a", "repo-b"}},
		{"C", []string{"repo-a", "repo-b", "repo-c"}},
	}
	for _, tc := range cases {
		repos, err := config.Select(tc.tier, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := repoNames(repos); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("tier %s: %v, want %v", tc.tier, got, tc.want)
		}
	}
}

func TestSelectOnlyNarrowsTheTier(t *testing.T) {
	config := loadSampleConfig(t)
	repos, err := config.Select("C", []string{"repo-b", "repo-a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := repoNames(repos); !reflect.DeepEqual(got, []string{"repo-a", "repo-b"}) {
		t.Fatalf("got %v", got)
	}
}

func TestSelectRefusals(t *testing.T) {
	config := loadSampleConfig(t)
	cases := []struct {
		name string
		tier string
		only []string
		want string
	}{
		{"bad tier", "D", nil, "tier"},
		{"name outside the tier", "A", []string{"repo-b"}, "repo-b"},
		{"name that does not exist", "C", []string{"repo-z"}, "repo-z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := config.Select(tc.tier, tc.only); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadConfigRefusals(t *testing.T) {
	cases := []struct {
		name  string
		tiers string
		want  string
	}{
		{"unknown field", `{"schemaVersion":1,"repos":[],"rpeos":[]}`, "rpeos"},
		{"unknown overlay field", `{"schemaVersion":1,"repos":[{"name":"a","tier":"A","clone":"x","pin":"p","overlay":{"config":{"feature":["F"]}}}]}`, "feature"},
		{"bad schema", `{"schemaVersion":9,"repos":[]}`, "schemaVersion"},
		{"bad tier", `{"schemaVersion":1,"repos":[{"name":"a","tier":"Z","clone":"x","pin":"p"}]}`, "tier"},
		{"missing pin", `{"schemaVersion":1,"repos":[{"name":"a","tier":"A","clone":"x"}]}`, "pin"},
		{"missing clone", `{"schemaVersion":1,"repos":[{"name":"a","tier":"A","pin":"p"}]}`, "clone"},
		{"duplicate", `{"schemaVersion":1,"repos":[{"name":"a","tier":"A","clone":"x","pin":"p"},{"name":"a","tier":"B","clone":"x","pin":"p"}]}`, "duplicate"},
		{"path in a name", `{"schemaVersion":1,"repos":[{"name":"../a","tier":"A","clone":"x","pin":"p"}]}`, "../a"},
		{"undeclared dep", `{"schemaVersion":1,"repos":[{"name":"a","tier":"A","clone":"x","pin":"p","overlay":{"deps":["nope"]}}]}`, "nope"},
		{"bad dep mode", `{"schemaVersion":1,"depSources":{"d":{"mode":"magic"}},"repos":[]}`, "magic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestConfig(t, dir, tc.tiers)
			if _, err := LoadConfig(dir); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	if _, err := LoadConfig(t.TempDir()); err == nil || !strings.Contains(err.Error(), TiersFile) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveCloneTakesTheFirstDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, sampleTiers)
	if err := os.MkdirAll(filepath.Join(dir, "clones", "repo-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	repos, err := config.Select("A", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := config.ResolvePath(repos[0].Clone)
	if !ok || got != filepath.Join(dir, "clones", "repo-a") {
		t.Fatalf("ResolvePath = %q, %v", got, ok)
	}
	if _, ok := config.ResolvePath(PathList{"missing/one", "missing/two"}); ok {
		t.Fatal("ResolvePath found a directory that does not exist")
	}
}

func TestResolvePathExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "clones", "repo-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := &Config{dir: t.TempDir()}
	got, ok := config.ResolvePath(PathList{"~/clones/repo-a"})
	if !ok || got != filepath.Join(home, "clones", "repo-a") {
		t.Fatalf("ResolvePath = %q, %v", got, ok)
	}
}

func TestPathListAcceptsStringOrList(t *testing.T) {
	var single, many PathList
	if err := json.Unmarshal([]byte(`"a"`), &single); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`["a","b"]`), &many); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(single, PathList{"a"}) || !reflect.DeepEqual(many, PathList{"a", "b"}) {
		t.Fatalf("single %v many %v", single, many)
	}
}
