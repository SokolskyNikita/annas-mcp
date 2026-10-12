// Check both dependency ownership and consumption from a separate Go module.
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, rmSync, copyFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { gunzipSync } from "node:zlib";
const root = fileURLToPath(new URL("../", import.meta.url));
const prefix = "github.com/SokolskyNikita/annas-mcp/";
function go(args, cwd = root, extraEnv = {}) {
 const result = spawnSync("go", args, { cwd, encoding: "utf8", env: { ...process.env, ...extraEnv } });
 if (result.error) throw result.error;
 if (result.status !== 0) throw new Error(`go ${args.join(" ")} failed:\n${result.stderr}\n${result.stdout}`);
 return result.stdout.trim();
}
const dependencies = go(["list", "-deps", "-f", "{{.ImportPath}}", "./anna"]).split("\n");
for (const pkg of dependencies) {
 if ((pkg.startsWith(prefix) && !pkg.startsWith(prefix + "anna")) || /cobra|modelcontextprotocol|godotenv|go.uber.org\/zap/.test(pkg)) throw new Error(`SDK runtime depends on ${pkg}`);
}
const packages = go(["list", "-f", '{{.ImportPath}}|{{join .Imports " "}}|{{join .TestImports " "}}|{{join .XTestImports " "}}', "./..."]);
for (const line of packages.split("\n")) {
 const [pkg, ...groups] = line.split("|");
 for (const dependency of groups.join(" ").split(" ").filter(Boolean)) {
  if (!dependency.startsWith(prefix)) continue;
  if (pkg.startsWith(prefix + "anna") && !dependency.startsWith(prefix + "anna")) throw new Error(`${pkg} imports application package ${dependency}`);
  if (!pkg.startsWith(prefix + "anna") && dependency.startsWith(prefix + "anna/")) throw new Error(`${pkg} imports SDK implementation ${dependency}`);
 }
}
const temp = mkdtempSync(path.join(tmpdir(), "annas-sdk-consumer-"));
try {
 const version = readFileSync(path.join(root, "go.mod"), "utf8").match(/^go (.+)$/m)[1];
 writeFileSync(path.join(temp, "go.mod"), `module example.com/independent-consumer\n\ngo ${version}\n\nrequire ${prefix.slice(0,-1)} v0.0.0\nreplace ${prefix.slice(0,-1)} => ${JSON.stringify(root)}\n`);
 copyFileSync(path.join(root, "anna/scripts/testdata/consumer/main.go"), path.join(temp, "main.go"));
 writeFileSync(path.join(temp, "search.html"), gunzipSync(readFileSync(path.join(root, "anna/internal/parse/testdata/pages/search_filtered_partial.html.gz"))));
 console.log(go(["run", "-mod=mod", "."], temp, { GOWORK: "off", GOPROXY: "off", GOSUMDB: "off" }));
 console.log("SDK runtime and test dependency boundaries passed");
} finally { rmSync(temp, { recursive: true, force: true }); }
