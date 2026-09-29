// Builds dist/annas-mcp.mcpb, an MCP Bundle for Claude Desktop and Smithery.
// The bundle holds only the npx launcher, which downloads and verifies the
// release binary on first run, so one bundle serves every platform.
import { spawnSync } from "node:child_process";
import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { checkVersion } from "./check-version.mjs";

const MCPB_CLI = "@anthropic-ai/mcpb@2.1.2";
const root = fileURLToPath(new URL("../", import.meta.url));
const dist = path.join(root, "dist");
const stage = path.join(dist, "mcpb");
const output = path.join(dist, "annas-mcp.mcpb");

function mcpb(...args) {
  const result = spawnSync("npx", ["-y", MCPB_CLI, ...args], {
    cwd: root,
    stdio: "inherit",
    shell: process.platform === "win32",
  });
  if (result.status !== 0) {
    throw new Error(`mcpb ${args[0]} failed`);
  }
}

checkVersion();
const pkg = JSON.parse(await readFile(path.join(root, "package.json"), "utf8"));
await rm(stage, { recursive: true, force: true });
await rm(output, { force: true });
await mkdir(stage, { recursive: true });
for (const entry of ["bin", "lib", "manifest.json", "LICENSE", "README.md"]) {
  await cp(path.join(root, entry), path.join(stage, entry), { recursive: true });
}
// lib/ is ESM; the bundle needs its own package.json to say so.
const bundlePackage = {
  name: pkg.name,
  version: pkg.version,
  license: pkg.license,
  type: pkg.type,
  private: true,
};
await writeFile(path.join(stage, "package.json"), `${JSON.stringify(bundlePackage, null, 2)}\n`);

mcpb("validate", path.join(stage, "manifest.json"));
mcpb("pack", stage, output);
console.log(output);
