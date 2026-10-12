import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";

const minimum = 80;
// Packages held to a higher bar, measured by their own tests: the parsers
// and the DOI identity check decide what the tools report.
const packageMinimums = {
  "anna/internal/parse": 95,
  "anna/internal/match": 95,
};
const modulePath = "github.com/SokolskyNikita/annas-mcp/";

const profile = process.argv[2] || "coverage.out";
const result = spawnSync("go", ["tool", "cover", "-func", profile], { encoding: "utf8" });
if (result.error) throw result.error;
if (result.status !== 0) {
  throw new Error(result.stderr || "Could not read Go coverage profile");
}
process.stdout.write(result.stdout);
const match = result.stdout.match(/^total:\s+\(statements\)\s+([\d.]+)%$/m);
if (!match) throw new Error("Go did not report total statement coverage");
const percentage = Number(match[1]);
console.log(`Required coverage: ${minimum}%`);
if (!Number.isFinite(percentage) || percentage < minimum) process.exitCode = 1;

// A profile line is "<file>:<start>,<end> <statements> <count>"; blocks may
// repeat when several test binaries cover the same file.
const blocks = new Map();
for (const line of readFileSync(profile, "utf8").split(/\r?\n/).slice(1)) {
  const fields = line.match(/^(.+):(\d+\.\d+,\d+\.\d+) (\d+) (\d+)$/);
  if (!fields) continue;
  const [, file, range, statements, count] = fields;
  const key = `${file}:${range}`;
  const previous = blocks.get(key);
  blocks.set(key, { file, statements: Number(statements), covered: (previous?.covered ?? false) || Number(count) > 0 });
}
for (const [pkg, required] of Object.entries(packageMinimums)) {
  let statements = 0;
  let covered = 0;
  for (const block of blocks.values()) {
    if (block.file.slice(0, block.file.lastIndexOf("/")) !== modulePath + pkg) continue;
    statements += block.statements;
    if (block.covered) covered += block.statements;
  }
  const share = statements === 0 ? 0 : (100 * covered) / statements;
  console.log(`${pkg}: ${share.toFixed(1)}% (required ${required}%)`);
  if (statements === 0 || share < required) process.exitCode = 1;
}
