package corpusrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverlayManagedAndStub(t *testing.T) {
	for _, mode := range []string{"managed", "stub"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "root")
			dep := filepath.Join(base, "dep")
			for _, dir := range []string{root, dep} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			manifest := `{"packageDirectories":[{"path":"src","dependencies":[{"package":"sample-package"}]}]}`
			if err := os.WriteFile(filepath.Join(root, "sfdx-project.json"), []byte(manifest), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dep, "sfdx-project.json"), []byte(`{"namespace":"example","packageDirectories":[{"path":"src"}]}`), 0644); err != nil {
				t.Fatal(err)
			}
			spec := DepSource{Paths: PathList{dep}, Mode: mode}
			// Decode the private config schema in the same way as a real config.
			if err := json.Unmarshal([]byte(`{"paths":["`+dep+`"],"mode":"`+mode+`","packages":["sample-package"],"markDefault":["src"]}`), &spec); err != nil {
				t.Fatal(err)
			}
			cfg := &Config{dir: base, DepSources: map[string]DepSource{"example": spec}}
			if err := applyOverlay(cfg, Repo{Overlay: Overlay{Deps: []string{"example"}}}, root); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, "glade.yml"))
			if err != nil {
				t.Fatal(err)
			}
			field := "managedPackageDependencies"
			if mode == "stub" {
				field = "dependencyPaths"
			}
			if !strings.Contains(string(data), field) {
				t.Fatalf("missing %s: %s", field, data)
			}
		})
	}
}

func TestOverlayMergesWithExistingConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sfdx-project.json"), []byte(`{"packageDirectories":[{"path":"src","dependencies":[{"package":"declared"}]}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	original := "project:\n  packageDirs: [src]\ntest:\n  parallelism: 2\n"
	if err := os.WriteFile(filepath.Join(root, "glade.yml"), []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{DepSources: map[string]DepSource{"sample": {Mode: "stub", Packages: []string{"declared"}}}}
	if err := applyOverlay(cfg, Repo{Overlay: Overlay{Deps: []string{"sample"}}}, root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "glade.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"packageDirs:", "parallelism:", "dependencyPaths:"} {
		if !strings.Contains(string(data), part) {
			t.Fatalf("missing %s: %s", part, data)
		}
	}
}

func TestOverlayProjectDependencyInsideRepo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sfdx-project.json"), []byte(`{"packageDirectories":[{"path":"src"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	dep := filepath.Join(root, "vendor", "sample")
	if err := os.MkdirAll(dep, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "sfdx-project.json"), []byte(`{"packageDirectories":[{"path":"src","package":"example"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	var spec DepSource
	if err := json.Unmarshal([]byte(`{"mode":"project","repoPath":"vendor/sample"}`), &spec); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{DepSources: map[string]DepSource{"sample": spec}}
	if err := applyOverlay(cfg, Repo{Overlay: Overlay{Deps: []string{"sample"}}}, root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "glade.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "dependencyPaths:") {
		t.Fatalf("project dependency not registered: %s", data)
	}
}

func TestOverlayRejectsInvalidDependencyDeclarations(t *testing.T) {
	t.Run("stub is not declared", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "sfdx-project.json"), []byte(`{"packageDirectories":[]}`), 0644); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{DepSources: map[string]DepSource{"sample": {Mode: "stub", Packages: []string{"undeclared"}}}}
		if err := applyOverlay(cfg, Repo{Overlay: Overlay{Deps: []string{"sample"}}}, root); err == nil {
			t.Fatal("undeclared stub accepted")
		}
	})
	t.Run("managed namespace is absent", func(t *testing.T) {
		root := t.TempDir()
		dep := t.TempDir()
		if err := os.WriteFile(filepath.Join(dep, "sfdx-project.json"), []byte(`{"packageDirectories":[]}`), 0644); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{DepSources: map[string]DepSource{"sample": {Mode: "managed", Paths: PathList{dep}}}}
		if err := applyOverlay(cfg, Repo{Overlay: Overlay{Deps: []string{"sample"}}}, root); err == nil {
			t.Fatal("managed dependency without namespace accepted")
		}
	})
}
