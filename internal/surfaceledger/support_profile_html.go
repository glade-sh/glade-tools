package surfaceledger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"sort"
	"strings"
)

// SupportProfileHTMLPage is the single embedded data object used by the
// Apex surface status page. Its rows are enriched profile rows, not a second
// inventory: each row is keyed by the profile's canonical SurfaceID.
type SupportProfileHTMLPage struct {
	Total                  int                        `json:"total"`
	ByDisposition          map[SupportDisposition]int `json:"byDisposition"`
	ByObligation           map[string]int             `json:"byObligation"`
	ByGapClass             map[string]int             `json:"byGapClass"`
	InventoryFailureCounts map[string]int             `json:"inventoryFailureCounts"`
	InventoryFailureCount  int                        `json:"inventoryFailureCount"`
	ByGapCategory          map[string]int             `json:"byGapCategory"`
	ByGapState             map[string]int             `json:"byGapState"`
	ByDeliveryState        map[string]int             `json:"byDeliveryState"`
	ByBehavior             map[string]int             `json:"byBehavior"`
	ByEvidence             map[string]int             `json:"byEvidence"`
	ByNamespace            map[string]int             `json:"byNamespace"`
	ByFamily               map[string]int             `json:"byFamily"`
	ByNextAction           map[string]int             `json:"byNextAction"`
	ByCorpusUse            map[string]int             `json:"byCorpusUse"`
	Gate                   SupportProfileHTMLGate     `json:"gate"`
	Inputs                 *SupportProfileInputs      `json:"inputs,omitempty"`
	ValidationErrors       []string                   `json:"validationErrors,omitempty"`
	Rows                   []SupportProfileHTMLRow    `json:"rows"`
}

// SupportProfileHTMLGate is a machine-derived completion claim. It is kept
// in the embedded payload so the visible banner and structural verifier use
// the same value.
type SupportProfileHTMLGate struct {
	Status                string   `json:"status"`
	Passed                bool     `json:"passed"`
	ValidationErrorCount  int      `json:"validationErrorCount"`
	BlockingRowCount      int      `json:"blockingRowCount"`
	InventoryFailureCount int      `json:"inventoryFailureCount"`
	BlockingReasons       []string `json:"blockingReasons,omitempty"`
}

// SupportProfileHTMLRow contains the profile facts and the ledger/corpus
// details needed to explain one Apex surface without embedding a second row
// collection in the page.
type SupportProfileHTMLRow struct {
	SurfaceID string `json:"surfaceId"`

	Product                string        `json:"product"`
	Area                   string        `json:"area"`
	Namespace              string        `json:"namespace,omitempty"`
	Family                 string        `json:"family,omitempty"`
	TypeName               string        `json:"typeName,omitempty"`
	MemberName             string        `json:"memberName,omitempty"`
	Resource               string        `json:"resource,omitempty"`
	FieldName              string        `json:"fieldName,omitempty"`
	Kind                   string        `json:"kind"`
	Signature              string        `json:"signature,omitempty"`
	ReturnType             string        `json:"returnType,omitempty"`
	Parameters             []string      `json:"parameters,omitempty"`
	DocsReturnType         string        `json:"docsReturnType,omitempty"`
	OrgReturnType          string        `json:"orgReturnType,omitempty"`
	GladeReturnType        string        `json:"gladeReturnType,omitempty"`
	DocsParameters         []string      `json:"docsParameters,omitempty"`
	OrgParameters          []string      `json:"orgParameters,omitempty"`
	GladeParameters        []string      `json:"gladeParameters,omitempty"`
	Docs                   SourceState   `json:"docs"`
	Org                    SourceState   `json:"org"`
	LedgerShape            ShapeState    `json:"ledgerShape"`
	Behavior               BehaviorState `json:"behavior"`
	Evidence               EvidenceState `json:"evidence"`
	Sources                []string      `json:"sources,omitempty"`
	DocsSource             string        `json:"docsSource,omitempty"`
	DocsTitle              string        `json:"docsTitle,omitempty"`
	APIVersion             string        `json:"apiVersion,omitempty"`
	ShapeSource            string        `json:"shapeSource,omitempty"`
	BehaviorSource         string        `json:"behaviorSource,omitempty"`
	ImplementationDecision string        `json:"implementationDecision,omitempty"`
	ImplementationTarget   string        `json:"implementationTarget,omitempty"`
	Owner                  string        `json:"owner,omitempty"`
	Bucket                 string        `json:"bucket,omitempty"`
	Priority               int           `json:"priority,omitempty"`
	Notes                  string        `json:"notes,omitempty"`

	UsageKey              string            `json:"usageKey,omitempty"`
	CorpusPassingRefs     int               `json:"corpusPassingRefs,omitempty"`
	CorpusFailureRefs     int               `json:"corpusFailureRefs,omitempty"`
	CorpusPassingProjects int               `json:"corpusPassingProjects,omitempty"`
	Corpus                *CorpusUsageEntry `json:"corpus,omitempty"`

	Disposition    SupportDisposition `json:"disposition"`
	MatchRule      string             `json:"matchRule"`
	Reason         string             `json:"reason"`
	Obligation     string             `json:"obligation"`
	GapClass       string             `json:"gapClass,omitempty"`
	LedgerGapClass string             `json:"ledgerGapClass,omitempty"`
	Open           bool               `json:"open"`
	Blocking       bool               `json:"blocking"`
	GapState       string             `json:"gapState"`
	DeliveryStates []string           `json:"deliveryStates"`
	NextActionKey  string             `json:"nextActionKey"`
	NextAction     string             `json:"nextAction"`
	CorpusUsed     bool               `json:"corpusUsed"`
}

const (
	htmlGatePass    = "PASS"
	htmlGateBlocked = "BLOCKED"
)

const (
	htmlGapStateBlocked  = "blocked"
	htmlGapStateDeferred = "deferred"
	htmlGapStateClosed   = "closed"
)

const (
	htmlCorpusUseUsed = "used"
	htmlCorpusUseZero = "zero-use"
)

