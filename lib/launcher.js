import { createHash, randomUUID } from "node:crypto";
import { spawn as defaultSpawn } from "node:child_process";
import { createWriteStream, realpathSync } from "node:fs";
import {
  chmod,
  copyFile,
  lstat,
  mkdir,
  mkdtemp,
  readFile,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { extractArchive, findBinary } from "./archive.js";
import {
  checksumFor,
  pathInside,
  releaseAssetDigest,
  goreleaserTarget,
  safePathComponent,
  selectAsset,
  selectChecksumAsset,
  sha256Digest,
} from "./target.js";

export const GITHUB_REPO = "SokolskyNikita/annas-mcp";
export const RELEASE_TIMEOUT_MS = 5_000;
export const DOWNLOAD_TIMEOUT_MS = 120_000;

export function releaseAPI(env = process.env) {
  return (
    env.ANNAS_MCP_RELEASE_API ||
    `https://api.github.com/repos/${GITHUB_REPO}/releases/latest`
  );
}

export function cacheDir(
  env = process.env,
  platform = process.platform,
  homeDirectory = os.homedir(),
) {
  if (env.ANNAS_MCP_CACHE_DIR) {
    return env.ANNAS_MCP_CACHE_DIR;
  }
  const base =
    platform === "win32"
      ? env.LOCALAPPDATA || path.join(homeDirectory, "AppData", "Local")
      : env.XDG_CACHE_HOME || path.join(homeDirectory, ".cache");
  return path.join(base, "annas-mcp");
}

export function isDirectRun(moduleURL = import.meta.url, argv = process.argv) {
  const entry = argv[1];
  if (!entry) {
    return false;
  }
  try {
    // npx may invoke the bin through a symlink. Compare resolved file URLs.
    const entryPath = realpathSync(entry);
    const modulePath = realpathSync(fileURLToPath(moduleURL));
    return pathToFileURL(entryPath).href === pathToFileURL(modulePath).href;
  } catch {
    return false;
  }
}

export async function readCacheMeta(dir) {
  try {
    const meta = JSON.parse(await readFile(path.join(dir, "current.json"), "utf8"));
    return meta && typeof meta === "object" && !Array.isArray(meta) ? meta : null;
  } catch (error) {
    if (error.code === "ENOENT" || error instanceof SyntaxError) {
      return null;
    }
    throw error;
  }
}

export async function sha256File(file) {
  const hash = createHash("sha256");
  hash.update(await readFile(file));
  return hash.digest("hex");
}

export async function cachedBinary(
  dir,
  release,
  asset,
  target,
  meta = undefined,
  expectedAssetSha256 = null,
) {
  const current = meta === undefined ? await readCacheMeta(dir) : meta;
  if (
    !current ||
    current.tag !== release.tag_name ||
    current.asset !== asset.name ||
    (target &&
      (current.target?.os !== target.os ||
        current.target?.arch !== target.arch ||
        current.target?.binaryName !== target.binaryName))
  ) {
    return null;
  }

  const binarySha256 = sha256Digest(current.binarySha256);
  const assetSha256 = sha256Digest(current.assetSha256);
  const expectedAssetDigest = expectedAssetSha256 || releaseAssetDigest(asset);
  if (!binarySha256 || !assetSha256 || (expectedAssetDigest && assetSha256 !== expectedAssetDigest)) {
    return null;
  }

  const binary = path.resolve(String(current.binary || ""));
  if (!pathInside(dir, binary)) {
    return null;
  }

  try {
    const info = await lstat(binary);
    if (!info.isFile() || info.isSymbolicLink()) {
      return null;
    }
    if ((await sha256File(binary)) !== binarySha256) {
      return null;
    }
    return binary;
  } catch {
    return null;
  }
}

function getFetch(fetchImpl) {
  const selected = fetchImpl || globalThis.fetch;
  if (typeof selected !== "function") {
    throw new Error("This Node.js runtime does not provide fetch");
  }
  return selected;
}

export async function fetchLatestRelease({
  env = process.env,
  fetchImpl = globalThis.fetch,
  timeoutMs = RELEASE_TIMEOUT_MS,
} = {}) {
  const fetcher = getFetch(fetchImpl);
  const api = releaseAPI(env);
  const response = await fetcher(api, {
    headers: {
      Accept: "application/vnd.github+json",
      "User-Agent": "annas-mcp",
      "X-GitHub-Api-Version": "2022-11-28",
    },
    signal: AbortSignal.timeout(timeoutMs),
  });
  if (!response.ok) {
    const body = await response.text();
    throw new Error(
      `GitHub release request failed (${response.status}) for ${api}: ${body.slice(0, 200)}`,
    );
  }
  const release = await response.json();
  if (!release?.tag_name || !Array.isArray(release.assets)) {
    throw new Error(`GitHub release response from ${api} is missing tag_name or assets`);
  }
  safePathComponent(release.tag_name, "release tag");
  return release;
}

export async function downloadText(
  url,
  { fetchImpl = globalThis.fetch, timeoutMs = RELEASE_TIMEOUT_MS } = {},
) {
  const response = await getFetch(fetchImpl)(url, {
    headers: { "User-Agent": "annas-mcp" },
    signal: AbortSignal.timeout(timeoutMs),
    redirect: "follow",
  });
  if (!response.ok) {
    throw new Error(`Download failed (${response.status}) for ${url}`);
  }
  return response.text();
}

export async function downloadFile(
  url,
  destination,
  { fetchImpl = globalThis.fetch, timeoutMs = DOWNLOAD_TIMEOUT_MS } = {},
) {
  const response = await getFetch(fetchImpl)(url, {
    headers: { "User-Agent": "annas-mcp" },
    signal: AbortSignal.timeout(timeoutMs),
    redirect: "follow",
  });
  if (!response.ok || !response.body) {
    throw new Error(`Download failed (${response.status}) for ${url}`);
  }
  await pipeline(Readable.fromWeb(response.body), createWriteStream(destination));
}

export async function expectedArchiveChecksum(
  release,
  asset,
  { env = process.env, fetchImpl = globalThis.fetch } = {},
) {
  const checksumAsset = selectChecksumAsset(release.assets);
  const checksumText = await downloadText(checksumAsset.browser_download_url, { fetchImpl });
  const expected = sha256Digest(checksumFor(checksumText, asset.name));
  if (!expected) {
    throw new Error(`Invalid checksum for ${asset.name}`);
  }
  const advertised = releaseAssetDigest(asset);
  if (advertised && advertised !== expected) {
    throw new Error(`Release digest disagrees with checksums for ${asset.name}`);
  }
  return expected;
}

export async function writeCacheMeta(dir, meta) {
  const temporary = path.join(dir, `.current.json.${process.pid}.${randomUUID()}.tmp`);
  try {
    await writeFile(temporary, `${JSON.stringify(meta)}\n`);
    await rename(temporary, path.join(dir, "current.json"));
  } finally {
    await rm(temporary, { force: true });
  }
}

export async function installBinary(
  release,
  asset,
  target,
  expectedAssetSha256,
  {
    env = process.env,
    fetchImpl = globalThis.fetch,
    downloadFileImpl = downloadFile,
    extractArchiveImpl = extractArchive,
    findBinaryImpl = findBinary,
  } = {},
) {
  const root = path.resolve(cacheDir(env));
  const releaseTag = safePathComponent(release.tag_name, "release tag");
  const assetName = safePathComponent(asset.name, "release asset name");
  const expected = sha256Digest(expectedAssetSha256);
  if (!expected) {
    throw new Error(`Invalid checksum for ${assetName}`);
  }
  await mkdir(root, { recursive: true });
  const work = await mkdtemp(path.join(root, ".tmp-"));
  const archivePath = path.join(work, assetName);
  try {
    await downloadFileImpl(asset.browser_download_url, archivePath, { fetchImpl });
    const actual = await sha256File(archivePath);
    if (actual !== expected) {
      throw new Error(`Checksum mismatch for ${assetName}`);
    }
    const extracted = path.join(work, "extracted");
    await mkdir(extracted);
    extractArchiveImpl(archivePath, extracted, target.extension);
    const found = await findBinaryImpl(extracted, target.binaryName);
    if (!found) {
      throw new Error(`Archive ${assetName} does not contain ${target.binaryName}`);
    }
    const binarySha256 = await sha256File(found);
    const binaryDir = path.join(root, "versions", releaseTag);
    await mkdir(binaryDir, { recursive: true });
    const binaryPath = path.join(binaryDir, `${actual}-${target.binaryName}`);
    const temporary = `${binaryPath}.${process.pid}.${randomUUID()}.tmp`;
    await copyFile(found, temporary);
    await chmod(temporary, 0o755);
    try {
      await rename(temporary, binaryPath);
    } catch (error) {
      if (!["EEXIST", "EPERM"].includes(error.code)) {
        throw error;
      }
      if ((await sha256File(binaryPath)) !== binarySha256) {
        throw error;
      }
      await rm(temporary, { force: true });
    }
    const meta = {
      tag: releaseTag,
      asset: assetName,
      assetSha256: actual,
      binarySha256,
      binary: binaryPath,
      target: {
        os: target.os,
        arch: target.arch,
        binaryName: target.binaryName,
      },
    };
    await writeCacheMeta(root, meta);
    return binaryPath;
  } finally {
    await rm(work, { recursive: true, force: true });
  }
}

export async function resolveBinary(
  target,
  {
    env = process.env,
    fetchImpl = globalThis.fetch,
    installBinaryImpl = installBinary,
  } = {},
) {
  const dir = path.resolve(cacheDir(env));
  const current = await readCacheMeta(dir);
  let cached = null;
  if (current) {
    cached = await cachedBinary(
      dir,
      { tag_name: current.tag },
      { name: current.asset },
      target,
      current,
    );
  }
  try {
    const release = await fetchLatestRelease({ env, fetchImpl });
    const asset = selectAsset(release.assets, target);
    const expectedAssetSha256 = await expectedArchiveChecksum(release, asset, { env, fetchImpl });
    const currentRelease = await cachedBinary(
      dir,
      release,
      asset,
      target,
      undefined,
      expectedAssetSha256,
    );
    if (currentRelease) {
      return currentRelease;
    }
    return await installBinaryImpl(release, asset, target, expectedAssetSha256, {
      env,
      fetchImpl,
    });
  } catch (error) {
    if (cached) {
      console.error(`annas-mcp: update check failed, using the cached binary. ${error.message}`);
      return cached;
    }
    throw error;
  }
}

export function runBinary(binary, args, { spawnImpl = defaultSpawn, processRef = process } = {}) {
  const child = spawnImpl(binary, args, { stdio: "inherit" });
  child.on("error", (error) => {
    console.error(error.message);
    processRef.exit(1);
  });
  for (const signal of ["SIGINT", "SIGTERM"]) {
    processRef.on(signal, () => {
      child.kill(signal);
    });
  }
  child.on("exit", (code, signal) => {
    if (signal) {
      processRef.exit(1);
    }
    processRef.exit(code ?? 1);
  });
  return child;
}

export async function main({ argv = process.argv.slice(2), env = process.env } = {}) {
  const target = goreleaserTarget();
  const commandArgs = argv.length === 0 ? ["mcp"] : argv;
  const binary = await resolveBinary(target, { env });
  return runBinary(binary, commandArgs);
}
