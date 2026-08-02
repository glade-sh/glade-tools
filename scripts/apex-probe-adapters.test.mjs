import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";

import {
  adapterFamilyForRule,
  applyProbeAdapter,
  buildContextProbeLines,
} from "./apex-probe-adapters.mjs";

function probeCase(rule, lines, source = {}) {
  return {
    caseId: `case-${rule}`,
    surfaceId: `apex:System.${rule}`,
    mapping: { status: "mapped", lines },
    source: { owner: "System.List", member: "deepClone", ...source },
    classification: { rule },
    diagnostic: { problems: [] },
  };
}

test("CB105 adapter families are derived from classifier rules, not surface ids", () => {
  assert.equal(adapterFamilyForRule("abstract-or-nonconstructible-instantiation"), "context-type-reference");
  assert.equal(adapterFamilyForRule("wrong-static-instance-owner"), "static-owner-context");
  assert.equal(adapterFamilyForRule("bare-generic"), "generic-placeholder");
  assert.equal(adapterFamilyForRule("exact-sobject-field-only-addError"), "sobject-field-add-error");
  assert.equal(adapterFamilyForRule("void-return-assignment"), "void-call");
  assert.equal(adapterFamilyForRule("probe-syntax-error"), "safe-context");
  assert.equal(adapterFamilyForRule("namespace-identity-probe"), "safe-context");
  assert.equal(adapterFamilyForRule("reserved-member-token"), "safe-context");
});

test("CB105 abstract probes become legal type-reference context probes", () => {
  const result = applyProbeAdapter(probeCase(
    "abstract-or-nonconstructible-instantiation",
    ["Object sink = new ApexPages.Component();"],
    { owner: "ApexPages.Component", member: "Component" },
  ));
  assert.equal(result.adapterEvidenceKind, "context");
  assert.match(result.mapping.lines.join("\n"), /ApexPages\.Component/);
  assert.doesNotMatch(result.mapping.lines.join("\n"), /new ApexPages\.Component/);
  assert.equal(result.syntaxRepresentable, true);
});

test("CB105 static-owner family corrects both static and instance context directions", () => {
  const staticResult = applyProbeAdapter({
    ...probeCase("wrong-static-instance-owner", ["Object sink = ((cache.OrgPartition)null).validateKey(false, 'cb70');"], { owner: "cache.OrgPartition", member: "validateKey" }),
    diagnostic: { problems: ["Static method cannot be referenced from a non static context: void cache.Partition.validateKey(Boolean, String)"] },
  });
  assert.match(staticResult.mapping.lines[0], /cache\.Partition\.validateKey/);
  assert.doesNotMatch(staticResult.mapping.lines[0], /\(\(cache\.OrgPartition\)null\)/);
  const instanceResult = applyProbeAdapter({
    ...probeCase("wrong-static-instance-owner", ["Object sink = Auth.AuthConfiguration.getAuthConfig();"], { owner: "Auth.AuthConfiguration", member: "getAuthConfig" }),
    diagnostic: { problems: ["Non static method cannot be referenced from a static context: AuthConfig Auth.AuthConfiguration.getAuthConfig()"] },
  });
  assert.match(instanceResult.mapping.lines[0], /\(\(Auth\.AuthConfiguration\)null\)\.getAuthConfig/);
});

test("CB105 generic and addError families preserve the target family while fixing probe shape", () => {
  const generic = applyProbeAdapter(probeCase("bare-generic", ["Object sink = ((List)null).deepClone();"]));
  assert.match(generic.mapping.lines[0], /\(\(List<Object>\)null\)\.deepClone/);
  assert.equal(generic.adapterEvidenceKind, "accepted");
  const batchable = applyProbeAdapter(probeCase("bare-generic", ["Database.Batchable value = (Database.Batchable)null;"], { owner: "Database.Batchable", member: "" }));
  assert.match(batchable.mapping.lines[0], /Database\.Batchable<SObject>/);
  const map = applyProbeAdapter(probeCase("bare-generic", ["Object sink = ((Map)null).getSObjectType();"], { owner: "Map", member: "getSObjectType" }));
  assert.equal(map.adapterEvidenceKind, "context");
  assert.match(map.mapping.lines[0], /Object cb105Context/);
  const omittedTarget = applyProbeAdapter({ ...probeCase("bare-generic", []), mapping: { status: "omitted", lines: [] }, source: { owner: "Comparator", member: "compare" } });
  assert.equal(omittedTarget.adapterEvidenceKind, "context");
  const placeholderTarget = applyProbeAdapter(probeCase("bare-generic", ["T12 value = (T12)null;"], { owner: "T12" }));
  assert.equal(placeholderTarget.adapterEvidenceKind, "context");
  const addError = applyProbeAdapter({
    ...probeCase("exact-sobject-field-only-addError", ["((Boolean)null).addError('cb70');"], { owner: "Boolean", member: "addError" }),
  });
  assert.match(addError.mapping.lines[0], /new Account\(\)\.Name\.addError/);
  assert.equal(addError.adapterEvidenceKind, "context");
});

test("CB105 nonrepresentable syntax families receive a legal context probe", () => {
  const result = applyProbeAdapter(probeCase("probe-syntax-error", []));
  assert.deepEqual(result.mapping.lines, buildContextProbeLines(result));
  assert.equal(result.adapterEvidenceKind, "context");
  assert.equal(result.syntaxRepresentable, true);
  assert.match(result.mapping.lines.join("\n"), /Object cb105Context/);
});

test("CB105 adapts every frozen adapter-defect row through a family rule", { skip: !process.env.CB105_INPUT_ROOT }, () => {
  const inputRoot = process.env.CB105_INPUT_ROOT;
  const classificationPath = `${inputRoot}/cb100-final-rejection-classification/rejection-classification.json`;
  const manifestPath = `${inputRoot}/cb70-bulk-org-sweeps/manifest.json`;
  const classification = JSON.parse(fs.readFileSync(classificationPath, "utf8"));
  const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  const casesById = new Map(manifest.cases.map((entry) => [entry.caseId, entry]));
  const rows = classification.rows.filter((row) => row.disposition === "probe-adapter-defect");
  const familyCounts = new Map();

  assert.equal(rows.length, 239);
  for (const row of rows) {
    const sourceCase = casesById.get(row.caseId);
    assert.ok(sourceCase, `missing manifest case ${row.caseId}`);
    const adapted = applyProbeAdapter({
      ...sourceCase,
      caseId: row.caseId,
      surfaceId: row.surfaceId,
      source: sourceCase.source,
      mapping: row.mapping,
      classification: row,
      diagnostic: row.diagnostic,
    });
    assert.ok(adapted.adapterFamily, `missing adapter family ${row.caseId}`);
    assert.equal(adapted.syntaxRepresentable, true, row.caseId);
    assert.ok(adapted.mapping.lines.length > 0, row.caseId);
    if (adapted.mapping.lines.some((line) => line.includes("cb105Context_"))) assert.equal(adapted.adapterEvidenceKind, "context", row.caseId);
    familyCounts.set(adapted.adapterFamily, (familyCounts.get(adapted.adapterFamily) || 0) + 1);
  }

  assert.equal([...familyCounts.values()].reduce((sum, count) => sum + count, 0), 239);
  assert.ok(familyCounts.size >= 2);
});
