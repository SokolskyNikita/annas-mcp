// Explicit live acceptance for article workflows. Never part of npm test.
// Uses only OPENALEX_API_KEY. Downloads at most two hosted PDFs by default,
// plus one public repository PDF. Shared output/cache tests fresh-process reuse.
// Requires pdftotext and pdfinfo so complete-file identity is inspected.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { mkdir, mkdtemp, readFile, realpath, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { MCPProcessClient } from "./mcp-session.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const args = process.argv.slice(2);
if (args.includes("--help") || args.includes("-h")) {
  console.log(`Usage: node scripts/test-paper-interfaces-live.mjs [binary] [--include-read] [--resume-output directory]

Build the native binary first, then export OPENALEX_API_KEY and run this opt-in
CLI and stdio MCP acceptance check. Requires pdfinfo and pdftotext on PATH.
Default artifact budget is two hosted PDFs and one public repository PDF.
--include-read adds one hosted TEI artifact and verifies reading continuation.
AA credentials are cleared. This runner never downloads from Anna's Archive.
--resume-output reuses a previous report's files and cache after a failed check.
Reports and downloaded sample files remain in the printed temporary directory.`);
  process.exit(0);
}
const includeRead = args.includes("--include-read");
const resumeIndex = args.indexOf("--resume-output");
const resumeOutput = resumeIndex >= 0 ? args[resumeIndex + 1] : undefined;
assert(resumeIndex < 0 || (resumeOutput && !resumeOutput.startsWith("--")), "--resume-output requires the previous output directory");
const remainingArgs = args.filter((arg, index) => arg !== "--include-read" && (resumeIndex < 0 || (index !== resumeIndex && index !== resumeIndex + 1)));
assert(remainingArgs.length <= 1 && remainingArgs.every(arg => !arg.startsWith("--")), "Usage: node scripts/test-paper-interfaces-live.mjs [binary] [--include-read] [--resume-output directory]");
const binaryArg = remainingArgs[0];
const binary = path.resolve(binaryArg || path.join(root, "annas-mcp"));
const key = process.env.OPENALEX_API_KEY;
assert(key, "Export OPENALEX_API_KEY before running live acceptance");
await stat(binary);
const cases = JSON.parse(await readFile(new URL("testdata/paper-interfaces-live.json", import.meta.url), "utf8"));
const output = await realpath(resumeOutput || await mkdtemp(path.join(os.tmpdir(), "annas-papers-live-")));
if (resumeOutput) {
  const previous = await readFile(path.join(output, "report.json"), "utf8");
  await writeFile(path.join(output, `report-before-resume-${Date.now()}.json`), previous);
}
const downloadDir = path.join(output, "files");
const cacheDir = path.join(output, "cache");
await mkdir(downloadDir, { recursive: true });
await mkdir(cacheDir, { recursive: true });
const report = { started: new Date().toISOString(), binary, output, resumed: Boolean(resumeOutput), hosted_pdf_budget: 2, optional_tei_budget: includeRead ? 1 : 0, calls: [], files: [], passed: false };
const sessions = [];
const diagnostics = [];
const secrets = [key, process.env.ANNAS_SECRET_KEY, process.env.ANNAS_ACCOUNT_COOKIE].filter(Boolean);
let hostedPDFs = 0;

