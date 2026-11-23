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
