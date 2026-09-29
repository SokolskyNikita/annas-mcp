import { spawnSync } from "node:child_process";

const minimum = 80;
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
