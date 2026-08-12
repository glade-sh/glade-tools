import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const workflow = readFileSync(
  new URL("../.github/workflows/ci.yml", import.meta.url),
  "utf8",
);

test("CI does not duplicate pull request checks on feature branch pushes", () => {
  assert.match(
    workflow,
    /push:\n\s+branches:\n\s+- main\n\s+tags:\n\s+- "\*"\n\s+pull_request:/,
  );
});

test("CI checks product evidence against the catalog-pinned Glade commit", () => {
  assert.match(workflow, /jq -r '\.gladeCommit'/);
  assert.match(workflow, /git -C \.\.\/glade rev-parse HEAD/);
  assert.doesNotMatch(workflow, /RELEASE_REF/);
  assert.doesNotMatch(workflow, /if:.*refs\/tags/);
});

test("CI uses a bounded timeout long enough for the release proof", () => {
  assert.match(workflow, /runs-on: ubuntu-latest\n\s+timeout-minutes: 15/);
  assert.match(workflow, /test:[\s\S]*?- run: scripts\/release-check\.sh/);
});

test("CI runs only the Darwin assurance package on a bounded macOS job", () => {
  assert.match(workflow, /assurance:\n\s+runs-on: macos-14\n\s+timeout-minutes: 15/);
  assert.match(workflow, /assurance:[\s\S]*?- run: go test -count=1 \.\/internal\/corpusassurance/);
  assert.doesNotMatch(workflow, /assurance:[\s\S]*?- run: scripts\/release-check\.sh/);
});

test("CI does not install a fake Salesforce CLI into a system path", () => {
  assert.doesNotMatch(workflow, /\/usr\/local\/bin\/sf/);
  assert.doesNotMatch(workflow, /sudo install/);
});
