import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { spawn, spawnSync } from "node:child_process";
import { EventEmitter } from "node:events";
import { appendFileSync, createReadStream, readdirSync, readFileSync } from "node:fs";
import { copyFile, mkdtemp, mkdir, rename, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import os from "node:os";
import path from "node:path";
import { PassThrough } from "node:stream";
import { fileURLToPath, pathToFileURL } from "node:url";
import {
  checksumFor,
  goreleaserTarget,
  safePathComponent,
  selectAsset,
  sha256Digest,
  targetForRelease,
} from "../lib/target.js";
import { cacheDir, readCacheMeta, releaseAPI, writeCacheMeta } from "../lib/launcher.js";
import { tarExecutable, validateArchiveEntryName } from "../lib/archive.js";
import { MCPProcessClient } from "./mcp-session.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const testdata = path.join(path.dirname(fileURLToPath(import.meta.url)), "testdata");

function fixture(name) {
  return readFileSync(path.join(testdata, name), "utf8");
}

function testAssetSelection() {
  const darwin = goreleaserTarget("darwin", "arm64");
  const windows = goreleaserTarget("win32", "x64");
  const assets = [
    { name: "annas-mcp_0.0.6--checksums.txt" },
    {
      name: "annas-mcp_0.0.6_darwin_arm64.tar.gz",
      browser_download_url: "https://example.test/darwin",
    },
    {
      name: "annas-mcp_0.0.6_darwin_arm64.tar.xz",
      browser_download_url: "https://example.test/darwin-xz",
    },
    {
      name: "annas-mcp_0.0.6_linux_arm64.tar.gz",
      browser_download_url: "https://example.test/linux",
    },
    {
      name: "annas-mcp_0.0.6_windows_amd64.zip",
      browser_download_url: "https://example.test/windows",
    },
  ];
  assert(
    selectAsset(assets, darwin).name === "annas-mcp_0.0.6_darwin_arm64.tar.gz",
    "darwin asset was not selected",
  );
  assert(
    selectAsset(assets, windows).name === "annas-mcp_0.0.6_windows_amd64.zip",
    "windows asset was not selected",
  );
  assert.equal(targetForRelease(assets, darwin), darwin, "gzip release should keep the .tar.gz target");
  const legacy = [{ name: "annas-mcp_0.1.1_linux_arm64.tar.xz", browser_download_url: "https://example.test/xz" }];
  const legacyTarget = targetForRelease(legacy, goreleaserTarget("linux", "arm64"));
  assert.equal(legacyTarget.extension, "tar.xz", "releases without .tar.gz should fall back to .tar.xz");
  assert.equal(selectAsset(legacy, legacyTarget).name, legacy[0].name);
  assert.equal(targetForRelease(assets, windows), windows, "windows keeps .zip");
  let rejected = false;
  try {
    selectAsset(assets, goreleaserTarget("linux", "arm"));
  } catch {
    rejected = true;
  }
  assert(rejected, "missing linux/arm asset should fail");
  const digest = "a".repeat(64);
  assert(
    checksumFor(`${digest}  annas-mcp_0.0.6_darwin_arm64.tar.gz\n`, "annas-mcp_0.0.6_darwin_arm64.tar.gz") === digest,
    "checksum line was not parsed",
  );
}

function testInputValidation() {
  const digest = "A".repeat(64);
  assert.equal(sha256Digest(`sha256:${digest}`), digest.toLowerCase());
  assert.equal(sha256Digest("not-a-digest"), null);
  assert.equal(
    releaseAPI({ ANNAS_MCP_RELEASE_API: "https://example.test/releases/latest" }),
    "https://example.test/releases/latest",
  );
  assert.equal(
    cacheDir({ ANNAS_MCP_CACHE_DIR: "/tmp/annas-mcp-test" }, "linux", "/home/tester"),
    "/tmp/annas-mcp-test",
  );
  assert.equal(validateArchiveEntryName("annas-mcp/annas-mcp"), "annas-mcp/annas-mcp");
  assert.equal(tarExecutable("linux", {}), "tar");
  assert.equal(
    tarExecutable("win32", { SystemRoot: "C:\\Windows" }),
    "C:\\Windows\\System32\\tar.exe",
  );
  assert.equal(
    tarExecutable("win32", { WINDIR: "D:\\Windows" }),
    "D:\\Windows\\System32\\tar.exe",
  );
  assert.equal(tarExecutable("win32", {}), "C:\\Windows\\System32\\tar.exe");
  for (const name of ["../escape", "/absolute", "C:/absolute", "nested/../escape"]) {
    assert.throws(() => validateArchiveEntryName(name), /unsafe archive path/);
  }
  assert.throws(() => safePathComponent("../release", "release tag"), /Invalid release tag/);
}

function fakeMCPProcess(onRequest) {
  const child = new EventEmitter();
  child.stdin = new PassThrough();
  child.stdout = new PassThrough();
  child.stderr = new PassThrough();
  child.exitCode = null;
  child.signalCode = null;
  child.kill = (signal) => {
    if (child.exitCode !== null || child.signalCode !== null) {
      return false;
    }
    child.signalCode = signal;
    child.stdout.end();
    child.stderr.end();
    child.emit("exit", null, signal);
    return true;
  };
  let input = "";
  child.stdin.on("data", (chunk) => {
    input += chunk.toString("utf8");
    const lines = input.split("\n");
    input = lines.pop() ?? "";
    for (const line of lines) {
      if (line.trim()) {
        onRequest(JSON.parse(line), child);
      }
    }
  });
  child.stdin.on("finish", () => {
    if (child.exitCode === null && child.signalCode === null) {
      child.exitCode = 0;
      child.stdout.end();
      child.stderr.end();
      child.emit("exit", 0, null);
    }
  });
  return child;
}

async function testMCPProcessClient() {
  const child = fakeMCPProcess((request, process) => {
    if (request.id === undefined) {
      return;
    }
    if (request.method === "denied") {
      process.stdout.write(
        `${JSON.stringify({
          jsonrpc: "2.0",
          id: request.id,
          error: { code: -32000, message: "denied" },
        })}\n`,
      );
      return;
    }
    process.stdout.write(
      `${JSON.stringify({ jsonrpc: "2.0", id: request.id, result: { method: request.method } })}\n`,
    );
  });
  const session = new MCPProcessClient(child);
  assert.deepEqual(await session.request("ping"), { method: "ping" });
  await assert.rejects(
    () => session.request("denied"),
    (error) => error.code === -32000 && /denied/.test(error.message),
  );
  await session.close();
  await session.close();

  const malformedChild = fakeMCPProcess((_request, process) => process.stdout.write("not-json\n"));
  const malformedSession = new MCPProcessClient(malformedChild);
  await assert.rejects(() => malformedSession.request("ping"), /malformed JSON/);
  await malformedSession.close();
}

function buildBinary(destination) {
  const result = spawnSync("go", ["build", "-o", destination, "./cmd/annas-mcp"], {
    cwd: root,
    encoding: "utf8",
  });
  if (result.status !== 0) {
    throw new Error(result.stderr || result.stdout || "go build failed");
  }
}

function powershellLiteral(value) {
  return `'${value.replaceAll("'", "''")}'`;
}

function npmInvocation(args, command = "npx") {
  if (process.platform !== "win32") {
    return { command, args };
  }
  const cli = path.join(path.dirname(process.execPath), "node_modules", "npm", "bin", `${command}-cli.js`);
  return { command: process.execPath, args: [cli, ...args] };
}

async function createArchive(binaryPath, archivePath, directoryName, extension, binaryName) {
  const staging = path.join(path.dirname(archivePath), "staging");
  const packed = path.join(staging, directoryName);
  await mkdir(packed, { recursive: true });
  await copyFile(binaryPath, path.join(packed, binaryName));
  const result =
    extension === "zip"
      ? process.platform === "win32"
        ? spawnSync(
            "powershell.exe",
            [
              "-NoLogo",
              "-NoProfile",
              "-NonInteractive",
              "-Command",
              `$ErrorActionPreference = 'Stop'; Compress-Archive -Path ${powershellLiteral(packed)} -DestinationPath ${powershellLiteral(archivePath)} -Force`,
            ],
            {
              encoding: "utf8",
            },
          )
        : spawnSync("zip", ["-qr", archivePath, directoryName], {
            cwd: staging,
            encoding: "utf8",
          })
      : spawnSync("tar", ["-czf", archivePath, "-C", staging, directoryName], {
          encoding: "utf8",
        });
  if (result.status !== 0) {
    throw new Error(result.stderr || `${extension} archive command failed`);
  }
}

function startReleaseServer(archivePath, assetName, releaseTag, releaseVersion) {
  let archiveDownloads = 0;
  const checksum = createHash("sha256").update(readFileSync(archivePath)).digest("hex");
  const checksumBody = fixture("checksums.txt")
    .replaceAll("{{ARCHIVE_CHECKSUM}}", checksum)
    .replaceAll("{{ARCHIVE_ASSET}}", assetName);
  const server = createServer((request, response) => {
    if (request.url === "/releases/latest") {
      const origin = `http://127.0.0.1:${server.address().port}`;
      const payload = JSON.parse(
        fixture("release-response.json")
          .replaceAll("{{RELEASE_TAG}}", releaseTag)
          .replaceAll("{{ARCHIVE_ASSET}}", assetName)
          .replaceAll("{{ARCHIVE_URL}}", `${origin}/${assetName}`)
          .replaceAll(
            "{{CHECKSUM_ASSET}}",
            `annas-mcp_${releaseVersion}--checksums.txt`,
          )
          .replaceAll("{{CHECKSUM_URL}}", `${origin}/checksums`),
      );
      response.setHeader("Content-Type", "application/json");
      response.end(JSON.stringify(payload));
      return;
    }
    if (request.url === "/checksums") {
      response.setHeader("Content-Type", "text/plain");
      response.end(checksumBody);
      return;
    }
    if (request.url === `/${assetName}`) {
      archiveDownloads += 1;
      response.setHeader("Content-Type", "application/octet-stream");
      createReadStream(archivePath).pipe(response);
      return;
    }
    response.statusCode = 404;
    response.end("not found");
  });
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      resolve({
        server,
        api: `http://127.0.0.1:${server.address().port}/releases/latest`,
        downloads: () => archiveDownloads,
      });
    });
  });
}

