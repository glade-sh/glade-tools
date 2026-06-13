import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

test("SObject overlay generator uses explicit property type for references", () => {
  const root = mkdtempSync(join(tmpdir(), "glade-sobject-overlay-"));
  const input = join(root, "apex-sobject-stubs");
  mkdirSync(input);
  const output = join(root, "overlay.go");

  writeFileSync(
    join(input, "Account.cls"),
    `/**
 * Schema object: Account (Account)
 * Plural label: Accounts
 */
global class Account extends SObject {
  global class SObjectFields {
    public SObjectField Id;
  }
  global Id Id { get; set; }
}
`,
  );
  writeFileSync(
    join(input, "Widget__c.cls"),
    `/**
 * Schema object: Widget (Widget)
 * Plural label: Widgets
 */
global class Widget__c extends SObject {
  global class SObjectFields {
    public SObjectField Id;
    public SObjectField ExternalId;
    public SObjectField AccountId;
    public SObjectField RunningUserEntityAccessId;
  }
  global Id Id { get; set; }
  global String ExternalId { get; set; }
  global Account AccountId { get; set; }
  /**
   * Parent relationship for AccountId
   */
  global Account Account { get; private set; }
  global String RunningUserEntityAccessId { get; private set; }
  /**
   * Parent relationship for RunningUserEntityAccessId
   */
  global UserEntityAccess RunningUserEntityAccess { get; private set; }
}
`,
  );

  const run = spawnSync("node", ["scripts/generate-sobject-stub-overlay.mjs", input, output], {
    cwd: new URL("..", import.meta.url),
    encoding: "utf8",
  });
  assert.equal(run.status, 0, run.stderr || run.stdout);

  const generated = readFileSync(output, "utf8");
  assert.match(generated, /"ExternalId":\s+Field\{APIName: "ExternalId", Label: "ExternalId", Type: FieldString, DisplayType: "STRING"\}/);
  assert.doesNotMatch(generated, /"ExternalId": Field\{[^}]*FieldReference/);
  assert.match(generated, /"AccountId":\s+Field\{[^}]*Type: FieldReference[^}]*ReferenceTo: \[\]string\{"Account"\}[^}]*RelationshipName: "Account"/);
  assert.match(generated, /"RunningUserEntityAccessId":\s+Field\{[^}]*Type: FieldReference[^}]*ReferenceTo: \[\]string\{"UserEntityAccess"\}[^}]*RelationshipName: "RunningUserEntityAccess"/);
});
