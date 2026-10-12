// Explicitly invoked live verification. Searches/downloads six public-domain
// books through each application interface and checks hash-based local reuse.
// Uses configured credentials and download allowance. Never part of npm test.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { mkdir, mkdtemp, readFile, realpath, rename, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { MCPProcessClient } from "./mcp-session.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const binary = path.resolve(process.argv[2] || path.join(root, "annas-mcp"));
await stat(binary);
const cases = JSON.parse(await readFile(new URL("testdata/book-interfaces-live.json", import.meta.url), "utf8"));
assert(cases.books.length >= 6);
const output = await realpath(await mkdtemp(path.join(os.tmpdir(), "annas-refactor-live-")));
const cache = path.join(output, "private-session");
for (const dir of [cache, "mcp", "cli", "offline-cache", "logs"]) await mkdir(path.isAbsolute(dir) ? dir : path.join(output, dir));
const secrets = [process.env.ANNAS_SECRET_KEY, process.env.ANNAS_ACCOUNT_COOKIE].filter(Boolean);
try {
  for (const line of (await readFile(path.join(root, ".env"), "utf8")).split(/\r?\n/)) {
    const match = line.match(/^\s*(?:export\s+)?ANNAS_(?:SECRET_KEY|ACCOUNT_COOKIE)\s*=\s*(.*)\s*$/);
    if (match) secrets.push(match[1].trim().replace(/^(['"])(.*)\1$/, "$2"));
  }
} catch (error) { if (error.code !== "ENOENT") throw error; }
function redact(value) {
  let text = String(value);
  for (const secret of secrets.filter(Boolean)) text = text.split(secret).join("[REDACTED]");
  return text.replace(/(aa_account_id2=)[^\s;"']+/gi, "$1[REDACTED]")
    .replace(/https?:\/\/[^\s"']+/g, (url) => { try { const u = new URL(url); if (u.search) return `${u.origin}${u.pathname}?[REDACTED]`; } catch {} return url; });
}
async function digest(file, algorithm = "md5") {
  const hash = createHash(algorithm);
  for await (const chunk of createReadStream(file)) hash.update(chunk);
  return hash.digest("hex");
}
const report = { started: new Date().toISOString(), binary, binary_sha256: await digest(binary, "sha256"), output, calls: [], downloads: [], deduplication: [], logs: [], passed: false };
const sessions = [];
const logBuffers = new Map();
async function checkpoint() { await writeFile(path.join(output, "report.json"), JSON.stringify(report, null, 2) + "\n"); }
function environment(mode, offline = false) {
  return { ...process.env, ANNAS_DOWNLOAD_PATH: path.join(output, mode), ANNAS_ACCOUNT_COOKIE: "", ANNAS_MCP_CACHE_DIR: cache,
    ...(offline ? { ANNAS_SECRET_KEY: "", ANNAS_ACCOUNT_COOKIE: "", ANNAS_BASE_URL: "https://127.0.0.1:1", ANNAS_AUTO_BASE_URL: "false", ANNAS_MCP_CACHE_DIR: path.join(output, "offline-cache") } : {}) };
}
function capture(child, label) {
  let stderr = "";
  child.stderr.on("data", (chunk) => { stderr += chunk.toString(); logBuffers.set(label, stderr); });
  logBuffers.set(label, "");
}
async function mcpSession(offline = false) {
  const child = spawn(binary, ["mcp"], { cwd: offline ? output : root, env: environment("mcp", offline), stdio: ["pipe", "pipe", "pipe"] });
  capture(child, offline ? "mcp-offline" : "mcp-live");
  const client = new MCPProcessClient(child);
  sessions.push(client);
  const initialized = await client.initialize();
  assert.equal(initialized.serverInfo.name, "annas-mcp");
  return client;
}
async function mcpCall(client, name, args, label) {
  const start = Date.now();
  const result = await client.request("tools/call", { name, arguments: { ...args, timeout_seconds: 180 } }, 210_000);
  assert(!result.isError, redact(result.content?.find(x => x.type === "text")?.text || "MCP tool failed"));
  assert(result.structuredContent, "missing structured MCP output");
  assert.deepEqual(JSON.parse(result.content.find(x => x.type === "text").text), result.structuredContent);
  report.calls.push({ interface: "mcp", label, operation: name, elapsed_ms: Date.now() - start, passed: true });
  await checkpoint();
  return result.structuredContent;
}
let cliSequence = 0;
async function cliCall(args, label, { offline = false, json = true } = {}) {
  const start = Date.now();
  const child = spawn(binary, [...args, "--timeout", "180s", ...(json ? ["--json"] : [])], { cwd: offline ? output : root, env: environment("cli", offline), stdio: ["ignore", "pipe", "pipe"] });
  const logLabel = `cli-${String(++cliSequence).padStart(2, "0")}`;
  capture(child, logLabel);
  let stdout = "";
  child.stdout.on("data", chunk => { stdout += chunk.toString(); });
  const timer = setTimeout(() => child.kill("SIGTERM"), 210_000);
  const code = await new Promise((resolve, reject) => { child.once("error", reject); child.once("close", resolve); }).finally(() => clearTimeout(timer));
  assert.equal(code, 0, `${label}: ${redact(logBuffers.get(logLabel))}`);
  report.calls.push({ interface: "cli", label, operation: args[0], elapsed_ms: Date.now() - start, exit_code: code, passed: true });
  await checkpoint();
  return json ? JSON.parse(stdout) : stdout;
}
const normalized = value => value.toLowerCase().normalize("NFKD").replace(/[^a-z0-9]/g, "");
function choose(results, book) {
  const authorWords = book.author.toLowerCase().match(/[a-z]+/g);
  return results.find(record => record.fast_download && /^epub$/i.test(record.format) && normalized(record.title) === normalized(book.title)
    && authorWords.every(word => (record.authors.toLowerCase().match(/[a-z]+/g) || []).includes(word)));
}
async function verifyFile(result, record, mode) {
  const file = await realpath(result.path);
  const relative = path.relative(path.join(output, mode), file);
  assert(relative && !relative.startsWith("..") && !path.isAbsolute(relative));
  const info = await stat(file);
  assert.equal(info.size, result.bytes);
  assert(info.size > 0 && info.isFile());
  assert.equal(await digest(file), record.hash.toLowerCase());
  return { path: file, bytes: info.size, inode: info.ino, mtime_ms: info.mtimeMs, md5: record.hash.toLowerCase() };
}
async function search(mode, client, book) {
  for (let page = 1; page <= 5; page++) {
    const result = mode === "mcp" ? await mcpCall(client, "book_search", { query: book.title, author: book.author, language: "en", format: "epub", sort: "smallest", downloadable_only: true, limit: 50, page }, `Search ${book.title}`)
      : await cliCall(["book-search", book.title, "--author", book.author, "--language", "en", "--format", "epub", "--sort", "smallest", "--downloadable-only", "--limit", "50", "--page", String(page)], `Search ${book.title}`);
    assert(Array.isArray(result.results));
    assert(result.results.every(x => /^epub$/i.test(x.format)), "format filter violated");
    const selected = choose(result.results, book);
    if (selected) return selected;
    if (!result.more_pages) break;
  }
  throw new Error(`${mode}: no exact title/author EPUB for ${book.title}`);
}
async function download(mode, client, record, title, offline = false) {
  return mode === "mcp" ? mcpCall(client, "book_download", { hash: record.hash, title, format: "epub" }, `Download ${title}`)
    : cliCall(["book-download", record.hash, `${title}.epub`], `Download ${title}`, { offline });
}
console.log(`Live verification output: ${output}`);
await checkpoint();
try {
  const client = await mcpSession();
  const status = await mcpCall(client, "account_status", {}, "Account before downloads");
  assert(status.member);
  report.downloads_left_before = status.downloads_left;
  for (const mode of ["mcp", "cli"]) {
    const hashes = new Set();
    for (const book of cases.books) {
      console.log(`${mode}: searching ${book.title}`);
      const record = await search(mode, client, book);
      assert.match(record.hash, /^[a-f0-9]{32}$/i);
      assert(!hashes.has(record.hash), "books resolved to duplicate hashes"); hashes.add(record.hash);
      const details = mode === "mcp" ? await mcpCall(client, "book_details", { id: record.hash }, `Details ${book.title}`)
        : await cliCall(["book-details", record.hash], `Details ${book.title}`);
      assert.equal(details.hash, record.hash);
      console.log(`${mode}: downloading ${book.title} (${record.hash})`);
      const result = await download(mode, client, record, book.title);
      const verified = await verifyFile(result, record, mode);
      report.downloads.push({ interface: mode, title: book.title, author: book.author, ...verified, downloads_left: result.downloads_left });
      await checkpoint();
      console.log(`${mode}: verified ${book.title}, MD5 ${verified.md5}, ${verified.bytes} bytes`);
    }
  }
  const after = await mcpCall(client, "account_status", {}, "Account after downloads");
  report.downloads_left_after = after.downloads_left;
  await client.close();
  // A fresh process with no credentials, no session, and an unusable origin
  // must reuse the bytes already on disk. It cannot successfully redownload.
  const offlineClient = await mcpSession(true);
  for (const mode of ["mcp", "cli"]) {
    for (const [index, saved] of report.downloads.filter(x => x.interface === mode).slice(0, 2).entries()) {
      const record = { hash: saved.md5 };
      for (const renamed of [false, true]) {
        if (renamed) {
          const moved = path.join(output, mode, `renamed-${index + 1}.data`);
          await rename(saved.path, moved); saved.path = moved;
        }
        const before = await stat(saved.path);
        const result = await download(mode, offlineClient, record, `Different requested title ${index + 1}`, true);
        const verified = await verifyFile(result, record, mode);
        assert.equal(verified.path, saved.path);
        assert.equal(verified.inode, before.ino);
        assert.equal(verified.mtime_ms, before.mtimeMs);
        assert.equal(result.downloads_left, undefined, "reuse should not consume a download grant");
        report.deduplication.push({ interface: mode, title: saved.title, renamed, credentialless: true, unusable_origin: true, unchanged_inode_and_mtime: true, ...verified });
        await checkpoint();
        console.log(`${mode}: dedupe passed for ${saved.title}${renamed ? " after renaming" : " with a different requested title"}`);
      }
    }
  }
  // Exercise human-readable rendering too, using existing hashes without downloading again.
  const sample = report.downloads.find(x => x.interface === "cli");
  const text = await cliCall(["book-download", sample.md5, "Another requested title.epub"], "Plain-text dedupe output", { offline: true, json: false });
  assert(text.includes("Book downloaded successfully to:") && text.includes(sample.path));
  report.plain_cli_output_verified = true;
  report.passed = true;
} catch (error) {
  report.error = redact(error.message);
  process.exitCode = 1;
} finally {
  for (const session of sessions) await session.close();
  for (const [label, raw] of logBuffers) {
    const text = redact(raw);
    const lines = text.split(/\r?\n/).filter(Boolean);
    const errors = lines.filter(line => { try { return ["ERROR", "FATAL", "PANIC", "DPANIC"].includes(String(JSON.parse(line).level).toUpperCase()); } catch { return /\[(CONFIG|UPSTREAM|UPSTREAM_BLOCKED|REQUEST_TIMEOUT|INVALID_ARGUMENT)\]|panic:|fatal|error/i.test(line); } });
    const warnings = lines.filter(line => { try { return String(JSON.parse(line).level).toUpperCase() === "WARN"; } catch { return /warn/i.test(line); } });
    await writeFile(path.join(output, "logs", `${label}.stderr.log`), text);
    report.logs.push({ label, bytes: Buffer.byteLength(text), lines: lines.length, errors: errors.length, warnings: warnings.length });
  }
  report.error_log_lines = report.logs.reduce((sum, item) => sum + item.errors, 0);
  report.warning_log_lines = report.logs.reduce((sum, item) => sum + item.warnings, 0);
  if (report.error_log_lines) { report.passed = false; process.exitCode = 1; }
  await rm(cache, { recursive: true, force: true });
  await rm(path.join(output, "offline-cache"), { recursive: true, force: true });
  report.finished = new Date().toISOString();
  await checkpoint();
  console.log(JSON.stringify({ passed: report.passed, downloads: report.downloads.length, dedupe_checks: report.deduplication.length, error_log_lines: report.error_log_lines, warning_log_lines: report.warning_log_lines, error: report.error, report: path.join(output, "report.json") }, null, 2));
}
