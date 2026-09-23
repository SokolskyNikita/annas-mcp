import { GITHUB_REPO } from "../lib/launcher.js";

const API_ROOT = `https://api.github.com/repos/${GITHUB_REPO}/releases`;
const DEFAULT_ATTEMPTS = 5;
const DEFAULT_RETRY_DELAY_MS = 1_000;
const MAX_RETRY_DELAY_MS = 8_000;
const DEFAULT_TIMEOUT_MS = 30_000;

export function releaseByTagURL(tag) {
  if (typeof tag !== "string" || !tag) {
    throw new Error("Invalid release tag");
  }
  return `${API_ROOT}/tags/${encodeURIComponent(tag)}`;
}

export function releaseByIDURL(id) {
  if (!Number.isSafeInteger(id) || id <= 0) {
    throw new Error("GitHub returned an invalid release ID");
  }
  return `${API_ROOT}/${id}`;
}

function positiveAttempts(value) {
  if (!Number.isInteger(value) || value < 1 || value > 10) {
    throw new Error("Release lookup attempts must be an integer from 1 to 10");
  }
  return value;
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function fetchReleaseJSON(fetchImpl, url, headers, timeoutMs, label) {
  const response = await fetchImpl(url, {
    headers,
    signal: AbortSignal.timeout(timeoutMs),
  });
  if (!response.ok) {
    throw new Error(`GitHub ${label} failed (${response.status}) for ${url}`);
  }
  const release = await response.json();
  if (!release || typeof release !== "object" || Array.isArray(release)) {
    throw new Error(`GitHub ${label} returned invalid release metadata for ${url}`);
  }
  return release;
}

// Resolve the requested tag to its numeric release ID, then use the stable
// release-by-ID endpoint for authoritative metadata and asset checks. GitHub
// can return a stale empty assets array from the tag endpoint after publishing.
export async function fetchPublishedRelease(
  tag,
  {
    fetchImpl = globalThis.fetch,
    headers = {},
    attempts = DEFAULT_ATTEMPTS,
    retryDelayMs = DEFAULT_RETRY_DELAY_MS,
    timeoutMs = DEFAULT_TIMEOUT_MS,
    sleepImpl = delay,
  } = {},
) {
  if (typeof fetchImpl !== "function") {
    throw new Error("A fetch implementation is required");
  }
  const maxAttempts = positiveAttempts(attempts);
  if (!Number.isFinite(retryDelayMs) || retryDelayMs < 0 || retryDelayMs > 60_000) {
    throw new Error("Release lookup retry delay must be between 0 and 60000 ms");
  }

  const tagURL = releaseByTagURL(tag);
  const tagged = await fetchReleaseJSON(fetchImpl, tagURL, headers, timeoutMs, "tag lookup");
  if (tagged.tag_name !== tag) {
    throw new Error(`GitHub returned tag ${JSON.stringify(tagged.tag_name)} when resolving ${tag}`);
  }
  const releaseId = tagged.id;
  const idURL = releaseByIDURL(releaseId);

  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    const release = await fetchReleaseJSON(fetchImpl, idURL, headers, timeoutMs, "release lookup");
    if (release.id !== releaseId || release.tag_name !== tag) {
      throw new Error(`GitHub release ID ${releaseId} did not match requested tag ${tag}`);
    }
    if (!Array.isArray(release.assets)) {
      throw new Error(`GitHub release ${tag} is missing its assets list`);
    }
    if (release.assets.length > 0) {
      return { release, apiURL: idURL };
    }
    if (attempt + 1 < maxAttempts) {
      const backoff = Math.min(retryDelayMs * 2 ** attempt, MAX_RETRY_DELAY_MS);
      await sleepImpl(backoff);
    }
  }

  throw new Error(
    `GitHub release ${tag} still has no assets after ${maxAttempts} release-ID lookups`,
  );
}
