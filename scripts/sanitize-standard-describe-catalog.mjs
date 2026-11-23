#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import {randomUUID} from "node:crypto";
import {gunzipSync, gzipSync} from "node:zlib";
import {
  STANDARD_DESCRIBE_MAX_COMPRESSED_INPUT_BYTES,
  STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES,
} from "./lib/standard-describe-canonical.mjs";

// Allowlist sanitizer. The output is rebuilt from scratch: only keys named in
// the specs below are copied, and only after their shape and value pass.
// Any key the specs do not know aborts the run with no output (fail closed).
//
// The reviewed string allowlist is GLOBAL: one flat set of strings drawn from
// the public catalog (picklist values and labels). A string passes at any
// position that accepts reviewed strings if it is in that set. It does not say
// which object, field or slot the string belongs to.
//
// Labels (describe, plural, field, picklist entry, supportedScopes) are kept
// only when they equal a reviewed public-catalog string; otherwise the key is
// dropped. Org-renamed labels therefore never pass, and standard labels are
// not guessed from the org export.
//
// Error messages are fixed text. They never include input.

// Custom entity suffixes identify org additions even when no namespace is present,
// in any letter case. A single namespace separator alone is also used by standard
// platform values, so a value needs a known custom suffix or two separators.
const customSuffix = /__(?:c|e|mdt|x|kav|b|r|s|share|history|feed|tag|ka|pe|changeevent|viewstat|votestat)$/i;
const customEntity = {test: (value) => customSuffix.test(value) || (value.match(/__/g)?.length ?? 0) >= 2};
const customName = (value) => typeof value === "string" && value.includes("__");

const TOP_DROP = new Set(["orgId", "instanceUrl", "username", "summary"]);
const placeholderHost = "https://schema-org-001.example.com";
const malformed = () => new Error("malformed describe catalog shape");
const unknownKey = () => new Error("catalog contains an unknown key");

const isObject = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
const need = (condition) => { if (!condition) throw malformed(); };
const needObject = (value) => need(isObject(value));
const needArray = (value) => need(Array.isArray(value));
const needStrings = (value) => { needArray(value); need(value.every((entry) => typeof entry === "string")); };

