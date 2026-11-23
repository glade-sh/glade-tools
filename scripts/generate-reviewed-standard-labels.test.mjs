import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {spawnSync} from "node:child_process";
import {gzipSync, gunzipSync} from "node:zlib";
import {canonicalString} from "./lib/reviewed-labels.mjs";

const generator = new URL("./generate-reviewed-standard-labels.mjs", import.meta.url).pathname;
const sanitizer = new URL("./sanitize-standard-describe-catalog.mjs", import.meta.url).pathname;
const run = (script, ...args) => spawnSync(process.execPath, [script, ...args], {encoding: "utf8"});

function inTemp(body) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "reviewed-labels-"));
  try { body(directory); } finally { fs.rmSync(directory, {recursive: true, force: true}); }
}
const write = (file, value) => { fs.writeFileSync(file, JSON.stringify(value)); return file; };
const gz = (file, value) => { fs.writeFileSync(file, gzipSync(JSON.stringify(value))); return file; };

const orgA = {name: "Acme Capture Org", id: "00D5e000000AAAA", username: "capture.a@example.net"};
const orgB = {name: "Zeta Capture Org", id: "00D5e000000BBBB", username: "capture.b@example.net"};

function describes(overrides = {}) {
  return {describes: {Account: {
    name: "Account", label: "Account", labelPlural: "Accounts",
    supportedScopes: [{name: "mine", label: "My accounts"}],
    fields: [
      {name: "Name", label: "Account Name"},
      {name: "Site", label: "Account Site"},
      {name: "Type", label: "Account Type", picklistValues: [{value: "Prospect", label: "Prospect", active: true}]},
      {name: "Phone", label: "Account Phone"},
      {name: "Fax", label: "Account Fax"},
      {name: "Rating", label: "Account Rating"},
      {name: "Industry", label: "Account Industry"},
    ],
    ...overrides,
  }}};
}
function twoOrgs(a = {}, b = {}) {
  return [describes(a), describes(b)];
}
function generate(directory, bundles, identities = [orgA, orgB], extra = ["--settings-differ"]) {
  const a = write(path.join(directory, "a.json"), bundles[0]);
  const b = write(path.join(directory, "b.json"), bundles[1]);
  const ids = write(path.join(directory, "ids.json"), identities);
  const output = path.join(directory, "labels.json");
  fs.rmSync(output, {force: true});
  const result = run(generator, a, b, ids, output, "--capture-id", "cap-67-0", "--api-version", "67.0", ...extra);
  return {result, output, document: () => JSON.parse(fs.readFileSync(output, "utf8"))};
}

test("keeps only labels identical across both orgs, with no identity text", () => inTemp((directory) => {
  const fieldsA = describes().describes.Account.fields.map((field) => ({...field}));
  const fieldsB = describes().describes.Account.fields.map((field) => ({...field}));
  fieldsA[0].label = "Acme Name Label";           // differs between orgs
  fieldsB[0].label = "Zeta Name Label";
  fieldsA[1].label = fieldsB[1].label = "Site of Acme Capture Org";   // carries the org name
  fieldsA[3].label = fieldsB[3].label = "owner@example.net";          // address
  fieldsA[4].label = fieldsB[4].label = "host.salesforce.com";        // hostname
  fieldsA[5].label = fieldsB[5].label = "Ref 001ABCDEFGHIJKL";        // 15-char id
  fieldsA[6].label = fieldsB[6].label = "Custom__c";                  // custom value
  const {result, output, document} = generate(directory, twoOrgs({fields: fieldsA}, {fields: fieldsB}));
  assert.equal(result.status, 0, result.stderr);
  const doc = document();
  assert.deepEqual(doc.objects.Account.fields.Type, {label: "Account Type", picklist: {Prospect: "Prospect"}});
  assert.deepEqual(Object.keys(doc.objects.Account.fields), ["Type"]);
  assert.equal(doc.objects.Account.label, "Account");
  assert.deepEqual(doc.objects.Account.scopes, {mine: "My accounts"});
  assert.deepEqual(doc.provenance, {generator: "generate-reviewed-standard-labels", captureId: "cap-67-0", apiVersion: "67.0", locale: "en_US", orgCount: 2, distinctSettingsAttested: true});
  const {sha256, ...rest} = doc;
  assert.equal(sha256, createHash("sha256").update(canonicalString(rest)).digest("hex"));
  assert.equal(JSON.parse(result.stdout).sha256, sha256);
  const text = fs.readFileSync(output, "utf8") + result.stdout + result.stderr;
  for (const needle of ["Acme", "Zeta", "00D5e", "capture.a", "capture.b", "example.net", "salesforce", "001ABCDEFGHIJKL", "Custom__c"]) assert.equal(text.includes(needle), false, needle);
}));

test("rejects identical identities, a missing attestation and one capture used twice", () => inTemp((directory) => {
  const bundles = twoOrgs();
  assert.notEqual(generate(directory, bundles, [orgA, {...orgB, name: orgA.name}]).result.status, 0);
  assert.notEqual(generate(directory, bundles, [orgA, {...orgB, id: orgA.id}]).result.status, 0);
  assert.notEqual(generate(directory, bundles, [orgA, {...orgB, username: orgA.username}]).result.status, 0);
  assert.notEqual(generate(directory, bundles, [orgA]).result.status, 0);
  const missing = generate(directory, bundles, [orgA, orgB], []);
  assert.notEqual(missing.result.status, 0);
  assert.equal(fs.existsSync(missing.output), false);
  const same = path.join(directory, "same.json");
  write(same, bundles[0]);
  const ids = write(path.join(directory, "ids.json"), [orgA, orgB]);
  const twice = run(generator, same, same, ids, path.join(directory, "x.json"), "--capture-id", "c", "--api-version", "67.0", "--settings-differ");
  assert.notEqual(twice.status, 0);
}));

