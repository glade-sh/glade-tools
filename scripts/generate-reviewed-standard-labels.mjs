#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import {randomUUID} from "node:crypto";
import {gunzipSync} from "node:zlib";
import {
  STANDARD_DESCRIBE_MAX_COMPRESSED_INPUT_BYTES,
  STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES,
} from "./lib/standard-describe-canonical.mjs";
import {
  REVIEWED_LABELS_FORMAT,
  REVIEWED_LABELS_GENERATOR,
  nameToken,
  reviewableLabel,
  reviewedLabelsDigest,
  validateReviewedLabels,
} from "./lib/reviewed-labels.mjs";

// Builds the reviewed-labels file for sanitize-standard-describe-catalog.mjs.
//
//   generate-reviewed-standard-labels.mjs DESCRIBE_A DESCRIBE_B IDENTITIES OUTPUT
//       --capture-id ID --api-version NN.0 --settings-differ
//
// DESCRIBE_A and DESCRIBE_B are describe exports (en_US) from two fresh scratch
// orgs. IDENTITIES is JSON: [{name, id, username}, {name, id, username}] for
// those two orgs. A label is kept only when it sits at the same object slot in
// both exports, is identical, passes the shared label filter, and contains none
// of either org's name, id or username. The orgs must differ in all three.
// --settings-differ attests that the two orgs were also configured differently.
// The generator never prints labels or identity values.

const customName = (value) => typeof value === "string" && value.includes("__");

function readJSON(file) {
  const stat = fs.statSync(file);
  if (!stat.isFile()) throw new Error("input must be a regular file");
  if (stat.size > STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES) throw new Error("input exceeds size limit");
  let bytes = fs.readFileSync(file);
  if (bytes[0] === 0x1f && bytes[1] === 0x8b) {
    if (bytes.length > STANDARD_DESCRIBE_MAX_COMPRESSED_INPUT_BYTES) throw new Error("compressed input exceeds size limit");
    bytes = gunzipSync(bytes, {maxOutputLength: STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES});
  }
  return JSON.parse(bytes.toString("utf8"));
}

const bad = () => new Error("malformed describe catalog shape");
const isObject = (value) => value !== null && typeof value === "object" && !Array.isArray(value);

// Flattens one export to Map(slotKey -> label). Slot keys are JSON arrays.
function slotsOf(bundle) {
  if (!isObject(bundle) || !isObject(bundle.describes)) throw bad();
  const slots = new Map();
  const put = (label, ...slot) => { if (typeof label === "string") slots.set(JSON.stringify(slot), label); };
  for (const [objectName, describe] of Object.entries(bundle.describes)) {
    if (!isObject(describe)) throw bad();
    if (customName(objectName) || customName(describe.name) || describe.custom === true || !nameToken.test(objectName)) continue;
    put(describe.label, objectName, "label");
    put(describe.labelPlural, objectName, "labelPlural");
    for (const scope of describe.supportedScopes ?? []) {
      if (!isObject(scope)) throw bad();
      if (typeof scope.name === "string" && nameToken.test(scope.name)) put(scope.label, objectName, "scopes", scope.name);
    }
    if (describe.fields !== undefined && !Array.isArray(describe.fields)) throw bad();
    for (const field of describe.fields ?? []) {
      if (!isObject(field)) throw bad();
      if (field.custom === true || customName(field.name) || typeof field.name !== "string" || !nameToken.test(field.name)) continue;
      put(field.label, objectName, "fields", field.name, "label");
      if (field.picklistValues !== undefined && !Array.isArray(field.picklistValues)) throw bad();
      for (const entry of field.picklistValues ?? []) {
        if (!isObject(entry)) throw bad();
        if (typeof entry.value === "string") put(entry.label, objectName, "fields", field.name, "picklist", entry.value, "label");
      }
    }
  }
  return slots;
}