const identifier = /^[A-Za-z0-9_:.#\- ]{1,255}$/;
const relativeURL = /^\/services\/data\/v\d{2}\.\d\/[A-Za-z0-9{}/._-]*$/;
const hostURL = /^https:\/\/[A-Za-z0-9.-]+(\/\{ID\}(?:\/e)?|\/[A-Za-z0-9]{3}\/e)$/;

// Normalizes a url to a relative service path or the public placeholder host.
function safeURL(value) {
  need(typeof value === "string");
  if (value.includes("__")) return undefined;
  if (relativeURL.test(value)) return value;
  const match = hostURL.exec(value);
  return match ? placeholderHost + match[1] : undefined;
}

const KINDS = {
  bool: (value) => typeof value === "boolean",
  boolNull: (value) => value === null || typeof value === "boolean",
  num: (value) => typeof value === "number" && Number.isFinite(value),
  numNull: (value) => value === null || (typeof value === "number" && Number.isFinite(value)),
  str: (value) => typeof value === "string" && identifier.test(value),
  strNull: (value) => value === null || (typeof value === "string" && identifier.test(value)),
};

// Copies one object through a spec: kinds map to scalar validators, `drop` keys are
// accepted and discarded, and `special` keys are handled by the caller.
function project(source, spec) {
  needObject(source);
  const out = {};
  for (const [key, value] of Object.entries(source)) {
    if (spec.drop?.includes(key) || spec.special?.includes(key)) continue;
    const kind = Object.keys(KINDS).find((candidate) => spec[candidate]?.includes(key));
    if (!kind) throw unknownKey();
    need(KINDS[kind](value));
    if (typeof value === "string" && value.includes("__")) continue;
    out[key] = value;
  }
  return out;
}

const DESCRIBE_SPEC = {
  bool: ["activateable", "compactLayoutable", "createable", "custom", "customSetting", "deepCloneable", "deletable", "deprecatedAndHidden", "feedEnabled", "hasSubtypes", "isInterface", "isSubtype", "layoutable", "mergeable", "mruEnabled", "queryable", "replicateable", "retrieveable", "searchLayoutable", "searchable", "triggerable", "undeletable", "updateable"],
  boolNull: ["listviewable", "lookupLayoutable"],
  str: ["name", "sobjectDescribeOption"],
  strNull: ["associateEntityType", "associateParentEntity", "keyPrefix"],
  special: ["label", "labelPlural", "fields", "childRelationships", "recordTypeInfos", "actionOverrides", "namedLayoutInfos", "supportedScopes", "urls", "networkScopeFieldName", "defaultImplementation", "extendedBy", "extendsInterfaces", "implementedBy", "implementsInterfaces"],
};
const NAME_LISTS = ["defaultImplementation", "extendedBy", "extendsInterfaces", "implementedBy", "implementsInterfaces"];
const FIELD_SPEC = {
  bool: ["aggregatable", "aiPredictionField", "autoNumber", "calculated", "cascadeDelete", "caseSensitive", "createable", "custom", "dependentPicklist", "deprecatedAndHidden", "defaultedOnCreate", "displayLocationInDecimal", "encrypted", "externalId", "filterable", "formulaTreatNullNumberAsZero", "groupable", "highScaleNumber", "htmlFormatted", "idLookup", "nameField", "namePointing", "nillable", "permissionable", "polymorphicForeignKey", "queryByDistance", "restrictedDelete", "restrictedPicklist", "searchPrefilterable", "sortable", "unique", "updateable", "writeRequiresMasterRead"],
  num: ["byteLength", "digits", "length", "precision", "scale"],
  numNull: ["relationshipOrder"],
  str: ["name", "type", "soapType"],
  strNull: ["compoundFieldName", "extraTypeInfo", "relationshipName"],
  // Free text and org-authored values never pass.
  drop: ["defaultValue", "defaultValueFormula", "inlineHelpText", "calculatedFormula"],
  special: ["label", "controllerName", "picklistValues", "referenceTo", "referenceTargetField", "mask", "maskType", "filteredLookupInfo"],
};
const NULL_ONLY_FIELD_KEYS = ["referenceTargetField", "mask", "maskType", "filteredLookupInfo"];
const PICKLIST_SPEC = {bool: ["active"], drop: ["defaultValue"], special: ["label", "validFor", "value"]};
const CHILD_SPEC = {bool: ["cascadeDelete", "deprecatedAndHidden", "restrictedDelete"], str: ["childSObject", "field"], strNull: ["relationshipName"], special: ["junctionIdListNames", "junctionReferenceTo"]};
const RECORD_TYPE_SPEC = {bool: ["active", "available", "defaultRecordTypeMapping", "master"], special: ["developerName", "name", "recordTypeId", "urls"]};
const ACTION_SPEC = {str: ["formFactor", "name"], bool: ["isAvailableInTouch"], drop: ["url", "pageId", "label"], special: []};
const URL_KEYS = new Set(["rowTemplate", "describe", "sobject", "compactLayouts", "approvalLayouts", "uiDetailTemplate", "uiEditTemplate", "listviews", "uiNewRecord", "quickActions", "layouts", "eventSchema", "caseArticleSuggestions", "caseRowArticleSuggestions", "eventSeriesUpdates", "push", "namedLayouts", "passwordUtilities", "workOrderRowArticleSuggestions", "workOrderArticleSuggestions", "workOrderLineItemRowArticleSuggestions", "workOrderLineItemArticleSuggestions"]);
const MASTER_RECORD_TYPE_ID = "012000000000000AAA";

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
  const add = (value) => { if (typeof value === "string" && value !== "" && !customEntity.test(value)) values.add(value); };
  for (const describe of Object.values(describesOf(bundle))) {
    needObject(describe);
    add(describe.label);
    add(describe.labelPlural);
    for (const scope of describe.supportedScopes ?? []) add(scope?.label);
    for (const field of describe.fields ?? []) {
      add(field?.label);
      for (const entry of field?.picklistValues ?? []) { add(entry?.value); add(entry?.label); }
    }
  }
  return [...values].sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)));
}

