package corpusassurance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteAssuranceHTML writes an offline explorer that embeds the sealed report
// JSON. It deliberately has no network dependencies and is create-only.
func WriteAssuranceHTML(reportPath, outputPath string) error {
	if !filepath.IsAbs(reportPath) || !filepath.IsAbs(outputPath) {
		return fmt.Errorf("absolute report and HTML paths are required")
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return fmt.Errorf("assurance HTML output already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	report, err := os.ReadFile(reportPath)
	if err != nil || !json.Valid(report) {
		return fmt.Errorf("read valid assurance JSON: %w", err)
	}
	if bytes.Contains(bytes.ToLower(report), []byte("</script")) || containsPrivateReportPath(report) {
		return fmt.Errorf("assurance JSON is not safe for public HTML")
	}
	page := "<!doctype html><meta charset=\"utf-8\"><title>Glade assurance</title>" +
		"<style>body{font:14px system-ui;margin:2rem}input{padding:.5rem;width:30rem}table{border-collapse:collapse;width:100%;margin-top:1rem}td,th{border:1px solid #ddd;padding:.4rem;text-align:left}</style>" +
		"<h1>Glade assurance</h1><input id=\"filter\" placeholder=\"Filter repository, namespace, or surface\"><table><thead><tr><th>Surface</th><th>Repositories</th><th>Disposition</th><th>Outcome</th></tr></thead><tbody id=\"rows\"></tbody></table>" +
		"<script id=\"assurance-data\" type=\"application/json\">" + string(report) + "</script>" +
		"<script>const d=JSON.parse(document.getElementById('assurance-data').textContent),b=document.getElementById('rows'),f=document.getElementById('filter');function e(x){return String(x??'').replace(/[&<>\"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','\"':'&quot;'}[c]))}function draw(){let q=f.value.toLowerCase();b.innerHTML=(d.rows||[]).filter(r=>JSON.stringify(r).toLowerCase().includes(q)).map(r=>'<tr><td>'+e(r.surfaceId)+'</td><td>'+e((r.repositoryIds||[]).join(', '))+'</td><td>'+e(r.disposition)+'</td><td>'+e(r.runtimeParityReady?'runtime-parity-ready':r.testReady?'test-ready':r.compileReady?'compile-ready':r.nonParity?'non-parity':'unknown')+'</td></tr>').join('')}f.oninput=draw;draw()</script>"
	return WriteNewJSONText(outputPath, page)
}

func containsPrivateReportPath(data []byte) bool {
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "/users/") || strings.Contains(lower, "/private/") || strings.Contains(lower, "src-nmb") || strings.Contains(lower, "nams") || strings.Contains(lower, "sf-cred")
}

func WriteNewJSONText(path, value string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(value)
	return err
}
