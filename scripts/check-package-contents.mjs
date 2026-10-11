// Checks that test data stays out of everything the project ships: the Go
// binary, the npm package, the container image, the release archives and the
// MCP Bundle. internal/anna/parse/testdata holds about a megabyte of captured
// pages that only the tests read.
//
// Usage: node scripts/check-package-contents.mjs
import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";

const root = new URL("../", import.meta.url);
const failures = [];

function run(command, args) {
  const result = spawnSync(command, args, { cwd: root, encoding: "utf8", shell: process.platform === "win32" });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} ${args.join(" ")} failed:\n${result.stderr}`);
  return result.stdout;
}

// Only _test.go files may embed testdata; files embedded by other files end
// up in the binary.
const embeds = run("go", ["list", "-f", "{{.ImportPath}}\t{{join .EmbedFiles \" \"}}", "./..."]);
for (const line of embeds.trim().split("\n")) {
  const [pkg, files = ""] = line.split("\t");
  if (files.includes("testdata")) failures.push(`${pkg} embeds test data in the binary: ${files}`);
}

const [pack] = JSON.parse(run("npm", ["pack", "--dry-run", "--json", "--ignore-scripts"]));
for (const file of pack.files) {
  if (file.path.includes("testdata") || file.path.startsWith("internal/")) failures.push(`the npm package includes ${file.path}`);
}

const dockerignore = (await readFile(new URL(".dockerignore", root), "utf8")).split(/\r?\n/).map((line) => line.trim());
if (!dockerignore.includes("**/testdata/")) failures.push(".dockerignore no longer excludes **/testdata/");

const goreleaser = await readFile(new URL(".goreleaser", root), "utf8");
for (const line of goreleaser.split(/\r?\n/).filter((line) => /^\s*files:/.test(line))) {
  if (/testdata|internal|\*\*/.test(line)) failures.push(`.goreleaser archives include more than the docs: ${line.trim()}`);
}

const bundle = await readFile(new URL("scripts/pack-mcpb.mjs", root), "utf8");
const entries = bundle.match(/for \(const entry of (\[[^\]]*\])\)/);
if (!entries) failures.push("could not find the MCP Bundle's file list in scripts/pack-mcpb.mjs");
else if (/testdata|internal/.test(entries[1])) failures.push(`the MCP Bundle includes ${entries[1]}`);

if (failures.length > 0) {
  console.error(failures.map((failure) => `- ${failure}`).join("\n"));
  process.exit(1);
}
console.log("Test data stays out of the binary, the npm package, the image, the archives and the bundle.");