const (
	htmlGapCategoryMissingShape     = "missing-shape"
	htmlGapCategoryMissingBehavior  = "missing-behavior"
	htmlGapCategoryMissingEvidence  = "missing-evidence"
	htmlGapCategoryMismatch         = "mismatch"
	htmlGapCategoryStale            = "stale/inconclusive"
	htmlGapCategoryUnclassified     = "unclassified"
	htmlGapCategoryInventoryFailure = "inventory-failure"
)

const (
	htmlDeliveryLocalRuntime          = "local-runtime"
	htmlDeliveryDeterministicMock     = "deterministic-mock"
	htmlDeliveryCompileShape          = "compile-shape"
	htmlDeliveryHostedDeferred        = "hosted-deferred"
	htmlDeliveryNotLocallyImplemented = "not-locally-implemented"
	htmlDeliveryCovered               = "covered"
	htmlDeliveryExplicitUnsupported   = "explicit-unsupported"
	htmlDeliveryOpen                  = "unimplemented/open"
)

const (
	htmlActionMissingShape     = "missing-shape"
	htmlActionMissingBehavior  = "missing-behavior"
	htmlActionMissingEvidence  = "missing-evidence"
	htmlActionHostedDeferred   = "hosted-deferred"
	htmlActionInventoryFailure = "inventory-failure"
	htmlActionOpen             = "open"
	htmlActionClosed           = "closed"
)

const (
	htmlNextMissingShape     = "implement/correct shape"
	htmlNextMissingBehavior  = "implement local behavior or the declared mock"
	htmlNextMissingEvidence  = "add the exact evidence required by the disposition"
	htmlNextHostedDeferred   = "monitor release/corpus and retain the stated reason"
	htmlNextInventoryFailure = "reconcile docs/org/Glade shape or parser failure"
	htmlNextOpen             = "classify/resolve open row"
	htmlNextClosed           = "no current-base action"
)

