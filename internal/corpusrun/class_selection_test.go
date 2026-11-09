package corpusrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectProjectClassesExcludesDependencies(t *testing.T) {
	root := t.TempDir()
	dep := filepath.Join(root, depsDir, "sample")
	if err := os.MkdirAll(dep, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "DependencyTest.cls"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "glade")
	script := `#!/bin/sh
while [ $# -gt 0 ]; do
 if [ "$1" = "--write-class-shards" ]; then mkdir -p "$2"; printf 'ProjectTest\nDependencyTest\nAnotherTest\n' > "$2/shard.txt"; exit 0; fi
 shift
done
exit 2
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := selectProjectClasses(context.Background(), Options{Glade: bin}, root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ProjectTest\nAnotherTest\n" {
		t.Fatalf("selected %q", strings.TrimSpace(string(data)))
	}
}