async function closeServer(server) {
  if (!server?.listening) {
    return;
  }
  await new Promise((resolve, reject) => {
    server.close((error) => {
      if (error && error.code !== "ERR_SERVER_NOT_RUNNING") {
        reject(error);
        return;
      }
      resolve();
    });
  });
}

function launcherEnv(api, cache) {
  return {
    ...process.env,
    ANNAS_MCP_RELEASE_API: api,
    ANNAS_MCP_CACHE_DIR: cache,
  };
}

function runCommand(command, args, { cwd = root, env = process.env } = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd,
      env,
      stdio: ["ignore", "pipe", "pipe"],
      timeout: 60_000,
    });
    let stdout = "";
    let stderr = "";
    child.stdout?.on("data", (chunk) => {
      stdout += chunk.toString("utf8");
    });
    child.stderr?.on("data", (chunk) => {
      stderr += chunk.toString("utf8");
    });
    child.on("error", reject);
    child.on("exit", (status, signal) => resolve({ child, status, signal, stdout, stderr }));
  });
}

function runLauncher(args, env, entry = "annas-mcp") {
  return runCommand(process.execPath, [`bin/${entry}.js`, ...args], { env });
}

async function assertHandshake(client, timeoutMs = 10_000) {
  const initialized = await client.initialize(timeoutMs);
  assert.equal(initialized?.serverInfo?.name, "annas-mcp", "initialize did not return annas-mcp");
  const tools = await client.request("tools/list", {}, timeoutMs);
  const names = (tools?.tools || []).map((tool) => tool.name).sort();
  assert.deepEqual(
    names,
    ["account_status", "article_download", "article_search", "book_details", "book_download", "book_search", "metadata_search"],
    `unexpected tools: ${names.join(", ")}`,
  );
}