// WriteSupportProfileHTML writes a self-contained status page from one
// computed profile and its source ledger. The profile remains authoritative
// for disposition, reason, gap, and aggregate corpus classification.
func WriteSupportProfileHTML(w io.Writer, profile SupportProfile, ledger SurfaceLedger) error {
	payload, err := buildSupportProfileHTMLPage(profile, ledger)
	if err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal Apex surface status data: %w", err)
	}
	var escaped bytes.Buffer
	json.HTMLEscape(&escaped, data)

	var b strings.Builder
	writeSupportProfileHTMLHead(&b)
	fmt.Fprintf(&b, `<body>
<main>
  <header class="hero">
    <p class="eyebrow">Glade · Salesforce compatibility</p>
    <h1>Apex surface status</h1>
    <p class="lede">One exhaustive view of the current Apex support profile, its claim boundaries, and the evidence behind every row.</p>
  </header>
  <section id="completion-gate" class="completion-gate gate-%s" data-gate-status="%s" aria-live="polite">
    <div><span class="gate-label">Current-base gate</span><strong>%s</strong></div>
    <p>%s</p>
  </section>
  <section class="summary" aria-label="Profile totals">
    <div class="metric metric-total"><span>Profile total</span><strong id="profile-total">%d</strong></div>
    <div class="metric"><span>Currently shown</span><strong id="shown-count">%d</strong></div>
    <div class="metric"><span>Local-runtime obligation</span><strong>%d</strong></div>
    <div class="metric"><span>Deterministic-mock obligation</span><strong>%d</strong></div>
    <div class="metric"><span>Compile-shape obligation</span><strong>%d</strong></div>
    <div class="metric"><span>Hosted-deferred obligation</span><strong>%d</strong></div>
    <div class="metric metric-inventory-failure"><span>Inventory failures</span><strong>%d</strong></div>
  </section>
  <section class="panel" aria-labelledby="delivery-heading">
    <div class="section-heading"><h2 id="delivery-heading">Delivery totals</h2><span class="muted">Derived from every row; states may overlap</span></div>
    <div class="summary delivery-summary">
      <div class="metric"><span>Covered</span><strong>%d</strong></div>
      <div class="metric"><span>Unimplemented / open</span><strong>%d</strong></div>
      <div class="metric"><span>Locally executable</span><strong>%d</strong></div>
      <div class="metric"><span>Deterministic mock implemented</span><strong>%d</strong></div>
      <div class="metric"><span>Compile-only shape</span><strong>%d</strong></div>
      <div class="metric"><span>Explicitly unsupported</span><strong>%d</strong></div>
      <div class="metric"><span>Not locally implemented</span><strong>%d</strong></div>
      <div class="metric"><span>Hosted deferred</span><strong>%d</strong></div>
    </div>
  </section>
  <section class="panel" aria-labelledby="gaps-heading">
    <div class="section-heading"><h2 id="gaps-heading">Gap totals</h2><span class="muted">Profile-owned counts</span></div>
    <div id="gap-summary" class="gap-summary">%s</div>
  </section>
  <section class="panel facet-summary" aria-labelledby="facet-heading">
    <div class="section-heading"><h2 id="facet-heading">Behavior and evidence totals</h2><span class="muted">Recomputed from embedded rows</span></div>
    <div class="facet-columns"><div><h3>Behavior</h3><div class="gap-summary">%s</div></div><div><h3>Evidence</h3><div class="gap-summary">%s</div></div><div><h3>Corpus use</h3><div class="gap-summary">%s</div></div></div>
  </section>
  <section class="panel" aria-labelledby="legend-heading">
    <div class="section-heading"><h2 id="legend-heading">Claim boundary</h2></div>
    <div class="legend">
      <p>Hosted-deferred is inventoried but not locally implemented.</p>
      <p>Deterministic mock is executable local behavior but not the hosted service.</p>
      <p>Compile shape is not a runtime claim.</p>
      <p>Open rows block completeness.</p>
      <p>Inventory failures: reconcile docs/org/Glade shape or parser failure.</p>
    </div>
  </section>
  <section id="validation-panel" class="panel validation-panel" aria-labelledby="validation-heading" hidden>
    <div class="section-heading"><h2 id="validation-heading">Profile validation errors</h2><span class="muted">These errors block completeness claims</span></div>
    <ul id="validation-errors"></ul>
  </section>
  <section class="panel" aria-labelledby="inputs-heading">
    <div class="section-heading"><h2 id="inputs-heading">Pinned inputs</h2><span class="muted">Names, paths, and SHA-256 identifiers</span></div>
    <div id="input-list" class="input-list"></div>
  </section>
  <section class="panel" aria-labelledby="rows-heading">
    <div class="section-heading"><h2 id="rows-heading">Apex surfaces</h2><span id="page-status" class="muted"></span></div>
    <div class="filters" role="search">
      <label>Search<input id="search" type="search" placeholder="SurfaceID, namespace, type/member, signature"></label>
      <label>Obligation / disposition<select id="obligation-filter"><option value="">All obligations</option><option value="local-runtime-required">Local runtime</option><option value="deterministic-mock-required">Deterministic mock</option><option value="compile-shape-required">Compile shape</option><option value="hosted-deferred">Hosted deferred</option></select></label>
      <label>Delivery state<select id="delivery-filter"><option value="">All delivery states</option><option value="local-runtime">Local runtime</option><option value="deterministic-mock">Deterministic mock</option><option value="compile-shape">Compile shape</option><option value="hosted-deferred">Hosted deferred</option><option value="not-locally-implemented">Not locally implemented</option><option value="covered">Covered</option><option value="explicit-unsupported">Explicit unsupported</option><option value="unimplemented/open">Unimplemented/open</option></select></label>
      <label>Behavior / implementation<select id="behavior-filter"><option value="">All behavior states</option><option value="supported">Supported</option><option value="partial">Partial</option><option value="passive">Passive</option><option value="stub-noop">Stub/no-op</option><option value="unsupported">Unsupported</option><option value="none">Absent</option></select></label>
      <label>Evidence state<select id="evidence-filter"><option value="">All evidence states</option><option value="fixture-and-oracle">Fixture and Salesforce oracle</option><option value="fixture">Fixture</option><option value="oracle">Salesforce oracle</option><option value="corpus">Corpus</option><option value="docs">Docs</option><option value="none">None</option></select></label>
      <label>Gap / blocking state<select id="gap-filter"><option value="">All gap/blocking states</option><option value="blocked">Open / blocking</option><option value="deferred">Hosted deferred / not locally implemented</option><option value="closed">Closed / non-blocking</option></select></label>
      <label>Namespace<select id="namespace-filter"><option value="">All namespaces</option></select></label>
      <label>Family<select id="family-filter"><option value="">All families</option></select></label>
      <label>Corpus usage<select id="corpus-filter"><option value="">Used or zero-use</option><option value="used">Corpus used</option><option value="zero-use">Corpus zero-use</option></select></label>
      <label>Next action<select id="action-filter"><option value="">All next actions</option><option value="missing-shape">Implement/correct shape</option><option value="missing-behavior">Implement local behavior or declared mock</option><option value="missing-evidence">Add exact evidence</option><option value="inventory-failure">Reconcile docs/org/Glade/parser failure</option><option value="hosted-deferred">Monitor release/corpus</option><option value="open">Classify/resolve open row</option><option value="closed">No current-base action</option></select></label>
    </div>
    <div class="result-bar"><span><strong id="shown-count-inline">%d</strong> rows shown</span><span class="muted">50 rows per page</span></div>
    <div id="row-list" class="row-list"></div>
    <nav class="pagination" aria-label="Surface pages"><button id="previous-page" type="button">Previous</button><span id="page-size" class="muted">Page 1</span><button id="next-page" type="button">Next</button></nav>
  </section>
</main>
<script id="page-data" type="application/json">`, strings.ToLower(payload.Gate.Status), strings.ToLower(payload.Gate.Status), payload.Gate.Status,
		renderSupportProfileHTMLGateMessage(payload.Gate),
		payload.Total, payload.Total,
		payload.ByDisposition[DispositionLocalRuntimeRequired],
		payload.ByDisposition[DispositionDeterministicMockRequired],
		payload.ByDisposition[DispositionCompileShapeRequired],
		payload.ByDisposition[DispositionHostedDeferred],
		payload.InventoryFailureCount,
		payload.ByDeliveryState[htmlDeliveryCovered],
		payload.ByDeliveryState[htmlDeliveryOpen],
		payload.ByDeliveryState[htmlDeliveryLocalRuntime],
		payload.ByDeliveryState[htmlDeliveryDeterministicMock],
		payload.ByDeliveryState[htmlDeliveryCompileShape],
		payload.ByDeliveryState[htmlDeliveryExplicitUnsupported],
		payload.ByDeliveryState[htmlDeliveryNotLocallyImplemented],
		payload.ByDeliveryState[htmlDeliveryHostedDeferred],
		renderSupportProfileHTMLGapSummary(payload.ByGapCategory),
		renderSupportProfileHTMLCountSummary(payload.ByBehavior),
		renderSupportProfileHTMLCountSummary(payload.ByEvidence),
		renderSupportProfileHTMLCountSummary(payload.ByCorpusUse), payload.Total)
	escaped.WriteTo(&b)
	fmt.Fprint(&b, `</script>
<script>
(() => {
  "use strict";
  const page = JSON.parse(document.getElementById("page-data").textContent);
  const rows = Array.isArray(page.rows) ? page.rows : [];
  const pageSize = 50;
  let pageIndex = 0;
  let filteredRows = rows;
  const byID = (id) => document.getElementById(id);
  const text = (value) => value === undefined || value === null || value === "" ? "—" : String(value);
  const lower = (value) => text(value).toLowerCase();
  const search = byID("search");
  const obligation = byID("obligation-filter");
  const delivery = byID("delivery-filter");
  const behavior = byID("behavior-filter");
  const evidence = byID("evidence-filter");
  const gap = byID("gap-filter");
  const namespace = byID("namespace-filter");
  const family = byID("family-filter");
  const corpus = byID("corpus-filter");
  const action = byID("action-filter");
  const rowList = byID("row-list");
  const deliveryLabels = {
    "local-runtime": "Locally executable",
    "deterministic-mock": "Deterministic mock",
    "compile-shape": "Compile-only shape",
    "hosted-deferred": "Hosted deferred",
    "not-locally-implemented": "Not locally implemented",
    "covered": "Covered",
    "explicit-unsupported": "Explicitly unsupported",
    "unimplemented/open": "Open / blocking"
  };

  function addPair(parent, label, value) {
    const item = document.createElement("div");
    item.className = "pair";
    const key = document.createElement("dt");
    key.textContent = label;
    const val = document.createElement("dd");
    val.textContent = text(value);
    item.append(key, val);
    parent.appendChild(item);
  }

  function addListPair(parent, label, values) {
    const item = document.createElement("div");
    item.className = "pair";
    const key = document.createElement("dt");
    key.textContent = label;
    const val = document.createElement("dd");
    if (!Array.isArray(values) || values.length === 0) {
      val.textContent = "—";
    } else {
      values.forEach((value) => {
        const line = document.createElement("div");
        line.textContent = text(value);
        val.appendChild(line);
      });
    }
    item.append(key, val);
    parent.appendChild(item);
  }

  function addSection(parent, title, pairs) {
    const section = document.createElement("section");
    section.className = "detail-section";
    const heading = document.createElement("h3");
    heading.textContent = title;
    const grid = document.createElement("dl");
    grid.className = "detail-grid";
    pairs.forEach((pair) => pair[2] ? addListPair(grid, pair[0], pair[1]) : addPair(grid, pair[0], pair[1]));
    section.append(heading, grid);
    parent.appendChild(section);
  }

  function renderRow(row) {
    const details = document.createElement("details");
    details.className = "surface-row";
    const summary = document.createElement("summary");
    const title = document.createElement("span");
    title.className = "surface-title";
    title.textContent = text(row.surfaceId);
    const state = document.createElement("span");
    state.className = "state-line";
    state.textContent = [text(row.disposition), ...(Array.isArray(row.deliveryStates) ? row.deliveryStates.map((value) => deliveryLabels[value] || value) : []), text(row.nextAction)].join(" · ");
    summary.append(title, state);
    details.appendChild(summary);
    const body = document.createElement("div");
    body.className = "detail-body";
    addSection(body, "Identity and shape", [
      ["Product", row.product], ["Area", row.area], ["Namespace", row.namespace], ["Family", row.family], ["Type", row.typeName], ["Member", row.memberName], ["Resource", row.resource], ["Field", row.fieldName], ["Kind", row.kind], ["Signature", row.signature],
      ["Canonical return type", row.returnType], ["Canonical parameters", row.parameters, true], ["Docs return type", row.docsReturnType], ["Docs parameters", row.docsParameters, true], ["Org return type", row.orgReturnType], ["Org parameters", row.orgParameters, true], ["Glade return type", row.gladeReturnType], ["Glade parameters", row.gladeParameters, true], ["Glade shape", row.ledgerShape], ["Behavior", row.behavior], ["Evidence", row.evidence]
    ]);
    addSection(body, "Claim and next action", [
      ["Disposition", row.disposition], ["Obligation", row.obligation], ["Match rule", row.matchRule], ["Reason", row.reason], ["Profile closure gap", row.gapClass || "none"], ["Ledger/inventory gap", row.ledgerGapClass || "none"], ["Ledger bucket", row.bucket || "none"], ["Gap / blocking state", row.gapState], ["Open", row.open ? "yes" : "no"], ["Blocking current-base completion", row.blocking ? "yes" : "no"], ["Delivery states", row.deliveryStates ? row.deliveryStates.map((value) => deliveryLabels[value] || value) : [], true], ["Next action", row.nextAction]
    ]);
    addSection(body, "Evidence and provenance", [
      ["Docs", row.docs], ["Org", row.org], ["Evidence/source IDs", row.sources, true], ["Docs source", row.docsSource], ["Docs title", row.docsTitle], ["API version", row.apiVersion], ["Shape source", row.shapeSource], ["Behavior source", row.behaviorSource], ["Implementation decision", row.implementationDecision], ["Implementation target", row.implementationTarget], ["Owner", row.owner], ["Bucket", row.bucket], ["Priority", row.priority], ["Notes", row.notes]
    ]);
    const corpusEntry = row.corpus || {};
    addSection(body, "Corpus usage", [
      ["Corpus use", row.corpusUsed ? "used" : "zero-use"], ["Usage key", row.usageKey], ["Passing refs", row.corpusPassingRefs], ["Failure refs", row.corpusFailureRefs], ["Passing projects", row.corpusPassingProjects],
      ["Public production refs", corpusEntry.pubProdRefs], ["Public test refs", corpusEntry.pubTestRefs], ["Expected-failure refs", corpusEntry.pubFailRefs], ["Private production refs", corpusEntry.privProdRefs], ["Private test refs", corpusEntry.privTestRefs],
      ["Public production projects", corpusEntry.pubProdProjects], ["Public test projects", corpusEntry.pubTestProjects], ["Expected-failure projects", corpusEntry.pubFailProjects], ["Private production projects", corpusEntry.privProdProjects], ["Private test projects", corpusEntry.privTestProjects]
    ]);
    details.appendChild(body);
    return details;
  }

  function matches(row) {
    const query = search.value.trim().toLowerCase();
    const searchable = [row.surfaceId, row.namespace, row.family, row.typeName, row.memberName, row.signature].map(lower).join(" ");
    if (query && !searchable.includes(query)) return false;
    if (obligation.value && row.disposition !== obligation.value) return false;
    if (delivery.value && (!Array.isArray(row.deliveryStates) || !row.deliveryStates.includes(delivery.value))) return false;
    if (behavior.value && row.behavior !== behavior.value) return false;
    if (evidence.value && row.evidence !== evidence.value) return false;
    if (gap.value && row.gapState !== gap.value) return false;
    if (namespace.value && (row.namespace || "__none__") !== namespace.value) return false;
    if (family.value && (row.family || "__none__") !== family.value) return false;
    if (corpus.value && (row.corpusUsed ? "used" : "zero-use") !== corpus.value) return false;
    if (action.value && row.nextActionKey !== action.value) return false;
    return true;
  }

  function updateFacetOptions(select, field, emptyLabel) {
    const selected = select.value;
    const values = [...new Set(rows.map((row) => row[field] || "__none__"))].sort();
    select.replaceChildren(new Option(emptyLabel, ""));
    values.forEach((value) => select.appendChild(new Option(value === "__none__" ? "(none)" : value, value)));
    select.value = values.includes(selected) ? selected : "";
  }

  function updateInputs() {
    const list = byID("input-list");
    list.replaceChildren();
    const inputs = page.inputs && Array.isArray(page.inputs.files) ? page.inputs.files : [];
    inputs.forEach((input) => {
      const item = document.createElement("div");
      item.className = "input-item";
      item.textContent = [text(input.name), text(input.path), text(input.sha256)].join(" · ");
      list.appendChild(item);
    });
  }

  function updateGapSummary() {
    const summary = byID("gap-summary");
    summary.replaceChildren();
    const gaps = Object.entries(page.byGapCategory || {});
    if (gaps.length === 0) {
      summary.textContent = "No open gap classes.";
      return;
    }
    gaps.forEach(([key, value]) => {
      const item = document.createElement("span");
      item.className = "gap-pill";
	      item.textContent = key + ": " + value;
      summary.appendChild(item);
    });
  }

  function updateValidationErrors() {
    const errors = Array.isArray(page.validationErrors) ? page.validationErrors : [];
    const panel = byID("validation-panel");
    const list = byID("validation-errors");
    list.replaceChildren();
    errors.forEach((message) => {
      const item = document.createElement("li");
      item.textContent = text(message);
      list.appendChild(item);
    });
    panel.hidden = errors.length === 0;
  }

  function render() {
    filteredRows = rows.filter(matches);
    const maxPage = Math.max(0, Math.ceil(filteredRows.length / pageSize) - 1);
    pageIndex = Math.min(pageIndex, maxPage);
    const start = pageIndex * pageSize;
    rowList.replaceChildren(...filteredRows.slice(start, start + pageSize).map(renderRow));
    byID("shown-count").textContent = filteredRows.length.toLocaleString();
    byID("shown-count-inline").textContent = filteredRows.length.toLocaleString();
	    byID("page-size").textContent = "Page " + (filteredRows.length === 0 ? 0 : pageIndex + 1) + " of " + Math.max(1, Math.ceil(filteredRows.length / pageSize));
	    byID("page-status").textContent = filteredRows.length.toLocaleString() + " matching rows";
    byID("previous-page").disabled = pageIndex === 0;
    byID("next-page").disabled = pageIndex >= maxPage;
  }

  [search, obligation, delivery, behavior, evidence, gap, namespace, family, corpus, action].forEach((control) => control.addEventListener("input", () => { pageIndex = 0; render(); }));
  byID("previous-page").addEventListener("click", () => { pageIndex = Math.max(0, pageIndex - 1); render(); });
  byID("next-page").addEventListener("click", () => { pageIndex += 1; render(); });
  updateInputs();
  updateGapSummary();
  updateValidationErrors();
  updateFacetOptions(namespace, "namespace", "All namespaces");
  updateFacetOptions(family, "family", "All families");
  render();
})();
</script>
</body>
</html>
`)
	_, err = io.WriteString(w, b.String())
	return err
}

