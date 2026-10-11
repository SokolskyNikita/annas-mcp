// Sets a new release version everywhere check-version.mjs looks for it and
// adds that release's heading to CHANGELOG.md.
//
// Usage: node scripts/bump-version.mjs <patch|minor|major|X.Y.Z>
//
// Only the version values change, so each file keeps its formatting.
import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { GITHUB_REPO } from "../lib/launcher.js";
import { checkVersion, validateMetadataVersions, validateVersion } from "./check-version.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const SEMVER = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const VERSION_FILE = "internal/version/version.txt";
const JSON_FILES = ["package.json", "server.json", "manifest.json"];

export function nextVersion(current, spec) {
  const [major, minor, patch] = SEMVER.exec(current)?.slice(1).map(Number) ?? [];
  if (major === undefined) {
    throw new Error(`Current version ${current} is not X.Y.Z`);
  }
  const next =
    spec === "major" ? `${major + 1}.0.0`
    : spec === "minor" ? `${major}.${minor + 1}.0`
    : spec === "patch" ? `${major}.${minor}.${patch + 1}`
    : String(spec ?? "").replace(/^v/, "");
  const [a, b, c] = SEMVER.exec(next)?.slice(1).map(Number) ?? [];
  if (a === undefined) {
    throw new Error("Usage: npm run bump <patch|minor|major|X.Y.Z>");
  }
  const newer = a !== major ? a > major : b !== minor ? b > minor : c > patch;
  if (!newer) {
    throw new Error(`Version ${next} must be newer than ${current}`);
  }
  return next;
}

export function bumpVersion(directory, spec, today = new Date().toISOString().slice(0, 10)) {
  const read = (file) => readFileSync(path.join(directory, file), "utf8");
  // The tag check only applies to release runs, which never bump.
  const current = checkVersion(directory, {}).slice(1);
  const next = nextVersion(current, spec);

  const updated = new Map();
  const field = new RegExp(`("version"\\s*:\\s*")${current.replaceAll(".", "\\.")}"`, "g");
  for (const file of JSON_FILES) {
    const text = read(file);
    const replaced = text.replace(field, `$1${next}"`);
    if (replaced === text) {
      throw new Error(`${file} has no "version": "${current}"`);
    }
    updated.set(file, replaced);
  }
  updated.set(VERSION_FILE, read(VERSION_FILE).replace(`v${current}`, `v${next}`));

  const changelog = read("CHANGELOG.md");
  if (!changelog.includes(`## [v${next}]`)) {
    const firstRelease = changelog.search(/^## /m);
    if (firstRelease < 0) {
      throw new Error("CHANGELOG.md has no release headings");
    }
    const eol = changelog.includes("\r\n") ? "\r\n" : "\n";
    const heading = `## [v${next}](https://github.com/${GITHUB_REPO}/releases/tag/v${next}) - ${today}`;
    updated.set(
      "CHANGELOG.md",
      `${changelog.slice(0, firstRelease)}${heading}${eol}${eol}${changelog.slice(firstRelease)}`,
    );
  }

  // Check the result before writing anything.
  const json = (file) => JSON.parse(updated.get(file));
  const pkg = json("package.json");
  validateMetadataVersions(pkg, json("server.json"), json("manifest.json"));
  validateVersion(updated.get(VERSION_FILE), pkg.version);
  for (const [file, text] of updated) {
    writeFileSync(path.join(directory, file), text);
  }
  return `v${next}`;
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    const version = bumpVersion(root, process.argv[2]);
    console.log(`Set version ${version}. Write its release notes in CHANGELOG.md.`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
