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

export function checkVersion(directory = root, env = process.env) {
  const embedded = readFileSync(path.join(directory, "internal/version/version.txt"), "utf8");
  const packageJSON = JSON.parse(readFileSync(path.join(directory, "package.json"), "utf8"));
  return validateVersion(embedded, packageJSON.version, env);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    console.log(checkVersion());
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
