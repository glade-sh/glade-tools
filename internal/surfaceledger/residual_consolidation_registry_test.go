package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResidualConsolidationHasOneActiveOwnerPerAcceptedRow(t *testing.T) {
	want := map[string]string{
		"apex:UserProvisioning.PluginBatchable":                      "current-base-userprovisioning-deterministic-mock-004-api67.json",
		"apex:UserProvisioning.UserProvisioningPlugin":               "current-base-userprovisioning-deterministic-mock-004-api67.json",
		"apex:UserProvisioning.UserProvisioningPlugin.clone()":       "current-base-userprovisioning-deterministic-mock-004-api67.json",
		"apex:Cache.Org.Org()":                                       "current-base-cache-tail-deterministic-api67.json",
		"apex:Cache.Session.Session()":                               "current-base-cache-tail-deterministic-api67.json",
		"apex:System.String.template(valueMap)":                      "core-runtime-system-string-template-value-map-api67.json",
		"apex:System.Http.send(HttpRequest)":                         "core-runtime-deterministic-tail-local-evidence-api67.json",
	}
	owners := make(map[string][]string, len(want))
	paths, err := filepath.Glob(filepath.Join("..", "..", "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct {
			EvidenceOnly bool `json:"evidenceOnly"`
			Evidence     []struct {
				SurfaceID string `json:"surfaceId"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(data, &fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.EvidenceOnly {
			continue
		}
		name := filepath.Base(path)
		for _, row := range fixture.Evidence {
			if _, ok := want[row.SurfaceID]; ok {
				owners[row.SurfaceID] = append(owners[row.SurfaceID], name)
			}
		}
	}
	for id, expected := range want {
		got := owners[id]
		if len(got) != 1 || got[0] != expected {
			t.Fatalf("active owners for %s = %v, want [%s]", id, got, expected)
		}
	}
}
