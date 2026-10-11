import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { checkVersion, validateMetadataVersions, validateVersion } from "./check-version.mjs";

const cases = JSON.parse(readFileSync(new URL("./testdata/version-cases.json", import.meta.url), "utf8"));
for (const entry of cases) {
  test(entry.name, () => {
    const run = () => validateVersion(entry.embedded, entry.package, entry.env);
    if (entry.error) assert.throws(run, new RegExp(entry.error));
    else assert.equal(run(), entry.expected);
  });
}

test("repository version metadata is consistent", () => {
  assert.match(checkVersion(), /^v\d+\.\d+\.\d+$/);
});

test("registry, bundle and extension metadata must match package.json", () => {
  const pkg = { version: "1.2.3", mcpName: "io.github.example/server" };
  const server = { name: pkg.mcpName, version: "1.2.3", packages: [{ registryType: "npm", version: "1.2.3" }] };
  const manifest = { version: "1.2.3" };
  const extension = { version: "1.2.3" };
  validateMetadataVersions(pkg, server, manifest, extension);
  assert.throws(() => validateMetadataVersions(pkg, { ...server, version: "1.2.2" }, manifest, extension), /server.json version/);
  assert.throws(
    () => validateMetadataVersions(pkg, { ...server, packages: [{ registryType: "npm", version: "1.2.2" }] }, manifest, extension),
    /npm package version/,
  );
  assert.throws(() => validateMetadataVersions(pkg, server, { version: "1.2.2" }, extension), /manifest.json version/);
  assert.throws(() => validateMetadataVersions(pkg, server, manifest, { version: "1.2.2" }), /gemini-extension.json version/);
  assert.throws(() => validateMetadataVersions(pkg, { ...server, name: "io.github.other/server" }, manifest, extension), /mcpName/);
});
