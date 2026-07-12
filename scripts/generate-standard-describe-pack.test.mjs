import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { execFileSync, spawn, spawnSync } from "node:child_process";
import { once } from "node:events";
import { gzipSync, gunzipSync } from "node:zlib";

import {
  buildStandardDescribePacks,
  canonicalJSONString,
  STANDARD_DESCRIBE_MAX_COMPRESSED_INPUT_BYTES,
  STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES,
} from "./lib/standard-describe-canonical.mjs";

const fixturePath = new URL("./testdata/standard-describe-pack/catalog.json", import.meta.url);
const fixture = JSON.parse(fs.readFileSync(fixturePath, "utf8"));
const generatorPath = new URL("./generate-standard-describe-pack.mjs", import.meta.url).pathname;

function shuffled(value) {
  if (Array.isArray(value)) return value.map(shuffled);
  if (!value || typeof value !== "object") return value;
  return Object.fromEntries(Object.entries(value).reverse().map(([key, child]) => [key, shuffled(child)]));
}

function decodeMember(pack, entry, reverse = false) {
  const offset = entry.offset;
  const length = entry.compressedLength;
  return JSON.parse(gunzipSync(pack.subarray(Number(offset), Number(offset) + length)));
}

function runCLIFailure(args, pattern) {
  const result = spawnSync(process.execPath, [generatorPath, ...args], {encoding: "utf8"});
  assert.notEqual(result.status, 0, `CLI unexpectedly succeeded: ${args.join(" ")}`);
  if (pattern) assert.match(result.stderr, pattern);
  return result;
}

test("canonical output ignores map and named-array order", () => {
  const reordered = structuredClone(fixture);
  reordered.describes = Object.fromEntries(Object.entries(reordered.describes).reverse());
  for (const describe of Object.values(reordered.describes)) {
    describe.fields?.reverse();
    describe.childRelationships?.reverse();
    describe.recordTypeInfos?.reverse();
  }
  const first = buildStandardDescribePacks(fixture);
  const second = buildStandardDescribePacks(shuffled(reordered));
  assert.deepEqual(second.catalogPack, first.catalogPack);
  assert.deepEqual(second.reversePack, first.reversePack);
  assert.equal(second.goSource, first.goSource);
});

test("recognized array names are structural only at the describe root", () => {
  const describe = structuredClone(fixture.describes.Zulu);
  describe.unknown = {
    fields: [{name: "B"}, {name: "A"}],
    childRelationships: [{childSObject: "B"}, {childSObject: "A"}],
    recordTypeInfos: [{developerName: "B"}, {developerName: "A"}],
  };
  const canonical = JSON.parse(canonicalJSONString(describe));
  assert.deepEqual(canonical.unknown, describe.unknown);
  for (const key of ["fields", "childRelationships", "recordTypeInfos"]) {
    const reversed = structuredClone(describe);
    reversed.unknown[key].reverse();
    assert.notEqual(canonicalJSONString(reversed), canonicalJSONString(describe), `nested ${key} order must remain observable`);
  }
});

test("picklist order and false null absent remain significant", () => {
  const base = canonicalJSONString(fixture.describes.Zulu);
  const picklist = structuredClone(fixture.describes.Zulu);
  picklist.fields[0].picklistValues.reverse();
  assert.notEqual(canonicalJSONString(picklist), base);
  const baseBuilt = buildStandardDescribePacks(fixture);
  const picklistBundle = structuredClone(fixture);
  picklistBundle.describes.Zulu = picklist;
  assert.notDeepEqual(buildStandardDescribePacks(picklistBundle).index.find((entry) => entry.name === "Zulu").sha256, baseBuilt.index.find((entry) => entry.name === "Zulu").sha256);
  for (const value of [false, null, undefined]) {
    const changed = structuredClone(fixture.describes.Zulu);
    if (value === undefined) delete changed.queryable;
    else changed.queryable = value;
    assert.notEqual(canonicalJSONString(changed), canonicalJSONString({...fixture.describes.Zulu, queryable: true}));
  }
  assert.notEqual(canonicalJSONString({...fixture.describes.Zulu, queryable: false}), canonicalJSONString({...fixture.describes.Zulu, queryable: null}));
  const absent = structuredClone(fixture.describes.Zulu);
  delete absent.queryable;
  assert.notEqual(canonicalJSONString({...fixture.describes.Zulu, queryable: false}), canonicalJSONString(absent));
});

