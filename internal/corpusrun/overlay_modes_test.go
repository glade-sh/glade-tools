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
