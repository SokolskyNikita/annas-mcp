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

test("registry and bundle metadata must match package.json", () => {
  const pkg = { version: "1.2.3", mcpName: "io.github.example/server" };
  const server = { name: pkg.mcpName, version: "1.2.3", packages: [{ registryType: "npm", version: "1.2.3" }] };
  const manifest = { version: "1.2.3" };
  validateMetadataVersions(pkg, server, manifest);
  assert.throws(() => validateMetadataVersions(pkg, { ...server, version: "1.2.2" }, manifest), /server.json version/);
  assert.throws(
    () => validateMetadataVersions(pkg, { ...server, packages: [{ registryType: "npm", version: "1.2.2" }] }, manifest),
    /npm package version/,
  );
  assert.throws(() => validateMetadataVersions(pkg, server, { version: "1.2.2" }), /manifest.json version/);
  assert.throws(() => validateMetadataVersions(pkg, { ...server, name: "io.github.other/server" }, manifest), /mcpName/);
});
