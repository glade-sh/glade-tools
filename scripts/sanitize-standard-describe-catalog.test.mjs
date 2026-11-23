import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {spawnSync} from "node:child_process";
import {gzipSync, gunzipSync} from "node:zlib";

const sanitizer = new URL("./sanitize-standard-describe-catalog.mjs", import.meta.url).pathname;

function inTemp(run) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "standard-describe-sanitize-"));
  try { run(directory); } finally { fs.rmSync(directory, {recursive: true, force: true}); }
}
function writeJSON(file, value) { fs.writeFileSync(file, gzipSync(JSON.stringify(value))); }
function readJSON(file) { return JSON.parse(gunzipSync(fs.readFileSync(file))); }
function invoke(...args) { return spawnSync(process.execPath, [sanitizer, ...args], {encoding: "utf8"}); }

const values = ["Standard", "standard__Choice", "owned__Choice__c", "Choice__c", "Choice__e", "Choice__mdt", "Choice__x"];
const catalog = {summary: {objectCount: 1}, describes: {Account: {
  name: "Account", fields: [
    {name: "Type", type: "picklist", picklistValues: values.map((value) => ({value, label: value, active: true}))},
    {name: "OwnerId", referenceTo: ["User", "Owned__c"]},
    {name: "Owned__c", custom: true},
  ],
  childRelationships: [
    {childSObject: "Contact", field: "AccountId", relationshipName: "Contacts"},
    {childSObject: "Owned__c", field: "AccountId", relationshipName: "Owned"},
  ],
  recordTypeInfos: [{developerName: "Master", master: true}, {developerName: "Owned", master: false}],
}}};

test("extracts reviewed allowlist without custom entity values and sanitizes catalogs deterministically", () => inTemp((directory) => {
  const input = path.join(directory, "input.json.gz");
  const allowlist = path.join(directory, "allowlist.json.gz");
  const output = path.join(directory, "output.json.gz");
  const second = path.join(directory, "second.json.gz");
  writeJSON(input, catalog);
  const extraction = invoke("--extract-allowlist", input, allowlist);
  assert.equal(extraction.status, 0, extraction.stderr);
  assert.deepEqual(readJSON(allowlist), ["Standard", "standard__Choice"]);
  const before = fs.readFileSync(input);
  const firstRun = invoke(input, allowlist, output);
  assert.equal(firstRun.status, 0, firstRun.stderr);
  const sanitized = readJSON(output);
  assert.deepEqual(sanitized.describes.Account.fields.map((field) => field.name), ["Type", "OwnerId"]);
  assert.deepEqual(sanitized.describes.Account.fields[0].picklistValues, catalog.describes.Account.fields[0].picklistValues.slice(0, 2));
  assert.deepEqual(sanitized.describes.Account.fields[1].referenceTo, ["User"]);
  assert.deepEqual(sanitized.describes.Account.childRelationships, catalog.describes.Account.childRelationships.slice(0, 1));
  assert.deepEqual(sanitized.describes.Account.recordTypeInfos, catalog.describes.Account.recordTypeInfos.slice(0, 1));
  assert.deepEqual(fs.readFileSync(input), before);
  assert.equal(invoke(output, allowlist, second).status, 0);
  assert.deepEqual(fs.readFileSync(second), fs.readFileSync(output));
  assert.equal(fs.readFileSync(output).readUInt32LE(4), 0);
}));

test("drops unreviewed plain and namespace values and never logs their text", () => inTemp((directory) => {
  const input = path.join(directory, "input.json.gz");
  const allowlist = path.join(directory, "allowlist.json.gz");
  const output = path.join(directory, "output.json.gz");
  writeJSON(input, catalog);
  writeJSON(allowlist, ["Standard"]);
  const run = invoke(input, allowlist, output);
  assert.equal(run.status, 0, run.stderr);
  assert.deepEqual(readJSON(output).describes.Account.fields[0].picklistValues.map(({value}) => value), ["Standard"]);
  for (const value of values.slice(1)) assert.equal((run.stdout + run.stderr).includes(value), false);
}));

