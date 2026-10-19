package releasecontract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RecheckCarriedSources is the network freshness gate of the normal release
// check. Analyze and generation remain deterministic, offline receipt checks.
// These are anonymous public documentation requests; no org/auth state is used.
func RecheckCarriedSources(contractPath string) error {
	return recheckCarriedSources(context.Background(), contractPath, &http.Client{Timeout: 30 * time.Second})
}

type docsClient interface {
	Do(*http.Request) (*http.Response, error)
}

func recheckCarriedSources(ctx context.Context, contractPath string, client docsClient) error {
	contract, root, err := Load(contractPath)
	if err != nil {
		return err
	}
	for _, release := range contract.Releases {
		var receipt ReleaseSourceReceipt
		if err := readStrict(filepath.Join(root, release.SourceReceipt), &receipt); err != nil {
			return err
		}
		for _, family := range receipt.Snapshot.Families {
			if family.CarriedFrom == "" {
				continue
			}
			docset := map[string]string{"lightning-aura": "lightning", "rest-api": "api_rest", "visualforce": "pages", "apex": "apexref", "tooling-api": "api_tooling"}[family.Name]
			if docset == "" {
				return fmt.Errorf("cannot recheck carried family %q", family.Name)
			}
			url := "https://developer.salesforce.com/docs/get_document/atlas.en-us." + receipt.Snapshot.AtlasVersion + "." + docset + ".meta"
			if err := checkAtlasPublication(ctx, client, url, receipt.Snapshot.AtlasVersion, family.CarriedFrom); err != nil {
				return fmt.Errorf("%s %s carry recheck: %w", release.Name, family.Name, err)
			}
		}
		if receipt.Snapshot.LWC.CarriedFrom != "" {
			if err := checkLWCPublication(ctx, client, receipt.Snapshot.LWC, receipt.Snapshot.AtlasVersion); err != nil {
				return fmt.Errorf("%s lwc carry recheck: %w", release.Name, err)
			}
		}
	}
	return nil
}

func checkAtlasPublication(ctx context.Context, client docsClient, url, target, previous string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("publication unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("publication unavailable: HTTP %d", response.StatusCode)
	}
	var metadata struct {
		Version string          `json:"version"`
		TOC     json.RawMessage `json:"toc"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&metadata); err != nil {
		return fmt.Errorf("publication metadata invalid: %w", err)
	}
	if metadata.Version == target {
		return fmt.Errorf("documentation %s is published; replace the carried receipt", target)
	}
	if metadata.Version != previous || len(metadata.TOC) == 0 {
		return fmt.Errorf("publication metadata does not prove %s is unpublished", target)
	}
	return nil
}

// LWC is a current-only guide rather than a numbered Atlas family. Only an
// explicit documentation release/API marker can prove that its carry remains
// current; an unrecognized page is a failed freshness check, never a silent
// carry. Fetch the same public configuration guide used by the LWC source.
func checkLWCPublication(ctx context.Context, client docsClient, lwc LWCSourceReceipt, target string) error {
	url := "https://developer.salesforce.com/docs/platform/lwc/guide/reference-configuration-tags.html"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("publication unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("publication unavailable: HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return err
	}
	// Markers deliberately require an API/release label, not an arbitrary number
	// in example code, URLs, a copyright year, or an availability row.
	markers := regexp.MustCompile(`(?i)(?:(?:current|latest|maximum)\s+(?:supported\s+)?API\s+(?:v|version\s*)?)([0-9]+)\.0`).FindAllStringSubmatch(string(data), -1)
	targetAPI := (apiInt(target) - 128) / 2
	previousAPI := (apiInt(lwc.CarriedFrom) - 128) / 2
	sawPrevious := false
	for _, match := range markers {
		version, _ := strconv.Atoi(match[1])
		if version >= targetAPI {
			return fmt.Errorf("documentation API %d.0 is published; replace the carried receipt", targetAPI)
		}
		sawPrevious = sawPrevious || version == previousAPI
	}
	// Winter '27 is a documentation release label, not a component source pin.
	if target == "264.0" && regexp.MustCompile(`(?i)Winter\s*(?:'|&#39;|&apos;)?\s*27`).Match(data) {
		return fmt.Errorf("documentation %s is published; replace the carried receipt", target)
	}
	if !sawPrevious || strings.TrimSpace(string(data)) == "" {
		return fmt.Errorf("current-only LWC guide does not prove %s documentation is unpublished", target)
	}
	return nil
}