func buildSupportProfileHTMLPage(profile SupportProfile, ledger SurfaceLedger) (SupportProfileHTMLPage, error) {
	ledgerByID := make(map[string]SurfaceLedgerRow, len(ledger.Rows))
	for _, row := range ledger.Rows {
		if _, exists := ledgerByID[row.SurfaceID]; exists {
			return SupportProfileHTMLPage{}, fmt.Errorf("duplicate ledger surface ID %q", row.SurfaceID)
		}
		ledgerByID[row.SurfaceID] = row
	}
	corpusByKey := make(map[string]CorpusUsageEntry, len(profile.CorpusUsage))
	for _, entry := range profile.CorpusUsage {
		if _, exists := corpusByKey[entry.UsageKey]; exists {
			return SupportProfileHTMLPage{}, fmt.Errorf("duplicate corpus usage key %q", entry.UsageKey)
		}
		corpusByKey[entry.UsageKey] = entry
	}

	page := SupportProfileHTMLPage{
		Total:                  profile.Total,
		ByDisposition:          make(map[SupportDisposition]int, 4),
		ByObligation:           make(map[string]int, 4),
		ByGapClass:             make(map[string]int, len(profile.ByGapClass)),
		InventoryFailureCounts: cloneSupportProfileHTMLCounts(ledger.Summary.Failures),
		InventoryFailureCount:  supportProfileHTMLFailureCount(ledger.Summary.Failures),
		ByGapCategory:          make(map[string]int, 6),
		ByGapState:             map[string]int{htmlGapStateBlocked: 0, htmlGapStateDeferred: 0, htmlGapStateClosed: 0},
		ByDeliveryState:        make(map[string]int, 8),
		ByBehavior:             make(map[string]int),
		ByEvidence:             make(map[string]int),
		ByNamespace:            make(map[string]int),
		ByFamily:               make(map[string]int),
		ByNextAction:           make(map[string]int),
		ByCorpusUse:            map[string]int{htmlCorpusUseUsed: 0, htmlCorpusUseZero: 0},
		Inputs:                 profile.Inputs,
		ValidationErrors:       append([]string(nil), profile.ValidationErrors...),
		Rows:                   make([]SupportProfileHTMLRow, 0, len(profile.Rows)),
	}
	for _, category := range []string{
		htmlGapCategoryMissingShape,
		htmlGapCategoryMissingBehavior,
		htmlGapCategoryMissingEvidence,
		htmlGapCategoryMismatch,
		htmlGapCategoryStale,
		htmlGapCategoryUnclassified,
		htmlGapCategoryInventoryFailure,
	} {
		page.ByGapCategory[category] = 0
	}
	for _, disposition := range []SupportDisposition{
		DispositionLocalRuntimeRequired,
		DispositionDeterministicMockRequired,
		DispositionCompileShapeRequired,
		DispositionHostedDeferred,
	} {
		page.ByDisposition[disposition] = profile.ByDisposition[disposition]
	}
	for gap, count := range profile.ByGapClass {
		page.ByGapClass[gap] = count
	}

	seenProfileIDs := make(map[string]bool, len(profile.Rows))
	for _, profileRow := range profile.Rows {
		if seenProfileIDs[profileRow.SurfaceID] {
			return SupportProfileHTMLPage{}, fmt.Errorf("duplicate profile surface ID %q", profileRow.SurfaceID)
		}
		seenProfileIDs[profileRow.SurfaceID] = true
		ledgerRow, ok := ledgerByID[profileRow.SurfaceID]
		if !ok {
			return SupportProfileHTMLPage{}, fmt.Errorf("profile surface ID %q is missing from ledger", profileRow.SurfaceID)
		}

		family := supportProfileHTMLFamily(ledgerRow, profileRow)
		actionKey, nextAction := supportProfileHTMLNextAction(profileRow, ledgerRow)
		blocking := supportProfileHTMLRowBlocking(profileRow, ledgerRow)
		row := SupportProfileHTMLRow{
			SurfaceID:              profileRow.SurfaceID,
			Product:                ledgerRow.Product,
			Area:                   ledgerRow.Area,
			Namespace:              profileRow.Namespace,
			Family:                 family,
			TypeName:               ledgerRow.TypeName,
			MemberName:             ledgerRow.MemberName,
			Resource:               ledgerRow.Resource,
			FieldName:              ledgerRow.FieldName,
			Kind:                   ledgerRow.Kind,
			Signature:              ledgerRow.Signature,
			ReturnType:             ledgerRow.ReturnType,
			Parameters:             append([]string(nil), ledgerRow.Parameters...),
			DocsReturnType:         ledgerRow.DocsReturnType,
			OrgReturnType:          ledgerRow.OrgReturnType,
			GladeReturnType:        ledgerRow.GladeReturnType,
			DocsParameters:         append([]string(nil), ledgerRow.DocsParameters...),
			OrgParameters:          append([]string(nil), ledgerRow.OrgParameters...),
			GladeParameters:        append([]string(nil), ledgerRow.GladeParameters...),
			Docs:                   ledgerRow.Docs,
			Org:                    ledgerRow.Org,
			LedgerShape:            profileRow.LedgerShape,
			Behavior:               profileRow.Behavior,
			Evidence:               profileRow.Evidence,
			Sources:                append([]string(nil), ledgerRow.Sources...),
			DocsSource:             ledgerRow.DocsSource,
			DocsTitle:              ledgerRow.DocsTitle,
			APIVersion:             ledgerRow.APIVersion,
			ShapeSource:            ledgerRow.ShapeSource,
			BehaviorSource:         ledgerRow.BehaviorSource,
			ImplementationDecision: ledgerRow.ImplementationDecision,
			ImplementationTarget:   ledgerRow.GladeImplementationTarget,
			Owner:                  ledgerRow.Owner,
			Bucket:                 ledgerRow.Bucket,
			Priority:               ledgerRow.Priority,
			Notes:                  ledgerRow.Notes,
			UsageKey:               profileRow.UsageKey,
			CorpusPassingRefs:      profileRow.CorpusPassingRefs,
			CorpusFailureRefs:      profileRow.CorpusFailureRefs,
			CorpusPassingProjects:  profileRow.CorpusPassingProjects,
			Disposition:            profileRow.Disposition,
			MatchRule:              profileRow.MatchRule,
			Reason:                 profileRow.Reason,
			Obligation:             profileRow.Obligation,
			GapClass:               profileRow.GapClass,
			LedgerGapClass:         ledgerRow.GapClass,
			Open:                   blocking,
			Blocking:               blocking,
			GapState:               supportProfileHTMLGapState(profileRow, ledgerRow),
			DeliveryStates:         supportProfileHTMLDeliveryStates(profileRow, ledgerRow),
			NextActionKey:          actionKey,
			NextAction:             nextAction,
			CorpusUsed:             supportProfileHTMLCorpusUsed(profileRow),
		}
		if profileRow.UsageKey != "" {
			if entry, exists := corpusByKey[profileRow.UsageKey]; exists {
				entryCopy := entry
				row.Corpus = &entryCopy
			}
		}
		for _, state := range row.DeliveryStates {
			page.ByDeliveryState[state]++
		}
		page.ByObligation[row.Obligation]++
		page.ByBehavior[string(row.Behavior)]++
		page.ByEvidence[string(row.Evidence)]++
		page.ByNamespace[row.Namespace]++
		page.ByFamily[row.Family]++
		page.ByGapState[row.GapState]++
		page.ByNextAction[row.NextActionKey]++
		if row.CorpusUsed {
			page.ByCorpusUse[htmlCorpusUseUsed]++
		} else {
			page.ByCorpusUse[htmlCorpusUseZero]++
		}
		if category := supportProfileHTMLGapCategory(row); category != "" {
			page.ByGapCategory[category]++
		}
		page.Rows = append(page.Rows, row)
	}
	sort.Slice(page.Rows, func(i, j int) bool { return page.Rows[i].SurfaceID < page.Rows[j].SurfaceID })
	page.Gate = supportProfileHTMLGate(profile, ledger, page.Rows)
	return page, nil
}

