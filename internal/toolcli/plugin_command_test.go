package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestManifestJSONListsCompatCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"manifest", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}

	var manifest struct {
		APIVersion string `json:"apiVersion"`
		Name       string `json:"name"`
		Version    string `json:"version"`
		Commands   []struct {
			Path    []string `json:"path"`
			Summary string   `json:"summary"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, stdout.String())
	}
	if manifest.APIVersion != "glade.plugin.v1" || manifest.Name != "compat" || manifest.Version == "" {
		t.Fatalf("unexpected manifest identity: %#v", manifest)
	}

	want := map[string]bool{
		"compat":      true,
		"surface":     true,
		"local-tests": true,
		"post-parity": true,
		"examples":    true,
		"dashboard":   true,
		"gaps":        true,
		"stdlib":      true,
	}
	for _, command := range manifest.Commands {
		if len(command.Path) == 1 {
			delete(want, command.Path[0])
		}
	}
	if len(want) != 0 {
		t.Fatalf("manifest missing command roots: %#v", want)
	}
}

func TestManifestRequiresJSONFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"manifest"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected manifest without --json to fail, stdout=%s", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("expected an error on stderr")
	}
}
