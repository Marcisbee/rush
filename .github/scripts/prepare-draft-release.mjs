import { appendFile, readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";

const versionPattern = /^(\d+)\.(\d+)\.(\d+)$/;

export function parseVersion(value) {
  const match = versionPattern.exec(value);
  if (!match) {
    throw new Error(`expected a stable semantic version, received ${JSON.stringify(value)}`);
  }
  return match.slice(1).map(Number);
}

export function compareVersions(left, right) {
  const leftParts = parseVersion(left);
  const rightParts = parseVersion(right);
  for (let index = 0; index < leftParts.length; index += 1) {
    if (leftParts[index] !== rightParts[index]) {
      return leftParts[index] - rightParts[index];
    }
  }
  return 0;
}

export function incrementPatch(version) {
  const [major, minor, patch] = parseVersion(version);
  return `${major}.${minor}.${patch + 1}`;
}

export function isManagedAssetName(name) {
  return (
    name === "rush-release.json" ||
    /^rush-v\d+\.\d+\.\d+-(?:darwin|linux)-(?:amd64|arm64)\.tar\.gz$/.test(name)
  );
}

export function planDraftRelease(releases, packageVersion) {
  parseVersion(packageVersion);

  const publishedVersions = releases
    .filter((release) => !release.draft && !release.prerelease)
    .map((release) => /^v(\d+\.\d+\.\d+)$/.exec(release.tag_name ?? ""))
    .filter(Boolean)
    .map((match) => match[1]);
  const baseline = [packageVersion, ...publishedVersions].sort(compareVersions).at(-1);

  const drafts = releases.filter(
    (release) => release.draft && /^v\d+\.\d+\.\d+$/.test(release.tag_name ?? ""),
  );
  if (drafts.length > 1) {
    throw new Error("more than one semantic-version draft release exists; resolve the drafts before retrying");
  }

  if (drafts.length === 1) {
    const version = drafts[0].tag_name.slice(1);
    if (compareVersions(version, baseline) <= 0) {
      throw new Error(`draft ${drafts[0].tag_name} is not newer than published baseline v${baseline}`);
    }
    return { draft: drafts[0], version, previousTag: latestPublishedTag(releases) };
  }

  return {
    draft: null,
    version: incrementPatch(baseline),
    previousTag: latestPublishedTag(releases),
  };
}

function latestPublishedTag(releases) {
  const candidates = releases
    .filter((release) => !release.draft && !release.prerelease)
    .map((release) => {
      const match = /^v(\d+\.\d+\.\d+)$/.exec(release.tag_name ?? "");
      return match ? { tag: release.tag_name, version: match[1] } : null;
    })
    .filter(Boolean)
    .sort((left, right) => compareVersions(left.version, right.version));
  return candidates.at(-1)?.tag;
}

async function request(path, options = {}) {
  const apiUrl = process.env.GITHUB_API_URL ?? "https://api.github.com";
  const response = await fetch(`${apiUrl}${path}`, {
    ...options,
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${process.env.GITHUB_TOKEN}`,
      "Content-Type": "application/json",
      "X-GitHub-Api-Version": "2022-11-28",
      ...options.headers,
    },
  });
  if (!response.ok) {
    throw new Error(`${options.method ?? "GET"} ${path} failed (${response.status}): ${await response.text()}`);
  }
  return response.status === 204 ? null : response.json();
}

async function main() {
  const repository = process.env.GITHUB_REPOSITORY;
  const token = process.env.GITHUB_TOKEN;
  const outputPath = process.env.GITHUB_OUTPUT;
  if (!repository || !token || !outputPath) {
    throw new Error("GITHUB_REPOSITORY, GITHUB_TOKEN, and GITHUB_OUTPUT are required");
  }

  const manifest = JSON.parse(await readFile("package.json", "utf8"));
  const releases = await request(`/repos/${repository}/releases?per_page=100`);
  const plan = planDraftRelease(releases, manifest.version);
  const tagName = `v${plan.version}`;
  const noteRequest = { tag_name: tagName, target_commitish: "main" };
  if (plan.previousTag) {
    noteRequest.previous_tag_name = plan.previousTag;
  }
  const notes = await request(`/repos/${repository}/releases/generate-notes`, {
    method: "POST",
    body: JSON.stringify(noteRequest),
  });
  const releaseRequest = {
    tag_name: tagName,
    target_commitish: "main",
    name: tagName,
    body: notes.body,
    draft: true,
    prerelease: false,
  };

  const release = plan.draft
    ? await request(`/repos/${repository}/releases/${plan.draft.id}`, {
        method: "PATCH",
        body: JSON.stringify(releaseRequest),
      })
    : await request(`/repos/${repository}/releases`, {
        method: "POST",
        body: JSON.stringify(releaseRequest),
      });

  const staleAssets = (release.assets ?? []).filter((asset) => isManagedAssetName(asset.name));
  for (const asset of staleAssets) {
    await request(`/repos/${repository}/releases/assets/${asset.id}`, { method: "DELETE" });
  }

  await appendFile(outputPath, `release_id=${release.id}\ntag_name=${tagName}\nversion=${plan.version}\n`);
  console.log(
    `${plan.draft ? "Updated" : "Created"} draft release ${tagName}; removed ${staleAssets.length} stale managed assets`,
  );
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}
