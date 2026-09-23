#!/usr/bin/env node

// npx downloads this repository, then this script downloads the matching
// release binary and runs it. With no arguments it starts the MCP server.

import { createHash, randomUUID } from "node:crypto";
import { spawn, spawnSync } from "node:child_process";
import { createWriteStream, realpathSync } from "node:fs";
import {
  chmod,
  copyFile,
  lstat,
  mkdir,
  mkdtemp,
  readdir,
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

const GITHUB_REPO = "SokolskyNikita/annas-mcp";
const RELEASE_API =
  process.env.ANNAS_MCP_RELEASE_API ||
  `https://api.github.com/repos/${GITHUB_REPO}/releases/latest`;

const RELEASE_TIMEOUT_MS = 5_000;
const DOWNLOAD_TIMEOUT_MS = 120_000;

const SUPPORTED_TARGETS = new Set([
  "darwin-amd64",
  "darwin-arm64",
  "linux-amd64",
  "linux-arm64",
  "linux-arm",
  "windows-amd64",
  "windows-arm64",
  "freebsd-amd64",
]);

export function goreleaserTarget(platform = process.platform, arch = process.arch) {
  const osName = platform === "win32" ? "windows" : platform;
  const archName = arch === "x64" ? "amd64" : arch;
  const key = `${osName}-${archName}`;
  if (!SUPPORTED_TARGETS.has(key)) {
    throw new Error(
      `Unsupported platform ${platform}/${arch}. Supported targets: ${[...SUPPORTED_TARGETS].join(", ")}`,
    );
  }
  return {
    os: osName,
    arch: archName,
    extension: osName === "windows" ? "zip" : "tar.xz",
    binaryName: osName === "windows" ? "annas-mcp.exe" : "annas-mcp",
  };
}

export function selectChecksumAsset(assets) {
  const matches = (assets || []).filter(
    (asset) => typeof asset?.name === "string" && asset.name.endsWith("--checksums.txt"),
  );
  if (matches.length !== 1) {
    const names = (assets || []).map((asset) => asset?.name).filter(Boolean);
    throw new Error(
      `Expected one checksums asset, found ${matches.length}. Assets: ${names.join(", ") || "(none)"}`,
    );
  }
  return validateDownloadAsset(matches[0]);
}

export function checksumFor(text, filename) {
  const base = path.basename(filename);
  for (const line of text.split(/\r?\n/)) {
    const match = line.trim().match(/^([a-fA-F0-9]{64})\s+\*?(.+)$/);
    if (!match) {
      continue;
    }
    const name = match[2].trim();
    if (name === filename || name === base || path.basename(name) === base) {
      return match[1].toLowerCase();
    }
  }
  throw new Error(`Checksum file has no entry for ${filename}`);
}

export function selectAsset(assets, target) {
  const suffix = `_${target.os}_${target.arch}.${target.extension}`;
  const matches = (assets || []).filter(
    (asset) =>
      typeof asset?.name === "string" &&
      asset.name.startsWith("annas-mcp_") &&
      asset.name.endsWith(suffix),
  );
  if (matches.length !== 1) {
    const names = (assets || []).map((asset) => asset?.name).filter(Boolean);
    throw new Error(
      `Expected one release asset ending in ${suffix}, found ${matches.length}. Assets: ${names.join(", ") || "(none)"}`,
    );
  }
  return validateDownloadAsset(matches[0]);
}

function cacheDir() {
  if (process.env.ANNAS_MCP_CACHE_DIR) {
    return process.env.ANNAS_MCP_CACHE_DIR;
  }
  const base =
    process.platform === "win32"
      ? process.env.LOCALAPPDATA || path.join(os.homedir(), "AppData", "Local")
      : process.env.XDG_CACHE_HOME || path.join(os.homedir(), ".cache");
  return path.join(base, "annas-mcp");
}

function safePathComponent(value, label) {
  if (
    typeof value !== "string" ||
    !value ||
    value === "." ||
    value === ".." ||
    value.includes("\0") ||
    value.includes("/") ||
    value.includes("\\") ||
    /[<>:"|?*]/.test(value) ||
    path.basename(value) !== value
  ) {
    throw new Error(`Invalid ${label}`);
  }
  return value;
}

function sha256Digest(value) {
  if (typeof value !== "string") {
    return null;
  }
  const match = value.trim().match(/^(?:sha256:)?([a-fA-F0-9]{64})$/);
  return match ? match[1].toLowerCase() : null;
}

function releaseAssetDigest(asset) {
  return sha256Digest(asset?.digest);
}

function pathInside(root, candidate) {
  const relative = path.relative(path.resolve(root), path.resolve(candidate));
  return relative && !relative.startsWith("..") && !path.isAbsolute(relative);
}

function validateDownloadAsset(asset) {
  safePathComponent(asset?.name, "release asset name");
  if (typeof asset?.browser_download_url !== "string" || !asset.browser_download_url) {
    throw new Error(`Release asset ${asset?.name || "(unnamed)"} has no download URL`);
  }
  return asset;
}

function isDirectRun() {
  const entry = process.argv[1];
  if (!entry) {
    return false;
  }
  try {
    // npx runs the bin through a symlink. argv is the link, import.meta is the real file.
    const entryPath = realpathSync(entry);
    const modulePath = realpathSync(fileURLToPath(import.meta.url));
    return pathToFileURL(entryPath).href === pathToFileURL(modulePath).href;
  } catch {
    return false;
  }
}

async function readCacheMeta(dir) {
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

async function cachedBinary(dir, release, asset, target, meta = undefined, expectedAssetSha256 = null) {
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

async function fetchLatestRelease() {
  const response = await fetch(RELEASE_API, {
    headers: {
      Accept: "application/vnd.github+json",
      "User-Agent": "annas-mcp",
      "X-GitHub-Api-Version": "2022-11-28",
    },
    signal: AbortSignal.timeout(RELEASE_TIMEOUT_MS),
  });
  if (!response.ok) {
    const body = await response.text();
    throw new Error(
      `GitHub release request failed (${response.status}) for ${RELEASE_API}: ${body.slice(0, 200)}`,
    );
  }
  const release = await response.json();
  if (!release?.tag_name || !Array.isArray(release.assets)) {
    throw new Error(`GitHub release response from ${RELEASE_API} is missing tag_name or assets`);
  }
  safePathComponent(release.tag_name, "release tag");
  return release;
}

async function downloadText(url) {
  const response = await fetch(url, {
    headers: { "User-Agent": "annas-mcp" },
    signal: AbortSignal.timeout(RELEASE_TIMEOUT_MS),
    redirect: "follow",
  });
  if (!response.ok) {
    throw new Error(`Download failed (${response.status}) for ${url}`);
  }
  return response.text();
}

async function sha256File(file) {
  const hash = createHash("sha256");
  hash.update(await readFile(file));
  return hash.digest("hex");
}

async function downloadFile(url, destination) {
  const response = await fetch(url, {
    headers: { "User-Agent": "annas-mcp" },
    signal: AbortSignal.timeout(DOWNLOAD_TIMEOUT_MS),
    redirect: "follow",
  });
  if (!response.ok || !response.body) {
    throw new Error(`Download failed (${response.status}) for ${url}`);
  }
  await pipeline(Readable.fromWeb(response.body), createWriteStream(destination));
}

async function expectedArchiveChecksum(release, asset) {
  const checksumAsset = selectChecksumAsset(release.assets);
  const checksumText = await downloadText(checksumAsset.browser_download_url);
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

function validateArchiveEntries(archive, extension) {
  const listingArgs = extension === "zip" ? ["-tf", archive] : ["-tJf", archive];
  const listing = spawnSync("tar", listingArgs, { encoding: "utf8" });
  if (listing.error) {
    throw new Error(`Failed to run tar: ${listing.error.message}`);
  }
  if (listing.status !== 0) {
    const detail = (listing.stderr || listing.stdout || "").trim();
    throw new Error(`Failed to inspect ${path.basename(archive)}${detail ? `: ${detail}` : ""}`);
  }
  for (const rawName of listing.stdout.split(/\r?\n/)) {
    const name = rawName.trim();
    if (!name) {
      continue;
    }
    const normalized = name.replaceAll("\\", "/").replace(/\/+$/, "");
    if (
      !normalized ||
      normalized.startsWith("/") ||
      /^[A-Za-z]:\//.test(normalized) ||
      normalized.split("/").includes("..")
    ) {
      throw new Error(`Refusing to extract an unsafe archive path: ${name}`);
    }
  }

  const verboseArgs = extension === "zip" ? ["-tvf", archive] : ["-tvJf", archive];
  const verbose = spawnSync("tar", verboseArgs, { encoding: "utf8" });
  if (verbose.error) {
    throw new Error(`Failed to run tar: ${verbose.error.message}`);
  }
  if (verbose.status !== 0) {
    const detail = (verbose.stderr || verbose.stdout || "").trim();
    throw new Error(`Failed to inspect ${path.basename(archive)}${detail ? `: ${detail}` : ""}`);
  }
  for (const line of verbose.stdout.split(/\r?\n/)) {
    if (!line.trim()) {
      continue;
    }
    const type = line[0];
    if (type !== "-" && type !== "d") {
      throw new Error(`Refusing non-file archive entry: ${line.trim()}`);
    }
  }
}

function extractArchive(archive, destination, extension) {
  validateArchiveEntries(archive, extension);
  const args =
    extension === "zip"
      ? ["-xf", archive, "-C", destination]
      : ["-xJf", archive, "-C", destination];
  const result = spawnSync("tar", args, { encoding: "utf8" });
  if (result.error) {
    throw new Error(`Failed to run tar: ${result.error.message}`);
  }
  if (result.status !== 0) {
    const detail = (result.stderr || result.stdout || "").trim();
    throw new Error(`Failed to extract ${path.basename(archive)}${detail ? `: ${detail}` : ""}`);
  }
}

async function findBinary(directory, binaryName, root = directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  for (const entry of entries) {
    if (entry.isSymbolicLink()) {
      continue;
    }
    const fullPath = path.join(directory, entry.name);
    const relative = path.relative(root, fullPath);
    if (relative.startsWith("..") || path.isAbsolute(relative)) {
      throw new Error(`Refusing to use a path outside the archive: ${entry.name}`);
    }
    if (entry.isDirectory()) {
      const found = await findBinary(fullPath, binaryName, root);
      if (found) {
        return found;
      }
      continue;
    }
    if (entry.isFile() && entry.name === binaryName) {
      return fullPath;
    }
  }
  return null;
}

async function writeCacheMeta(dir, meta) {
  const temporary = path.join(dir, `.current.json.${process.pid}.${randomUUID()}.tmp`);
  try {
    await writeFile(temporary, `${JSON.stringify(meta)}\n`);
    await rename(temporary, path.join(dir, "current.json"));
  } finally {
    await rm(temporary, { force: true });
  }
}

async function installBinary(release, asset, target, expectedAssetSha256) {
  const root = path.resolve(cacheDir());
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
    await downloadFile(asset.browser_download_url, archivePath);
    const actual = await sha256File(archivePath);
    if (actual !== expected) {
      throw new Error(`Checksum mismatch for ${assetName}`);
    }
    const extracted = path.join(work, "extracted");
    await mkdir(extracted);
    extractArchive(archivePath, extracted, target.extension);
    const found = await findBinary(extracted, target.binaryName);
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

function runBinary(binary, args) {
  const child = spawn(binary, args, { stdio: "inherit" });
  child.on("error", (error) => {
    console.error(error.message);
    process.exit(1);
  });
  for (const signal of ["SIGINT", "SIGTERM"]) {
    process.on(signal, () => {
      child.kill(signal);
    });
  }
  child.on("exit", (code, signal) => {
    if (signal) {
      process.exit(1);
    }
    process.exit(code ?? 1);
  });
}

async function resolveBinary(target) {
  const dir = path.resolve(cacheDir());
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
    const release = await fetchLatestRelease();
    const asset = selectAsset(release.assets, target);
    const expectedAssetSha256 = await expectedArchiveChecksum(release, asset);
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
    return await installBinary(release, asset, target, expectedAssetSha256);
  } catch (error) {
    if (cached) {
      console.error(`annas-mcp: update check failed, using the cached binary. ${error.message}`);
      return cached;
    }
    throw error;
  }
}

async function main() {
  const target = goreleaserTarget();
  const args = process.argv.slice(2);
  const commandArgs = args.length === 0 ? ["mcp"] : args;
  const binary = await resolveBinary(target);
  runBinary(binary, commandArgs);
}

if (isDirectRun()) {
  main().catch((error) => {
    console.error(error.message);
    process.exit(1);
  });
}
