// Publishes the current release's MCP Bundle to Smithery.
//
// Usage: node scripts/publish-smithery.mjs [--dry-run] [org/name]
//
// Smithery rejects bundle tools without inputSchema, while the MCPB schema
// forbids that field (smithery-ai/cli issue #806). So this downloads the
// released annas-mcp.mcpb, asks that release's server for its tool schemas,
// and publishes a copy whose manifest carries them. The release asset itself
// stays valid MCPB. Requires `zip` and a prior `smithery auth login`.
import { spawn, spawnSync } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { GITHUB_REPO } from "../lib/launcher.js";
import { checkVersion } from "./check-version.mjs";
import { MCPProcessClient } from "./mcp-session.mjs";

const SMITHERY_CLI = "@smithery/cli@4.11.1";
const DEFAULT_NAME = "sokolx/annas-mcp";
const root = fileURLToPath(new URL("../", import.meta.url));
const args = process.argv.slice(2);
const dryRun = args.includes("--dry-run");
const name = args.find((arg) => !arg.startsWith("--")) || DEFAULT_NAME;
const tag = checkVersion();
const outDir = path.join(root, "dist", "smithery");
const bundle = path.join(outDir, "annas-mcp.mcpb");

function run(command, commandArgs, options = {}) {
  const result = spawnSync(command, commandArgs, {
    stdio: "inherit",
    shell: process.platform === "win32",
    ...options,
  });
  if (result.error || result.status !== 0) {
    throw new Error(`${command} ${commandArgs[0]} failed${result.error ? `: ${result.error.message}` : ""}`);
  }
}

async function downloadReleaseBundle() {
  const url = `https://github.com/${GITHUB_REPO}/releases/download/${tag}/annas-mcp.mcpb`;
  const response = await fetch(url, { redirect: "follow" });
  if (!response.ok) {
    throw new Error(`Download failed (${response.status}) for ${url}; is ${tag} released?`);
  }
  await mkdir(outDir, { recursive: true });
  await writeFile(bundle, Buffer.from(await response.arrayBuffer()));
}

// Runs this tag's release binary through the launcher and lists its tools.
async function releasedToolSchemas() {
  const cache = await mkdtemp(path.join(os.tmpdir(), "annas-mcp-smithery-"));
  const child = spawn(process.execPath, [path.join(root, "bin", "annas-mcp.js"), "mcp"], {
    cwd: cache,
    env: {
      ...process.env,
      ANNAS_MCP_CACHE_DIR: cache,
      ANNAS_MCP_RELEASE_API: `https://api.github.com/repos/${GITHUB_REPO}/releases/tags/${tag}`,
      ANNAS_ACCOUNT_COOKIE: "",
      ANNAS_SECRET_KEY: "",
      ANNAS_DOWNLOAD_PATH: "",
    },
    stdio: ["pipe", "pipe", "pipe"],
  });
  const client = new MCPProcessClient(child);
  try {
    const initialized = await client.initialize(60_000);
    if (initialized.serverInfo.version !== tag) {
      throw new Error(`Expected server ${tag}, got ${initialized.serverInfo.version}`);
    }
    const { tools } = await client.request("tools/list");
    return new Map(tools.map((tool) => [tool.name, tool.inputSchema]));
  } finally {
    await client.close();
    await rm(cache, { recursive: true, force: true });
  }
}

await downloadReleaseBundle();
const schemas = await releasedToolSchemas();
const manifest = JSON.parse(await readFile(path.join(root, "manifest.json"), "utf8"));
for (const tool of manifest.tools) {
  const schema = schemas.get(tool.name);
  if (!schema) {
    throw new Error(`Release ${tag} has no tool named ${tool.name}`);
  }
  tool.inputSchema = schema;
}
if (schemas.size !== manifest.tools.length) {
  throw new Error(`manifest.json lists ${manifest.tools.length} tools; release ${tag} has ${schemas.size}`);
}
// Replace manifest.json inside the downloaded bundle.
const patch = await mkdtemp(path.join(os.tmpdir(), "annas-mcp-manifest-"));
try {
  await writeFile(path.join(patch, "manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`);
  run("zip", ["-q", bundle, "manifest.json"], { cwd: patch });
} finally {
  await rm(patch, { recursive: true, force: true });
}

console.log(`Prepared ${bundle} for ${name} (${tag}, ${schemas.size} tools)`);
if (!dryRun) {
  run("npx", ["-y", SMITHERY_CLI, "mcp", "publish", bundle, "-n", name]);
}