func cloneSupportProfileHTMLCounts(counts map[string]int) map[string]int {
	clone := make(map[string]int, len(counts))
	for key, count := range counts {
		clone[key] = count
	}
	return clone
}

func supportProfileHTMLFamily(ledgerRow SurfaceLedgerRow, profileRow SupportProfileRow) string {
	family := ledgerRow.SalesforceSurfaceFamily
	if family == "" || family == surfaceFamilyForProduct(ProductApex) {
		family = profileRow.TypeFamily
	}
	if family == "" || family == surfaceFamilyForProduct(ProductApex) {
		family = qualifiedName(ledgerRow.Namespace, ledgerRow.TypeName)
	}
	return family
}

func supportProfileHTMLFailureCount(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		if count > 0 {
			total += count
		}
	}
	return total
}

func supportProfileHTMLRowBlocking(row SupportProfileRow, ledgerRow SurfaceLedgerRow) bool {
	return ledgerRow.Bucket == BucketFailure || row.Disposition != DispositionHostedDeferred && (row.Disposition == "" || row.GapClass != "")
}

func supportProfileHTMLGapState(row SupportProfileRow, ledgerRow SurfaceLedgerRow) string {
	if supportProfileHTMLRowBlocking(row, ledgerRow) {
		return htmlGapStateBlocked
	}
	if row.Disposition == DispositionHostedDeferred {
		return htmlGapStateDeferred
	}
	return htmlGapStateClosed
}

