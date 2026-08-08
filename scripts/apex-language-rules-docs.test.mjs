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
  const gapCount = catalog.rules.filter((rule) => rule.status === "confirmed-gap").length;
  const pendingCount = catalog.rules.filter((rule) => rule.status === "oracle-pending").length;

  assert.equal(catalog.rules.length, 422);
  assert.equal(reservedCount, 121);
  assert.equal(acceptCount, 68);
  assert.equal(rejectCount, 354);
  assert.equal(supportedCount, 422);
  assert.equal(gapCount, 0);
  assert.equal(pendingCount, 0);
  assert.ok(catalog.rules.some((rule) => rule.id === "APEX-RESERVED-CURRENCY"));

  assert.match(readme, /\[Apex language-rule evidence\]\(docs\/apex-language-rules\.md\)/);
  assert.match(guide, /^# Apex language-rule evidence/m);
  assert.match(guide, new RegExp(`${catalog.rules.length} checked rows`));
  assert.match(guide, new RegExp(`${reservedCount} reserved identifiers`));
  assert.match(guide, new RegExp(`${acceptCount} accept controls`));
  assert.match(guide, new RegExp(`${rejectCount} rejection controls`));
  assert.match(guide, new RegExp(`${supportedCount} supported rows`));
  assert.match(guide, new RegExp(`${gapCount} confirmed Glade gaps`));
  assert.match(guide, new RegExp(`${pendingCount} oracle-pending rows`));
  assert.match(guide, /`currency`/);
  assert.match(guide, /glade-tools apex-rules validate/);
  assert.match(guide, /glade-tools apex-rules compare/);
  assert.match(guide, /`gladeCommit`/);
  assert.match(guide, /Salesforce scratch org/);
});
