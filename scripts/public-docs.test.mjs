import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";

const read = (path) => readFile(new URL(`../${path}`, import.meta.url), "utf8");
const [readme, security, contributing] = await Promise.all([
  read("README.md"),
  read("SECURITY.md"),
  read("CONTRIBUTING.md"),
]);

test("public docs name the current tested product and plugin pair", () => {
  assert.match(readme, /performance\s+0\.2\.13 with product v0\.2\.14/);
  assert.doesNotMatch(readme, /performance\s+0\.2\.12 with product v0\.2\.13/);
});

test("published contacts are not described as pending verification", () => {
  for (const document of [security, contributing]) {
    assert.doesNotMatch(document, /verify(?: that| this)? alias|must be verified/i);
  }
});
