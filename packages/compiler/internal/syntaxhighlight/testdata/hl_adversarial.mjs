// Feeds hostile input to the tokenizer and reports how long each case took, to
// catch regexes that backtrack catastrophically. Every case runs in a worker so
// a runaway match can be stopped.
// usage: node hl_adversarial.mjs <repo-root> <bundle.json> <lang> <out.json> <timeout-ms> <size>
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { Worker, isMainThread, parentPort, workerData } from "node:worker_threads";

function compile(repo) {
  const require = createRequire(path.join(repo, "packages", "runtime", "package.json"));
  const esbuild = require("esbuild");
  const src = fs.readFileSync(path.join(repo, "packages", "runtime", "src", "highlight.ts"), "utf8");
  return esbuild.transformSync(src, { loader: "ts", format: "esm", target: "es2022" }).code;
}

async function load(js, bundlePath) {
  const mod = await import("data:text/javascript;base64," + Buffer.from(js).toString("base64"));
  const bundle = JSON.parse(fs.readFileSync(bundlePath, "utf8"));
  const names = new Map();
  for (const [k, v] of Object.entries(bundle.keys)) {
    if (!names.has(v)) names.set(v, []);
    names.get(v).push(k);
  }
  for (const t of bundle.tables) mod.registerLanguage(t, names.get(t.n.toLowerCase()) || []);
  if (bundle.fences) {
    const fences = bundle.fences;
    mod.setFenceResolver((n) => Object.prototype.hasOwnProperty.call(fences, n.trim().toLowerCase()));
  }
  return mod;
}

if (!isMainThread) {
  const { js, bundlePath, lang, text } = workerData;
  const mod = await load(js, bundlePath);
  const t0 = process.hrtime.bigint();
  mod.highlight(text, lang, undefined, { maxLength: Infinity });
  parentPort.postMessage(Number(process.hrtime.bigint() - t0) / 1e6);
} else {
  const [repo, bundlePath, lang, outPath, timeoutArg, sizeArg] = process.argv.slice(2);
  const timeout = Number(timeoutArg);
  const js = compile(repo);
  const size = Number(sizeArg);
  const rep = (s, n) => s.repeat(Math.ceil(n / s.length)).slice(0, n);
  const singles = ['"', "'", "`", "/", "*", "\\", "{", "}", "(", ")", "[", "]", "<", ">", "-", "#", "$", "@", ":", ";", ",", ".", "=", "!", "&", "|", "%", "+", "~", "^", "?", " ", "\t", "a", "0", "_", "\u00e9", "\u{1F600}", "\n", "\r\n"];
  const seqs = ['a"', "a'", "/*", "/*/", "*/", "<a", "<a ", '<a b="', "<a b='", "</", "<!--", "-->", "<![CDATA[", "<?", '"""', "'''", "```", "#{", "${", "{{", "}}", '\\"', "\\\\", "0x", "0b", "1e", "1.", ".5.", "---\n", "\n  ", "  - ", "a: ", "a:\n", "- ", "# ", "// ", "\\\n", "a\\\n", "$(", "$((", "`$(", "<<EOF\n", "<<'EOF'\n", "'\\''", "$a${", "(?", "[[", "]]", "((", "))", "{{{", "}}}", "a,", "a.", "a->", "a::", "a=>", "@a", "#a", "&a", "%a", "!a", "\u00e9a", "a\u00e9", "\u200d", "/a/", "/[", "r#\"", '"#', "b'", "'a", "'\\", "'\\u{", "\"\\u", "<a><b>", "</a></b>", "a{", "a{b:", "a{b:c;", "@media{", "@import \"", "url(", "url('", "calc(", "var(--", ":not(", "::before", "!important", "#fff", "0.5em", "1px solid", "a-b-c-", "a_b_c_", "A.B.C.", "a1b2c3", "\u0000"];
  // Lines and nesting, as real-ish hostile input.
  const shapes = {
    nested_open: "(".repeat(size / 2) + ")".repeat(size / 2),
    nested_brackets: "[".repeat(size / 2) + "]".repeat(size / 2),
    nested_braces: "{".repeat(size / 2) + "}".repeat(size / 2),
    long_word: "a".repeat(size) + "!",
    long_number: "1".repeat(size) + "x",
    long_string_open: '"' + "a".repeat(size),
    long_comment_open: "/*" + "a ".repeat(size / 2),
    long_line_of_words: rep("word ", size),
    many_short_lines: rep("x\n", size),
    many_quotes_lines: rep('"\n', size),
    alt_quotes: rep("\"'", size),
    escapes: rep("\\", size),
    long_tag: "<a " + rep('b="c" ', size),
    long_attr_value: '<a b="' + "c".repeat(size),
    long_indent: rep(" ", size) + "x",
    tabs_and_spaces: rep(" \t", size),
    unicode_word: rep("\u00e9", size),
    astral: rep("\u{1F600}", size),
    mixed_garbage: rep("a\"'`/*\\{}()[]<>-#$@:;,.=!&|%+~^? \t\n\u00e9", size),
  };
  // The tokenizer refuses lines over HIGHLIGHT_MAX_LINE, so hostile text is
  // also tried broken into lines just under it, the worst a caller can send.
  const chunk = (t) => (t.length > 1500 ? t.replace(/[^]{1500}/gu, (m) => m + "\n") : t);
  const cases = [];
  // The default run samples every third delimiter and sequence; HL_ADV_FULL=1
  // runs them all.
  const every = process.env.HL_ADV_FULL ? 1 : 3;
  singles.forEach((s, i) => i % every === 0 && cases.push([JSON.stringify(s), chunk(rep(s, size))]));
  seqs.forEach((s, i) => i % every === 0 && cases.push([JSON.stringify(s), chunk(rep(s, size))]));
  for (const [k, v] of Object.entries(shapes)) {
    cases.push([k + " (one line)", v]);
    cases.push([k + " (lines)", chunk(v)]);
  }
  const results = [];
  async function run(name, text) {
    return new Promise((resolve) => {
      const w = new Worker(new URL(import.meta.url), { workerData: { js, bundlePath, lang, text } });
      const timer = setTimeout(() => {
        w.terminate();
        resolve({ name, len: text.length, timeout: true });
      }, timeout);
      w.on("message", (ms) => {
        clearTimeout(timer);
        w.terminate();
        resolve({ name, len: text.length, ms });
      });
      w.on("error", (e) => {
        clearTimeout(timer);
        resolve({ name, len: text.length, error: String(e && e.stack || e).slice(0, 300) });
      });
    });
  }
  // A few workers at a time; each is its own isolate and thread.
  const limit = Number(process.env.HL_ADV_WORKERS || 2);
  let next = 0;
  await Promise.all(
    Array.from({ length: limit }, async () => {
      while (next < cases.length) {
        const [name, text] = cases[next++];
        results.push(await run(name, text));
      }
    }),
  );
  fs.writeFileSync(outPath, JSON.stringify(results));
}
