import {createHash} from "node:crypto";

// Shared by the reviewed-labels generator and the describe sanitizer.
// A reviewed-labels document binds labels to exact slots, never to a global set:
//   objects[Object] = {label, labelPlural, scopes: {name: label},
//                      fields: {Field: {label, picklist: {value: label}}}}
// It is script-generated from two fresh scratch-org captures, carries provenance
// and a sha256 over its canonical content. Never hand-edit it.

export const REVIEWED_LABELS_FORMAT = "glade-reviewed-labels/1";
export const REVIEWED_LABELS_GENERATOR = "generate-reviewed-standard-labels";

const customSuffix = /__(?:c|e|mdt|x|kav|b|r|s|share|history|feed|tag|ka|pe|changeevent|viewstat|votestat)$/i;
export const customEntity = {test: (value) => customSuffix.test(value) || (value.match(/__/g)?.length ?? 0) >= 2};
export const nameToken = /^[A-Za-z][A-Za-z0-9_]{0,254}$/;

const idToken = /(?<![A-Za-z0-9])(?=[A-Za-z0-9]*\d)(?:[A-Za-z0-9]{15}|[A-Za-z0-9]{18})(?![A-Za-z0-9])/;

// Label filter shared by both sides. It rejects anything that looks like org
// identity: addresses, hostnames, record ids, custom entity values.
export function reviewableLabel(value) {
  return typeof value === "string"
    && value.length > 0 && value.length <= 255
    && !/[\u0000-\u001f\u007f]/.test(value)
    && !value.includes("@")
    && !/\.salesforce\./i.test(value)
    && !idToken.test(value)
    && !value.includes("__")
    && !customEntity.test(value);
}

export function canonicalString(value) {
  if (Array.isArray(value)) return `[${value.map(canonicalString).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonicalString(value[key])}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

export function reviewedLabelsDigest(document) {
  const {sha256: _ignored, ...rest} = document;
  return createHash("sha256").update(canonicalString(rest)).digest("hex");
}

const fail = () => new Error("reviewed labels file is invalid");
const plain = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
const onlyKeys = (value, keys) => { if (!plain(value) || Object.keys(value).some((key) => !keys.includes(key))) throw fail(); };
const labelOrFail = (value) => { if (!reviewableLabel(value)) throw fail(); return value; };

// Validates a document and, when a pin is given, its digest. Returns the document.
export function validateReviewedLabels(document, pin) {
  onlyKeys(document, ["format", "provenance", "objects", "sha256"]);
  if (document.format !== REVIEWED_LABELS_FORMAT) throw fail();
  const provenance = document.provenance;
  onlyKeys(provenance, ["generator", "captureId", "apiVersion", "locale", "orgCount", "distinctSettingsAttested"]);
  if (provenance.generator !== REVIEWED_LABELS_GENERATOR || provenance.locale !== "en_US" || provenance.orgCount !== 2
    || provenance.distinctSettingsAttested !== true
    || typeof provenance.captureId !== "string" || !/^[A-Za-z0-9._-]{1,64}$/.test(provenance.captureId) || idToken.test(provenance.captureId)
    || typeof provenance.apiVersion !== "string" || !/^\d{2}\.0$/.test(provenance.apiVersion)) throw fail();
  if (typeof document.sha256 !== "string" || !/^[0-9a-f]{64}$/.test(document.sha256)) throw fail();
  if (document.sha256 !== reviewedLabelsDigest(document)) throw fail();
  if (typeof pin !== "string" || !/^[0-9a-f]{64}$/.test(pin) || pin !== document.sha256) throw fail();
  onlyKeys(document.objects, Object.keys(document.objects ?? {}));
  for (const [objectName, entry] of Object.entries(document.objects)) {
    if (!nameToken.test(objectName) || objectName.includes("__")) throw fail();
    onlyKeys(entry, ["label", "labelPlural", "scopes", "fields"]);
    for (const key of ["label", "labelPlural"]) if (key in entry) labelOrFail(entry[key]);
    if ("scopes" in entry) {
      onlyKeys(entry.scopes, Object.keys(entry.scopes));
      for (const [scope, label] of Object.entries(entry.scopes)) { if (!nameToken.test(scope)) throw fail(); labelOrFail(label); }
    }
    if ("fields" in entry) {
      onlyKeys(entry.fields, Object.keys(entry.fields));
      for (const [fieldName, field] of Object.entries(entry.fields)) {
        if (!nameToken.test(fieldName) || fieldName.includes("__")) throw fail();
        onlyKeys(field, ["label", "picklist"]);
        if ("label" in field) labelOrFail(field.label);
        if ("picklist" in field) {
          onlyKeys(field.picklist, Object.keys(field.picklist));
          for (const [value, label] of Object.entries(field.picklist)) {
            if (!reviewableLabel(value)) throw fail();
            labelOrFail(label);
          }
        }
      }
    }
  }
  return document;
}

// Returns the bound label for a slot, or undefined. Path: [object, ...slot].
export function boundLabel(document, objectName, ...slot) {
  if (!document || !Object.hasOwn(document.objects, objectName)) return undefined;
  let node = document.objects[objectName];
  for (const step of slot) {
    if (node === null || typeof node !== "object" || !Object.hasOwn(node, step)) return undefined;
    node = node[step];
  }
  return typeof node === "string" ? node : undefined;
}
