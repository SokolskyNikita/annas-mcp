import assert from "node:assert/strict";
import test from "node:test";
import { GITHUB_REPO } from "../lib/launcher.js";
import {
  fetchPublishedRelease,
  releaseByIDURL,
  releaseByTagURL,
} from "./release-metadata.mjs";

const tag = "v0.0.13";
const releaseId = 394418233;
const apiRoot = `https://api.github.com/repos/${GITHUB_REPO}/releases`;
const assets = [
  { name: "annas-mcp_0.0.13_linux_amd64.tar.xz", size: 1 },
  { name: "annas-mcp_0.0.13--checksums.txt", size: 1 },
];

function jsonResponse(payload, status = 200) {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

test("resolves tag metadata by ID when tag endpoint has a stale empty asset list", async () => {
  const calls = [];
  const fetchImpl = async (input) => {
    const url = String(input);
    calls.push(url);
    if (url === releaseByTagURL(tag)) {
      return jsonResponse({
        id: releaseId,
        tag_name: tag,
        assets: [],
        assets_url: "https://attacker.example/releases/assets",
      });
    }
    if (url === releaseByIDURL(releaseId)) {
      return jsonResponse({ id: releaseId, tag_name: tag, assets });
    }
    throw new Error(`unexpected URL: ${url}`);
  };

  const result = await fetchPublishedRelease(tag, { fetchImpl });
  assert.equal(result.release.id, releaseId);
  assert.deepEqual(result.release.assets, assets);
  assert.equal(result.apiURL, releaseByIDURL(releaseId));
  assert.deepEqual(calls, [releaseByTagURL(tag), releaseByIDURL(releaseId)]);
  assert(calls.every((url) => new URL(url).origin === "https://api.github.com"));
});

test("retries an empty release-ID response with a bounded backoff", async () => {
  let idLookups = 0;
  const waits = [];
  const fetchImpl = async (input) => {
    const url = String(input);
    if (url === releaseByTagURL(tag)) {
      return jsonResponse({ id: releaseId, tag_name: tag, assets: [] });
    }
    if (url === releaseByIDURL(releaseId)) {
      idLookups += 1;
      return jsonResponse({
        id: releaseId,
        tag_name: tag,
        assets: idLookups < 3 ? [] : assets,
      });
    }
    throw new Error(`unexpected URL: ${url}`);
  };

  const result = await fetchPublishedRelease(tag, {
    fetchImpl,
    attempts: 3,
    retryDelayMs: 10,
    sleepImpl: async (milliseconds) => waits.push(milliseconds),
  });
  assert.deepEqual(result.release.assets, assets);
  assert.equal(idLookups, 3);
  assert.deepEqual(waits, [10, 20]);
});

test("rejects a tag endpoint response for a different tag before looking up an ID", async () => {
  const calls = [];
  await assert.rejects(
    fetchPublishedRelease(tag, {
      fetchImpl: async (input) => {
        calls.push(String(input));
        return jsonResponse({ id: releaseId, tag_name: "v0.0.12", assets: assets });
      },
    }),
    /returned tag .* when resolving v0\.0\.13/,
  );
  assert.deepEqual(calls, [releaseByTagURL(tag)]);
});

test("rejects a release-ID response with a different ID or tag", async (t) => {
  for (const response of [
    { id: releaseId + 1, tag_name: tag, assets },
    { id: releaseId, tag_name: "v0.0.12", assets },
  ]) {
    await t.test(JSON.stringify(response), async () => {
      const calls = [];
      await assert.rejects(
        fetchPublishedRelease(tag, {
          fetchImpl: async (input) => {
            const url = String(input);
            calls.push(url);
            return url === releaseByTagURL(tag)
              ? jsonResponse({ id: releaseId, tag_name: tag, assets: [] })
              : jsonResponse(response);
          },
        }),
        /did not match requested tag/,
      );
      assert.deepEqual(calls, [releaseByTagURL(tag), releaseByIDURL(releaseId)]);
    });
  }
});

test("rejects invalid release IDs and never fetches a supplied off-site URL", async () => {
  for (const id of [0, -1, "394418233", 1.5, Number.MAX_SAFE_INTEGER + 1]) {
    await assert.rejects(Promise.resolve().then(() => releaseByIDURL(id)), /invalid release ID/);
  }

  const calls = [];
  await assert.rejects(
    fetchPublishedRelease(tag, {
      fetchImpl: async (input) => {
        calls.push(String(input));
        return jsonResponse({
          id: "394418233",
          tag_name: tag,
          assets_url: "https://attacker.example/releases/assets",
          assets: [],
        });
      },
    }),
    /invalid release ID/,
  );
  assert.deepEqual(calls, [releaseByTagURL(tag)]);
  assert(calls.every((url) => new URL(url).origin === "https://api.github.com"));
});

test("reports GitHub HTTP errors for tag and release-ID lookups", async (t) => {
  await t.test("tag lookup", async () => {
    await assert.rejects(
      fetchPublishedRelease(tag, {
        fetchImpl: async () => jsonResponse({ message: "missing" }, 404),
      }),
      /tag lookup failed \(404\)/,
    );
  });

  await t.test("release-ID lookup", async () => {
    const fetchImpl = async (input) => {
      return String(input) === releaseByTagURL(tag)
        ? jsonResponse({ id: releaseId, tag_name: tag, assets: [] })
        : jsonResponse({ message: "unavailable" }, 502);
    };
    await assert.rejects(
      fetchPublishedRelease(tag, { fetchImpl }),
      /release lookup failed \(502\)/,
    );
  });
});

test("tag and ID endpoints are constructed from the fixed official API origin", () => {
  assert.equal(releaseByTagURL(tag), `${apiRoot}/tags/${tag}`);
  assert.equal(releaseByIDURL(releaseId), `${apiRoot}/${releaseId}`);
  assert.equal(
    new URL(releaseByTagURL("https://attacker.example/x")).origin,
    "https://api.github.com",
  );
});
