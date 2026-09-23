import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { checkVersion, validateVersion } from "./check-version.mjs";

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