function redact(value) {
  let text = String(value);
  for (const secret of secrets) text = text.split(secret).join("[REDACTED]");
  return text.replace(/https?:\/\/[^\s"']+/g, value => {
    try { const url = new URL(value); return url.search ? `${url.origin}${url.pathname}?[REDACTED]` : value; } catch { return value; }
  });
}
async function checkpoint() {
  await writeFile(path.join(output, "report.json"), JSON.stringify(report, null, 2) + "\n");
}
function environment({ noKeys = false, invalidAA = false } = {}) {
  return { ...process.env, OPENALEX_API_KEY: noKeys ? "" : key, ANNAS_SECRET_KEY: "", ANNAS_ACCOUNT_COOKIE: "", ANNAS_DOWNLOAD_PATH: downloadDir,
    ANNAS_MCP_CACHE_DIR: cacheDir, ANNAS_BASE_URL: invalidAA ? "invalid unrelated AA base URL" : "https://127.0.0.1:1", ANNAS_AUTO_BASE_URL: invalidAA ? "invalid boolean" : "false" };
}
async function run(command, commandArgs, { env = process.env, timeout = 210_000 } = {}) {
  const child = spawn(command, commandArgs, { cwd: output, env, stdio: ["ignore", "pipe", "pipe"] });
  let stdout = "", stderr = "";
  child.stdout.on("data", data => { stdout += data; if (stdout.length > 2 ** 21) child.kill("SIGTERM"); });
  child.stderr.on("data", data => { stderr = (stderr + data).slice(-(64 << 10)); });
  const timer = setTimeout(() => child.kill("SIGTERM"), timeout);
  const code = await new Promise((resolve, reject) => { child.once("error", reject); child.once("close", resolve); }).finally(() => clearTimeout(timer));
  diagnostics.push({ operation: path.basename(command), code, stderr: redact(stderr) });
  return { code, stdout, stderr };
}
async function cli(commandArgs, label, settings = {}) {
  const started = Date.now();
  const result = await run(binary, [...commandArgs, "--timeout", "180s", "--json"], { env: environment(settings) });
  assert.equal(result.code, 0, `${label}: ${redact(result.stderr)}`);
  const data = JSON.parse(result.stdout);
  report.calls.push({ interface: "cli", label, operation: commandArgs[0], elapsed_ms: Date.now() - started, passed: true });
  await checkpoint();
  return data;
}
async function session(settings = {}) {
  const child = spawn(binary, ["mcp"], { cwd: output, env: environment(settings), stdio: ["pipe", "pipe", "pipe"] });
  let stderr = "";
  child.stderr.on("data", chunk => { stderr = (stderr + chunk).slice(-(64 << 10)); });
  child.once("close", code => diagnostics.push({ operation: "mcp", code, stderr: redact(stderr) }));
  const client = new MCPProcessClient(child);
  sessions.push(client);
  assert.equal((await client.initialize()).serverInfo.name, "annas-mcp");
  const { tools } = await client.request("tools/list");
  for (const name of ["article_search", "article_details", "article_download", "article_read", "account_status"]) assert(tools.some(tool => tool.name === name), `Missing ${name}`);
  return client;
}
async function mcp(client, name, arguments_, label) {
  const started = Date.now();
  const result = await client.request("tools/call", { name, arguments: { ...arguments_, timeout_seconds: 180 } }, 210_000);
  assert(!result.isError, `${label}: ${redact(result.content?.find(item => item.type === "text")?.text || "MCP tool failed")}`);
  const text = result.content?.find(item => item.type === "text")?.text;
  const data = result.structuredContent || JSON.parse(text);
  if (result.structuredContent && text) assert.deepEqual(JSON.parse(text), data, "MCP text and structured results disagree");
  report.calls.push({ interface: "mcp", label, operation: name, elapsed_ms: Date.now() - started, passed: true });
  await checkpoint();
  return data;
}
function normalized(value) { return String(value).toLowerCase().normalize("NFKD").replace(/[^a-z0-9]/g, ""); }
function shortID(value) { return String(value).replace(/^https:\/\/openalex.org\//, ""); }
async function digest(file) {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(file)) hash.update(chunk);
  return hash.digest("hex");
}
async function verifyPDF(result, testCase, expected = {}) {
  assert.equal(result.provider, "openalex");
  assert.equal(shortID(result.openalex_id), testCase.id);
  if (expected.reused !== undefined) assert.equal(result.reused, expected.reused);
  if (expected.location) assert.equal(result.location_id, expected.location);
  if (expected.version) assert.equal(result.version, expected.version);
  const file = await realpath(result.path);
  const relative = path.relative(downloadDir, file);
  assert(relative && !relative.startsWith("..") && !path.isAbsolute(relative), "File escaped download directory");
  const info = await stat(file);
  assert(info.isFile() && info.size > 0);
  assert.equal(info.size, result.bytes);
  const bytes = await readFile(file);
  assert(bytes.subarray(0, 5).equals(Buffer.from("%PDF-")), "Invalid PDF signature");
  const sha256 = await digest(file);
  if (result.sha256) assert.equal(result.sha256, sha256);
  const pdfinfo = await run("pdfinfo", [file]);
  assert.equal(pdfinfo.code, 0, "pdfinfo could not parse complete file");
  assert(/Pages:\s+[1-9]/.test(pdfinfo.stdout));
  const text = await run("pdftotext", ["-f", "1", "-l", "5", file, "-"]);
  assert.equal(text.code, 0, "pdftotext could not inspect identity");
  for (const phrase of testCase.pdf_phrases) assert(normalized(text.stdout).includes(normalized(phrase)), `PDF missing expected identity phrase ${phrase}`);
  report.files.push({ case: testCase.label, path: file, bytes: info.size, sha256, reused: result.reused, route: result.route, location_id: result.location_id, version: result.version });
  await checkpoint();
  return { path: file, inode: info.ino, mtime: info.mtimeMs };
}
async function hosted(testCase, operation) {
  assert(hostedPDFs < report.hosted_pdf_budget, "Hosted PDF request budget exceeded");
  hostedPDFs++;
  return operation();
}

console.log(`Paper acceptance output: ${output}`);
await checkpoint();
try {
  for (const command of ["pdfinfo", "pdftotext"]) assert.equal((await run(command, ["-v"], { timeout: 10_000 })).code, 0, `${command} is required`);
  const noKeys = await session({ noKeys: true, invalidAA: true });
  await noKeys.close();
  report.credentialless_discovery_passed = true;
  const client = await session({ invalidAA: true });
  const status = await mcp(client, "account_status", { provider: "openalex" }, "OpenAlex status");
  report.openalex_status = status;

  const paper = cases.hosted_paper;
  const details = await cli(["article-details", paper.id, "--provider", "openalex"], "Paper details tolerate unrelated invalid AA configuration", { invalidAA: true });
  assert.equal(shortID(details.openalex_id), paper.id);
  assert.equal(normalized(details.title), normalized(paper.title));
  const first = await hosted(paper, () => cli(["article-download", paper.doi], "One-call ordinary DOI download"));
  assert.equal(first.route, "hosted_pdf", "Ordinary paper did not exercise hosted PDF retrieval");
  const original = await verifyPDF(first, paper, resumeOutput ? {} : { reused: false });
  const replayed = await mcp(client, "article_download", { doi: paper.doi }, "MCP reuses CLI file");
  const reused = await verifyPDF(replayed, paper, { reused: true });
  assert.deepEqual(reused, original, "Reuse changed the file");

  const thesis = cases.no_doi_thesis;
  const search = await mcp(client, "article_search", { query: thesis.search, work_type: "dissertation", provider: "openalex", limit: 10 }, "Search no-DOI thesis");
  const selected = search.results.find(work => shortID(work.openalex_id) === thesis.id);
  assert(selected, "No expected thesis in first search page");
  assert(!selected.doi, "No-DOI case gained a DOI; update frozen acceptance data");
  assert.equal(selected.work_type, "dissertation");
  assert(selected.download_args, "Search does not give directly reusable download arguments");
  const thesisResult = await hosted(thesis, () => mcp(client, "article_download", selected.download_args, "Replay selected thesis arguments"));
  assert.equal(thesisResult.route, "hosted_pdf", "Thesis did not exercise hosted PDF retrieval");
  await verifyPDF(thesisResult, thesis, resumeOutput ? {} : { reused: false });
  await client.close();

  const fresh = await session();
  const repeated = await mcp(fresh, "article_download", { openalex_id: thesis.id }, "Thesis reuse after MCP restart");
  await verifyPDF(repeated, thesis, { reused: true });
  const cliRepeat = await cli(["article-download", "--openalex-id", thesis.id], "CLI reuses MCP thesis");
  await verifyPDF(cliRepeat, thesis, { reused: true });

  const repository = cases.repository_only;
  const copies = await mcp(fresh, "article_details", { id: repository.id, provider: "openalex" }, "Inspect repository-only work");
  assert(!copies.access.includes("hosted_pdf"), "Frozen repository-only work now has hosted content; update the case");
  let location = copies.locations.find(item => item.id === repository.location_id);
  let copyPage = copies;
  for (let page = 0; !location && copyPage.next_details_args && page < 5; page++) {
    copyPage = await mcp(fresh, "article_details", copyPage.next_details_args, "Replay next copy page");
    location = copyPage.locations.find(item => item.id === repository.location_id);
  }
  assert(location?.download_args, "Pinned repository location has no replayable arguments");
  const direct = await mcp(fresh, "article_download", { ...location.download_args, version: repository.version }, "Download selected repository manuscript");
  assert.equal(direct.route, "external_pdf", "Repository case did not exercise external retrieval");
  await verifyPDF(direct, repository, { ...(resumeOutput ? {} : { reused: false }), location: repository.location_id, version: repository.version });
  const repoReplay = await cli(["article-download", "--openalex-id", repository.id, "--location-id", repository.location_id, "--version", repository.version], "CLI reuses pinned repository manuscript");
  await verifyPDF(repoReplay, repository, { reused: true, location: repository.location_id, version: repository.version });

  if (includeRead) {
    const reading = await mcp(fresh, "article_read", { id: paper.id, limit: 1000 }, "Read bounded structured text");
    assert(reading.text && reading.text.length <= 1200, "Reading is empty or exceeds requested bound");
    assert(reading.next_args, "Expected continuation for full paper");
    const next = await mcp(fresh, "article_read", reading.next_args, "Replay reading continuation");
    assert(next.text && next.text !== reading.text);
    assert.equal(next.sha256, reading.sha256);
    report.structured_reading_passed = true;
  }
  report.passed = true;
} catch (error) {
  report.error = redact(error.message);
  process.exitCode = 1;
} finally {
  for (const client of sessions) await client.close();
  report.finished = new Date().toISOString();
  report.hosted_pdf_attempts = hostedPDFs;
  report.diagnostics = diagnostics;
  await checkpoint();
  console.log(JSON.stringify({ passed: report.passed, files_verified: report.files.length, hosted_pdf_attempts: hostedPDFs, error: report.error, report: path.join(output, "report.json") }, null, 2));
}