test("rejects a contaminated allowlist and input/output aliases without changing files", () => inTemp((directory) => {
  const input = path.join(directory, "input.json.gz");
  const allowlist = path.join(directory, "allowlist.json.gz");
  const output = path.join(directory, "output.json.gz");
  writeJSON(input, catalog);
  writeJSON(allowlist, ["Choice__c"]);
  assert.notEqual(invoke(input, allowlist, output).status, 0);
  assert.equal(fs.existsSync(output), false);
  writeJSON(allowlist, ["Standard"]);
  const before = fs.readFileSync(input);
  assert.notEqual(invoke(input, allowlist, input).status, 0);
  assert.deepEqual(fs.readFileSync(input), before);
}));

// Fail-closed cases. Every sentinel below stands for org data that must never
// reach the output file, stdout or stderr.
function describeOf(overrides = {}) {
  return {name: "Account", fields: [{name: "Name", type: "string"}], childRelationships: [], recordTypeInfos: [], ...overrides};
}
function sanitized(directory, bundle, reviewed = ["Standard"]) {
  const input = path.join(directory, "in.json.gz");
  const allowlist = path.join(directory, "allow.json.gz");
  const output = path.join(directory, "out.json.gz");
  fs.rmSync(output, {force: true});
  writeJSON(input, bundle);
  writeJSON(allowlist, reviewed);
  const run = invoke(input, allowlist, output);
  return {run, output, text: () => fs.existsSync(output) ? gunzipSync(fs.readFileSync(output)).toString() : ""};
}
function assertClean(result, sentinels) {
  const seen = result.run.stdout + result.run.stderr + result.text();
  for (const sentinel of sentinels) assert.equal(seen.includes(sentinel), false, "sentinel leaked");
}
function assertAborts(directory, bundle, sentinels = []) {
  const result = sanitized(directory, bundle);
  assert.notEqual(result.run.status, 0);
  assert.equal(fs.existsSync(result.output), false);
  assertClean(result, sentinels);
}

test("rebuilds the top level from an allowlist and rejects unknown keys", () => inTemp((directory) => {
  const sentinels = ["00D-ORG-SENTINEL", "https://org-sentinel.example.net", "user-sentinel@example.net", "summary-sentinel"];
  const result = sanitized(directory, {orgId: sentinels[0], instanceUrl: sentinels[1], username: sentinels[2], summary: {note: sentinels[3]}, describes: {Account: describeOf()}});
  assert.equal(result.run.status, 0, result.run.stderr);
  assert.deepEqual(Object.keys(readJSON(result.output)), ["describes"]);
  assertClean(result, sentinels);
  assertAborts(directory, {describes: {}, extraTop: "top-sentinel"}, ["top-sentinel"]);
}));

test("rejects unknown describe, field, picklist, child and record type keys", () => inTemp((directory) => {
  assertAborts(directory, {describes: {Account: describeOf({orgNote: "describe-sentinel"})}}, ["describe-sentinel"]);
  assertAborts(directory, {describes: {Account: describeOf({fields: [{name: "Name", orgNote: "field-sentinel"}]})}}, ["field-sentinel"]);
  assertAborts(directory, {describes: {Account: describeOf({fields: [{name: "Type", picklistValues: [{value: "Standard", orgNote: "pick-sentinel"}]}]})}}, ["pick-sentinel"]);
  assertAborts(directory, {describes: {Account: describeOf({childRelationships: [{childSObject: "Contact", orgNote: "child-sentinel"}]})}}, ["child-sentinel"]);
  assertAborts(directory, {describes: {Account: describeOf({recordTypeInfos: [{master: true, orgNote: "rt-sentinel"}]})}}, ["rt-sentinel"]);
  assertAborts(directory, {describes: {Account: describeOf({urls: {orgNote: "/services/data/v67.0/sobjects/url-sentinel"}})}}, ["url-sentinel"]);
}));

