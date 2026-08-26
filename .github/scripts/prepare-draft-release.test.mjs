import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  compareVersions,
  incrementPatch,
  isManagedAssetName,
  parseVersion,
  planDraftRelease,
} from "./prepare-draft-release.mjs";

const draftReleaseWorkflow = await readFile(
  new URL("../workflows/draft-release.yml", import.meta.url),
  "utf8",
);

test("validates and increments stable semantic versions", () => {
  assert.deepEqual(parseVersion("0.1.9"), [0, 1, 9]);
  assert.equal(incrementPatch("0.1.9"), "0.1.10");
  assert.equal(compareVersions("1.0.0", "0.99.99") > 0, true);
  assert.throws(() => parseVersion("v1.0.0"), /stable semantic version/);
});

test("recognizes only release assets managed by the draft workflow", () => {
  assert.equal(isManagedAssetName("rush-v0.1.1-linux-amd64.tar.gz"), true);
  assert.equal(isManagedAssetName("rush-v0.1.1-darwin-arm64.tar.gz"), true);
  assert.equal(isManagedAssetName("rush-release.json"), true);
  assert.equal(isManagedAssetName("rush-v0.1.0-linux-amd64.tar.gz"), true);
  assert.equal(isManagedAssetName("maintainer-notes.txt"), false);
});

test("starts one patch beyond the package baseline before the first GitHub release", () => {
  assert.deepEqual(planDraftRelease([], "0.1.0"), {
    draft: null,
    version: "0.1.1",
    previousTag: undefined,
  });
});

test("reuses an open draft while selecting the newest published release for notes", () => {
  const draft = { id: 3, tag_name: "v1.3.0", draft: true, prerelease: false };
  assert.deepEqual(
    planDraftRelease(
      [
        { id: 1, tag_name: "v1.1.0", draft: false, prerelease: false },
        { id: 2, tag_name: "v1.2.0", draft: false, prerelease: false },
        draft,
      ],
      "0.1.0",
    ),
    { draft, version: "1.3.0", previousTag: "v1.2.0" },
  );
});

test("creates the next patch after the newest published release", () => {
  assert.deepEqual(
    planDraftRelease(
      [
        { id: 1, tag_name: "v0.2.4", draft: false, prerelease: false },
        { id: 2, tag_name: "v0.10.0", draft: false, prerelease: false },
      ],
      "0.1.0",
    ),
    { draft: null, version: "0.10.1", previousTag: "v0.10.0" },
  );
});

test("refuses ambiguous or stale semantic-version drafts", () => {
  assert.throws(
    () =>
      planDraftRelease(
        [
          { id: 1, tag_name: "v0.1.1", draft: true, prerelease: false },
          { id: 2, tag_name: "v0.1.2", draft: true, prerelease: false },
        ],
        "0.1.0",
      ),
    /more than one/,
  );
  assert.throws(
    () =>
      planDraftRelease(
        [
          { id: 1, tag_name: "v0.2.0", draft: false, prerelease: false },
          { id: 2, tag_name: "v0.1.1", draft: true, prerelease: false },
        ],
        "0.1.0",
      ),
    /not newer/,
  );
});

test("the checkout-less readiness job identifies its GitHub repository", () => {
  const readyJob = draftReleaseWorkflow.match(/\n  ready:\n(?<job>[\s\S]+)$/)?.groups?.job;

  assert.ok(readyJob, "expected the draft release workflow to define a ready job");
  assert.match(readyJob, /\n          GH_REPO: \$\{\{ github\.repository \}\}/);
  assert.match(readyJob, /gh release upload "\$RELEASE_TAG" rush-release\.json --clobber/);
});