func supportProfileHTMLCorpusUsed(row SupportProfileRow) bool {
	return row.CorpusPassingRefs > 0 || row.CorpusFailureRefs > 0 || row.CorpusPassingProjects > 0
}

func supportProfileHTMLGapCategory(row SupportProfileHTMLRow) string {
	if row.Bucket == BucketFailure {
		return htmlGapCategoryInventoryFailure
	}
	if row.Disposition == "" {
		return htmlGapCategoryUnclassified
	}
	switch row.GapClass {
	case "":
		return ""
	case GapMissingShape:
		return htmlGapCategoryMissingShape
	case GapMissingBehavior:
		return htmlGapCategoryMissingBehavior
	case GapMissingEvidence:
		return htmlGapCategoryMissingEvidence
	case GapStaleGladeShape, GapAPIVersionChange:
		return htmlGapCategoryStale
	case GapDocsOrgMismatch, GapReturnTypeMismatch, GapParameterMismatch, GapSignatureChanged:
		return htmlGapCategoryMismatch
	default:
		return htmlGapCategoryUnclassified
	}
}

func supportProfileHTMLGate(profile SupportProfile, ledger SurfaceLedger, rows []SupportProfileHTMLRow) SupportProfileHTMLGate {
	blockingRows := 0
	for _, row := range rows {
		if row.Blocking {
			blockingRows++
		}
	}
	gate := SupportProfileHTMLGate{
		Status:                htmlGatePass,
		Passed:                true,
		ValidationErrorCount:  len(profile.ValidationErrors),
		BlockingRowCount:      blockingRows,
		InventoryFailureCount: supportProfileHTMLFailureCount(ledger.Summary.Failures),
	}
	if gate.ValidationErrorCount > 0 {
		gate.Status = htmlGateBlocked
		gate.Passed = false
		gate.BlockingReasons = append(gate.BlockingReasons, "profile validation errors")
	}
	if blockingRows > 0 {
		gate.Status = htmlGateBlocked
		gate.Passed = false
		gate.BlockingReasons = append(gate.BlockingReasons, "non-deferred gap/open rows")
	}
	if gate.InventoryFailureCount > 0 {
		gate.Status = htmlGateBlocked
		gate.Passed = false
		gate.BlockingReasons = append(gate.BlockingReasons, "pinned ledger inventory failures")
	}
	return gate
}

