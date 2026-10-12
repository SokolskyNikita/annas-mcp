// Lists the commits to Anna's Archive's source that may break the parsers in
// anna/internal/parse, from the files and hooks in anna/scripts/upstream-watch.json.
//
// Usage:
//   node anna/scripts/check-upstream.mjs                 report commits since the reviewed one
//   node anna/scripts/check-upstream.mjs --accept        mark everything up to now as reviewed
//   node anna/scripts/check-upstream.mjs --update-issue  open, update or close the GitHub issue (CI)
//
// Options: --repo <dir> keeps the partial clone of Anna's repository there
// (default: ~/.cache/annas-mcp/annas-archive.git); --json <file> writes the
// report as JSON.
//
// A commit counts as relevant when it changes a line near one of the file's
// hooks. Commits that only change whitespace, comments, scripts, styles,
// classes the parsers do not use, Python formatting, the ads toggle or the
// placeholder syntax of translations are left out; the rest are listed as
// touching no hook, so a change the hooks miss stays visible.
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
export const WATCH_FILE = path.join(root, "scripts", "upstream-watch.json");
const ISSUE_LABEL = "upstream-change";
const EXCERPT_LINES = 40;
const EXCERPT_CHARS = 3000;
const CONTEXT_LINES = 2;

// --- Normalizing a file to tell cosmetic changes apart --------------------