function checkIdentities(value) {
  if (!Array.isArray(value) || value.length !== 2) throw new Error("identities must list exactly two orgs");
  const keys = ["name", "id", "username"];
  for (const org of value) {
    if (!isObject(org) || keys.some((key) => typeof org[key] !== "string" || org[key].trim() === "")) throw new Error("each org needs a name, id and username");
  }
  for (const key of keys) {
    if (value[0][key].toLowerCase() === value[1][key].toLowerCase()) throw new Error("the two orgs must differ in name, id and username");
  }
  // Identity text a label must not contain, in any letter case.
  const needles = new Set();
  for (const org of value) {
    for (const key of keys) needles.add(org[key].trim().toLowerCase());
    needles.add(org.id.trim().toLowerCase().slice(0, 15));
    needles.add(org.username.split("@")[0].trim().toLowerCase());
  }
  needles.delete("");
  return [...needles];
}

function build(bundleA, bundleB, identities) {
  const needles = checkIdentities(identities);
  const left = slotsOf(bundleA);
  const right = slotsOf(bundleB);
  const objects = {};
  let kept = 0;
  for (const [key, label] of left) {
    if (right.get(key) !== label) continue;
    if (!reviewableLabel(label)) continue;
    const folded = label.toLowerCase();
    if (needles.some((needle) => folded.includes(needle))) continue;
    const [objectName, kind, ...rest] = JSON.parse(key);
    const entry = (objects[objectName] ??= {});
    if (kind === "label" || kind === "labelPlural") entry[kind] = label;
    else if (kind === "scopes") (entry.scopes ??= {})[rest[0]] = label;
    else if (kind === "fields") {
      const [fieldName, slot, value] = rest;
      const field = ((entry.fields ??= {})[fieldName] ??= {});
      if (slot === "label") field.label = label;
      else (field.picklist ??= {})[value] = label;
    }
    kept++;
  }
  return {objects, kept};
}

function writeDocument(destination, document) {
  const temporary = path.join(path.dirname(destination), `.${path.basename(destination)}.labels-${randomUUID()}`);
  try {
    fs.writeFileSync(temporary, JSON.stringify(document, null, 1) + "\n", {flag: "wx"});
    fs.renameSync(temporary, destination);
  } finally {
    fs.rmSync(temporary, {force: true});
  }
}

try {
  let args = process.argv.slice(2);
  const take = (flag, boolean = false) => {
    const at = args.indexOf(flag);
    if (at < 0) return undefined;
    if (boolean) { args = [...args.slice(0, at), ...args.slice(at + 1)]; return true; }
    const value = args[at + 1];
    if (value === undefined) throw new Error("missing option value");
    args = [...args.slice(0, at), ...args.slice(at + 2)];
    return value;
  };
  const captureId = take("--capture-id");
  const apiVersion = take("--api-version");
  const settingsDiffer = take("--settings-differ", true);
  if (args.length !== 4 || captureId === undefined || apiVersion === undefined || !settingsDiffer) {
    throw new Error("usage: generate-reviewed-standard-labels.mjs DESCRIBE_A DESCRIBE_B IDENTITIES OUTPUT --capture-id ID --api-version NN.0 --settings-differ");
  }
  const [fileA, fileB, identitiesFile, output] = args;
  const destination = path.resolve(output);
  const address = path.join(fs.realpathSync(path.dirname(destination)), path.basename(destination));
  for (const input of [fileA, fileB, identitiesFile]) {
    if (address === fs.realpathSync(input)) throw new Error("input and output paths must be distinct");
  }
  if (fs.existsSync(destination) && !fs.statSync(destination).isFile()) throw new Error("output must be a regular file");
  if (fs.realpathSync(fileA) === fs.realpathSync(fileB)) throw new Error("the two captures must be different files");
  const {objects, kept} = build(readJSON(fileA), readJSON(fileB), readJSON(identitiesFile));
  const document = {
    format: REVIEWED_LABELS_FORMAT,
    provenance: {generator: REVIEWED_LABELS_GENERATOR, captureId, apiVersion, locale: "en_US", orgCount: 2, distinctSettingsAttested: true},
    objects,
  };
  document.sha256 = reviewedLabelsDigest(document);
  // The writer refuses its own output unless the sanitizer would accept it.
  validateReviewedLabels(document, document.sha256);
  writeDocument(destination, document);
  process.stdout.write(JSON.stringify({labels: kept, sha256: document.sha256}) + "\n");
} catch (error) {
  process.stderr.write(error instanceof SyntaxError ? "invalid JSON input\n" : `${error.message}\n`);
  process.exitCode = 1;
}
