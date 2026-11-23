#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import {randomUUID} from "node:crypto";
import {gunzipSync, gzipSync} from "node:zlib";
import {
  STANDARD_DESCRIBE_MAX_COMPRESSED_INPUT_BYTES,
  STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES,
} from "./lib/standard-describe-canonical.mjs";

// Custom entity suffixes identify org additions even when no namespace is present.
// A single namespace separator alone is also used by standard platform values.
const customEntity = /__(?:c|e|mdt|x)$/;
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

function describesOf(bundle) {
  if (!bundle?.describes || typeof bundle.describes !== "object" || Array.isArray(bundle.describes)) {
    throw new Error("input must contain a describes object");
  }
  return bundle.describes;
}

function preflightOutput(inputs, output) {
  const destination = path.resolve(output);
  const address = path.join(fs.realpathSync(path.dirname(destination)), path.basename(destination));
  const existing = fs.existsSync(destination) ? fs.statSync(destination) : null;
  if (existing && !existing.isFile()) throw new Error("output must be a regular file");
  for (const input of inputs) {
    const inputStat = fs.statSync(input);
    if (address === fs.realpathSync(input) || (existing && existing.dev === inputStat.dev && existing.ino === inputStat.ino)) {
      throw new Error("input and output paths must be distinct");
    }
  }
  return destination;
}

function writeGzipJSON(destination, value) {
  const bytes = Buffer.from(gzipSync(Buffer.from(JSON.stringify(value) + "\n"), {level: 9}));
  bytes.fill(0, 4, 8);
  bytes[9] = 255;
  const temporary = path.join(path.dirname(destination), `.${path.basename(destination)}.sanitize-${randomUUID()}`);
  try {
    fs.writeFileSync(temporary, bytes, {flag: "wx"});
    fs.renameSync(temporary, destination);
  } finally {
    fs.rmSync(temporary, {force: true});
  }
}

function extractAllowlist(bundle) {
  const values = new Set();
  for (const describe of Object.values(describesOf(bundle))) {
    for (const field of describe.fields ?? []) {
      for (const entry of field.picklistValues ?? []) {
        if (typeof entry.value === "string" && !customEntity.test(entry.value)) values.add(entry.value);
      }
    }
  }
  return [...values].sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)));
}

function sanitize(bundle, reviewedValues) {
  if (!Array.isArray(reviewedValues) || reviewedValues.some((value) => typeof value !== "string" || customEntity.test(value))) {
    throw new Error("allowlist must contain reviewed standard string values only");
  }
  const allowed = new Set(reviewedValues);
  const describes = describesOf(bundle);
  const counts = {objects: 0, fields: 0, references: 0, childRows: 0, recordTypes: 0, picklists: 0};
  for (const [objectName, describe] of Object.entries(describes)) {
    if (customName(objectName) || customName(describe.name) || describe.custom === true) {
      delete describes[objectName];
      counts.objects++;
      continue;
    }
    if (Array.isArray(describe.fields)) {
      describe.fields = describe.fields.filter((field) => {
        if (field.custom === true || customName(field.name)) { counts.fields++; return false; }
        if (Array.isArray(field.referenceTo)) {
          field.referenceTo = field.referenceTo.filter((target) => {
            if (!customName(target)) return true;
            counts.references++;
            return false;
          });
        }
        if (Array.isArray(field.picklistValues)) {
          field.picklistValues = field.picklistValues.filter((entry) => {
            if (typeof entry.value === "string" && !customEntity.test(entry.value) && allowed.has(entry.value)) return true;
            counts.picklists++;
            return false;
          });
        }
        return true;
      });
    }
    if (Array.isArray(describe.childRelationships)) {
      describe.childRelationships = describe.childRelationships.filter((row) => {
        if (![row.childSObject, row.field, row.relationshipName].some(customName)) return true;
        counts.childRows++;
        return false;
      });
    }
    if (Array.isArray(describe.recordTypeInfos)) {
      describe.recordTypeInfos = describe.recordTypeInfos.filter((recordType) => {
        if (recordType.master === true) return true;
        counts.recordTypes++;
        return false;
      });
    }
  }
  return counts;
}

try {
  const args = process.argv.slice(2);
  if (args.length !== 3) {
    throw new Error("usage: sanitize-standard-describe-catalog.mjs INPUT ALLOWLIST OUTPUT_GZ | --extract-allowlist PUBLIC_CATALOG OUTPUT_GZ");
  }
  if (args[0] === "--extract-allowlist") {
    const destination = preflightOutput([args[1]], args[2]);
    const values = extractAllowlist(readJSON(args[1]));
    writeGzipJSON(destination, values);
    process.stdout.write(JSON.stringify({values: values.length}) + "\n");
  } else {
    const destination = preflightOutput(args.slice(0, 2), args[2]);
    const bundle = readJSON(args[0]);
    const counts = sanitize(bundle, readJSON(args[1]));
    writeGzipJSON(destination, bundle);
    process.stdout.write(JSON.stringify({removed: counts}) + "\n");
  }
} catch (error) {
  // JSON parsing errors can contain source fragments; never echo input text.
  process.stderr.write(error instanceof SyntaxError ? "invalid JSON input\n" : `${error.message}\n`);
  process.exitCode = 1;
}