test("rejects case-fold collisions and preserves duplicate set multiplicity", () => {
  const collision = structuredClone(fixture);
  collision.describes.alpha = {...collision.describes.Alpha, name: "alpha"};
  assert.throws(() => buildStandardDescribePacks(collision), /case-fold collision/);
  const nestedNames = structuredClone(fixture);
  nestedNames.describes.Zulu.fields.push({name: "parentID", type: "string"});
  assert.equal(decodeMember(buildStandardDescribePacks(nestedNames).catalogPack, buildStandardDescribePacks(nestedNames).index.find((entry) => entry.name === "Zulu")).fields.length, 3);
  const duplicateReference = structuredClone(fixture);
  duplicateReference.describes.Zulu.fields[1].referenceTo = ["User", "Group", "Group"];
  const built = buildStandardDescribePacks(duplicateReference);
  const zulu = built.index.find((entry) => entry.name === "Zulu");
  assert.deepEqual(decodeMember(built.catalogPack, zulu).fields.find((field) => field.name === "ParentId").referenceTo, ["Group", "Group", "User"]);
  duplicateReference.describes.Zulu.fields[1].referenceTo.reverse();
  assert.deepEqual(buildStandardDescribePacks(duplicateReference).catalogPack, built.catalogPack);
});

test("reverse conflicts and delete flags are sticky", () => {
  const built = buildStandardDescribePacks(fixture);
  const zulu = built.reverseIndex.find((entry) => entry.name === "Zulu");
  const reverse = decodeMember(built.reversePack, zulu, true);
  assert.deepEqual(reverse.fields, [{field: "ParentId", relationshipName: "First", cascadeDelete: true, restrictedDelete: true, conflict: true}]);
});

test("reverse members preserve nested field spelling and reject child index collisions", () => {
  const nested = structuredClone(fixture);
  nested.describes.Alpha.childRelationships.push({childSObject: "Zulu", field: "parentID", relationshipName: "Third", cascadeDelete: false, restrictedDelete: true, deprecatedAndHidden: false});
  const built = buildStandardDescribePacks(nested);
  const zulu = built.reverseIndex.find((entry) => entry.name === "Zulu");
  assert.deepEqual(decodeMember(built.reversePack, zulu, true).fields.map((field) => field.field), ["ParentId", "parentID"]);

  const collision = structuredClone(nested);
  collision.describes.Alpha.childRelationships.push({childSObject: "zULU", field: "OtherId", relationshipName: "Other", deprecatedAndHidden: false});
  assert.throws(() => buildStandardDescribePacks(collision), /case-fold collision in reverse child objects/);
});

test("every member is independently decodable at exact bounds", () => {
  const built = buildStandardDescribePacks(fixture);
  for (const entry of built.index) {
    assert.equal(decodeMember(built.catalogPack, entry).name, entry.name);
  }
  for (const entry of built.reverseIndex) assert.equal(decodeMember(built.reversePack, entry, true).childSObject, entry.name);
});

test("two complete CLI runs are byte-identical", () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), "standard-describe-pack-"));
  try {
    const outputs = [];
    for (const suffix of ["a", "b"]) {
      const files = ["catalog.pack", "reverse.pack", "index.go"].map((name) => path.join(temp, `${suffix}-${name}`));
      execFileSync(process.execPath, [generatorPath, fixturePath.pathname, ...files]);
      outputs.push(files.map((file) => fs.readFileSync(file)));
    }
    for (let index = 0; index < outputs[0].length; index++) assert.deepEqual(outputs[1][index], outputs[0][index]);
  } finally {
    fs.rmSync(temp, {recursive: true, force: true});
  }
});

test("CLI rejects input/output and same-inode output aliases without changing files", () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), "standard-describe-alias-"));
  try {
    const input = path.join(temp, "input.json");
    const catalog = path.join(temp, "catalog.pack");
    const reverseAlias = path.join(temp, "reverse.pack");
    const index = path.join(temp, "index.go");
    fs.copyFileSync(fixturePath, input);
    const inputBefore = fs.readFileSync(input);
    runCLIFailure([input, input, reverseAlias, index], /alias|distinct/i);
    assert.deepEqual(fs.readFileSync(input), inputBefore);

    fs.writeFileSync(catalog, "catalog-before");
    const outputBefore = fs.readFileSync(catalog);
    runCLIFailure([input, catalog, catalog, index], /alias|distinct/i);
    assert.deepEqual(fs.readFileSync(catalog), outputBefore);
    assert.equal(fs.existsSync(index), false);

    fs.linkSync(catalog, reverseAlias);
    runCLIFailure([input, catalog, reverseAlias, index], /alias|distinct/i);
    assert.deepEqual(fs.readFileSync(catalog), outputBefore);
    assert.deepEqual(fs.readFileSync(reverseAlias), outputBefore);
    assert.equal(fs.existsSync(index), false);
  } finally {
    fs.rmSync(temp, {recursive: true, force: true});
  }
});

