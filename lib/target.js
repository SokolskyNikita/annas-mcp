import path from "node:path";

const SUPPORTED_TARGETS = new Set([
  "darwin-amd64",
  "darwin-arm64",
  "linux-amd64",
  "linux-arm64",
  "linux-arm",
  "windows-amd64",
  "windows-arm64",
  "freebsd-amd64",
]);

export function goreleaserTarget(platform = process.platform, arch = process.arch) {
  const osName = platform === "win32" ? "windows" : platform;
  const archName = arch === "x64" ? "amd64" : arch;
  const key = `${osName}-${archName}`;
  if (!SUPPORTED_TARGETS.has(key)) {
    throw new Error(
      `Unsupported platform ${platform}/${arch}. Supported targets: ${[...SUPPORTED_TARGETS].join(", ")}`,
    );
  }
  return {
    os: osName,
    arch: archName,
    extension: osName === "windows" ? "zip" : "tar.xz",
    binaryName: osName === "windows" ? "annas-mcp.exe" : "annas-mcp",
  };
}

export function safePathComponent(value, label) {
  if (
    typeof value !== "string" ||
    !value ||
    value === "." ||
    value === ".." ||
    value.includes("\0") ||
    value.includes("/") ||
    value.includes("\\") ||
    /[<>:"|?*]/.test(value) ||
    path.basename(value) !== value
  ) {
    throw new Error(`Invalid ${label}`);
  }
  return value;
}

export function sha256Digest(value) {
  if (typeof value !== "string") {
    return null;
  }
  const match = value.trim().match(/^(?:sha256:)?([a-fA-F0-9]{64})$/);
  return match ? match[1].toLowerCase() : null;
}

export function releaseAssetDigest(asset) {
  return sha256Digest(asset?.digest);
}

export function pathInside(root, candidate) {
  const relative = path.relative(path.resolve(root), path.resolve(candidate));
  return relative && !relative.startsWith("..") && !path.isAbsolute(relative);
}

export function validateDownloadAsset(asset) {
  safePathComponent(asset?.name, "release asset name");
  if (typeof asset?.browser_download_url !== "string" || !asset.browser_download_url) {
    throw new Error(`Release asset ${asset?.name || "(unnamed)"} has no download URL`);
  }
  return asset;
}

export function selectChecksumAsset(assets) {
  const matches = (assets || []).filter(
    (asset) => typeof asset?.name === "string" && asset.name.endsWith("--checksums.txt"),
  );
  if (matches.length !== 1) {
    const names = (assets || []).map((asset) => asset?.name).filter(Boolean);
    throw new Error(
      `Expected one checksums asset, found ${matches.length}. Assets: ${names.join(", ") || "(none)"}`,
    );
  }
  return validateDownloadAsset(matches[0]);
}

export function checksumFor(text, filename) {
  const base = path.basename(filename);
  for (const line of text.split(/\r?\n/)) {
    const match = line.trim().match(/^([a-fA-F0-9]{64})\s+\*?(.+)$/);
    if (!match) {
      continue;
    }
    const name = match[2].trim();
    if (name === filename || name === base || path.basename(name) === base) {
      return match[1].toLowerCase();
    }
  }
  throw new Error(`Checksum file has no entry for ${filename}`);
}

export function selectAsset(assets, target) {
  const suffix = `_${target.os}_${target.arch}.${target.extension}`;
  const matches = (assets || []).filter(
    (asset) =>
      typeof asset?.name === "string" &&
      asset.name.startsWith("annas-mcp_") &&
      asset.name.endsWith(suffix),
  );
  if (matches.length !== 1) {
    const names = (assets || []).map((asset) => asset?.name).filter(Boolean);
    throw new Error(
      `Expected one release asset ending in ${suffix}, found ${matches.length}. Assets: ${names.join(", ") || "(none)"}`,
    );
  }
  return validateDownloadAsset(matches[0]);
}
