#!/usr/bin/env node

// npx downloads this repository, then this script downloads the matching
// release binary and runs it. With no arguments it starts the MCP server.

import { createHash } from "node:crypto";
import { spawn, spawnSync } from "node:child_process";
import { createWriteStream, realpathSync } from "node:fs";
import {
  chmod,
  copyFile,
  mkdir,
  readdir,
  readFile,
  rename,
  rm,
  stat,
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
  if (!matches[0].browser_download_url) {
    throw new Error(`Release asset ${matches[0].name} has no download URL`);
  }
  return matches[0];
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
  if (!matches[0].browser_download_url) {
    throw new Error(`Release asset ${matches[0].name} has no download URL`);
  }
  return matches[0];
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
    return JSON.parse(await readFile(path.join(dir, "current.json"), "utf8"));
  } catch (error) {
    if (error.code === "ENOENT") {
      return null;
    }
    throw error;
  }
}

async function cachedBinary(dir, release, asset) {
  const meta = await readCacheMeta(dir);
  if (!meta || meta.tag !== release.tag_name || meta.asset !== asset.name) {
    return null;
  }
  try {
    const info = await stat(meta.binary);
    if (!info.isFile()) {
      return null;
    }
    return meta.binary;
  } catch (error) {
    if (error.code === "ENOENT") {
      return null;
    }
    throw error;
  }
}

async function fetchLatestRelease() {
  const response = await fetch(RELEASE_API, {
    headers: {
      Accept: "application/vnd.github+json",
      "User-Agent": "annas-mcp",
      "X-GitHub-Api-Version": "2022-11-28",
    },
    signal: AbortSignal.timeout(30_000),
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
  return release;
}

async function downloadText(url) {
  const response = await fetch(url, {
    headers: { "User-Agent": "annas-mcp" },
    signal: AbortSignal.timeout(30_000),
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
    signal: AbortSignal.timeout(120_000),
    redirect: "follow",
  });
  if (!response.ok || !response.body) {
    throw new Error(`Download failed (${response.status}) for ${url}`);
  }
  await pipeline(Readable.fromWeb(response.body), createWriteStream(destination));
}

function extractArchive(archive, destination, extension) {
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

async function installBinary(release, asset, target) {
  const root = cacheDir();
  await mkdir(root, { recursive: true });
  const work = path.join(root, "tmp");
  await rm(work, { recursive: true, force: true });
  await mkdir(work, { recursive: true });
  const archivePath = path.join(work, asset.name);
  try {
    const checksumAsset = selectChecksumAsset(release.assets);
    const checksumText = await downloadText(checksumAsset.browser_download_url);
    const expected = checksumFor(checksumText, asset.name);
    await downloadFile(asset.browser_download_url, archivePath);
    const actual = await sha256File(archivePath);
    if (actual !== expected) {
      throw new Error(`Checksum mismatch for ${asset.name}`);
    }
    const extracted = path.join(work, "extracted");
    await mkdir(extracted);
    extractArchive(archivePath, extracted, target.extension);
    const found = await findBinary(extracted, target.binaryName);
    if (!found) {
      throw new Error(`Archive ${asset.name} does not contain ${target.binaryName}`);
    }
    const binaryDir = path.join(root, "versions", release.tag_name);
    await mkdir(binaryDir, { recursive: true });
    const binaryPath = path.join(binaryDir, target.binaryName);
    const temporary = `${binaryPath}.tmp`;
    await copyFile(found, temporary);
    await chmod(temporary, 0o755);
    await rename(temporary, binaryPath);
    const meta = {
      tag: release.tag_name,
      asset: asset.name,
      binary: binaryPath,
    };
    await writeFile(path.join(root, "current.json"), `${JSON.stringify(meta)}\n`);
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
  const current = await readCacheMeta(cacheDir());
  let cached = null;
  if (current?.binary && (await cachedBinary(cacheDir(), { tag_name: current.tag }, { name: current.asset }))) {
    cached = current.binary;
  }
  try {
    const release = await fetchLatestRelease();
    const asset = selectAsset(release.assets, target);
    const currentRelease = await cachedBinary(cacheDir(), release, asset);
    if (currentRelease) {
      return currentRelease;
    }
    return await installBinary(release, asset, target);
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
