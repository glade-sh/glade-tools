package surfaceledger

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type SourceIdentity struct {
	SourceRoot      string                          `json:"sourceRoot,omitempty"`
	ManifestSHA256  string                          `json:"manifestSHA256,omitempty"`
	ManifestEntries int                             `json:"manifestEntries,omitempty"`
	Host            string                          `json:"host,omitempty"`
	User            string                          `json:"user,omitempty"`
	LatestAtlas     string                          `json:"latestAtlas,omitempty"`
	FallbackDocsets map[string]SourceDocsetIdentity `json:"fallbackDocsets,omitempty"`
	LatestDocsets   map[string]SourceDocsetIdentity `json:"latestDocsets,omitempty"`
}

type SourceDocsetIdentity struct {
	AtlasVersion string `json:"atlasVersion"`
	Pages        int    `json:"pages,omitempty"`
}

func ReadSourceIdentity(path string) (SourceIdentity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SourceIdentity{}, err
	}
	var identity SourceIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return SourceIdentity{}, fmt.Errorf("decode source identity %s: %w", path, err)
	}
	return identity, nil
}

func ValidateSourceIdentity(identity SourceIdentity, docsSource string) error {
	docsRoot, err := filepath.Abs(docsSource)
	if err != nil {
		return fmt.Errorf("resolve docs source: %w", err)
	}
	sourceRoot, err := filepath.Abs(identity.SourceRoot)
	if err != nil || identity.SourceRoot == "" || sourceRoot != docsRoot {
		return fmt.Errorf("source identity root %q does not match docs source %q", identity.SourceRoot, docsSource)
	}
	if identity.ManifestSHA256 == "" || identity.ManifestEntries <= 0 {
		return fmt.Errorf("source identity is missing manifest binding")
	}
	manifestPath := filepath.Join(docsRoot, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read docs manifest: %w", err)
	}
	actualHash := fmt.Sprintf("%x", sha256.Sum256(data))
	if !strings.EqualFold(actualHash, identity.ManifestSHA256) {
		return fmt.Errorf("docs manifest hash %s does not match source identity %s", actualHash, identity.ManifestSHA256)
	}
	var entries []struct {
		Docset string `json:"docset"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("decode docs manifest: %w", err)
	}
	if len(entries) != identity.ManifestEntries {
		return fmt.Errorf("docs manifest entries %d do not match source identity %d", len(entries), identity.ManifestEntries)
	}
	docsets := map[string]bool{}
	for _, entry := range entries {
		if entry.Docset != "" {
			docsets[entry.Docset] = true
		}
	}
	if len(docsets) == 0 {
		return nil
	}
	for docset := range docsets {
		fallback, isFallback := identity.FallbackDocsets[docset]
		latest, isLatest := identity.LatestDocsets[docset]
		if isFallback == isLatest {
			return fmt.Errorf("manifest docset %q must have exactly one latest or fallback binding", docset)
		}
		versionData, err := os.ReadFile(filepath.Join(docsRoot, docset, "_version.json"))
		if err != nil {
			return fmt.Errorf("read docset identity %s: %w", docset, err)
		}
		var version struct {
			Version           string `json:"version"`
			AtlasVersionLabel string `json:"atlas_version_label"`
		}
		if err := json.Unmarshal(versionData, &version); err != nil {
			return fmt.Errorf("decode docset identity %s: %w", docset, err)
		}
		if isFallback {
			if fallback.AtlasVersion == "" || version.Version != fallback.AtlasVersion {
				return fmt.Errorf("fallback docset %q version %q does not match %q", docset, version.Version, fallback.AtlasVersion)
			}
		} else if latest.AtlasVersion == "" || version.Version != "latest" || !strings.Contains(version.AtlasVersionLabel, latest.AtlasVersion) {
			return fmt.Errorf("latest docset %q release metadata does not match %q", docset, latest.AtlasVersion)
		}
	}
	for docset := range identity.FallbackDocsets {
		if !docsets[docset] {
			return fmt.Errorf("fallback binding %q is not present in the docs manifest", docset)
		}
	}
	for docset := range identity.LatestDocsets {
		if !docsets[docset] {
			return fmt.Errorf("latest binding %q is not present in the docs manifest", docset)
		}
	}
	return nil
}

func ApplySourceIdentity(ledger *SurfaceLedger, identity SourceIdentity) {
	copy := identity
	ledger.SourceIdentity = &copy
	for i := range ledger.Rows {
		row := &ledger.Rows[i]
		family := manifestSourceDir(row.DocsSource)
		if family == "" {
			continue
		}
		if fallback, ok := identity.FallbackDocsets[family]; ok {
			row.DocsSourceAtlasVersion = fallback.AtlasVersion
			row.DocsSourceReleaseStatus = "non-parity-fallback"
			continue
		}
		if latest, ok := identity.LatestDocsets[family]; ok {
			row.DocsSourceAtlasVersion = latest.AtlasVersion
			row.DocsSourceReleaseStatus = "latest"
		}
	}
}

func sourceReleaseIdentity(rows []SurfaceLedgerRow) (string, string) {
	versions := map[string]bool{}
	statuses := map[string]bool{}
	for _, row := range rows {
		if row.DocsSourceAtlasVersion != "" {
			versions[row.DocsSourceAtlasVersion] = true
		}
		if row.DocsSourceReleaseStatus != "" {
			statuses[row.DocsSourceReleaseStatus] = true
		}
	}
	return oneOrMixed(versions), oneOrMixed(statuses)
}

func oneOrMixed(values map[string]bool) string {
	if len(values) == 1 {
		for value := range values {
			return value
		}
	}
	if len(values) > 1 {
		return "mixed"
	}
	return ""
}
