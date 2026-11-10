package corpusrun

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOverlayListsUseGladeInlineFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "glade.yml")
	if err := os.WriteFile(path, []byte("project:\n  packageDirs: [src]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	additions := map[string]map[string][]string{"project": {
		"dependencyPaths":            {"deps/one", "deps/two"},
		"managedPackageDependencies": {"example:deps/managed"},
	}}
	if err := mergeOverlayYAML(path, additions, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"packageDirs", "dependencyPaths", "managedPackageDependencies"} {
		pattern := `(?m)^\s*` + key + `:\s*\[[^\n]*\]\s*$`
		if !regexp.MustCompile(pattern).Match(data) {
			t.Errorf("%s must use the inline list syntax accepted by Glade: %s", key, data)
		}
	}
}

func TestManagedOverlayAcceptsMetadataWithoutManifest(t *testing.T) {
	for _, dirs := range [][]string{nil, {"src"}} {
		t.Run(fmt.Sprint(dirs), func(t *testing.T) {
			base := t.TempDir()
			root, dep := filepath.Join(base, "root"), filepath.Join(base, "dep")
			if err := os.MkdirAll(root, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(dep, "src"), 0755); err != nil {
				t.Fatal(err)
			}
			spec := DepSource{Mode: "managed", Namespace: "example", Paths: PathList{dep}, Dirs: dirs}
			cfg := &Config{DepSources: map[string]DepSource{"sample": spec}}
			if err := applyOverlay(cfg, Repo{Overlay: Overlay{Deps: []string{"sample"}}}, root); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, depsDir, "sample", "sfdx-project.json"))
			if err != nil {
				t.Fatal(err)
			}
			var project struct {
				Namespace          string `json:"namespace"`
				PackageDirectories []struct {
					Path string `json:"path"`
				} `json:"packageDirectories"`
			}
			if err := json.Unmarshal(data, &project); err != nil {
				t.Fatal(err)
			}
			want := "."
			if len(dirs) > 0 {
				want = dirs[0]
			}
			if project.Namespace != "example" || len(project.PackageDirectories) != 1 || project.PackageDirectories[0].Path != want {
				t.Fatalf("unexpected project: %+v", project)
			}
		})
	}
}

func TestManagedOverlayRejectsUnsafeManifestlessSources(t *testing.T) {
	for _, tc := range []struct{ name, namespace, dir string }{
		{"missing namespace", "", "."},
		{"escaping directory", "example", "../outside"},
		{"missing directory", "example", "absent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			root, dep := filepath.Join(base, "root"), filepath.Join(base, "dep")
			if err := os.MkdirAll(root, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(dep, 0755); err != nil {
				t.Fatal(err)
			}
			spec := DepSource{Mode: "managed", Namespace: tc.namespace, Paths: PathList{dep}, Dirs: []string{tc.dir}}
			cfg := &Config{DepSources: map[string]DepSource{"sample": spec}}
			if err := applyOverlay(cfg, Repo{Overlay: Overlay{Deps: []string{"sample"}}}, root); err == nil {
				t.Fatal("unsafe source accepted")
			}
		})
	}
}

func TestOverlayPreservesStructuredManagedDependencies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "glade.yml")
	original := "project:\n  managedPackageDependencies:\n    - namespace: existing\n      sourceRoot: vendor/existing\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	add := map[string]map[string][]string{"project": {"managedPackageDependencies": {"extra:deps/extra"}}}
	if err := mergeOverlayYAML(path, add, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Project struct {
			Managed []map[string]string `yaml:"managedPackageDependencies"`
		} `yaml:"project"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("structured dependencies must stay mappings: %v\n%s", err, data)
	}
	if len(cfg.Project.Managed) != 2 || cfg.Project.Managed[1]["namespace"] != "extra" || cfg.Project.Managed[1]["sourceRoot"] != "deps/extra" {
		t.Fatalf("unexpected managed entries: %+v", cfg.Project.Managed)
	}
}
