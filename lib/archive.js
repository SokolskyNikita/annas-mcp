import { spawnSync as defaultSpawnSync } from "node:child_process";
import { readdir } from "node:fs/promises";
import path from "node:path";

function tarFailure(action, archive, result) {
  if (result.error) {
    return new Error(`Failed to run tar: ${result.error.message}`);
  }
  const detail = (result.stderr || result.stdout || "").trim();
  return new Error(`Failed to ${action} ${path.basename(archive)}${detail ? `: ${detail}` : ""}`);
}

// Git Bash can put its GNU tar before the Windows native tar on PATH. GNU tar
// treats a drive-qualified path such as C:\\tmp\\archive.zip as a remote
// archive name, so use the native bsdtar explicitly on Windows.
export function tarExecutable(platform = process.platform, env = process.env) {
  if (platform !== "win32") {
    return "tar";
  }
  const systemRoot = env.SystemRoot || env.WINDIR || "C:\\Windows";
  return path.win32.join(systemRoot, "System32", "tar.exe");
}

export function validateArchiveEntryName(rawName) {
  const name = rawName.trim().replaceAll("\\", "/").replace(/\/+$/, "");
  if (
    !name ||
    name.startsWith("/") ||
    /^[A-Za-z]:\//.test(name) ||
    name.split("/").includes("..")
  ) {
    throw new Error(`Refusing to extract an unsafe archive path: ${rawName}`);
  }
  return name;
}

export function validateArchiveEntries(archive, extension, spawnSync = defaultSpawnSync) {
  const tar = tarExecutable();
  const listingArgs = extension === "zip" ? ["-tf", archive] : ["-tJf", archive];
  const listing = spawnSync(tar, listingArgs, { encoding: "utf8" });
  if (listing.error || listing.status !== 0) {
    throw tarFailure("inspect", archive, listing);
  }
  for (const rawName of listing.stdout.split(/\r?\n/)) {
    if (rawName.trim()) {
      validateArchiveEntryName(rawName);
    }
  }

  const verboseArgs = extension === "zip" ? ["-tvf", archive] : ["-tvJf", archive];
  const verbose = spawnSync(tar, verboseArgs, { encoding: "utf8" });
  if (verbose.error || verbose.status !== 0) {
    throw tarFailure("inspect", archive, verbose);
  }
  for (const line of verbose.stdout.split(/\r?\n/)) {
    if (!line.trim()) {
      continue;
    }
    const type = line[0];
    if (type !== "-" && type !== "d") {
      throw new Error(`Refusing non-file archive entry: ${line.trim()}`);
    }
  }
}

export function extractArchive(archive, destination, extension, spawnSync = defaultSpawnSync) {
  validateArchiveEntries(archive, extension, spawnSync);
  const tar = tarExecutable();
  const args =
    extension === "zip"
      ? ["-xf", archive, "-C", destination]
      : ["-xJf", archive, "-C", destination];
  const result = spawnSync(tar, args, { encoding: "utf8" });
  if (result.error || result.status !== 0) {
    throw tarFailure("extract", archive, result);
  }
}

export async function findBinary(directory, binaryName, root = directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  for (const entry of entries) {
    if (entry.isSymbolicLink()) {
      continue;
    }
    const fullPath = path.join(directory, entry.name);
    const relative = path.relative(root, fullPath);
    if (relative.startsWith("..") || path.isAbsolute(relative)) {
      throw new Error(`Refusing to use a path outside the archive: ${entry.name}`);
    }
    if (entry.isDirectory()) {
      const found = await findBinary(fullPath, binaryName, root);
      if (found) {
        return found;
      }
      continue;
    }
    if (entry.isFile() && entry.name === binaryName) {
      return fullPath;
    }
  }
  return null;
}