func supportProfileHTMLNextAction(row SupportProfileRow, ledgerRow SurfaceLedgerRow) (string, string) {
	if ledgerRow.Bucket == BucketFailure {
		return htmlActionInventoryFailure, htmlNextInventoryFailure
	}
	if row.Disposition == DispositionHostedDeferred {
		return htmlActionHostedDeferred, htmlNextHostedDeferred
	}
	switch row.GapClass {
	case GapMissingShape:
		return htmlActionMissingShape, htmlNextMissingShape
	case GapMissingBehavior:
		return htmlActionMissingBehavior, htmlNextMissingBehavior
	case GapMissingEvidence:
		return htmlActionMissingEvidence, htmlNextMissingEvidence
	}
	if row.Disposition == "" {
		return htmlActionOpen, htmlNextOpen
	}
	return htmlActionClosed, htmlNextClosed
}

func supportProfileHTMLDeliveryStates(profileRow SupportProfileRow, ledgerRow SurfaceLedgerRow) []string {
	states := make([]string, 0, 3)
	appendState := func(state string) {
		for _, existing := range states {
			if existing == state {
				return
			}
		}
		states = append(states, state)
	}
	switch profileRow.Disposition {
	case DispositionLocalRuntimeRequired:
		if profileRow.GapClass != GapMissingShape && profileRow.GapClass != GapMissingBehavior {
			appendState(htmlDeliveryLocalRuntime)
		}
	case DispositionDeterministicMockRequired:
		if profileRow.GapClass != GapMissingShape && profileRow.GapClass != GapMissingBehavior {
			appendState(htmlDeliveryDeterministicMock)
		}
	case DispositionCompileShapeRequired:
		if profileRow.GapClass != GapMissingShape {
			appendState(htmlDeliveryCompileShape)
		}
	case DispositionHostedDeferred:
		appendState(htmlDeliveryHostedDeferred)
		appendState(htmlDeliveryNotLocallyImplemented)
	}
	if profileRow.Disposition != DispositionHostedDeferred {
		if profileRow.GapClass == "" && ledgerRow.Bucket != BucketFailure {
			appendState(htmlDeliveryCovered)
		}
		if ledgerRow.GladeBehavior == BehaviorUnsupported {
			appendState(htmlDeliveryExplicitUnsupported)
		}
	}
	if ledgerRow.Bucket == BucketFailure || profileRow.Disposition == "" || profileRow.GapClass != "" {
		appendState(htmlDeliveryOpen)
	}
	return states
}

