import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";

const read = (path) => readFile(new URL(path, import.meta.url), "utf8").catch(() => "");

const readme = await read("../README.md");
const guide = await read("../docs/apex-language-rules.md");
const catalog = JSON.parse(await read("../docs/fixtures/apex-language-rules.json"));

test("Apex language-rule evidence is documented and discoverable", () => {
  const reservedCount = catalog.rules.filter((rule) => rule.id.startsWith("APEX-RESERVED-")).length;
  const acceptCount = catalog.rules.filter((rule) => rule.oracle === "accept").length;
  const rejectCount = catalog.rules.filter((rule) => rule.oracle === "reject").length;
  const supportedCount = catalog.rules.filter((rule) => rule.status === "supported").length;

  assert.equal(catalog.rules.length, 400);
  assert.equal(reservedCount, 121);
  assert.equal(acceptCount, 51);
  assert.equal(rejectCount, 349);
  assert.equal(supportedCount, catalog.rules.length);
  assert.ok(catalog.rules.some((rule) => rule.id === "APEX-RESERVED-CURRENCY"));

  assert.match(readme, /\[Apex language-rule evidence\]\(docs\/apex-language-rules\.md\)/);
  assert.match(guide, /^# Apex language-rule evidence/m);
  assert.match(guide, new RegExp(`${catalog.rules.length} checked rows`));
  assert.match(guide, new RegExp(`${reservedCount} reserved identifiers`));
  assert.match(guide, new RegExp(`${acceptCount} accept controls`));
  assert.match(guide, new RegExp(`${rejectCount} rejection controls`));
  assert.match(guide, new RegExp("All " + supportedCount + " rows currently have `supported` status"));
  assert.match(guide, /`currency`/);
  assert.match(guide, /glade-tools apex-rules validate/);
  assert.match(guide, /glade-tools apex-rules compare/);
  assert.match(guide, /`gladeCommit`/);
  assert.match(guide, /Salesforce scratch org/);
});
