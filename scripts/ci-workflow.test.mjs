import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const workflow = readFileSync(
  new URL("../.github/workflows/ci.yml", import.meta.url),
  "utf8",
);
const salesforceCorrectnessWorkflow = readFileSync(
  new URL("../.github/workflows/salesforce-correctness.yml", import.meta.url),
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

test("CI does not duplicate assurance already covered by release-check", () => {
  assert.doesNotMatch(workflow, /^  assurance:/m);
  assert.match(workflow, /test:[\s\S]*?- run: scripts\/release-check\.sh/);
  assert.match(
    workflow,
    /macos-release-upload:[\s\S]*?go test \.\/internal\/corpusassurance/,
  );
});

test("CI does not install a fake Salesforce CLI into a system path", () => {
  assert.doesNotMatch(workflow, /\/usr\/local\/bin\/sf/);
  assert.doesNotMatch(workflow, /sudo install/);
});

test("manual Salesforce correctness uses attempt-unique server cleanup authority", () => {
  assert.match(salesforceCorrectnessWorkflow, /GITHUB_RUN_ID.*GITHUB_RUN_ATTEMPT/);
  assert.match(salesforceCorrectnessWorkflow, /--name "\$SF_SCRATCH_MARKER"/);
  assert.match(salesforceCorrectnessWorkflow, /FROM ScratchOrgInfo WHERE OrgName/);
  assert.match(salesforceCorrectnessWorkflow, /for poll in \{1\.\.12\}/);
  assert.match(salesforceCorrectnessWorkflow, /sleep 5/);
  assert.match(salesforceCorrectnessWorkflow, /FROM ActiveScratchOrg WHERE ScratchOrg/);
  assert.match(salesforceCorrectnessWorkflow, /--sobject ActiveScratchOrg/);
  assert.match(salesforceCorrectnessWorkflow, /remaining ActiveScratchOrg residue/);
});
