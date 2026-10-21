package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

// These are policy exclusions, not inferred declarations from unavailable
// pages. Known declarations and unavailable public TOC references are separate.
type excludedReference struct {
	Page        string `json:"page"`
	SourceURL   string `json:"sourceURL"`
	Namespace   string `json:"namespace"`
	Acquisition string `json:"acquisition"`
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
}

type httpResponse struct {
	URL    string `json:"url"`
	Status int    `json:"status"`
}

type policyAcquisition struct {
	SchemaVersion int                 `json:"schemaVersion"`
	AtlasVersion  string              `json:"atlasVersion"`
	Policy        string              `json:"policy"`
	References    []excludedReference `json:"references"`
	TOCSHA256     string              `json:"tocSHA256"`
	HTTPEvidence  struct {
		StatusCounts map[string]int `json:"statusCounts"`
		Responses    []httpResponse `json:"responses"`
	} `json:"httpEvidence"`
}

func connectAPIClassifications(inv apexdocs.Inventory, atlas string) []surfaceledger.ReleaseClassification {
	seen := map[string]bool{}
	result := []surfaceledger.ReleaseClassification{}
	for _, row := range surfaceledger.ReleaseRowsFromDocsInventory(inv) {
		if row.Namespace != "ConnectApi" || row.Product != "apex" {
			continue
		}
		key := surfaceledger.CanonicalSurfaceIDKey(row.SurfaceID)
		if seen[key] {
			continue
		}
		seen[key] = true
		page := strings.TrimPrefix(row.DocsSource, "apex/")
		page = strings.TrimSuffix(page, ".md") + ".htm"
		source := "https://developer.salesforce.com/docs/get_document_content/apexref/" + page + "/en-us/" + atlas
		result = append(result, surfaceledger.ReleaseClassification{
			SurfaceID: row.SurfaceID, Scope: "outside-claim", Disposition: "explicit-unsupported",
			ReasonRef: "ConnectApi is excluded and unsupported by product scope policy. Known public declaration: " + row.Signature + " (" + source + "). No implementation or parity is claimed.",
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SurfaceID < result[j].SurfaceID })
	return result
}

func exportNamespacePolicy(inventoryPath, acquisitionPath, output string, prior ...string) error {
	inv, err := apexdocs.ReadInventory(inventoryPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(acquisitionPath)
	if err != nil {
		return err
	}
	var acquisition policyAcquisition
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&acquisition); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return fmt.Errorf("policy receipt must contain one JSON object")
	}
	if acquisition.SchemaVersion != 1 || acquisition.Policy != "ConnectApi is excluded and unsupported" || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(acquisition.TOCSHA256) || !regexp.MustCompile(`^[1-9][0-9]*\.0$`).MatchString(acquisition.AtlasVersion) {
		return fmt.Errorf("invalid ConnectApi acquisition policy identity")
	}
	prefix := "https://developer.salesforce.com/docs/get_document_content/apexref/"
	suffix := "/en-us/" + acquisition.AtlasVersion
	seen := map[string]bool{}
	for _, ref := range acquisition.References {
		page := strings.ToLower(ref.Page)
		if (!strings.HasPrefix(page, "apex_connectapi_") && !strings.HasPrefix(page, "connectapi_")) || !strings.HasSuffix(page, ".htm") || strings.ContainsAny(page, "/\\") || ref.Namespace != "ConnectApi" || ref.Acquisition != "policy-excluded" || ref.Disposition != "explicitly-unsupported" || ref.Reason != "excluded and unsupported; documentation not acquired" || ref.SourceURL != prefix+ref.Page+suffix || seen[ref.Page] {
			return fmt.Errorf("invalid ConnectApi policy reference %q", ref.Page)
		}
		seen[ref.Page] = true
	}
	if len(acquisition.HTTPEvidence.StatusCounts) != 1 || acquisition.HTTPEvidence.StatusCounts["403"] != len(acquisition.HTTPEvidence.Responses) {
		return fmt.Errorf("invalid retained HTTP evidence counts")
	}
	for _, response := range acquisition.HTTPEvidence.Responses {
		if response.Status != 403 || !strings.HasPrefix(strings.ToLower(response.URL), prefix+"apex_connectapi_") || !strings.HasSuffix(response.URL, suffix) {
			return fmt.Errorf("retained HTTP evidence is not an excluded ConnectApi 403")
		}
	}
	historical := []surfaceledger.ReleaseClassification{}
	historicalHash := ""
	if len(prior) == 2 && prior[0] != "" {
		if !regexp.MustCompile(`^[1-9][0-9]*\.0$`).MatchString(prior[1]) {
			return fmt.Errorf("historical namespace classifications require --previous-api")
		}
		api, _ := strconv.Atoi(strings.TrimSuffix(prior[1], ".0"))
		atlas := fmt.Sprintf("%d.0", 2*api+128)
		previous, err := apexdocs.ReadInventory(prior[0])
		if err != nil {
			return err
		}
		historicalHash = fileSHA256(prior[0])
		for _, row := range connectAPIClassifications(previous, atlas) {
			row.ReasonRef = "Historical source evidence only; current documentation was not inferred from it. " + row.ReasonRef
			historical = append(historical, row)
		}
	}
	write(output, struct {
		SchemaVersion             int                                   `json:"schemaVersion"`
		Namespace                 string                                `json:"namespace"`
		InventorySHA256           string                                `json:"inventorySHA256"`
		ReceiptSHA256             string                                `json:"acquisitionReceiptSHA256"`
		Acquisition               policyAcquisition                     `json:"acquisition"`
		Classifications           []surfaceledger.ReleaseClassification `json:"classifications"`
		HistoricalInventorySHA256 string                                `json:"historicalInventorySHA256,omitempty"`
		HistoricalClassifications []surfaceledger.ReleaseClassification `json:"historicalClassifications,omitempty"`
	}{1, "ConnectApi", fileSHA256(inventoryPath), fmt.Sprintf("%x", sha256.Sum256(data)), acquisition, connectAPIClassifications(inv, acquisition.AtlasVersion), historicalHash, historical})
	return nil
}

func fileSHA256(path string) string {
	b, err := os.ReadFile(path)
	must(err)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
