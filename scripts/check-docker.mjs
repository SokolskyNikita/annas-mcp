// Checks a built container image: the MCP handshake reports this checkout's
// version and the tools listed in manifest.json.
//
// Usage: node scripts/check-docker.mjs [image]   (default annas-mcp:ci)
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";
import { checkVersion } from "./check-version.mjs";
import { MCPProcessClient } from "./mcp-session.mjs";

const image = process.argv[2] || "annas-mcp:ci";
const tag = checkVersion();
const manifest = JSON.parse(await readFile(new URL("../manifest.json", import.meta.url), "utf8"));
const toolNames = manifest.tools.map((tool) => tool.name).sort();

const child = spawn("docker", ["run", "-i", "--rm", image], { stdio: ["pipe", "pipe", "pipe"] });
const session = new MCPProcessClient(child);
try {
  const initialized = await session.initialize(60_000);
  assert.equal(initialized?.serverInfo?.name, "annas-mcp");
  assert.equal(initialized?.serverInfo?.version, tag, "image reports the wrong version");
  const listed = await session.request("tools/list", {}, 30_000);
  assert.deepEqual((listed?.tools || []).map((tool) => tool.name).sort(), toolNames, "image exposes an unexpected tool set");
} finally {
  await session.close();
}
console.log(`${image}: annas-mcp ${tag} with ${toolNames.length} tools`);
