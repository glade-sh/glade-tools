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
const fullFixturesWorkflow = readFileSync(
  new URL("../.github/workflows/full-fixtures.yml", import.meta.url),
  "utf8",
);
const jobs = workflow.slice(workflow.indexOf("\njobs:\n"));

function job(name) {
  const match = jobs.match(new RegExp(`^  ${name}:\\n([\\s\\S]*?)(?=^  \\w[\\w-]*:|(?![\\s\\S]))`, "m"));
  assert.ok(match, `missing ${name} job`);
  return match[1];
}

function catalogPinnedSetup(lane) {
  const start = lane.indexOf("      - uses: actions/checkout@v6");
  const end = lane.indexOf("      - run:", start);
  assert.ok(start >= 0 && end > start, "missing catalog-pinned setup");
  return lane.slice(start, end).trim();
}

function activeGoTestCommands(lane) {
  return lane.split("\n")
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith("#") && line.includes("go test "))
    .map((line) => line.replace(/^- run: /, ""));
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

test("full fixtures is a single bounded weekly and manual lane", () => {
  assert.equal((fullFixturesWorkflow.match(/^name: Full Fixtures$/gm) ?? []).length, 1);
  const triggers = fullFixturesWorkflow.match(/^on:\n([\s\S]*?)^permissions:/m)?.[1];
  assert.ok(triggers, "missing top-level on block");
  assert.deepEqual(
    [...triggers.matchAll(/^  ([a-z_]+):/gm)].map((match) => match[1]),
    ["schedule", "workflow_dispatch"],
  );
  assert.match(triggers, /^  schedule:\n    - cron: "0 0 \* \* 0"$/m);
  assert.match(fullFixturesWorkflow, /permissions:\n  contents: read/);

  const fixtureJobs = fullFixturesWorkflow.slice(fullFixturesWorkflow.indexOf("\njobs:\n"));
  assert.deepEqual(
    [...fixtureJobs.matchAll(/^  (\w[\w-]*):$/gm)].map((match) => match[1]),
    ["full-fixtures"],
  );
  assert.match(
    fixtureJobs,
    /full-fixtures:\n    name: full-fixtures\n    runs-on: ubuntu-latest\n    timeout-minutes: 30/,
  );
  const fixtureJob = fixtureJobs.slice(fixtureJobs.indexOf("  full-fixtures:\n"));
  const setup = catalogPinnedSetup(fixtureJob);
  const currentCiSetup = catalogPinnedSetup(job("core"));
  assert.equal(setup, currentCiSetup);
  for (const requiredSetup of [
    "- uses: actions/checkout@v6\n        with:\n          path: glade-tools",
    "- uses: actions/create-github-app-token@v3\n        id: app-token\n        with:\n          client-id: ${{ vars.GLADE_APP_CLIENT_ID }}\n          private-key: ${{ secrets.GLADE_APP_PRIVATE_KEY }}\n          owner: ${{ github.repository_owner }}\n          repositories: |\n            glade\n            glade-tools\n          permission-contents: read",
    "GLADE_REMOTE: https://x-access-token:${{ steps.app-token.outputs.token }}@github.com/glade-sh/glade.git\n        run: |\n          requested_ref=\"$(jq -r '.gladeCommit' docs/fixtures/apex-language-rules.json)\"\n          scripts/resolve-sibling-ref.sh \"$GLADE_REMOTE\" \"$requested_ref\" main",
    "- uses: actions/checkout@v6\n        with:\n          repository: glade-sh/glade\n          path: glade\n          ref: ${{ steps.glade-ref.outputs.ref }}\n          token: ${{ steps.app-token.outputs.token }}",
    "run: test \"$(git -C ../glade rev-parse HEAD)\" = \"$(jq -r '.gladeCommit' docs/fixtures/apex-language-rules.json)\"",
    '- uses: actions/setup-go@v6\n        with:\n          go-version: "1.26.3"\n          cache-dependency-path: glade-tools/go.sum',
  ]) {
    assert.ok(currentCiSetup.includes(requiredSetup), `current CI missing ${requiredSetup}`);
  }
  assert.deepEqual(
    activeGoTestCommands(fixtureJob),
    [
      "GLADE_TOOLS_RUN_FULL_COMPAT_FIXTURES=1 go test ./internal/compat -run '^TestRunDocumentedFixtures$' -count=1 -timeout=10m",
      "GLADE_TOOLS_RUN_FULL_LOCAL_TEST_FIXTURES=1 go test ./internal/compat -run 'TestRunLocalTests.*FixtureReady|TestCheckLocalTestCorpusFixture' -count=1 -timeout=10m",
    ],
  );
  assert.doesNotMatch(fixtureJob, /go test \.\/\.\.\//);
});