// Windows reports EPERM when two launchers replace current.json at once.
async function testCacheMetaSharingErrors() {
  const dir = await mkdtemp(path.join(os.tmpdir(), "annas-mcp-meta-"));
  const sharingError = (code) => Object.assign(new Error(code), { code });
  try {
    let calls = 0;
    const flaky = async (from, to) => {
      calls += 1;
      if (calls < 3) throw sharingError("EPERM");
      await rename(from, to);
    };
    await writeCacheMeta(dir, { tag: "v1.0.0" }, { renameImpl: flaky, retryDelayMs: 0 });
    assert.equal(calls, 3, "writeCacheMeta should retry transient sharing errors");
    assert.equal((await readCacheMeta(dir)).tag, "v1.0.0");

    const busy = async () => {
      throw sharingError("EBUSY");
    };
    await assert.rejects(writeCacheMeta(dir, {}, { renameImpl: busy, retryDelayMs: 0 }), /EBUSY/);
    const full = async () => {
      throw sharingError("ENOSPC");
    };
    await assert.rejects(writeCacheMeta(dir, {}, { renameImpl: full, retryDelayMs: 0 }), /ENOSPC/);
    assert.deepEqual(
      readdirSync(dir),
      ["current.json"],
      "failed writes must not leave temporary files behind",
    );
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

async function main() {
  testAssetSelection();
  testInputValidation();
  await testMCPProcessClient();
  await testCacheMetaSharingErrors();
  const target = goreleaserTarget();
  const releaseTag = readFileSync(path.join(root, "internal/version/version.txt"), "utf8").trim();
  assert(/^v[0-9]+\.[0-9]+\.[0-9]+$/.test(releaseTag), `invalid test release version: ${releaseTag}`);
  const releaseVersion = releaseTag.slice(1);
  const work = await mkdtemp(path.join(os.tmpdir(), "annas-mcp-npx-"));
  const cache = path.join(work, "cache");
  const concurrentCache = path.join(work, "concurrent-cache");
  const binaryPath = path.join(work, target.binaryName);
  const assetName = `annas-mcp_${releaseVersion}_${target.os}_${target.arch}.${target.extension}`;
  const archivePath = path.join(work, assetName);
  let release;
  try {
    buildBinary(binaryPath);
    await createArchive(
      binaryPath,
      archivePath,
      `annas-mcp_${releaseVersion}_${target.os}_${target.arch}`,
      target.extension,
      target.binaryName,
    );
    release = await startReleaseServer(archivePath, assetName, releaseTag, releaseVersion);

    const env = launcherEnv(release.api, cache);
    const version = await runLauncher(["--version"], env);
    const expectedVersion = releaseTag;
    assert(version.status === 0, version.stderr || "version command failed");
    assert(version.stdout.includes(expectedVersion), `version stdout was ${JSON.stringify(version.stdout)}`);
    assert(release.downloads() === 1, "version command should download the archive once");

    const nativeHelp = await runCommand(binaryPath, []);
    assert.equal(nativeHelp.status, 0, nativeHelp.stderr);
    assert.match(nativeHelp.stdout, /annas-mcp \[command\]/);
    for (const args of [[], ["--help"], ["book-search", "--help"], ["help", "article-download"]]) {
      const help = await runLauncher(args, env, "annas-cli");
      assert.equal(help.status, 0, help.stderr);
      assert.match(help.stdout, /Usage:\s+annas-cli /);
      assert.doesNotMatch(help.stdout, /annas-mcp/);
      assert.equal(help.stderr, "");
    }
    const cliVersion = await runLauncher(["--version"], env, "annas-cli");
    assert.equal(cliVersion.status, 0, cliVersion.stderr);
    assert.equal(cliVersion.stdout, version.stdout);
    const invalidCLI = await runLauncher(["book-search", "example", "--timeout", "0s"], env, "annas-cli");
    assert.equal(invalidCLI.status, 1, "CLI must preserve the binary's failure exit code");
    assert.equal(invalidCLI.stdout, "");
    assert.match(invalidCLI.stderr, /^\[INVALID_ARGUMENT\].*timeout/);

    const server = spawn(process.execPath, ["bin/annas-mcp.js"], {
      cwd: root,
      env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    const session = new MCPProcessClient(server);
    try {
      await assertHandshake(session);
    } catch (error) {
      throw new Error(`default MCP launcher: ${error.message}`);
    } finally {
      await session.close();
    }
    assert(release.downloads() === 1, "cached launch should not download the archive again");

    const cliServer = spawn(process.execPath, ["bin/annas-cli.js", "mcp"], {
      cwd: root,
      env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    const cliSession = new MCPProcessClient(cliServer);
    try {
      await assertHandshake(cliSession);
    } catch (error) {
      throw new Error(`explicit MCP command through annas-cli: ${error.message}`);
    } finally {
      await cliSession.close();
    }
    assert.equal(release.downloads(), 1, "both launchers must share the binary cache");

    await writeFile(path.join(cache, "current.json"), "{");
    const recovered = await runLauncher(["--version"], env);
    assert(recovered.status === 0, recovered.stderr || "corrupt metadata recovery failed");
    assert(recovered.stdout.includes(expectedVersion), "corrupt metadata did not recover");
    assert(release.downloads() === 2, "corrupt metadata should trigger one archive download");

    const current = JSON.parse(readFileSync(path.join(cache, "current.json"), "utf8"));
    appendFileSync(current.binary, Buffer.from("corrupted cache\n"));
    const repaired = await runLauncher(["--version"], env);
    assert(repaired.status === 0, repaired.stderr || "corrupt binary recovery failed");
    assert(repaired.stdout.includes(expectedVersion), "corrupt binary did not recover");
    assert(release.downloads() === 3, "corrupt binary should trigger one archive download");

    const concurrent = await Promise.all([
      runLauncher(["--version"], launcherEnv(release.api, concurrentCache)),
      runLauncher(["--version"], launcherEnv(release.api, concurrentCache)),
    ]);
    for (const result of concurrent) {
      assert(result.status === 0, result.stderr || "concurrent launcher failed");
      assert(result.stdout.includes(expectedVersion), "concurrent launcher returned the wrong version");
    }
    const concurrentMeta = JSON.parse(
      readFileSync(path.join(concurrentCache, "current.json"), "utf8"),
    );
    assert(concurrentMeta.binarySha256, "concurrent install did not write binary integrity metadata");

    const npxCache = path.join(work, "npx-cache");
    await mkdir(npxCache);
    const npx = npmInvocation([
      "--yes",
      "--loglevel=error",
      "--package",
      process.platform === "win32" ? pathToFileURL(root).href : `file:${root}`,
      "--",
      "annas-mcp",
    ]);
    const viaNpx = spawn(npx.command, npx.args, {
      cwd: work,
      env: launcherEnv(release.api, npxCache),
      stdio: ["pipe", "pipe", "pipe"],
    });
    const npxSession = new MCPProcessClient(viaNpx);
    try {
      await assertHandshake(npxSession, 60_000);
    } catch (error) {
      throw new Error(`npx with explicit annas-mcp command: ${error.message}`);
    } finally {
      await npxSession.close();
    }

    // Install the shipped tarball into an isolated global prefix, exercising
    // npm's Unix symlinks and Windows command shims without changing the user's install.
    const packageEnv = { ...env, npm_config_cache: path.join(work, "npm-cache") };
    const pack = npmInvocation(["pack", "--json", "--ignore-scripts", "--pack-destination", work], "npm");
    const packed = await runCommand(pack.command, pack.args, { env: packageEnv });
    assert.equal(packed.status, 0, packed.stderr);
    const tarball = path.join(work, JSON.parse(packed.stdout)[0].filename);
    const prefix = path.join(work, "global install");
    const install = npmInvocation([
      "install", "--global", "--prefix", prefix, "--offline", "--ignore-scripts", "--no-audit", "--no-fund", tarball,
    ], "npm");
    const installed = await runCommand(install.command, install.args, { cwd: work, env: packageEnv });
    assert.equal(installed.status, 0, installed.stderr);
    for (const name of ["annas-cli", "annas-mcp"]) {
      const executable = process.platform === "win32" ? path.join(prefix, `${name}.cmd`) : path.join(prefix, "bin", name);
      const command = process.platform === "win32" ? "powershell.exe" : executable;
      const args = process.platform === "win32"
        ? ["-NoLogo", "-NoProfile", "-NonInteractive", "-Command", `& ${powershellLiteral(executable)} --help; exit $LASTEXITCODE`]
        : ["--help"];
      const help = await runCommand(command, args, { cwd: work, env: packageEnv });
      assert.equal(help.status, 0, help.stderr);
      assert(help.stdout.includes(`${name} [command]`), `installed ${name} did not show its help`);
    }
    const oneOff = npmInvocation(["--yes", "--offline", `--package=${tarball}`, "annas-cli", "book-search", "--help"]);
    const oneOffHelp = await runCommand(oneOff.command, oneOff.args, { cwd: work, env: packageEnv });
    assert.equal(oneOffHelp.status, 0, oneOffHelp.stderr);
    assert.match(oneOffHelp.stdout, /annas-cli book-search \[query\]/);

    // With multiple bin entries, npx must still pick the package-named MCP entry point.
    const inferred = npmInvocation(["--yes", "--offline", pathToFileURL(tarball).href]);
    const inferredServer = spawn(inferred.command, inferred.args, {
      cwd: work,
      env: packageEnv,
      stdio: ["pipe", "pipe", "pipe"],
    });
    const inferredSession = new MCPProcessClient(inferredServer);
    try {
      await assertHandshake(inferredSession, 60_000);
    } catch (error) {
      throw new Error(`npx with inferred package command: ${error.message}`);
    } finally {
      await inferredSession.close();
    }
    await closeServer(release.server);

    const offline = spawn(process.execPath, ["bin/annas-mcp.js"], {
      cwd: root,
      env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    const offlineSession = new MCPProcessClient(offline);
    try {
      await assertHandshake(offlineSession);
    } catch (error) {
      throw new Error(`${error.message}\noffline start should use the cached binary`);
    } finally {
      await offlineSession.close();
    }

    const mismatched = JSON.parse(readFileSync(path.join(cache, "current.json"), "utf8"));
    mismatched.target.arch = mismatched.target.arch === "amd64" ? "arm64" : "amd64";
    await writeFile(path.join(cache, "current.json"), `${JSON.stringify(mismatched)}\n`);
    const wrongTarget = await runLauncher(["--version"], env);
    assert(
      wrongTarget.status !== 0,
      "offline fallback reused a cache entry built for another architecture",
    );
    console.log("npx launcher test passed");
  } finally {
    await closeServer(release?.server);
    await rm(work, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error(error.stack || error.message);
  process.exit(1);
});