function sanitize(directory, input, labelsFile, pin, allowed = []) {
  const inputFile = gz(path.join(directory, "in.json.gz"), input);
  const allow = gz(path.join(directory, "allow.json.gz"), allowed);
  const out = path.join(directory, "out.json.gz");
  fs.rmSync(out, {force: true});
  const args = [inputFile, allow, out];
  if (labelsFile) args.push("--reviewed-labels", labelsFile);
  if (pin) args.push("--reviewed-labels-sha256", pin);
  const result = run(sanitizer, ...args);
  return {result, out, read: () => JSON.parse(gunzipSync(fs.readFileSync(out)))};
}

test("sanitizer keeps a label only at its exact bound slot", () => inTemp((directory) => {
  const {result, document} = generate(directory, twoOrgs());
  assert.equal(result.status, 0, result.stderr);
  const sha = document().sha256;
  const labels = path.join(directory, "labels.json");
  // Org input: Name carries Account Site's reviewed label (wrong slot), Site its own.
  const input = describes({fields: [
    {name: "Name", label: "Account Site"},
    {name: "Site", label: "Account Site"},
    {name: "Type", label: "Org renamed type", picklistValues: [{value: "Prospect", label: "Prospect", active: true}]},
  ], label: "Org Account", labelPlural: "Accounts", supportedScopes: [{name: "mine", label: "My accounts"}, {name: "team", label: "My accounts"}]});
  const kept = sanitize(directory, input, labels, sha, ["Prospect"]);
  assert.equal(kept.result.status, 0, kept.result.stderr);
  const account = kept.read().describes.Account;
  assert.equal("label" in account, false);
  assert.equal(account.labelPlural, "Accounts");
  assert.deepEqual(account.supportedScopes, [{name: "mine", label: "My accounts"}, {name: "team"}]);
  assert.equal("label" in account.fields[0], false);
  assert.equal(account.fields[1].label, "Account Site");
  assert.equal("label" in account.fields[2], false);
  assert.equal(account.fields[2].picklistValues[0].label, "Prospect");
  assert.equal(JSON.parse(kept.result.stdout).removed.labelsBound > 0, true);
  // Without the option, nothing is bound: only the global allowlist applies.
  const plain = sanitize(directory, input, null, null, ["Prospect"]).read().describes.Account;
  assert.equal("label" in plain.fields[1], false);
}));

test("sanitizer verifies the pin, the embedded digest and the file contents", () => inTemp((directory) => {
  const {result, document, output} = generate(directory, twoOrgs());
  assert.equal(result.status, 0, result.stderr);
  const doc = document();
  const input = describes();
  const run1 = (file, pin) => sanitize(directory, input, file, pin);
  assert.equal(run1(output, doc.sha256).result.status, 0);
  assert.notEqual(run1(output, undefined).result.status, 0);                 // pin is required
  assert.notEqual(run1(output, "0".repeat(64)).result.status, 0);            // wrong pin
  const edited = structuredClone(doc);
  edited.objects.Account.fields.Name.label = "Hand Edited Label";
  const editedFile = write(path.join(directory, "edited.json"), edited);
  assert.notEqual(run1(editedFile, doc.sha256).result.status, 0);            // embedded digest stale
  const rehashed = structuredClone(edited);
  rehashed.sha256 = createHash("sha256").update(canonicalString((({sha256, ...rest}) => rest)(rehashed))).digest("hex");
  const rehashedFile = write(path.join(directory, "rehashed.json"), rehashed);
  assert.notEqual(run1(rehashedFile, doc.sha256).result.status, 0);          // pin mismatch
  assert.equal(run1(rehashedFile, rehashed.sha256).result.status, 0);        // consistent hand edits pass only if the pin is also replaced
}));

test("sanitizer rejects reviewed entries that fail the label filters", () => inTemp((directory) => {
  const {result, document} = generate(directory, twoOrgs());
  assert.equal(result.status, 0, result.stderr);
  const base = document();
  const seal = (doc) => { doc.sha256 = createHash("sha256").update(canonicalString((({sha256, ...rest}) => rest)(doc))).digest("hex"); return doc; };
  const bad = ["owner@example.net", "host.salesforce.com", "Ref 001ABCDEFGHIJKL", "Ref 001ABCDEFGHIJKLMNO", "Thing__kav", "Thing__C"];
  for (const label of bad) {
    const doc = structuredClone(base);
    doc.objects.Account.label = label;
    seal(doc);
    const file = write(path.join(directory, "bad.json"), doc);
    const outcome = sanitize(directory, describes(), file, doc.sha256);
    assert.notEqual(outcome.result.status, 0, label);
    assert.equal((outcome.result.stdout + outcome.result.stderr).includes(label), false);
  }
  for (const mutate of [
    (doc) => { doc.extra = 1; },
    (doc) => { doc.provenance.locale = "fr"; },
    (doc) => { doc.provenance.orgCount = 1; },
    (doc) => { doc.provenance.extra = true; },
    (doc) => { doc.objects.Account.unknown = "x"; },
    (doc) => { doc.format = "other"; },
  ]) {
    const doc = structuredClone(base);
    mutate(doc);
    seal(doc);
    const file = write(path.join(directory, "bad.json"), doc);
    assert.notEqual(sanitize(directory, describes(), file, doc.sha256).result.status, 0);
  }
}));