const collapse = (text) => text.replace(/\s+/g, " ").trim();
const withoutComments = (text) => text.replace(/\{#[\s\S]*?#\}/g, "").replace(/<!--[\s\S]*?-->/g, "");
const withoutScripts = (text) =>
  text.replace(/(<script\b[^>]*>)[\s\S]*?(<\/script>)/gi, "$1$2").replace(/(<style\b[^>]*>)[\s\S]*?(<\/style>)/gi, "$1$2");

// usedClass reports whether a class token is one the parsers read: a js-
// hook, or one containing a hook string.
function usedClass(token, hooks) {
  return token.startsWith("js-") || hooks.some((hook) => hook.length > 2 && token.includes(hook));
}

function withoutUnusedClasses(text, hooks) {
  return text.replace(/class="([^"]*)"/g, (_, classes) => `class="${classes.split(/\s+/).filter((token) => usedClass(token, hooks)).sort().join(" ")}"`);
}

// htmlSteps normalize a template more and more; the first under which the
// old and new file agree names the kind of change.
export function htmlSteps(hooks) {
  return [
    ["whitespace", (text) => collapse(text)],
    ["comments", (text) => collapse(withoutComments(text))],
    ["scripts and styles", (text) => collapse(withoutScripts(withoutComments(text)))],
    ["classes the parsers do not use", (text) => collapse(withoutUnusedClasses(withoutScripts(withoutComments(text)), hooks))],
    ["the ads toggle", (text) => collapse(withoutUnusedClasses(withoutScripts(withoutComments(text)), hooks).replace(/ads=(True|False)/g, "ads=_"))],
  ];
}

function pythonCode(text) {
  return text
    .split("\n")
    .map((line) => (line.trim().startsWith("#") ? "" : line.replace(/\s+#.*$/, "").trim()))
    .filter(Boolean)
    .join("\n");
}

export const pythonSteps = [
  ["whitespace and comments", (text) => pythonCode(text)],
  ["formatting (quotes, trailing commas, line wraps)", (text) => pythonCode(text).replaceAll("'", '"').replace(/\s+/g, "").replace(/,([)\]}])/g, "$1")],
];

// regionText is the code of the functions after the decorators matching
// patterns: each from its decorator through its body, up to the next
// top-level decorator, function or class.
export function regionText(source, patterns) {
  return patterns
    .map((pattern) => {
      const match = new RegExp(pattern).exec(source);
      if (!match) return "";
      const rest = source.slice(match.index);
      const definition = rest.search(/^def /m);
      if (definition < 0) return rest;
      const body = rest.slice(definition + 1);
      const end = body.search(/\n(?:@|def |class )/);
      return end < 0 ? rest : rest.slice(0, definition + 1 + end + 1);
    })
    .join("\n");
}

// watchedStrings maps the watched msgids of a translation file to their
// English text, with placeholders such as %(total)s and {total:d} written
// the same way.
export function watchedStrings(po, msgids) {
  const strings = {};
  for (const match of po.matchAll(/msgid "([^"]+)"\nmsgstr "([^"]*)"/g)) {
    if (msgids.includes(match[1])) {
      strings[match[1]] = match[2].replace(/%\((\w+)\)[sd]|\{(\w+)(?::d)?\}/g, (_, a, b) => `{${a ?? b}}`);
    }
  }
  return JSON.stringify(strings, Object.keys(strings).sort());
}

// touchedHooks are the hooks on changed lines of a unified diff, or within
// two lines of one.
export function touchedHooks(diff, hooks) {
  const lines = diff.split("\n").filter((line) => !/^(diff |index |--- |\+\+\+ |@@)/.test(line));
  const changed = new Set();
  lines.forEach((line, index) => {
    if (line.startsWith("+") || line.startsWith("-")) {
      for (let near = index - CONTEXT_LINES; near <= index + CONTEXT_LINES; near++) changed.add(near);
    }
  });
  const found = new Set();
  for (const index of changed) {
    const line = lines[index];
    if (line === undefined) continue;
    for (const hook of hooks) if (line.includes(hook)) found.add(hook);
  }
  return [...found].sort();
}

// classify sorts one commit's change to a watched file.
export function classify(entry, { before, after, diff }) {
  if (entry.regions) {
    const old = before === null ? "" : regionText(before, entry.regions);
    const now = after === null ? "" : regionText(after, entry.regions);
    if (old === now) return { outcome: "outside the watched functions" };
    if (pythonSteps.some(([, normalize]) => normalize(old) === normalize(now))) return { outcome: "cosmetic" };
    return { outcome: "relevant", hooks: ["watched functions"] };
  }
  if (entry.msgids) {
    const old = before === null ? "" : watchedStrings(before, entry.msgids);
    const now = after === null ? "" : watchedStrings(after, entry.msgids);
    return old === now ? { outcome: "other strings or placeholder syntax" } : { outcome: "relevant", hooks: ["English strings"] };
  }
  if (before !== null && after !== null) {
    const steps = entry.path.endsWith(".py") ? pythonSteps : htmlSteps(entry.hooks);
    const step = steps.find(([, normalize]) => normalize(before) === normalize(after));
    if (step) return { outcome: "cosmetic", reason: step[0] };
  }
  const hooks = touchedHooks(diff, entry.hooks);
  return hooks.length > 0 ? { outcome: "relevant", hooks } : { outcome: "no hook touched" };
}

// --- Reading Anna's repository --------------------------------------------

function git(repo, ...args) {
  return execFileSync("git", ["-C", repo, ...args], { encoding: "utf8", maxBuffer: 256 << 20 });
}

function show(repo, revision, file) {
  try {
    return execFileSync("git", ["-C", repo, "show", `${revision}:${file}`], { encoding: "utf8", maxBuffer: 256 << 20, stdio: ["ignore", "pipe", "ignore"] });
  } catch {
    return null;
  }
}

// syncRepository keeps a partial clone: commits and trees, with file
// contents fetched only when a check needs them.
export function syncRepository(repo, watch) {
  if (!existsSync(path.join(repo, "HEAD"))) {
    mkdirSync(path.dirname(repo), { recursive: true });
    execFileSync("git", ["clone", "--quiet", "--bare", "--filter=blob:none", watch.repository, repo], { stdio: "inherit" });
  } else {
    git(repo, "fetch", "--quiet", "--filter=blob:none", "origin", `+refs/heads/${watch.branch}:refs/heads/${watch.branch}`);
  }
  return git(repo, "rev-parse", watch.branch).trim();
}

export function checkUpstream(repo, watch) {
  const head = syncRepository(repo, watch);
  const commits = [];
  for (const entry of watch.files) {
    const log = git(repo, "log", "--format=%H%x09%cs", `${watch.reviewed}..${head}`, "--", entry.path).trim();
    for (const line of log ? log.split("\n") : []) {
      const [sha, date] = line.split("\t");
      const diff = git(repo, "show", `-U${CONTEXT_LINES}`, "--format=", sha, "--", entry.path);
      const result = classify(entry, { before: show(repo, `${sha}^`, entry.path), after: show(repo, sha, entry.path), diff });
      commits.push({ sha, date, path: entry.path, ...result, diff });
    }
  }
  commits.sort((a, b) => (a.date === b.date ? a.path.localeCompare(b.path) : b.date.localeCompare(a.date)));
  return { reviewed: watch.reviewed, head, commits };
}

// --- Reporting --------------------------------------------------------------

function excerpt(diff) {
  let lines = diff.split("\n").filter((line) => !/^(diff |index )/.test(line));
  let truncated = false;
  if (lines.length > EXCERPT_LINES) {
    lines = lines.slice(0, EXCERPT_LINES);
    truncated = true;
  }
  let text = lines.join("\n");
  if (text.length > EXCERPT_CHARS) {
    text = text.slice(0, EXCERPT_CHARS);
    truncated = true;
  }
  return truncated ? `${text}\n… (cut; see the commit)` : text;
}

const shortSha = (sha) => sha.slice(0, 10);

export function textReport(report, watch) {
  const relevant = report.commits.filter((commit) => commit.outcome === "relevant");
  const lines = [`Anna's Archive commits to watched files since ${shortSha(report.reviewed)} (now at ${shortSha(report.head)}): ${report.commits.length}, ${relevant.length} relevant.`];
  for (const commit of report.commits) {
    const detail = commit.outcome === "relevant" ? `hooks: ${commit.hooks.join(", ")}` : commit.reason ?? "";
    lines.push(`${commit.outcome === "relevant" ? "RELEVANT" : "        "} ${commit.date} ${shortSha(commit.sha)} ${commit.path}  ${commit.outcome}${detail ? ` (${detail})` : ""}`);
  }
  for (const commit of relevant) {
    lines.push("", `--- ${commit.date} ${shortSha(commit.sha)} ${commit.path}: ${watch.web}/-/commit/${commit.sha}`, excerpt(commit.diff));
  }
  if (relevant.length > 0) {
    lines.push("", "Check each relevant change against anna/internal/parse (and the captured pages), then run `npm run check:upstream -- --accept` and commit anna/scripts/upstream-watch.json.");
  }
  return lines.join("\n");
}

function commitSection(commit, watch) {
  return [
    `### ${commit.date} \`${shortSha(commit.sha)}\` ${commit.path}`,
    "",
    `[Commit](${watch.web}/-/commit/${commit.sha}). Hooks near the change: ${commit.hooks.map((hook) => `\`${hook}\``).join(", ")}.`,
    "",
    "```diff",
    excerpt(commit.diff).replaceAll("```", "`​``"),
    "```",
  ].join("\n");
}

