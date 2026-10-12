import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { classify, issueBody, regionText, touchedHooks, watchedStrings, WATCH_FILE } from "./check-upstream.mjs";

const template = { path: "templates/macros/aarecord_list.html", hooks: ["js-vim-focus", "new_bottom_row", "text-gray-800", "opacity-40"] };
const card = `<div class="flex {% if x %}opacity-40{% endif %} pt-3">
  <a href="{{ path }}" class="js-vim-focus custom-a text-lg">{{ title }}</a>
  <div class="text-gray-800 font-semibold">{{ new_bottom_row }}</div>
  <script>var x = 1;</script>
</div>`;

function diff(before, after) {
  // A unified diff without context, as git show -U0 writes one; enough for
  // touchedHooks, which only looks at changed lines and their neighbours.
  const old = (before ?? "").split("\n");
  const now = after.split("\n");
  const lines = [];
  for (let i = 0; i < Math.max(old.length, now.length); i++) {
    if (old[i] === now[i]) lines.push(` ${old[i] ?? ""}`);
    else {
      if (old[i] !== undefined) lines.push(`-${old[i]}`);
      if (now[i] !== undefined) lines.push(`+${now[i]}`);
    }
  }
  return lines.join("\n");
}

function outcome(entry, before, after) {
  return classify(entry, { before, after, diff: diff(before, after) });
}

test("cosmetic template changes are left out", () => {
  for (const [reason, after] of [
    ["whitespace", card.replace("pt-3\">", "pt-3\">\n\n")],
    ["comments", card.replace("</div>\n  <script>", "</div> <!-- note -->\n  <script>")],
    ["scripts and styles", card.replace("var x = 1;", "var x = 2;")],
    ["classes the parsers do not use", card.replace("text-lg", "text-xl")],
  ]) {
    assert.deepEqual(outcome(template, card, after), { outcome: "cosmetic", reason }, reason);
  }
  const ads = "{{ aarecord_list(search_aarecords, ads=True) }}";
  assert.deepEqual(outcome({ path: "search.html", hooks: ["aarecord_list("] }, ads, ads.replace("True", "False")), { outcome: "cosmetic", reason: "the ads toggle" });
});

test("a change near a hook is relevant, others touch no hook", () => {
  const renamed = outcome(template, card, card.replace("js-vim-focus", "js-title"));
  assert.equal(renamed.outcome, "relevant");
  assert.ok(renamed.hooks.includes("js-vim-focus"), renamed.hooks);
  // Removing a class the parsers use is not cosmetic.
  assert.equal(outcome(template, card, card.replace("text-gray-800 ", "")).outcome, "relevant");
  const far = card + "\n\n\n\n<p>{{ footer }}</p>";
  assert.deepEqual(outcome(template, far, far.replace("footer", "footnote")), { outcome: "no hook touched" });
  assert.deepEqual(outcome(template, null, card).outcome, "relevant");
});

test("touchedHooks looks two lines around each change", () => {
  const lines = ["a hook", "b", "c", "-d", "+e", "f", "g", "h hook"].join("\n");
  assert.deepEqual(touchedHooks(lines, ["hook"]), []);
  assert.deepEqual(touchedHooks(["x", "hook", "y", "-z"].join("\n"), ["hook"]), ["hook"]);
});

test("Python files ignore formatting, and regions limit them to watched functions", () => {
  const python = { path: "search.py", hooks: ["request.args"] };
  const code = "def f():\n    x = request.args.get('q')  # the query\n    return x\n";
  assert.deepEqual(outcome(python, code, code.replace("# the query", "# the search")), { outcome: "cosmetic", reason: "whitespace and comments" });
  assert.equal(outcome(python, code, code.replace("'q'", '"q"')).reason, "formatting (quotes, trailing commas, line wraps)");
  assert.equal(outcome(python, code, code.replace("'q'", "'query'")).outcome, "relevant");

  const views = '@dyn.get("/dyn/up/")\ndef up():\n    return {"aa_logged_in": 0}\n\n@dyn.get("/other")\ndef other():\n    return 1\n';
  const dyn = { path: "dyn/views.py", regions: ['@dyn\\.get\\("/dyn/up/"\\)'] };
  assert.match(regionText(views, dyn.regions), /aa_logged_in/);
  assert.doesNotMatch(regionText(views, dyn.regions), /other/);
  // A decorator stacked under the watched one belongs to its function.
  const stacked = '@dyn.get("/dyn/up/")\n@allthethings.utils.no_cache()\ndef up():\n    return 1\n\ndef later():\n    return 2\n';
  assert.match(regionText(stacked, dyn.regions), /return 1/);
  assert.doesNotMatch(regionText(stacked, dyn.regions), /later/);
  assert.deepEqual(outcome(dyn, views, views.replace("return 1", "return 2")), { outcome: "outside the watched functions" });
  assert.equal(outcome(dyn, views, views.replace("aa_logged_in", "logged_in")).outcome, "relevant");
});

test("translations count only for the watched strings, whatever their placeholder syntax", () => {
  const po = 'msgid "page.search.results.numbers_pages"\nmsgstr "Results %(from)s-%(to)s (%(total)s total)"\n\nmsgid "other"\nmsgstr "Other"\n';
  const strings = { path: "messages.po", msgids: ["page.search.results.numbers_pages"] };
  assert.equal(watchedStrings(po, strings.msgids), watchedStrings(po.replace("%(from)s-%(to)s (%(total)s", "{from}-{to} ({total:d}"), strings.msgids));
  assert.deepEqual(outcome(strings, po, po.replace("Other", "Another")), { outcome: "other strings or placeholder syntax" });
  assert.equal(outcome(strings, po, po.replace("total)", "in all)")).outcome, "relevant");
});

test("the issue lists relevant commits with short excerpts and folds the rest", () => {
  const watch = { web: "https://code.example/anna" };
  const long = Array.from({ length: 200 }, (_, i) => `+line ${i} ${"x".repeat(40)}`).join("\n");
  const body = issueBody({ reviewed: "a".repeat(40), head: "b".repeat(40) }, watch, [
    { sha: "c".repeat(40), date: "2026-10-01", path: "search.html", outcome: "relevant", hooks: ["partial"], diff: long },
    { sha: "d".repeat(40), date: "2026-09-01", path: "aarecord.py", outcome: "no hook touched", diff: "" },
  ]);
  assert.match(body, /https:\/\/code\.example\/anna\/-\/commit\/c{40}/);
  assert.match(body, /<details><summary>Also changed, touching no hook \(1\)<\/summary>/);
  assert.match(body, /cut; see the commit/);
  const excerpt = body.slice(body.indexOf("```diff"), body.indexOf("```", body.indexOf("```diff") + 7));
  assert.ok(excerpt.split("\n").length <= 43 && excerpt.length <= 3100, `excerpt of ${excerpt.length} characters`);
});

test("the watch file is well formed", () => {
  const watch = JSON.parse(readFileSync(WATCH_FILE, "utf8"));
  assert.match(watch.reviewed, /^[0-9a-f]{40}$/);
  for (const entry of watch.files) {
    assert.equal([entry.hooks, entry.regions, entry.msgids].filter(Boolean).length, 1, entry.path);
  }
});