function sanitize(bundle, reviewedValues) {
  if (!Array.isArray(reviewedValues) || reviewedValues.some((value) => typeof value !== "string" || customEntity.test(value))) {
    throw new Error("allowlist must contain reviewed standard string values only");
  }
  needObject(bundle);
  for (const key of Object.keys(bundle)) {
    if (key !== "describes" && !TOP_DROP.has(key)) throw unknownKey();
  }
  const allowed = new Set(reviewedValues);
  const reviewed = (value) => typeof value === "string" && !customEntity.test(value) && allowed.has(value);
  const label = (value) => { need(value === null || typeof value === "string"); return reviewed(value); };
  const describes = describesOf(bundle);
  const counts = {objects: 0, fields: 0, references: 0, childRows: 0, recordTypes: 0, picklists: 0, labels: 0, urls: 0, dropped: 0};
  const keepLabel = (out, key, value) => { if (label(value)) out[key] = value; else counts.labels++; };
  const keepURL = (value) => {
    const url = safeURL(value);
    if (url === undefined) counts.urls++;
    return url;
  };
  const result = {};
  for (const [objectName, describe] of Object.entries(describes)) {
    needObject(describe);
    const out = project(describe, DESCRIBE_SPEC);
    for (const key of ["fields", "childRelationships", "recordTypeInfos", "actionOverrides", "namedLayoutInfos", "supportedScopes"]) {
      if (key in describe) needArray(describe[key]);
    }
    if (customName(objectName) || customName(describe.name) || describe.custom === true) {
      counts.objects++;
      continue;
    }
    for (const key of ["label", "labelPlural"]) if (key in describe) keepLabel(out, key, describe[key]);
    for (const key of NAME_LISTS) {
      if (!(key in describe)) continue;
      const value = describe[key];
      if (value === null) out[key] = null;
      else if (typeof value === "string") out[key] = customName(value) ? null : value;
      else { needStrings(value); out[key] = value.filter((entry) => !customName(entry)); }
    }
    if ("networkScopeFieldName" in describe) {
      const value = describe.networkScopeFieldName;
      need(value === null || typeof value === "string");
      if (customName(value)) counts.dropped++; else out.networkScopeFieldName = value;
    }
    if ("urls" in describe) {
      needObject(describe.urls);
      out.urls = {};
      for (const [key, value] of Object.entries(describe.urls)) {
        if (!URL_KEYS.has(key)) throw unknownKey();
        const url = keepURL(value);
        if (url !== undefined) out.urls[key] = url;
      }
    }
    if ("supportedScopes" in describe) {
      out.supportedScopes = describe.supportedScopes.map((scope) => {
        needObject(scope);
        const row = {};
        for (const [key, value] of Object.entries(scope)) {
          if (key === "label") keepLabel(row, key, value);
          else if (key === "name") { need(KINDS.str(value)); row.name = value; }
          else throw unknownKey();
        }
        return row;
      });
    }
    if ("actionOverrides" in describe) {
      out.actionOverrides = describe.actionOverrides.map((row) => {
        const copy = project(row, ACTION_SPEC);
        if ("label" in row) keepLabel(copy, "label", row.label);
        if ("url" in row || "pageId" in row) counts.dropped++;
        return copy;
      });
    }
    if ("namedLayoutInfos" in describe) {
      // Named layouts are org-authored. Validate the shape, then keep none.
      for (const row of describe.namedLayoutInfos) {
        needObject(row);
        for (const key of Object.keys(row)) if (key !== "name" && key !== "urls") throw unknownKey();
        counts.dropped++;
      }
      out.namedLayoutInfos = [];
    }
    if ("fields" in describe) {
      const rows = describe.fields.map((field) => {
        needObject(field);
        const row = project(field, FIELD_SPEC);
        for (const key of NULL_ONLY_FIELD_KEYS) {
          if (!(key in field)) continue;
          need(field[key] === null);
          row[key] = null;
        }
        if ("label" in field) keepLabel(row, "label", field.label);
        if ("referenceTo" in field) { needStrings(field.referenceTo); row.referenceTo = field.referenceTo; }
        if ("picklistValues" in field) {
          needArray(field.picklistValues);
          row.picklistValues = field.picklistValues.map((entry) => {
            const item = project(entry, PICKLIST_SPEC);
            need(typeof entry.value === "string");
            item.value = entry.value;
            if ("label" in entry) keepLabel(item, "label", entry.label);
            if ("validFor" in entry) {
              need(entry.validFor === null || (typeof entry.validFor === "string" && /^[A-Za-z0-9+/=]*$/.test(entry.validFor)));
              item.validFor = entry.validFor;
            }
            return item;
          });
        }
        return {row, field};
      });
      const standard = rows.filter(({row, field}) => !(field.custom === true || customName(field.name) || customName(row.name)));
      counts.fields += rows.length - standard.length;
      const names = new Set(standard.map(({field}) => field.name));
      out.fields = standard.map(({row, field}) => {
        if ("controllerName" in field) {
          const controller = field.controllerName;
          need(controller === null || typeof controller === "string");
          if (typeof controller === "string" && names.has(controller)) row.controllerName = controller;
          else if (controller !== null) counts.dropped++;
          else row.controllerName = null;
        }
        if (row.picklistValues) {
          row.picklistValues = row.picklistValues.filter((entry) => {
            if (entry.value !== undefined && reviewed(entry.value)) return true;
            counts.picklists++;
            return false;
          });
          // Dependency bitmaps only pass for fields with a standard controller.
          for (const entry of row.picklistValues) if (typeof entry.validFor === "string" && !row.controllerName) { delete entry.validFor; counts.dropped++; }
        }
        if (row.referenceTo) {
          row.referenceTo = row.referenceTo.filter((target) => {
            if (!customName(target)) return true;
            counts.references++;
            return false;
          });
        }
        return row;
      });
    }
    if ("childRelationships" in describe) {
      out.childRelationships = [];
      for (const source of describe.childRelationships) {
        const row = project(source, CHILD_SPEC);
        for (const key of ["junctionIdListNames", "junctionReferenceTo"]) {
          if (!(key in source)) continue;
          needStrings(source[key]);
          row[key] = source[key].filter((entry) => !customName(entry));
        }
        if ([source.childSObject, source.field, source.relationshipName].some(customName)) { counts.childRows++; continue; }
        out.childRelationships.push(row);
      }
    }
    if ("recordTypeInfos" in describe) {
      out.recordTypeInfos = [];
      for (const source of describe.recordTypeInfos) {
        const row = project(source, RECORD_TYPE_SPEC);
        need(source.master === undefined || typeof source.master === "boolean");
        if (source.master !== true) { counts.recordTypes++; continue; }
        // Master is the standard record type: its name and id are fixed values.
        if (source.developerName === "Master") row.developerName = "Master";
        if (source.name === "Master") row.name = "Master";
        if (source.recordTypeId === MASTER_RECORD_TYPE_ID) row.recordTypeId = source.recordTypeId;
        if ("urls" in source) {
          needObject(source.urls);
          const urls = {};
          for (const [key, value] of Object.entries(source.urls)) {
            if (key !== "layout") throw unknownKey();
            const url = source.recordTypeId === MASTER_RECORD_TYPE_ID && typeof value === "string" && value.endsWith("/" + MASTER_RECORD_TYPE_ID) ? keepURL(value) : undefined;
            if (url !== undefined) urls.layout = url; else counts.urls++;
          }
          row.urls = urls;
        }
        out.recordTypeInfos.push(row);
      }
    }
    result[objectName] = out;
  }
  bundle.describes = result;
  for (const key of Object.keys(bundle)) if (key !== "describes") { delete bundle[key]; counts.dropped++; }
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