function otherList(commits) {
  if (commits.length === 0) return "";
  const rows = commits.map((commit) => `- ${commit.date} \`${shortSha(commit.sha)}\` ${commit.path}`);
  return ["", "<details><summary>Also changed, touching no hook (" + commits.length + ")</summary>", "", ...rows, "", "</details>"].join("\n");
}

export function issueBody(report, watch, commits) {
  const relevant = commits.filter((commit) => commit.outcome === "relevant");
  const other = commits.filter((commit) => commit.outcome === "no hook touched");
  return [
    `Anna's Archive changed code that the parsers in \`anna/internal/parse\` depend on (reviewed up to \`${shortSha(report.reviewed)}\`, now at \`${shortSha(report.head)}\`).`,
    "",
    ...relevant.map((commit) => commitSection(commit, watch) + "\n"),
    otherList(other),
    "",
    "To review: check each change against the parsers, recapture affected pages if needed (`ANNAS_MCP_CAPTURE=<names> go test ./anna -run '^TestCapturePages$'`), then run `npm run check:upstream -- --accept` and commit `anna/scripts/upstream-watch.json`. The next weekly run closes this issue.",
  ].join("\n");
}

// --- GitHub issue (CI) ------------------------------------------------------

async function github(method, url, body) {
  const response = await fetch(`https://api.github.com${url}`, {
    method,
    headers: {
      Authorization: `Bearer ${process.env.GITHUB_TOKEN}`,
      Accept: "application/vnd.github+json",
      "X-GitHub-Api-Version": "2022-11-28",
      ...(body ? { "Content-Type": "application/json" } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!response.ok && !(method === "POST" && url.endsWith("/labels") && response.status === 422)) {
    throw new Error(`GitHub ${method} ${url} answered ${response.status}: ${await response.text()}`);
  }
  return response.status === 204 ? null : response.json();
}

export async function updateIssue(report, watch) {
  const repo = process.env.GITHUB_REPOSITORY;
  if (!repo || !process.env.GITHUB_TOKEN) throw new Error("--update-issue needs GITHUB_REPOSITORY and GITHUB_TOKEN");
  const relevant = report.commits.filter((commit) => commit.outcome === "relevant");
  const [open] = await github("GET", `/repos/${repo}/issues?labels=${ISSUE_LABEL}&state=open&per_page=1`);
  if (relevant.length === 0) {
    if (open) {
      await github("POST", `/repos/${repo}/issues/${open.number}/comments`, { body: `Every change up to \`${shortSha(report.head)}\` is reviewed.` });
      await github("PATCH", `/repos/${repo}/issues/${open.number}`, { state: "closed", state_reason: "completed" });
      console.log(`Closed issue #${open.number}.`);
    }
    return;
  }
  if (!open) {
    await github("POST", `/repos/${repo}/labels`, { name: ISSUE_LABEL, color: "d93f0b", description: "Anna's Archive changed code the parsers depend on" });
    const issue = await github("POST", `/repos/${repo}/issues`, {
      title: `Anna's Archive changed code the parsers depend on (${relevant.length} commit${relevant.length === 1 ? "" : "s"})`,
      body: issueBody(report, watch, report.commits),
      labels: [ISSUE_LABEL],
    });
    console.log(`Opened issue #${issue.number}.`);
    return;
  }
  const comments = await github("GET", `/repos/${repo}/issues/${open.number}/comments?per_page=100`);
  const seen = [open.body ?? "", ...comments.map((comment) => comment.body ?? "")].join("\n");
  const fresh = report.commits.filter((commit) => commit.outcome !== "cosmetic" && !seen.includes(shortSha(commit.sha)));
  if (fresh.some((commit) => commit.outcome === "relevant")) {
    await github("POST", `/repos/${repo}/issues/${open.number}/comments`, { body: issueBody(report, watch, fresh) });
    console.log(`Added ${fresh.length} commits to issue #${open.number}.`);
  } else {
    console.log(`Issue #${open.number} already lists every relevant commit.`);
  }
}

// --- Command line ------------------------------------------------------------

function option(args, name) {
  const index = args.indexOf(name);
  return index >= 0 ? args[index + 1] : undefined;
}

async function main(args) {
  const watch = JSON.parse(readFileSync(WATCH_FILE, "utf8"));
  const cache = process.env.XDG_CACHE_HOME || path.join(os.homedir(), ".cache");
  const repo = path.resolve(option(args, "--repo") ?? path.join(cache, "annas-mcp", "annas-archive.git"));
  if (args.includes("--accept")) {
    const head = syncRepository(repo, watch);
    const text = readFileSync(WATCH_FILE, "utf8").replace(`"reviewed": "${watch.reviewed}"`, `"reviewed": "${head}"`);
    writeFileSync(WATCH_FILE, text);
    console.log(`Marked Anna's Archive as reviewed up to ${head}.`);
    return;
  }
  const report = checkUpstream(repo, watch);
  const json = option(args, "--json");
  if (json) writeFileSync(json, JSON.stringify(report, null, 2));
  console.log(textReport(report, watch));
  if (args.includes("--update-issue")) await updateIssue(report, watch);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main(process.argv.slice(2)).catch((error) => {
    console.error(error.message);
    process.exit(2);
  });
}