test("keeps labels only when they equal a reviewed public value", () => inTemp((directory) => {
  const labels = ["Org Renamed Account", "Org Renamed Accounts", "Org Field Label", "Org Pick Label", "Org Scope Label"];
  const result = sanitized(directory, {describes: {Account: describeOf({
    label: labels[0], labelPlural: labels[1],
    supportedScopes: [{label: labels[4], name: "mine"}, {label: "Standard", name: "team"}],
    fields: [{name: "Name", label: labels[2]}, {name: "Type", label: "Standard", picklistValues: [{value: "Standard", label: labels[3], active: true}, {value: "Standard", label: "Standard", active: true}]}],
  })}});
  assert.equal(result.run.status, 0, result.run.stderr);
  const account = readJSON(result.output).describes.Account;
  assert.equal("label" in account || "labelPlural" in account, false);
  assert.deepEqual(account.supportedScopes, [{name: "mine"}, {label: "Standard", name: "team"}]);
  assert.equal("label" in account.fields[0], false);
  assert.equal(account.fields[1].label, "Standard");
  assert.deepEqual(account.fields[1].picklistValues.map((entry) => entry.label), [undefined, "Standard"]);
  assertClean(result, labels);
}));

test("drops default values, formulas, help text and picklist default flags", () => inTemp((directory) => {
  const sentinels = ["default-sentinel", "formula-sentinel", "help-sentinel", "calc-sentinel"];
  const result = sanitized(directory, {describes: {Account: describeOf({fields: [
    {name: "Name", defaultValue: sentinels[0], defaultValueFormula: sentinels[1], inlineHelpText: sentinels[2], calculatedFormula: sentinels[3]},
    {name: "Type", picklistValues: [{value: "Standard", active: true, defaultValue: true}]},
  ]})}});
  assert.equal(result.run.status, 0, result.run.stderr);
  const [name, type] = readJSON(result.output).describes.Account.fields;
  for (const key of ["defaultValue", "defaultValueFormula", "inlineHelpText", "calculatedFormula"]) assert.equal(key in name, false);
  assert.equal("defaultValue" in type.picklistValues[0], false);
  assertClean(result, sentinels);
}));

test("keeps controllerName only when it names a kept standard field", () => inTemp((directory) => {
  const result = sanitized(directory, {describes: {Account: describeOf({fields: [
    {name: "Country", type: "picklist"},
    {name: "State", controllerName: "Country"},
    {name: "City", controllerName: "Org_Controller__c"},
    {name: "Town", controllerName: "Unknown_Standard_Looking"},
    {name: "Org_Controller__c", custom: true},
  ]})}});
  assert.equal(result.run.status, 0, result.run.stderr);
  const fields = readJSON(result.output).describes.Account.fields;
  assert.equal(fields.find((field) => field.name === "State").controllerName, "Country");
  assert.equal("controllerName" in fields.find((field) => field.name === "City"), false);
  assert.equal("controllerName" in fields.find((field) => field.name === "Town"), false);
  assertClean(result, ["Org_Controller__c", "Unknown_Standard_Looking"]);
}));

