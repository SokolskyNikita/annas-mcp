// Opt-in real-network checks. Uses the account configured in env/.env and its download quota.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { mkdtemp, open, readFile, realpath, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { MCPProcessClient } from "./mcp-session.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const cases = JSON.parse(await readFile(new URL("./testdata/live-smoke.json", import.meta.url), "utf8"));
const binary = path.resolve(process.argv[2] || path.join(root, process.platform === "win32" ? "annas-mcp.exe" : "annas-mcp"));
await stat(binary); // Fail before creating a directory if the binary was not built.
const output = await realpath(await mkdtemp(path.join(os.tmpdir(), "annas-mcp-live-")));
const client = new MCPProcessClient(spawn(binary, ["mcp"], {
  cwd: root,
  env: { ...process.env, ANNAS_DOWNLOAD_PATH: output },
  stdio: ["pipe", "pipe", "pipe"],
}));
const report = { binary, output, calls: [], downloads: [] };

async function call(name, args, label = name) {
  console.log(`Checking ${label}...`);
  const started = Date.now();
  const result = await client.request("tools/call", { name, arguments: args }, 240_000);
  assert(!result.isError, `${label}: ${result.content?.find((item) => item.type === "text")?.text || "tool failed"}`);
  assert(result.structuredContent, `${label}: missing structured content`);
  const text = result.content?.find((item) => item.type === "text")?.text;
  assert.deepEqual(JSON.parse(text), result.structuredContent, `${label}: text and structured output differ`);
  report.calls.push({ name, label, elapsed_ms: Date.now() - started });
  console.log(`Passed ${label}`);
  return result.structuredContent;
}

async function verifyDownload(result, record, label) {
  assert(path.isAbsolute(result.path), `${label}: path must be absolute`);
  const actualPath = await realpath(result.path);
  const relative = path.relative(output, actualPath);
  assert(relative && !relative.startsWith("..") && !path.isAbsolute(relative), `${label}: file escaped download directory`);
  const info = await stat(actualPath);
  assert(info.isFile() && info.size > 0 && info.size === result.bytes, `${label}: incorrect byte count`);
  const digest = createHash("md5");
  for await (const chunk of createReadStream(actualPath)) digest.update(chunk);
  const md5 = digest.digest("hex");
  assert.match(record.hash, /^[a-f0-9]{32}$/i);
  assert.equal(md5, record.hash.toLowerCase(), `${label}: checksum mismatch`);
  const file = await open(actualPath, "r");
  try {
    const header = Buffer.alloc(8);
    await file.read(header, 0, header.length, 0);
    if (path.extname(actualPath).toLowerCase() === ".pdf") assert.equal(header.subarray(0, 5).toString(), "%PDF-");
    if (path.extname(actualPath).toLowerCase() === ".epub") assert.equal(header.subarray(0, 2).toString(), "PK");
  } finally {
    await file.close();
  }
  report.downloads.push({ label, title: record.title, path: actualPath, bytes: result.bytes, md5 });
  console.log(`Verified ${label}: ${result.bytes} bytes, MD5 ${md5}`);
}

try {
  const initialized = await client.initialize();
  assert.equal(initialized.serverInfo.name, "annas-mcp");
  report.server = initialized.serverInfo;
  const listed = await client.request("tools/list");
  assert.deepEqual(listed.tools.map(({ name }) => name).sort(), ["article_download", "article_search", "book_download", "book_search"]);

  const books = await call("book_search", cases.book);
  assert(books.results.length > 0, "Book search returned no records");
  const book = books.results.find((record) =>
    record.title.includes(cases.book_title) && record.authors.includes(cases.book_author) && /^epub$/i.test(record.format),
  ) || books.results.find((record) =>
    record.title.includes(cases.book_title) && record.authors.includes(cases.book_author) && /^pdf$/i.test(record.format),
  );
  assert(book, "No matching PDF/EPUB found for the configured public-domain book");
  report.book = { title: book.title, authors: book.authors, format: book.format, hash: book.hash };

  const papers = await call("article_search", { query: cases.article_query, limit: 5, timeout_seconds: 90 }, "article_search (keywords)");
  assert(papers.results.length > 0, "Article keyword search returned no records");
  const article = await call("article_search", { query: `https://doi.org/${cases.article_doi}`, timeout_seconds: 90 }, "article_search (DOI URL)");
  assert.equal(article.doi.toLowerCase(), cases.article_doi.toLowerCase());
  assert.equal(article.title.toLowerCase(), cases.article_title.toLowerCase());
  report.article = { title: article.title, doi: article.doi, hash: article.hash };

  const bookFile = await call("book_download", { hash: book.hash, title: book.title, format: book.format.toLowerCase(), timeout_seconds: 180 });
  await verifyDownload(bookFile, book, "book_download");
  const hashFile = await call("article_download", { hash: article.hash, title: `${article.title} - hash`, format: "pdf", timeout_seconds: 180 }, "article_download (hash)");
  await verifyDownload(hashFile, article, "article_download (hash)");
  const doiFile = await call("article_download", { doi: cases.article_doi, title: `${article.title} - doi`, format: "pdf", timeout_seconds: 180 }, "article_download (DOI)");
  await verifyDownload(doiFile, article, "article_download (DOI)");
  report.passed = true;
} catch (error) {
  report.passed = false;
  // Tool errors redact account credentials; avoid dumping process env or stderr.
  report.error = error.message;
  process.exitCode = 1;
} finally {
  await client.close();
  await writeFile(path.join(output, "report.json"), `${JSON.stringify(report, null, 2)}\n`);
  console.log(JSON.stringify(report, null, 2));
}
