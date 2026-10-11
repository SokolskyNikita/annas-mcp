import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { bumpVersion, nextVersion } from "./bump-version.mjs";
import { checkVersion } from "./check-version.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const FILES = ["package.json", "server.json", "manifest.json", "gemini-extension.json", "internal/version/version.txt", "CHANGELOG.md"];

// Copies the repository's version files into a temporary directory.
function fixture(t, edit = (text) => text) {
  const dir = mkdtempSync(path.join(os.tmpdir(), "annas-mcp-bump-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  for (const file of FILES) {
    mkdirSync(path.dirname(path.join(dir, file)), { recursive: true });
    writeFileSync(path.join(dir, file), edit(readFileSync(path.join(root, file), "utf8"), file));
  }
  return dir;
}

test("next version from a keyword or an explicit version", () => {
  assert.equal(nextVersion("1.2.3", "patch"), "1.2.4");
  assert.equal(nextVersion("1.2.3", "minor"), "1.3.0");
  assert.equal(nextVersion("1.2.3", "major"), "2.0.0");
  assert.equal(nextVersion("1.2.3", "1.10.0"), "1.10.0");
  assert.equal(nextVersion("1.2.3", "v1.2.10"), "1.2.10");
  assert.throws(() => nextVersion("1.2.3", undefined), /Usage/);
  assert.throws(() => nextVersion("1.2.3", "1.2"), /Usage/);
  assert.throws(() => nextVersion("1.2.3", "1.2.4-rc.1"), /Usage/);
  assert.throws(() => nextVersion("1.2.3", "1.2.3"), /newer/);
  assert.throws(() => nextVersion("1.2.3", "1.1.9"), /newer/);
  assert.throws(() => nextVersion("1.2.3", "0.9.9"), /newer/);
});

test("bump rewrites only the version values and adds a changelog heading", (t) => {
  const dir = fixture(t);
  const current = checkVersion(root, {}).slice(1);
  const next = nextVersion(current, "patch");

  assert.equal(bumpVersion(dir, "patch", "2030-01-02"), `v${next}`);
  assert.equal(checkVersion(dir, {}), `v${next}`);
  for (const file of ["package.json", "server.json", "manifest.json", "gemini-extension.json", "internal/version/version.txt"]) {
    const before = readFileSync(path.join(root, file), "utf8");
    const after = readFileSync(path.join(dir, file), "utf8");
    assert.equal(after, before.replaceAll(`"version": "${current}"`, `"version": "${next}"`).replace(`v${current}`, `v${next}`), file);
  }
  const changelog = readFileSync(path.join(dir, "CHANGELOG.md"), "utf8").replaceAll("\r\n", "\n");
  const heading = `## [v${next}](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v${next}) - 2030-01-02\n\n## [v${current}]`;
  assert.ok(changelog.includes(heading));

  // Bumping again keeps the existing heading for the newer release.
  const later = nextVersion(next, "minor");
  bumpVersion(dir, "minor", "2030-01-03");
  const again = readFileSync(path.join(dir, "CHANGELOG.md"), "utf8");
  assert.equal(again.split(`## [v${later}]`).length, 2);
  assert.equal(again.split(`## [v${next}]`).length, 2);
});

test("bump keeps CRLF line endings in the changelog", (t) => {
  const dir = fixture(t, (text, file) => (file === "CHANGELOG.md" ? text.replace(/\r?\n/g, "\r\n") : text));
  bumpVersion(dir, "patch", "2030-01-02");
  const changelog = readFileSync(path.join(dir, "CHANGELOG.md"), "utf8");
  assert.ok(!/[^\r]\n/.test(changelog));
});

test("bump refuses inconsistent metadata and writes nothing", (t) => {
  const dir = fixture(t, (text, file) => (file === "manifest.json" ? text.replace(/"version": "[^"]+"/, '"version": "0.0.1"') : text));
  const before = FILES.map((file) => readFileSync(path.join(dir, file), "utf8"));
  assert.throws(() => bumpVersion(dir, "patch"), /manifest.json version/);
  assert.deepEqual(FILES.map((file) => readFileSync(path.join(dir, file), "utf8")), before);
});

test("bump refuses an older version and writes nothing", (t) => {
  const dir = fixture(t);
  const before = FILES.map((file) => readFileSync(path.join(dir, file), "utf8"));
  assert.throws(() => bumpVersion(dir, "0.0.1"), /newer/);
  assert.deepEqual(FILES.map((file) => readFileSync(path.join(dir, file), "utf8")), before);
});
