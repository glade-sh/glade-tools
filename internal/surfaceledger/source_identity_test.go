package surfaceledger

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSourceIdentityBindsDocsRootAndManifest(t *testing.T) {
	root := t.TempDir()
	manifest := []byte(`[{"path":"one.md"}]`)
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	identity := SourceIdentity{
		SourceRoot:      root,
		ManifestSHA256:  fmt.Sprintf("%x", sha256.Sum256(manifest)),
		ManifestEntries: 1,
	}
	if err := ValidateSourceIdentity(identity, root); err != nil {
		t.Fatalf("valid identity rejected: %v", err)
	}
	identity.SourceRoot = filepath.Join(root, "other")
	if err := ValidateSourceIdentity(identity, root); err == nil {
		t.Fatal("source root mismatch accepted")
	}
	identity.SourceRoot = root
	identity.ManifestSHA256 = "stale"
	if err := ValidateSourceIdentity(identity, root); err == nil {
		t.Fatal("manifest hash mismatch accepted")
	}
}

func TestApplySourceIdentityAnnotatesFallbackRows(t *testing.T) {
	ledger := SurfaceLedger{Rows: []SurfaceLedgerRow{
		{SurfaceID: "connect-rest-api:quickreference", DocsSource: "connect-rest-api/quickreference.md"},
		{SurfaceID: "apex:System.List", DocsSource: "apex/apex_class_system_list.md"},
	}}
	identity := SourceIdentity{
		ManifestSHA256:  "manifest-sha",
		LatestAtlas:     "262.0",
		FallbackDocsets: map[string]SourceDocsetIdentity{"connect-rest-api": {AtlasVersion: "260.0"}},
	}
	ApplySourceIdentity(&ledger, identity)
	if got := ledger.Rows[0].DocsSourceAtlasVersion; got != "260.0" {
		t.Fatalf("fallback source version = %q", got)
	}
	if got := ledger.Rows[0].DocsSourceReleaseStatus; got != "non-parity-fallback" {
		t.Fatalf("fallback release status = %q", got)
	}
	if got := ledger.Rows[1].DocsSourceAtlasVersion; got != "262.0" {
		t.Fatalf("latest source version = %q", got)
	}
	if got := ledger.Rows[1].DocsSourceReleaseStatus; got != "latest" {
		t.Fatalf("latest release status = %q", got)
	}
	if ledger.SourceIdentity == nil || ledger.SourceIdentity.ManifestSHA256 != "manifest-sha" {
		t.Fatalf("ledger source identity = %#v", ledger.SourceIdentity)
	}
}

func TestPacketManifestCarriesSourceReleaseIdentity(t *testing.T) {
	ledger := SurfaceLedger{Rows: []SurfaceLedgerRow{
		{SurfaceID: "connect-rest-api:quickreference", DocsSource: "connect-rest-api/quickreference.md", Bucket: BucketGap, DocsSourceAtlasVersion: "260.0", DocsSourceReleaseStatus: "non-parity-fallback"},
	}}
	manifest := BuildPacketManifest(ledger)
	if len(manifest.Packets) != 1 {
		t.Fatalf("packets = %#v", manifest.Packets)
	}
	packet := manifest.Packets[0]
	if packet.SourceAtlasVersion != "260.0" || packet.SourceReleaseStatus != "non-parity-fallback" {
		t.Fatalf("packet source identity = %#v", packet)
	}
}