test("drops org-authored layout names, override urls, scope fields, record type names and hosts", () => inTemp((directory) => {
  const sentinels = ["LayoutSentinel", "override-sentinel", "Scope_Sentinel__c", "RecordTypeSentinel", "org-host-sentinel", "0123456789ABCDEFGH"];
  const result = sanitized(directory, {describes: {Account: describeOf({
    namedLayoutInfos: [{name: sentinels[0], urls: {layout: `/services/data/v67.0/sobjects/Account/describe/namedLayouts/${sentinels[0]}`}}],
    actionOverrides: [{name: "View", formFactor: "LARGE", isAvailableInTouch: false, url: `https://${sentinels[1]}.example.net/page`, pageId: sentinels[5]}],
    networkScopeFieldName: sentinels[2],
    urls: {describe: "/services/data/v67.0/sobjects/Account/describe", uiDetailTemplate: `https://${sentinels[4]}.example.net/{ID}`, uiEditTemplate: `https://${sentinels[4]}.example.net/not-a-template`},
    recordTypeInfos: [{master: true, developerName: sentinels[3], name: sentinels[3], recordTypeId: sentinels[5], urls: {layout: `https://${sentinels[4]}.example.net/services/${sentinels[5]}`}}],
  })}});
  assert.equal(result.run.status, 0, result.run.stderr);
  const account = readJSON(result.output).describes.Account;
  assert.deepEqual(account.namedLayoutInfos, []);
  assert.deepEqual(account.actionOverrides, [{name: "View", formFactor: "LARGE", isAvailableInTouch: false}]);
  assert.equal("networkScopeFieldName" in account, false);
  assert.deepEqual(Object.keys(account.urls).sort(), ["describe", "uiDetailTemplate"]);
  assert.equal(account.urls.uiDetailTemplate, "https://schema-org-001.example.com/{ID}");
  assert.deepEqual(account.recordTypeInfos, [{master: true, urls: {}}]);
  assertClean(result, sentinels);
}));

test("aborts on malformed array shapes and element shapes without echoing input", () => inTemp((directory) => {
  const sentinel = "shape-sentinel";
  assertAborts(directory, {describes: {Account: describeOf({fields: {name: sentinel}})}}, [sentinel]);
  assertAborts(directory, {describes: {Account: describeOf({childRelationships: sentinel})}}, [sentinel]);
  assertAborts(directory, {describes: {Account: describeOf({recordTypeInfos: {master: sentinel}})}}, [sentinel]);
  assertAborts(directory, {describes: {Account: describeOf({fields: [{name: "Type", picklistValues: {value: sentinel}}]})}}, [sentinel]);
  assertAborts(directory, {describes: {Account: describeOf({fields: [sentinel]})}}, [sentinel]);
  assertAborts(directory, {describes: {Account: describeOf({fields: [{name: "Name", referenceTo: [sentinel, 7]}]})}}, [sentinel]);
  assertAborts(directory, {describes: {Account: describeOf({fields: [{name: "Name", nillable: sentinel}]})}}, [sentinel]);
  assertAborts(directory, {describes: {Account: describeOf({childRelationships: [sentinel]})}}, [sentinel]);
  assertAborts(directory, {describes: {Account: describeOf({recordTypeInfos: [{master: true, urls: [sentinel]}]})}}, [sentinel]);
  assertAborts(directory, {describes: {Account: describeOf({fields: [{name: "Type", picklistValues: [{value: 5}]}]})}}, []);
}));

test("treats custom suffixes in any case as org data", () => inTemp((directory) => {
  const suffixed = ["Thing__kav", "Thing__b", "Thing__r", "Thing__Share", "Thing__C", "Thing__History", "A__B__c"];
  for (const value of suffixed) {
    const allowlist = path.join(directory, "allow.json.gz");
    writeJSON(allowlist, [value]);
    const input = path.join(directory, "in.json.gz");
    writeJSON(input, {describes: {}});
    assert.notEqual(invoke(input, allowlist, path.join(directory, "out.json.gz")).status, 0, value);
  }
  const result = sanitized(directory, {describes: {Account: describeOf({fields: [
    {name: "Type", picklistValues: suffixed.map((value) => ({value, active: true})).concat([{value: "Standard", active: true}])},
    {name: "Case_Mixed__C"}, {name: "Other__kav"},
  ]})}});
  assert.equal(result.run.status, 0, result.run.stderr);
  const fields = readJSON(result.output).describes.Account.fields;
  assert.deepEqual(fields.map((field) => field.name), ["Type"]);
  assert.deepEqual(fields[0].picklistValues.map((entry) => entry.value), ["Standard"]);
  assertClean(result, suffixed);
}));
