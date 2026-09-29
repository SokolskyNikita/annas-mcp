import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));

export function validateVersion(embedded, packageVersion, env = {}) {
  const version = embedded.trim();
  if (!/^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version)) {
    throw new Error("version.txt must contain a v-prefixed release version, such as v1.2.3");
  }
  if (version.slice(1) !== packageVersion) {
    throw new Error(`Version mismatch: version.txt=${version}, package.json=${packageVersion}`);
  }
  if (env.GITHUB_REF_TYPE === "tag" && env.GITHUB_REF_NAME !== version) {
    throw new Error(`Release tag ${env.GITHUB_REF_NAME} does not match repository version ${version}`);
  }
  return version;
}

// server.json (MCP Registry) and manifest.json (MCP Bundle) repeat the version.
export function validateMetadataVersions(packageJSON, serverJSON, manifestJSON) {
  const expected = packageJSON.version;
  const found = {
    "server.json version": serverJSON.version,
    "server.json npm package version": serverJSON.packages?.find((p) => p.registryType === "npm")?.version,
    "manifest.json version": manifestJSON.version,
  };
  for (const [label, version] of Object.entries(found)) {
    if (version !== expected) {
      throw new Error(`Version mismatch: ${label}=${version}, package.json=${expected}`);
    }
  }
  if (serverJSON.name !== packageJSON.mcpName) {
    throw new Error(`server.json name ${serverJSON.name} must match package.json mcpName ${packageJSON.mcpName}`);
  }
}

export function checkVersion(directory = root, env = process.env) {
  const read = (file) => readFileSync(path.join(directory, file), "utf8");
  const packageJSON = JSON.parse(read("package.json"));
  validateMetadataVersions(packageJSON, JSON.parse(read("server.json")), JSON.parse(read("manifest.json")));
  return validateVersion(read("internal/version/version.txt"), packageJSON.version, env);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    console.log(checkVersion());
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
