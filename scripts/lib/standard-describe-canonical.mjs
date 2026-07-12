import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";

const UINT32_MAX = 0xffffffff;
// Ceilings intentionally exceed the owned 6,173,001-byte gzip and
// 129,343,530-byte expanded catalog while bounding maintenance generation.
export const STANDARD_DESCRIBE_MAX_COMPRESSED_INPUT_BYTES = 8 * 1024 * 1024;
export const STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES = 140 * 1024 * 1024;
const CATALOG_MAGIC = Buffer.from([0x47, 0x4c, 0x41, 0x44, 0x45, 0x43, 0x32, 0x00]);
const REVERSE_MAGIC = Buffer.from([0x47, 0x4c, 0x41, 0x44, 0x45, 0x52, 0x32, 0x00]);
const SET_ARRAY_KEYS = new Set(["referenceTo", "junctionReferenceTo", "junctionIdListNames"]);

function byteCompare(left, right) {
  return Buffer.compare(Buffer.from(String(left), "utf8"), Buffer.from(String(right), "utf8"));
}

function tupleCompare(keys) {
  return (left, right) => {
    for (const key of keys) {
      const compared = byteCompare(left?.[key] ?? "", right?.[key] ?? "");
      if (compared) return compared;
    }
    return 0;
  };
}

function requireNames(values, key, context) {
  for (const value of values ?? []) {
    const name = String(value?.[key] ?? "");
    if (!name.trim()) throw new Error(`empty ${context} name`);
  }
}

function normalize(value, key = "", context = "root", depth = 0) {
  if (Array.isArray(value)) {
    let values = value.map((child, index) => normalize(child, "", `${context}[${index}]`, depth + 1));
    if (SET_ARRAY_KEYS.has(key)) {
      values.sort((left, right) => byteCompare(typeof left === "string" ? left : JSON.stringify(left), typeof right === "string" ? right : JSON.stringify(right)));
    } else if (depth === 1 && key === "fields") {
      const nameKey = values.some((child) => Object.hasOwn(child ?? {}, "name")) ? "name" : "field";
      requireNames(values, nameKey, `${context}.fields`);
      const compareName = tupleCompare([nameKey]);
      values.sort((left, right) => compareName(left, right) || byteCompare(JSON.stringify(left), JSON.stringify(right)));
    } else if (depth === 1 && key === "childRelationships") {
      const compareTuple = tupleCompare(["childSObject", "field", "relationshipName"]);
      values.sort((left, right) => compareTuple(left, right) || byteCompare(JSON.stringify(left), JSON.stringify(right)));
    } else if (depth === 1 && key === "recordTypeInfos") {
      requireNames(values, "developerName", `${context}.recordTypeInfos`);
      const compareRecordType = tupleCompare(["developerName", "recordTypeId"]);
      values.sort((left, right) => compareRecordType(left, right) || byteCompare(JSON.stringify(left), JSON.stringify(right)));
    }
    return values;
  }
  if (!value || typeof value !== "object") return value;
  const output = {};
  for (const childKey of Object.keys(value).sort(byteCompare)) {
    output[childKey] = normalize(value[childKey], childKey, `${context}.${childKey}`, depth + 1);
  }
  return output;
}

export function canonicalJSONString(value) {
  return JSON.stringify(normalize(value));
}

function deterministicGzip(bytes) {
  const compressed = Buffer.from(gzipSync(bytes, {level: 9}));
  compressed.fill(0, 4, 8);
  compressed[9] = 255;
  return compressed;
}

function sha256(bytes) {
  return createHash("sha256").update(bytes).digest();
}

function packHeader(magic, count) {
  if (count > UINT32_MAX) throw new Error(`member count exceeds uint32: ${count}`);
  const header = Buffer.alloc(16);
  magic.copy(header, 0);
  header.writeUInt32BE(2, 8);
  header.writeUInt32BE(count, 12);
  return header;
}

function memberEntry(name, offset, uncompressed, compressed) {
  if (compressed.length > UINT32_MAX || uncompressed.length > UINT32_MAX) throw new Error(`member exceeds uint32 lengths: ${name}`);
  if (!Number.isSafeInteger(offset)) throw new Error(`pack offset is not exactly representable: ${offset}`);
  return {
    name,
    offset,
    compressedLength: compressed.length,
    uncompressedLength: uncompressed.length,
    sha256: sha256(uncompressed),
  };
}

function buildPack(magic, members) {
  const parts = [packHeader(magic, members.length)];
  const index = [];
  let offset = parts[0].length;
  for (const {name, bytes} of members) {
    const compressed = deterministicGzip(bytes);
    index.push(memberEntry(name, offset, bytes, compressed));
    parts.push(compressed);
    offset += compressed.length;
  }
  return {pack: Buffer.concat(parts), index};
}