test("CLI publish failure preserves every destination and leaves no temp files", () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), "standard-describe-publish-"));
  try {
    const catalog = path.join(temp, "catalog.pack");
    const reverse = path.join(temp, "reverse.pack");
    fs.writeFileSync(catalog, "catalog-before");
    fs.writeFileSync(reverse, "reverse-before");
    const before = [fs.readFileSync(catalog), fs.readFileSync(reverse)];
    const invalidIndex = path.join(temp, "missing", "index.go");
    runCLIFailure([fixturePath.pathname, catalog, reverse, invalidIndex]);
    assert.deepEqual(fs.readFileSync(catalog), before[0]);
    assert.deepEqual(fs.readFileSync(reverse), before[1]);
    assert.deepEqual(fs.readdirSync(temp).filter((name) => name.includes(".tmp-") || name.includes(".bak-")), []);

    const failingFinal = path.join(temp, `${"x".repeat(240)}.go`);
    runCLIFailure([fixturePath.pathname, catalog, reverse, failingFinal]);
    assert.deepEqual(fs.readFileSync(catalog), before[0]);
    assert.deepEqual(fs.readFileSync(reverse), before[1]);
    assert.equal(fs.existsSync(failingFinal), false);
    assert.deepEqual(fs.readdirSync(temp).filter((name) => name.includes(".tmp-") || name.includes(".bak-")), []);
  } finally {
    fs.rmSync(temp, {recursive: true, force: true});
  }
});

test("CLI interruption leaves destinations unchanged and no temp files", async () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), "standard-describe-interrupt-"));
  try {
    const input = path.join(temp, "large.json");
    const describes = {};
    for (let index = 0; index < 10000; index++) {
      const name = `Object${String(index).padStart(5, "0")}`;
      describes[name] = {name, fields: [], childRelationships: [], recordTypeInfos: []};
    }
    fs.writeFileSync(input, JSON.stringify({describes}));
    const outputs = ["catalog.pack", "reverse.pack", "index.go"].map((name) => path.join(temp, name));
    const before = outputs.map((file, index) => {
      const value = Buffer.from(`before-${index}`);
      fs.writeFileSync(file, value);
      return value;
    });
    const child = spawn(process.execPath, [generatorPath, input, ...outputs], {stdio: "ignore"});
    await new Promise((resolve) => setTimeout(resolve, 100));
    assert.equal(child.kill("SIGTERM"), true);
    const [code, signal] = await once(child, "exit");
    assert.ok(signal === "SIGTERM" || code === 143, `unexpected interruption exit code=${code} signal=${signal}`);
    for (let index = 0; index < outputs.length; index++) assert.deepEqual(fs.readFileSync(outputs[index]), before[index]);
    assert.deepEqual(fs.readdirSync(temp).filter((name) => name.includes(".tmp-") || name.includes(".bak-")), []);
  } finally {
    fs.rmSync(temp, {recursive: true, force: true});
  }
});

test("CLI rejects compressed and raw expansion limits before creating outputs", () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), "standard-describe-limit-"));
  try {
    const outputs = ["catalog.pack", "reverse.pack", "index.go"].map((name) => path.join(temp, name));
    const compressedOversize = path.join(temp, "compressed.gz");
    const compressed = Buffer.alloc(STANDARD_DESCRIBE_MAX_COMPRESSED_INPUT_BYTES + 1);
    compressed[0] = 0x1f;
    compressed[1] = 0x8b;
    fs.writeFileSync(compressedOversize, compressed);
    runCLIFailure([compressedOversize, ...outputs], /compressed input.*limit/i);
    assert.deepEqual(outputs.map((file) => fs.existsSync(file)), [false, false, false]);

    const rawOversize = path.join(temp, "expanded.gz");
    fs.writeFileSync(rawOversize, gzipSync(Buffer.alloc(STANDARD_DESCRIBE_MAX_RAW_INPUT_BYTES + 1), {level: 1}));
    runCLIFailure([rawOversize, ...outputs], /raw input.*limit/i);
    assert.deepEqual(outputs.map((file) => fs.existsSync(file)), [false, false, false]);
    assert.deepEqual(fs.readdirSync(temp).filter((name) => name.includes(".tmp-") || name.includes(".bak-")), []);
  } finally {
    fs.rmSync(temp, {recursive: true, force: true});
  }
});
