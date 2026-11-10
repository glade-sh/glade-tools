package corpusrun

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
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
