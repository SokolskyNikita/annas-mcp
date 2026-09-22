import { createHash } from "node:crypto";
import { spawn, spawnSync } from "node:child_process";
import { createReadStream, readFileSync } from "node:fs";
import { mkdtemp, mkdir, rm } from "node:fs/promises";
import { createServer } from "node:http";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
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

function createArchive(binaryPath, archivePath, directoryName) {
  const staging = path.join(path.dirname(archivePath), "staging");
  const packed = path.join(staging, directoryName);
  spawnSync("mkdir", ["-p", packed], { stdio: "inherit" });
  spawnSync("cp", [binaryPath, path.join(packed, "annas-mcp")], { stdio: "inherit" });
  const result = spawnSync("tar", ["-cJf", archivePath, "-C", staging, directoryName], {
    encoding: "utf8",
  });
  if (result.status !== 0) {
    throw new Error(result.stderr || "tar failed");
  }
}

function startReleaseServer(archivePath, assetName) {
  let archiveDownloads = 0;
  const checksum = createHash("sha256").update(readFileSync(archivePath)).digest("hex");
  const checksumBody = `${checksum}  ${assetName}\n`;
  const server = createServer((request, response) => {
    if (request.url === "/releases/latest") {
      const origin = `http://127.0.0.1:${server.address().port}`;
      const payload = {
        tag_name: "v0.0.6",
        assets: [
          {
            name: assetName,
            browser_download_url: `${origin}/${assetName}`,
          },
          {
            name: "annas-mcp_0.0.6--checksums.txt",
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
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      resolve({
        server,
        api: `http://127.0.0.1:${server.address().port}/releases/latest`,
        downloads: () => archiveDownloads,
      });
    });
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

function waitFor(messages, predicate, timeoutMs, label) {
  const started = Date.now();
  return new Promise((resolve, reject) => {
    const timer = setInterval(() => {
      const found = messages.find(predicate);
      if (found) {
        clearInterval(timer);
        resolve(found);
        return;
      }
      if (Date.now() - started > timeoutMs) {
        clearInterval(timer);
        reject(new Error(`Timed out waiting for ${label}. Messages: ${JSON.stringify(messages)}`));
      }
    }, 20);
  });
}

async function handshake(child) {
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
  const initialized = await waitFor(messages, (message) => message.id === 1, 10_000, "initialize");
  assert(initialized.result?.serverInfo?.name === "annas-mcp", "initialize did not return annas-mcp");
  child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" })}\n`);
  child.stdin.write(
    `${JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/list", params: {} })}\n`,
  );
  const tools = await waitFor(messages, (message) => message.id === 2, 10_000, "tools/list");
  const names = (tools.result?.tools || []).map((tool) => tool.name).sort();
  assert(
    JSON.stringify(names) ===
      JSON.stringify(["article_download", "article_search", "book_download", "book_search"]),
    `unexpected tools: ${names.join(", ")}`,
  );
}

async function stopChild(child) {
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
  assert(target.extension === "tar.xz", "this test builds a tar.xz archive for the host OS");
  const work = await mkdtemp(path.join(os.tmpdir(), "annas-mcp-npx-"));
  const cache = path.join(work, "cache");
  const binaryPath = path.join(work, "annas-mcp");
  const assetName = `annas-mcp_0.0.6_${target.os}_${target.arch}.tar.xz`;
  const archivePath = path.join(work, assetName);
  try {
    buildBinary(binaryPath);
    await mkdir(cache);
    createArchive(binaryPath, archivePath, `annas-mcp_0.0.6_${target.os}_${target.arch}`);
    const release = await startReleaseServer(archivePath, assetName);

    const version = await new Promise((resolve, reject) => {
      const child = spawn(process.execPath, ["bin/annas-mcp.js", "--version"], {
        cwd: root,
        env: {
          ...process.env,
          ANNAS_MCP_RELEASE_API: release.api,
          ANNAS_MCP_CACHE_DIR: cache,
        },
        stdio: ["ignore", "pipe", "pipe"],
      });
      let stdout = "";
      let stderr = "";
      child.stdout.on("data", (chunk) => {
        stdout += chunk.toString("utf8");
      });
      child.stderr.on("data", (chunk) => {
        stderr += chunk.toString("utf8");
      });
      child.on("error", reject);
      child.on("exit", (status) => resolve({ status, stdout, stderr }));
    });
    assert(version.status === 0, version.stderr || "version command failed");
    assert(version.stdout.includes("v0.0.6"), `version stdout was ${JSON.stringify(version.stdout)}`);
    assert(release.downloads() === 1, "version command should download the archive once");

    const server = spawn(process.execPath, ["bin/annas-mcp.js"], {
      cwd: root,
      env: {
        ...process.env,
        ANNAS_MCP_RELEASE_API: release.api,
        ANNAS_MCP_CACHE_DIR: cache,
      },
      stdio: ["pipe", "pipe", "pipe"],
    });
    const stderr = [];
    server.stderr.on("data", (chunk) => stderr.push(chunk.toString("utf8")));
    try {
      await handshake(server);
    } catch (error) {
      throw new Error(`${error.message}\nstderr: ${stderr.join("")}`);
    }
    assert(release.downloads() === 1, "cached launch should not download the archive again");
    await stopChild(server);

    const npxCache = path.join(work, "npx-cache");
    await mkdir(npxCache);
    const viaNpx = spawn(
      "npx",
      ["--yes", "--loglevel=error", "--package", `file:${root}`, "--", "annas-mcp"],
      {
        cwd: work,
        env: {
          ...process.env,
          ANNAS_MCP_RELEASE_API: release.api,
          ANNAS_MCP_CACHE_DIR: npxCache,
        },
        stdio: ["pipe", "pipe", "pipe"],
      },
    );
    const npxStderr = [];
    viaNpx.stderr.on("data", (chunk) => npxStderr.push(chunk.toString("utf8")));
    try {
      await handshake(viaNpx);
    } catch (error) {
      throw new Error(`${error.message}\nnpx stderr: ${npxStderr.join("")}`);
    }
    await stopChild(viaNpx);
    release.server.close();

    const offline = spawn(process.execPath, ["bin/annas-mcp.js"], {
      cwd: root,
      env: {
        ...process.env,
        ANNAS_MCP_RELEASE_API: release.api,
        ANNAS_MCP_CACHE_DIR: cache,
      },
      stdio: ["pipe", "pipe", "pipe"],
    });
    try {
      await handshake(offline);
    } catch (error) {
      throw new Error(`${error.message}\noffline start should use the cached binary`);
    }
    await stopChild(offline);
    console.log("npx launcher test passed");
  } finally {
    await rm(work, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error(error.stack || error.message);
  process.exit(1);
});
