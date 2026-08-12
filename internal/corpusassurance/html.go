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
	return writeAssuranceHTMLBytes(report, outputPath)
}

func writeAssuranceHTMLBytes(report []byte, outputPath string) error {
	if !filepath.IsAbs(outputPath) {
		return fmt.Errorf("absolute HTML path is required")
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return fmt.Errorf("assurance HTML output already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	page, err := renderAssuranceHTML(report)
	if err != nil {
		return err
	}
	return WriteNewJSONText(outputPath, string(page))
}

func renderAssuranceHTML(report []byte) ([]byte, error) {
	if !json.Valid(report) {
		return nil, fmt.Errorf("assurance JSON is invalid")
	}
	if bytes.Contains(bytes.ToLower(report), []byte("</script")) || containsPrivateReportPath(report) {
		return nil, fmt.Errorf("assurance JSON is not safe for public HTML")
	}
	page := "<!doctype html><meta charset=\"utf-8\"><title>Glade assurance</title>" +
		"<style>body{font:14px system-ui;margin:2rem}input,select{padding:.5rem;margin:.2rem;min-width:12rem}table{border-collapse:collapse;width:100%;margin-top:1rem}td,th{border:1px solid #ddd;padding:.4rem;text-align:left}</style>" +
		"<h1>Glade assurance</h1><p>Filter the sealed surface rows by namespace, disposition, repository, evidence, exclusion, or text.</p><div><select id=\"namespace\"><option value=\"\">All namespaces</option></select><select id=\"disposition\"><option value=\"\">All dispositions</option></select><select id=\"repository\"><option value=\"\">All repositories</option></select><select id=\"evidence\"><option value=\"\">All evidence</option></select><select id=\"exclusion\"><option value=\"\">All exclusions</option></select><input id=\"text\" placeholder=\"Search surface or usage\"></div><table><thead><tr><th>Surface</th><th>Repositories</th><th>Disposition</th><th>Local</th><th>Salesforce</th><th>Readiness</th><th>Non-parity</th></tr></thead><tbody id=\"rows\"></tbody></table><h2>Repository × surface</h2><table><thead><tr><th>Repository</th><th>Surface</th><th>Compile</th><th>Test</th><th>Runtime parity</th><th>Readiness</th><th>Non-parity</th></tr></thead><tbody id=\"repository-rows\"></tbody></table><h2>Repository summaries</h2><table><thead><tr><th>Repository</th><th>Surfaces</th><th>Compile</th><th>Test</th><th>Runtime parity</th><th>Non-parity</th></tr></thead><tbody id=\"repository-summaries\"></tbody></table>" +
		"<script id=\"assurance-data\" type=\"application/json\">" + string(report) + "</script>" +
		"<script>const d=JSON.parse(document.getElementById('assurance-data').textContent),r=d.rows||[],p=d.repositorySurfaceRows||[],s=d.repositorySummaries||[],b=document.getElementById('rows'),bp=document.getElementById('repository-rows'),bs=document.getElementById('repository-summaries'),ids=['namespace','disposition','repository','evidence','exclusion'],c=Object.fromEntries(ids.map(x=>[x,document.getElementById(x)])),t=document.getElementById('text');function e(x){return String(x??'').replace(/[&<>\"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','\"':'&quot;'}[c]))}function vals(f){return [...new Set(r.flatMap(f).filter(Boolean))].sort()}function opts(id,v){c[id].innerHTML+=[...v].map(x=>'<option>'+e(x)+'</option>').join('')}opts('namespace',vals(x=>[x.namespace]));opts('disposition',vals(x=>[x.disposition]));opts('repository',vals(x=>x.repositoryIds||[]));opts('evidence',vals(x=>[x.localEvidence,x.salesforceEvidence]));opts('exclusion',vals(x=>[x.exclusionClass]));function readiness(x){return x.runtimeParityReady?'runtime-parity-ready':x.testReady?'test-ready':x.compileReady?'compile-ready':'not-ready'}function nonparity(x){return x.nonParity?(x.exclusionReason||x.nonParityReason||'non-parity'):''}function draw(){let q=t.value.toLowerCase(),match=x=>(!c.namespace.value||x.namespace===c.namespace.value)&&(!c.disposition.value||x.disposition===c.disposition.value)&&(!c.repository.value||(x.repositoryIds||[x.repositoryId]).includes(c.repository.value))&&(!c.evidence.value||x.localEvidence===c.evidence.value||x.salesforceEvidence===c.evidence.value)&&(!c.exclusion.value||x.exclusionClass===c.exclusion.value)&&JSON.stringify(x).toLowerCase().includes(q);b.innerHTML=r.filter(match).map(x=>'<tr><td>'+e(x.surfaceId)+'</td><td>'+e((x.repositoryIds||[]).join(', '))+'</td><td>'+e(x.disposition)+'</td><td>'+e(x.localEvidence)+'</td><td>'+e(x.salesforceEvidence)+'</td><td>'+e(readiness(x))+'</td><td>'+e(nonparity(x))+'</td></tr>').join('');bp.innerHTML=p.filter(match).map(x=>'<tr><td>'+e(x.repositoryId)+'</td><td>'+e(x.surfaceId)+'</td><td>'+e(x.compileReady)+'</td><td>'+e(x.testReady)+'</td><td>'+e(x.runtimeParityReady)+'</td><td>'+e(readiness(x))+'</td><td>'+e(nonparity(x))+'</td></tr>').join('');bs.innerHTML=s.filter(x=>!c.repository.value||x.repositoryId===c.repository.value).map(x=>'<tr><td>'+e(x.repositoryId)+'</td><td>'+e(x.surfaceCount)+'</td><td>'+e(x.compileReady)+'</td><td>'+e(x.testReady)+'</td><td>'+e(x.runtimeParityReady)+'</td><td>'+e(x.nonParity?x.nonParityReason:'')+'</td></tr>').join('')}Object.values(c).forEach(x=>x.oninput=draw);t.oninput=draw;draw()</script>"
	return []byte(page), nil
}

func containsPrivateReportPath(data []byte) bool {
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "/users/") || strings.Contains(lower, "/private/") || strings.Contains(lower, ".sf-repo-analysis")
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