func writeSupportProfileHTMLHead(b *strings.Builder) {
	b.WriteString(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Apex surface status</title>
<style>
:root { color-scheme: dark; --bg: #0d1117; --panel: #161b22; --border: #30363d; --text: #e6edf3; --muted: #8b949e; --blue: #58a6ff; --green: #3fb950; --orange: #d29922; --red: #f85149; --purple: #bc8cff; }
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--text); font: 14px/1.5 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
main { width: min(1500px, calc(100% - 36px)); margin: 0 auto; padding: 34px 0 80px; }
.hero { margin-bottom: 26px; }
.eyebrow { margin: 0 0 7px; color: var(--blue); font-size: 12px; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; }
h1, h2, h3, p { margin-top: 0; }
h1 { margin-bottom: 8px; font-size: clamp(28px, 4vw, 46px); letter-spacing: -.03em; }
h2 { margin-bottom: 0; font-size: 18px; }
h3 { margin-bottom: 12px; color: var(--blue); font-size: 14px; }
.lede { max-width: 760px; margin-bottom: 0; color: var(--muted); font-size: 16px; }
.completion-gate { display: flex; align-items: center; justify-content: space-between; gap: 18px; margin-bottom: 14px; padding: 16px 18px; border: 1px solid var(--border); border-radius: 12px; background: var(--panel); }
.completion-gate.gate-pass { border-color: rgba(63,185,80,.7); }
.completion-gate.gate-pass strong { color: var(--green); }
.completion-gate.gate-blocked { border-color: rgba(248,81,73,.8); }
.completion-gate.gate-blocked strong { color: var(--red); }
.completion-gate div { display: flex; align-items: baseline; gap: 12px; }
.completion-gate strong { font-size: 25px; letter-spacing: .04em; }
.completion-gate p { margin: 0; color: var(--muted); text-align: right; }
.gate-label { color: var(--muted); font-size: 12px; text-transform: uppercase; letter-spacing: .08em; }
.gate-pass { border-color: rgba(63,185,80,.75); }
.gate-pass strong { color: var(--green); }
.gate-blocked { border-color: rgba(248,81,73,.75); }
.gate-blocked strong { color: var(--red); }
.summary { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 10px; margin-bottom: 14px; }
.delivery-summary { grid-template-columns: repeat(4, minmax(0, 1fr)); margin-bottom: 0; }
.metric, .panel { border: 1px solid var(--border); border-radius: 12px; background: var(--panel); }
.metric { min-height: 90px; padding: 15px; }
.metric span, .muted { color: var(--muted); }
.metric strong { display: block; margin-top: 8px; font-size: 26px; }
.metric-total { border-color: rgba(88,166,255,.65); }
.panel { margin-top: 14px; padding: 20px; }
.section-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 18px; margin-bottom: 15px; }
.facet-columns { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 18px; }
.gap-summary { display: flex; flex-wrap: wrap; gap: 8px; }
.gap-pill, .badge { padding: 4px 9px; border: 1px solid var(--border); border-radius: 999px; color: var(--orange); background: rgba(210,153,34,.1); font-size: 12px; }
.legend { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
.legend p { margin-bottom: 0; color: var(--muted); }
.legend strong { color: var(--text); }
.validation-panel { border-color: rgba(248,81,73,.7); }
.validation-panel ul { margin: 0; padding-left: 20px; color: var(--red); }
.input-list { display: grid; gap: 6px; color: var(--muted); font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; overflow-wrap: anywhere; }
.input-item { padding: 8px 10px; border: 1px solid var(--border); border-radius: 8px; background: rgba(255,255,255,.02); }
.filters { display: grid; grid-template-columns: 2fr repeat(3, 1fr); gap: 10px; }
label { display: grid; gap: 6px; color: var(--muted); font-size: 12px; font-weight: 600; }
input, select, button { min-height: 38px; border: 1px solid var(--border); border-radius: 8px; background: #0d1117; color: var(--text); font: inherit; }
input, select { width: 100%; padding: 0 10px; }
button { padding: 0 14px; cursor: pointer; }
button:hover:not(:disabled) { border-color: var(--blue); color: var(--blue); }
button:disabled { cursor: not-allowed; opacity: .45; }
.result-bar, .pagination { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 15px; }
.row-list { display: grid; gap: 8px; margin-top: 12px; }
.surface-row { border: 1px solid var(--border); border-radius: 9px; background: rgba(255,255,255,.02); }
.surface-row[open] { border-color: rgba(88,166,255,.55); }
.surface-row summary { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 13px 15px; cursor: pointer; list-style-position: inside; }
.surface-title { min-width: 0; color: var(--text); font: 13px ui-monospace, SFMono-Regular, Menlo, monospace; overflow-wrap: anywhere; }
.state-line { flex: 0 0 auto; color: var(--muted); font-size: 12px; text-align: right; }
.detail-body { padding: 0 15px 15px 34px; }
.detail-section { padding-top: 15px; border-top: 1px solid var(--border); }
.detail-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px 14px; margin: 0; }
.pair { min-width: 0; }
dt { color: var(--muted); font-size: 11px; }
dd { margin: 2px 0 0; overflow-wrap: anywhere; }
.pagination { justify-content: center; }
@media (max-width: 1050px) { .summary, .delivery-summary { grid-template-columns: repeat(3, 1fr); } .filters { grid-template-columns: 1fr 1fr; } .legend, .facet-columns { grid-template-columns: 1fr 1fr; } .detail-grid { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 620px) { main { width: min(100% - 20px, 1500px); padding-top: 22px; } .summary, .filters, .legend, .facet-columns, .detail-grid { grid-template-columns: 1fr; } .completion-gate { display: block; } .completion-gate p { margin-top: 8px; text-align: left; } .surface-row summary { display: block; } .state-line { display: block; margin: 8px 0 0 19px; text-align: left; } .detail-body { padding-left: 28px; } }
</style>
</head>
`)
}

func renderSupportProfileHTMLGapSummary(gaps map[string]int) string {
	if len(gaps) == 0 {
		return `<span class="muted">No open gap classes.</span>`
	}
	keys := make([]string, 0, len(gaps))
	for key := range gaps {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, `<span class="gap-pill">%s: %d</span>`, html.EscapeString(key), gaps[key])
	}
	return b.String()
}

func renderSupportProfileHTMLCountSummary(counts map[string]int) string {
	return renderSupportProfileHTMLGapSummary(counts)
}

func renderSupportProfileHTMLGateMessage(gate SupportProfileHTMLGate) string {
	if gate.Passed {
		return "No validation errors, non-deferred gap/open rows, or pinned inventory failures."
	}
	if len(gate.BlockingReasons) == 0 {
		return "Completeness claim is blocked by the embedded profile state."
	}
	return strings.Join(gate.BlockingReasons, "; ") + "."
}
