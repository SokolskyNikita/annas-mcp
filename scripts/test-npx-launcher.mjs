import { createHash } from "node:crypto";
import { spawn, spawnSync } from "node:child_process";
import { appendFileSync, createReadStream, readFileSync } from "node:fs";
import { copyFile, mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { checksumFor, goreleaserTarget, selectAsset } from "../bin/annas-mcp.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function testAssetSelection() {
  const darwin = goreleaserTarget("darwin", "arm64");
  const windows = goreleaserTarget("win32", "x64");
  const assets = [
    { name: "annas-mcp_0.0.6--checksums.txt" },
    {
      name: "annas-mcp_0.0.6_darwin_arm64.tar.xz",
      browser_download_url: "https://example.test/darwin",
    },
    {
      name: "annas-mcp_0.0.6_linux_arm64.tar.xz",
      browser_download_url: "https://example.test/linux",
    },
    {
      name: "annas-mcp_0.0.6_windows_amd64.zip",
      browser_download_url: "https://example.test/windows",
    },
  ];
  assert(
    selectAsset(assets, darwin).name === "annas-mcp_0.0.6_darwin_arm64.tar.xz",
    "darwin asset was not selected",
  );
  assert(
    selectAsset(assets, windows).name === "annas-mcp_0.0.6_windows_amd64.zip",
    "windows asset was not selected",
  );
  let rejected = false;
  try {
    selectAsset(assets, goreleaserTarget("linux", "arm"));
  } catch {
    rejected = true;
  }
  assert(rejected, "missing linux/arm asset should fail");
  const digest = "a".repeat(64);
  assert(
    checksumFor(`${digest}  annas-mcp_0.0.6_darwin_arm64.tar.xz\n`, "annas-mcp_0.0.6_darwin_arm64.tar.xz") === digest,
    "checksum line was not parsed",
  );
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

function npxInvocation(args) {
  if (process.platform !== "win32") {
    return { command: "npx", args };
  }
  const npxCli = path.join(path.dirname(process.execPath), "node_modules", "npm", "bin", "npx-cli.js");
  return { command: process.execPath, args: [npxCli, ...args] };
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
      : spawnSync("tar", ["-cJf", archivePath, "-C", staging, directoryName], {
          encoding: "utf8",
        });
  if (result.status !== 0) {
    throw new Error(result.stderr || `${extension} archive command failed`);
  }
}

function startReleaseServer(archivePath, assetName, releaseTag, releaseVersion) {
  let archiveDownloads = 0;
  const checksum = createHash("sha256").update(readFileSync(archivePath)).digest("hex");
  const checksumBody = `${checksum}  ${assetName}\n`;
  const server = createServer((request, response) => {
    if (request.url === "/releases/latest") {
      const origin = `http://127.0.0.1:${server.address().port}`;
      const payload = {
        tag_name: releaseTag,
        assets: [
          {
            name: assetName,
            browser_download_url: `${origin}/${assetName}`,
          },
          {
            name: `annas-mcp_${releaseVersion}--checksums.txt`,
            browser_download_url: `${origin}/checksums`,
          },
        ],
      };
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

function runLauncher(args, env, stdio = ["ignore", "pipe", "pipe"]) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, ["bin/annas-mcp.js", ...args], {
      cwd: root,
      env,
      stdio,
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

function readMessages(stream) {
  let buffer = "";
  const messages = [];
  stream.on("data", (chunk) => {
    buffer += chunk.toString("utf8");
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? "";
    for (const line of lines) {
      const trimmed = line.trim();
      if (!trimmed) {
        continue;
      }
      messages.push(JSON.parse(trimmed));
    }
  });
  return messages;
}

function waitFor(messages, predicate, timeoutMs, label, child) {
  return new Promise((resolve, reject) => {
    let timer;
    let timeout;
    let settled = false;
    const cleanup = () => {
      clearInterval(timer);
      clearTimeout(timeout);
      child?.off("error", onError);
      child?.off("exit", onExit);
    };
    const finish = (callback, value) => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      callback(value);
    };
    const onError = (error) => {
      finish(reject, new Error(`${label} child error: ${error.message}`));
    };
    const onExit = (status, signal) => {
      finish(
        reject,
        new Error(
          `${label} child exited before response (code ${status ?? "null"}, signal ${signal ?? "none"}). Messages: ${JSON.stringify(messages)}`,
        ),
      );
    };
    const check = () => {
      const found = messages.find(predicate);
      if (found) {
        finish(resolve, found);
      }
    };

    child?.once("error", onError);
    child?.once("exit", onExit);
    if (child && (child.exitCode !== null || child.signalCode !== null)) {
      onExit(child.exitCode, child.signalCode);
      return;
    }
    check();
    if (settled) {
      return;
    }
    timer = setInterval(check, 20);
    timeout = setTimeout(() => {
      finish(
        reject,
        new Error(`Timed out waiting for ${label}. Messages: ${JSON.stringify(messages)}`),
      );
    }, timeoutMs);
  });
}

async function handshake(child, timeoutMs = 10_000) {
  const messages = readMessages(child.stdout);
  child.stdin.write(
    `${JSON.stringify({
      jsonrpc: "2.0",
      id: 1,
      method: "initialize",
      params: {
        protocolVersion: "2025-03-26",
        capabilities: {},
        clientInfo: { name: "annas-mcp-test", version: "0.0.0" },
      },
    })}\n`,
  );
  const initialized = await waitFor(
    messages,
    (message) => message.id === 1,
    timeoutMs,
    "initialize",
    child,
  );
  assert(initialized.result?.serverInfo?.name === "annas-mcp", "initialize did not return annas-mcp");
  child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" })}\n`);
  child.stdin.write(
    `${JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/list", params: {} })}\n`,
  );
  const tools = await waitFor(
    messages,
    (message) => message.id === 2,
    timeoutMs,
    "tools/list",
    child,
  );
  const names = (tools.result?.tools || []).map((tool) => tool.name).sort();
  assert(
    JSON.stringify(names) ===
      JSON.stringify(["article_download", "article_search", "book_download", "book_search"]),
    `unexpected tools: ${names.join(", ")}`,
  );
}

async function stopChild(child) {
  if (child.exitCode !== null || child.signalCode !== null) {
    return;
  }
  child.stdin.end();
  await new Promise((resolve) => {
    const timer = setTimeout(() => {
      child.kill("SIGTERM");
    }, 2_000);
    child.on("exit", () => {
      clearTimeout(timer);
      resolve();
    });
  });
}

async function main() {
  testAssetSelection();
  const target = goreleaserTarget();
  const releaseTag = readFileSync(path.join(root, "internal/version/version.txt"), "utf8").trim();
  assert(/^v[0-9]+\.[0-9]+\.[0-9]+$/.test(releaseTag), `invalid test release version: ${releaseTag}`);
  const releaseVersion = releaseTag.slice(1);
  const work = await mkdtemp(path.join(os.tmpdir(), "annas-mcp-npx-"));
  const cache = path.join(work, "cache");
  const concurrentCache = path.join(work, "concurrent-cache");
  const binaryPath = path.join(work, "annas-mcp");
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

    const server = spawn(process.execPath, ["bin/annas-mcp.js"], {
      cwd: root,
      env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    const stderr = [];
    server.stderr.on("data", (chunk) => stderr.push(chunk.toString("utf8")));
    try {
      await handshake(server);
    } catch (error) {
      throw new Error(`${error.message}\nstderr: ${stderr.join("")}`);
    } finally {
      await stopChild(server);
    }
    assert(release.downloads() === 1, "cached launch should not download the archive again");

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
    const npx = npxInvocation([
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
    const npxStderr = [];
    viaNpx.stderr.on("data", (chunk) => npxStderr.push(chunk.toString("utf8")));
    try {
      await handshake(viaNpx, 60_000);
    } catch (error) {
      throw new Error(`${error.message}\nnpx stderr: ${npxStderr.join("")}`);
    } finally {
      await stopChild(viaNpx);
    }
    await closeServer(release.server);

    const offline = spawn(process.execPath, ["bin/annas-mcp.js"], {
      cwd: root,
      env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    try {
      await handshake(offline);
    } catch (error) {
      throw new Error(`${error.message}\noffline start should use the cached binary`);
    } finally {
      await stopChild(offline);
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