function validateDescribes(describes) {
  if (!describes || typeof describes !== "object" || Array.isArray(describes)) throw new Error("input must contain a describes object");
  const seen = new Map();
  for (const [mapName, describe] of Object.entries(describes)) {
    const name = String(describe?.name || mapName);
    if (!mapName.trim() || !name.trim()) throw new Error("empty object name");
    const folded = name.toLowerCase();
    if (seen.has(folded)) throw new Error(`case-fold collision in objects: ${seen.get(folded)} and ${name}`);
    seen.set(folded, name);
    describe.name = name;
  }
}

function reverseMembers(describes) {
  const children = new Map();
  for (const describe of Object.values(describes)) {
    for (const relationship of describe.childRelationships ?? []) {
      if (relationship?.deprecatedAndHidden || !String(relationship?.childSObject ?? "").trim() || !String(relationship?.field ?? "").trim() || !String(relationship?.relationshipName ?? "").trim()) continue;
      const childName = String(relationship.childSObject);
      const childKey = childName.toLowerCase();
      let child = children.get(childKey);
      if (!child) {
        child = {name: childName, fields: new Map()};
        children.set(childKey, child);
      } else if (child.name !== childName) {
        throw new Error(`case-fold collision in reverse child objects: ${child.name} and ${childName}`);
      }
      const fieldName = String(relationship.field);
      const fieldKey = fieldName;
      let field = child.fields.get(fieldKey);
      if (!field) {
        field = {field: fieldName, names: new Set(), cascadeDelete: false, restrictedDelete: false};
        child.fields.set(fieldKey, field);
      }
      const relationshipName = String(relationship.relationshipName ?? "");
      if (relationshipName) field.names.add(relationshipName);
      field.cascadeDelete ||= relationship.cascadeDelete === true;
      field.restrictedDelete ||= relationship.restrictedDelete === true;
    }
  }
  return [...children.values()].sort((a, b) => byteCompare(a.name, b.name)).map((child) => {
    const fields = [...child.fields.values()].sort((a, b) => byteCompare(a.field, b.field)).map((field) => {
      const names = [...field.names].sort(byteCompare);
      return {
        field: field.field,
        relationshipName: names[0] ?? "",
        cascadeDelete: field.cascadeDelete,
        restrictedDelete: field.restrictedDelete,
        conflict: names.length > 1,
      };
    });
    return {name: child.name, bytes: Buffer.from(canonicalJSONString({childSObject: child.name, fields}))};
  });
}

function goByteArray(bytes) {
  return `[32]byte{${[...bytes].map((byte) => `0x${byte.toString(16).padStart(2, "0")}`).join(", ")}}`;
}

function goIndexLiteral(entry) {
  return `\t{Name: ${JSON.stringify(entry.name)}, Offset: ${entry.offset}, CompressedLength: ${entry.compressedLength}, UncompressedLength: ${entry.uncompressedLength}, SHA256: ${goByteArray(entry.sha256)}},`;
}

function generatedGoSource(catalog, reverse, catalogPack, reversePack) {
  const lookupOrder = (left, right) => byteCompare(left.name.toLowerCase(), right.name.toLowerCase()) || byteCompare(left.name, right.name);
  const catalogLookup = [...catalog].sort(lookupOrder);
  const reverseLookup = [...reverse].sort(lookupOrder);
  return `// Code generated by ../glade-tools/scripts/generate-standard-describe-pack.mjs; DO NOT EDIT.\n\npackage storage\n\nvar standardDescribeCatalogV2Index = []standardDescribeCatalogV2IndexEntry{\n${catalogLookup.map(goIndexLiteral).join("\n")}\n}\n\nvar standardDescribeChildRelationshipsV2Index = []standardDescribeCatalogV2IndexEntry{\n${reverseLookup.map(goIndexLiteral).join("\n")}\n}\n\nvar standardDescribeCatalogV2PackSHA256 = ${goByteArray(sha256(catalogPack))}\nvar standardDescribeChildRelationshipsV2PackSHA256 = ${goByteArray(sha256(reversePack))}\n`;
}

export function buildStandardDescribePacks(bundle) {
  const input = structuredClone(bundle);
  validateDescribes(input.describes);
  const catalogMembers = Object.values(input.describes)
    .sort((left, right) => byteCompare(left.name, right.name))
    .map((describe) => ({name: describe.name, bytes: Buffer.from(canonicalJSONString(describe))}));
  const reverse = reverseMembers(input.describes);
  const catalogBuilt = buildPack(CATALOG_MAGIC, catalogMembers);
  const reverseBuilt = buildPack(REVERSE_MAGIC, reverse);
  return {
    catalogPack: catalogBuilt.pack,
    reversePack: reverseBuilt.pack,
    index: catalogBuilt.index,
    reverseIndex: reverseBuilt.index,
    goSource: generatedGoSource(catalogBuilt.index, reverseBuilt.index, catalogBuilt.pack, reverseBuilt.pack),
  };
}
