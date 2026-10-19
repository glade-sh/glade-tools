package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/glade-sh/glade/tools/internal/apexdocs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func write(path string, value interface{}) {
	b, e := json.MarshalIndent(value, "", "  ")
	must(e)
	must(os.WriteFile(path, append(b, '\n'), 0644))
}
func main() {
	atlas := flag.String("atlas", "", "Local explicit Atlas264 scrape directory")
	previous := flag.String("previous-inventory", "", "Checked previousGA inventory")
	out := flag.String("output", "", "Output fixture directory")
	notes := flag.String("notes", "", "Local release-notes264 directory")
	flag.Parse()
	if *atlas == "" || *previous == "" || *out == "" || *notes == "" {
		panic("all input/output flags are required")
	}
	inv, e := apexdocs.BuildInventory(*atlas)
	must(e)
	prior, e := apexdocs.ReadInventory(*previous)
	must(e)
	inv, exclusions := composeInventory(inv, prior)
	f, e := os.Create(filepath.Join(*out, "salesforce-docs-inventory-winter-27.json"))
	must(e)
	must(apexdocs.WriteJSON(f, inv))
	must(f.Close())
	manifest := apexdocs.NewReleaseManifest("Winter '27", "68.0", apexdocs.CanonicalDigest(inv), "Owned Atlas264.0 scrape with explicit previousGA262.0 documentation carries; normalized checked Summer26 inventory supplies frozen carried documents.", []string{"apex-reference", "aura-reference", "lwc-reference", "rest-api-reference", "tooling-api-reference", "visualforce-reference"})
	write(filepath.Join(*out, "salesforce-release-winter-27.json"), manifest)
	noteInv, e := apexdocs.BuildInventory(*notes)
	must(e)
	f, e = os.Create(filepath.Join(*out, "salesforce-release-notes-inventory-winter-27.json"))
	must(e)
	must(apexdocs.WriteJSON(f, noteInv))
	must(f.Close())
	write(filepath.Join(*out, "salesforce-release-inventory-filter-winter-27.json"), map[string]interface{}{"schemaVersion": 1, "targetAPIVersion": "68.0", "excludedFutureDeclarations": exclusions, "inventoryDigest": manifest.Digest, "releaseNotesDigest": apexdocs.CanonicalDigest(noteInv)})
	fmt.Println(manifest.Digest)
}

func summarize(docs []apexdocs.Document) ([]apexdocs.NamespaceSummary, int) {
	summaries := map[string]*apexdocs.NamespaceSummary{}
	totalMembers := 0
	for _, doc := range docs {
		ns := doc.Namespace
		if ns == "" {
			ns = "(none)"
		}
		summary := summaries[ns]
		if summary == nil {
			summary = &apexdocs.NamespaceSummary{Namespace: ns}
			summaries[ns] = summary
		}
		summary.Documents++
		summary.Members += len(doc.Members)
		totalMembers += len(doc.Members)
		switch doc.Kind {
		case "namespace":
			summary.Namespaces++
		case "class":
			summary.Classes++
		case "interface":
			summary.Interfaces++
		case "enum":
			summary.Enums++
		case "input":
			summary.Inputs++
		case "output":
			summary.Outputs++
		}
	}
	out := make([]apexdocs.NamespaceSummary, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, *summary)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Namespace < out[j].Namespace
	})
	return out, totalMembers
}

func composeInventory(inv, prior apexdocs.Inventory) (apexdocs.Inventory, []string) {
	for _, d := range prior.Documents {
		family := strings.Split(d.SourcePath, "/")[0]
		if family == "lightning-aura" || family == "rest-api" || family == "visualforce" || family == "lwc" {
			inv.Documents = append(inv.Documents, d)
		}
	}
	filtered := inv.Documents[:0]
	var exclusions []string
	for _, d := range inv.Documents {
		v, _ := strconv.ParseFloat(d.APIVersion, 64)
		if v > 68 {
			exclusions = append(exclusions, d.SourcePath+": API"+d.APIVersion)
			continue
		}
		ms := d.Members[:0]
		for _, m := range d.Members {
			v, _ := strconv.ParseFloat(m.APIVersion, 64)
			if v <= 68 {
				ms = append(ms, m)
			} else {
				exclusions = append(exclusions, d.SourcePath+":"+m.Name+": API"+m.APIVersion)
			}
		}
		d.Members = ms
		filtered = append(filtered, d)
	}
	inv.Documents = filtered
	sort.Slice(inv.Documents, func(i, j int) bool { return inv.Documents[i].SourcePath < inv.Documents[j].SourcePath })
	inv.TotalFiles = len(inv.Documents)
	inv.Namespaces, inv.TotalMembers = summarize(inv.Documents)
	return inv, exclusions
}
