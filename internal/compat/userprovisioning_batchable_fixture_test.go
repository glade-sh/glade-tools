package compat

import (
	"path/filepath"
	"testing"
)

func TestUserProvisioningBatchableLifecycleFixture(t *testing.T) {
	fixture, err := LoadFile(filepath.Join("..", "..", "docs", "fixtures", "async-userprovisioning-batchable-lifecycle.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("fixture result = %#v", result)
	}
}
