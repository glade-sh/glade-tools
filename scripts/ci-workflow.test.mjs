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
const jobs = workflow.slice(workflow.indexOf("\njobs:\n"));

function job(name) {
  const match = jobs.match(new RegExp(`^  ${name}:\\n([\\s\\S]*?)(?=^  \\w[\\w-]*:|(?![\\s\\S]))`, "m"));
  assert.ok(match, `missing ${name} job`);
  return match[1];
}

test("CI has exactly core and release executing test lanes plus stable test aggregate", () => {
  assert.deepEqual(
    [...jobs.matchAll(/^  (\w[\w-]*):$/gm)].map((match) => match[1]),
    ["core", "release", "test", "macos-release-upload"],
  );
  for (const name of ["core", "release"]) {
    const lane = job(name);
    assert.match(lane, /runs-on: ubuntu-latest\n\s+timeout-minutes: 15/);
    for (const step of [
      "actions/checkout@v6",
      "actions/create-github-app-token@v3",
      "scripts/resolve-sibling-ref.sh",
      "repository: glade-sh/glade",
      "test \"$(git -C ../glade rev-parse HEAD)\" = \"$(jq -r '.gladeCommit' docs/fixtures/apex-language-rules.json)\"",
      "actions/setup-go@v6",
    ]) {
      assert.ok(lane.includes(step), `${name} missing ${step}`);
    }
  }
  assert.match(job("core"), /scripts\/release-check\.sh core/);
  assert.doesNotMatch(job("core"), /node --test/);
  assert.match(job("release"), /scripts\/release-check\.sh release/);
  assert.doesNotMatch(job("release"), /node --test/);

  const aggregate = job("test");
  assert.match(aggregate, /needs:\n\s+- core\n\s+- release/);
  assert.match(aggregate, /if: always\(\)/);
  assert.match(aggregate, /test "\$\{\{ needs\.core\.result \}\}" = success && test "\$\{\{ needs\.release\.result \}\}" = success/);
  assert.doesNotMatch(aggregate, /actions\/|resolve-sibling-ref|setup-go|node --test|release-check/);
});

test("CI runs only for main pushes and pull requests", () => {
  assert.match(workflow, /push:\n\s+branches:\n\s+- main\n\s+pull_request:/);
  assert.doesNotMatch(workflow, /\n\s+tags:/);
});

test("CI keeps macOS release upload coverage unchanged", () => {
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
