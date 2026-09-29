import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { MCPProcessClient } from "./mcp-session.mjs";
import { checkVersion } from "./check-version.mjs";
import {
  expectedArchiveChecksum,
  installBinary,
  readCacheMeta,
  sha256File,
} from "../lib/launcher.js";
import { fetchPublishedRelease } from "./release-metadata.mjs";
import {
  goreleaserTarget,
  selectAsset,
  selectChecksumAsset,
} from "../lib/target.js";

const root = fileURLToPath(new URL("../", import.meta.url));
const entrypoint = path.join(root, "bin", "annas-mcp.js");
const MAX_OUTPUT_BYTES = 64 << 10;
const toolNames = ["article_download", "article_search", "book_download", "book_search"];
const releaseTargets = [
  ["linux", "amd64"],
  ["linux", "arm64"],
  ["linux", "arm"],
  ["darwin", "amd64"],
  ["darwin", "arm64"],
  ["freebsd", "amd64"],
  ["win32", "x64"],
  ["win32", "arm64"],
];

function authenticatedFetch(input, options = {}) {
  const url = new URL(input);
  const headers = new Headers(options.headers);
  const token = process.env.GH_TOKEN || process.env.GITHUB_TOKEN;
  if (url.origin === "https://api.github.com" && token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  return fetch(input, { ...options, headers, redirect: "follow" });
}

function releaseAssets(release, tag) {
  assert.equal(release.tag_name, tag, "GitHub returned a different release tag");
  assert.equal(release.draft, false, "release must not be a draft");
  assert.equal(release.prerelease, false, "release must not be a prerelease");
  assert(Array.isArray(release.assets), "release has no assets list");

  const version = tag.slice(1);
  for (const [platform, arch] of releaseTargets) {
    const target = goreleaserTarget(platform, arch);
    const expected = `annas-mcp_${version}_${target.os}_${target.arch}.${target.extension}`;
    const asset = selectAsset(release.assets, target);
    assert.equal(asset.name, expected, `unexpected ${platform}/${arch} asset`);
    assert(Number.isInteger(asset.size) && asset.size > 0, `${expected} is empty`);
  }

  const checksumName = `annas-mcp_${version}--checksums.txt`;
  const checksum = selectChecksumAsset(release.assets);
  assert.equal(checksum.name, checksumName, "release checksum asset has the wrong name");
  assert(Number.isInteger(checksum.size) && checksum.size > 0, "checksum asset is empty");
}

function childEnvironment(cache, apiURL) {
  const env = { ...process.env };
  for (const name of [
    "ANNAS_ACCOUNT_COOKIE",
    "ANNAS_SECRET_KEY",
    "ANNAS_DOWNLOAD_PATH",
    "GH_TOKEN",
    "GITHUB_TOKEN",
  ]) {
    env[name] = "";
  }
  env.ANNAS_BASE_URL = "annas-archive.gl";
  env.ANNAS_AUTO_BASE_URL = "false";
  env.ANNAS_MCP_CACHE_DIR = cache;
  env.ANNAS_MCP_RELEASE_API = apiURL;
  return env;
}

function runVersion(env, cwd, tag) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [entrypoint, "--version"], {
      cwd,
      env,
      stdio: ["ignore", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    let settled = false;
    let timer;
    const finish = (callback) => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      callback();
    };
    timer = setTimeout(() => {
      try {
        child.kill("SIGTERM");
      } catch {
        // The process may have exited between the timeout and kill call.
      }
      finish(() => reject(new Error("launcher --version timed out")));
    }, 30_000);
    child.stdout.on("data", (chunk) => {
      stdout += chunk.toString().slice(0, MAX_OUTPUT_BYTES - stdout.length);
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk.toString().slice(0, MAX_OUTPUT_BYTES - stderr.length);
    });
    child.once("error", (error) => {
      finish(() => reject(error));
    });
    child.once("close", (code, signal) => {
      if (code !== 0) {
        finish(() => reject(new Error(
          `launcher --version failed (${signal || code}): ${stderr.slice(0, 300)}`,
        )));
        return;
      }
      finish(() => {
        try {
          assert.equal(stdout.trim(), tag, "launcher returned the wrong version");
          resolve();
        } catch (error) {
          reject(error);
        }
      });
    });
  });
}

async function verifyServer(env, cwd, tag) {
  const child = spawn(process.execPath, [entrypoint, "mcp"], {
    cwd,
    env,
    stdio: ["pipe", "pipe", "pipe"],
  });
  const session = new MCPProcessClient(child);
  try {
    const initialized = await session.initialize(30_000);
    assert.equal(initialized?.serverInfo?.name, "annas-mcp");
    assert.equal(initialized?.serverInfo?.version, tag, "server returned the wrong version");
    const listed = await session.request("tools/list", {}, 30_000);
    assert.deepEqual(
      (listed?.tools || []).map((tool) => tool.name).sort(),
      toolNames,
      "published binary exposed an unexpected tool set",
    );
  } finally {
    await session.close();
  }
}

async function main() {
  const explicitTag = process.argv[2];
  const currentVersion = checkVersion(root, {});
  const tag = explicitTag || currentVersion;
  assert.match(tag, /^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/, "invalid release tag");
  if (process.env.GITHUB_REF_TYPE === "tag") {
    assert.equal(process.env.GITHUB_REF_NAME, tag, "workflow tag does not match requested release");
    assert.equal(currentVersion, tag, "workflow tag does not match repository version");
  }

  const { release, apiURL } = await fetchPublishedRelease(tag, {
    fetchImpl: authenticatedFetch,
    headers: {
      Accept: "application/vnd.github+json",
      "User-Agent": "annas-mcp-release-verifier",
      "X-GitHub-Api-Version": "2022-11-28",
    },
  });
  releaseAssets(release, tag);
  const target = goreleaserTarget();
  const asset = selectAsset(release.assets, target);
  const cache = await mkdtemp(path.join(os.tmpdir(), "annas-mcp-release-"));
  const env = childEnvironment(cache, apiURL);
  try {
    const expected = await expectedArchiveChecksum(release, asset, {
      env,
      fetchImpl: authenticatedFetch,
    });
    const binary = await installBinary(release, asset, target, expected, {
      env,
      fetchImpl: authenticatedFetch,
    });
    const metadata = await readCacheMeta(cache);
    assert.equal(
      await sha256File(binary),
      metadata.binarySha256,
      "cached binary checksum changed",
    );
    await runVersion(env, cache, tag);
    await verifyServer(env, cache, tag);
    console.log(`Release ${tag} verified for ${target.os}/${target.arch}`);
  } finally {
    await rm(cache, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error(`release verification failed: ${error.message}`);
  process.exitCode = 1;
});
