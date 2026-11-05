/**
 * Syntax highlighting that matches chroma, the highlighter Krate uses at build
 * time. The compiler turns the chroma lexers a site needs into small tables
 * (rule lists with JavaScript regular expressions); this file runs them. It
 * follows chroma's RegexLexer state machine and its HTML formatter, so for the
 * same code and language the markup is identical to what a build produces:
 * one `<span class="line"><span class="cl">` per line and one classed span per
 * token, coloured by the same stylesheet in light and dark.
 *
 * Nothing here touches the DOM or any global, so it runs unchanged on the
 * server (SSR sidecars) and in the browser.
 */

/** One rule: [pattern, emitter, mutator steps]. A missing pattern ("") always matches. */
type TableRule = [string, Emit?, number[]?];

/** Token type index, -1 (emit nothing), or an array starting with an emitter code. */
type Emit = number | EmitArray | null | undefined;
type EmitArray = [number, ...unknown[]];

/** A lexer as the compiler exports it. */
export interface HighlightTable {
  /** Lexer name. */
  n: string;
  /** CSS class of each token type. */
  t: string[];
  /** Rules by state index; state 0 is "root". */
  s: TableRule[][];
  /** Add a trailing newline when the text lacks one. */
  e?: 1;
  /** Markdown: lex a leading YAML frontmatter block first. */
  f?: 1;
  /** Picks a sublexer by name at run time (fenced code blocks). */
  g?: 1;
  /** Names of tables this one calls into. */
  d?: string[];
}

interface Lexer {
  table: HighlightTable;
  res: (RegExp | null | undefined)[][];
}

type Tok = [string | null, string];

const BY_GROUPS = 0;
const USING_SELF = 1;
const USING = 2;
const USING_GROUP = 3;
const USING_MAP = 4;
const MUT_POP = -1;
const MUT_SELF = -2;

/** Longest text the highlighter will take; above this callers show plain text. */
export const HIGHLIGHT_MAX_LENGTH = 65536;

/**
 * Most time one highlight call may spend. Lexers can be slow on hostile input
 * (a long line of unmatched characters makes some rules rescan it for every
 * character), so the work is checked against this between tokens and the call
 * gives up, returning null, when it runs out.
 */
export const HIGHLIGHT_BUDGET_MS = 400;

/**
 * Longest line the highlighter will take. A few lexer rules rescan the rest of
 * a line for every character they fail on, so their cost grows with the square
 * of the line length; capping it keeps one match attempt cheap. Lines this long
 * are data or minified code, which is not worth colouring.
 */
export const HIGHLIGHT_MAX_LINE = 2000;

let deadline = Infinity;
let steps = 0;

// Languages registered so far. The registry lives on globalThis so every copy
// of this module in a process shares it: the Node renderer is bundled with its
// own copy while page bundles import the package's, and both must see the
// tables the renderer loads at startup.
interface Registry {
  lexers: Map<string, Lexer>;
  langKeys: Map<string, string>;
  fenceKnown?: (name: string) => boolean;
}
const REGISTRY_KEY = Symbol.for("krate.highlight.registry");
const registry: Registry = ((globalThis as Record<symbol, unknown>)[REGISTRY_KEY] as Registry | undefined) || {
  lexers: new Map(),
  langKeys: new Map(),
};
(globalThis as Record<symbol, unknown>)[REGISTRY_KEY] = registry;
const lexers = registry.lexers;
const langKeys = registry.langKeys;

/**
 * Registers a lexer table and the language names that select it. Names are
 * matched case-insensitively.
 */
export function registerLanguage(table: HighlightTable, names?: string[]): void {
  const key = table.n.toLowerCase();
  lexers.set(key, { table, res: table.s.map(() => []) });
  langKeys.set(key, key);
  if (names) for (const n of names) langKeys.set(n.toLowerCase(), key);
}

/** True when a language name selects a registered lexer. */
export function hasLanguage(lang: string): boolean {
  return langKeys.has(lang.trim().toLowerCase());
}

function lexerFor(name: string): Lexer | undefined {
  const key = langKeys.get(name.trim().toLowerCase());
  return key === undefined ? undefined : lexers.get(key);
}

function ensureLF(text: string): string {
  if (text.indexOf("\r") < 0) return text;
  return text.replace(/\r\n?/g, "\n");
}

function regexFor(lx: Lexer, state: number, i: number): RegExp {
  const row = lx.res[state];
  const cached = row[i];
  if (cached) return cached;
  const re = new RegExp(lx.table.s[state][i][0], "yu");
  row[i] = re;
  return re;
}

/** Thrown when text cannot be highlighted exactly: a language is not loaded or time ran out. */
class Incomplete extends Error {}

function remapClass(table: HighlightTable, idx: number): string {
  return table.t[idx];
}

/** Runs a lexer over text, appending tokens to out. Mirrors chroma's RegexLexer. */
function lex(lx: Lexer, text: string, startState: number, nested: boolean, out: Tok[]): void {
  const table = lx.table;
  if (!nested) text = ensureLF(text);
  let added = false;
  if (!nested && table.e && !text.endsWith("\n")) {
    text += "\n";
    added = true;
  }
  const end = text.length - (added ? 1 : 0);
  const stack: number[] = [startState];
  let pos = 0;
  while (pos < end && stack.length > 0) {
    if ((++steps & 3) === 0 && Date.now() > deadline) throw new Incomplete("time");
    const state = stack[stack.length - 1];
    const rules = table.s[state];
    let m: RegExpExecArray | null = null;
    let rule: TableRule | undefined;
    for (let i = 0; i < rules.length; i++) {
      const r = rules[i];
      if (r[0] === "") {
        m = [""] as unknown as RegExpExecArray;
        rule = r;
        break;
      }
      const re = regexFor(lx, state, i);
      re.lastIndex = pos;
      m = re.exec(text);
      if (m !== null) {
        rule = r;
        break;
      }
    }
    if (m === null || rule === undefined) {
      if (text.charCodeAt(pos) === 10 && state !== startState) {
        stack.length = 0;
        stack.push(startState);
        continue;
      }
      const cp = text.codePointAt(pos) as number;
      const n = cp > 0xffff ? 2 : 1;
      out.push(["err", text.substr(pos, n)]);
      pos += n;
      continue;
    }
    pos += m[0].length;
    const mut = rule[2];
    if (mut !== undefined) {
      for (let i = 0; i < mut.length; i++) {
        const step = mut[i];
        if (step === MUT_POP) stack.pop();
        else if (step === MUT_SELF) stack.push(state);
        else stack.push(step);
      }
    }
    const emit = rule[1];
    if (emit !== undefined && emit !== null) emitTokens(lx, emit, m, out);
  }
  if (pos < text.length && stack.length === 0) out.push(["err", text.slice(pos)]);
}

function group(m: ArrayLike<string | undefined>, i: number): string {
  const v = m[i];
  return v === undefined ? "" : v;
}

function emitTokens(lx: Lexer, emit: Emit, groups: ArrayLike<string | undefined>, out: Tok[]): void {
  const table = lx.table;
  if (typeof emit === "number") {
    if (emit >= 0) out.push([table.t[emit], group(groups, 0)]);
    return;
  }
  if (emit === null || emit === undefined) return;
  switch (emit[0]) {
    case BY_GROUPS: {
      for (let i = 1; i < emit.length; i++) {
        const e = emit[i] as Emit;
        if (e === null || e === undefined) continue;
        emitTokens(lx, e, [group(groups, i)], out);
      }
      return;
    }
    case USING_SELF:
      lex(lx, group(groups, 0), emit[1] as number, true, out);
      return;
    case USING: {
      const sub = lexerFor(emit[1] as string);
      if (!sub) throw new Incomplete(emit[1] as string);
      lexTop(sub, group(groups, 0), true, out);
      return;
    }
    case USING_MAP: {
      const sub = lexerFor(emit[1] as string);
      if (!sub) throw new Incomplete(emit[1] as string);
      const tmp: Tok[] = [];
      lexTop(sub, group(groups, 0), true, tmp);
      const pair = emit[2] as number[];
      const from = remapClass(table, pair[0]);
      const to = remapClass(table, pair[1]);
      for (const t of tmp) out.push(t[0] === from ? [to, t[1]] : t);
      return;
    }
    case USING_GROUP: {
      const nameGroup = emit[1] as number;
      const codeGroup = emit[2] as number;
      const name = group(groups, nameGroup);
      const sub = fenceLexer(name);
      for (let i = 3; i < emit.length; i++) {
        const gi = i - 2; // group number of this emitter
        if (gi === codeGroup && sub) {
          lexTop(sub, group(groups, codeGroup), false, out);
        } else {
          const e = emit[i] as Emit;
          if (e !== null && e !== undefined) emitTokens(lx, e, [group(groups, gi)], out);
        }
      }
      return;
    }
  }
}

/**
 * Tells the tokenizer which names chroma itself would resolve to a lexer. A
 * fence naming a lexer that chroma has but that is not loaded cannot be
 * highlighted exactly, so the whole snippet is left as plain text.
 */
export function setFenceResolver(known: (name: string) => boolean): void {
  registry.fenceKnown = known;
}

function fenceLexer(name: string): Lexer | undefined {
  const lx = lexerFor(name);
  if (lx) return lx;
  if (registry.fenceKnown && registry.fenceKnown(name)) throw new Incomplete(name);
  return undefined;
}

const ESC: Record<string, string> = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&#34;", "'": "&#39;" };

/** HTML-escapes the way Go's html.EscapeString does. */
export function escapeHTML(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ESC[c]);
}

/** Parses a line spec such as "1,3-5" into 1-based inclusive ranges. */
export function parseLineRanges(spec: string): [number, number][] {
  const out: [number, number][] = [];
  for (let part of spec.split(",")) {
    part = part.trim();
    if (part === "") continue;
    const dash = part.indexOf("-");
    const lo = dash < 0 ? part : part.slice(0, dash);
    const a = atoi(lo.trim());
    if (a === null || a < 1) continue;
    let b = a;
    if (dash >= 0) {
      const hi = atoi(part.slice(dash + 1).trim());
      if (hi === null || hi < a) continue;
      b = hi;
    }
    out.push([a, b]);
  }
  return out;
}

function atoi(s: string): number | null {
  if (!/^[+-]?\d+$/.test(s)) return null;
  const n = Number(s);
  return Number.isSafeInteger(n) ? n : null;
}

/** Splits tokens at newlines the way chroma.SplitTokensIntoLines does. */
function splitLines(tokens: Tok[]): Tok[][] {
  const out: Tok[][] = [];
  let line: Tok[] = [];
  outer: for (const tok of tokens) {
    let value = tok[1];
    let nl = value.indexOf("\n");
    while (nl >= 0) {
      line.push([tok[0], value.slice(0, nl + 1)]);
      out.push(line);
      line = [];
      value = value.slice(nl + 1);
      if (value.length === 0) continue outer;
      nl = value.indexOf("\n");
    }
    line.push([tok[0], value]);
  }
  if (line.length > 0) out.push(line);
  if (out.length > 0) {
    const last = out[out.length - 1];
    if (last.length === 1 && last[0][1] === "") out.pop();
  }
  return out;
}

function shouldHighlight(ranges: [number, number][], idx: number, line: number): [boolean, number] {
  while (idx < ranges.length && line > ranges[idx][1]) idx++;
  if (idx < ranges.length && line >= ranges[idx][0] && line <= ranges[idx][1]) return [true, idx];
  return [false, idx];
}

/** Writes tokens as chroma's HTML formatter does (class mode, no line numbers). */
function format(tokens: Tok[], ranges: [number, number][]): string {
  const lines = splitLines(tokens);
  let html = "";
  let idx = 0;
  for (let i = 0; i < lines.length; i++) {
    const [hl, next] = shouldHighlight(ranges, idx, i + 1);
    idx = next;
    html += hl ? '<span class="line hl"><span class="cl">' : '<span class="line"><span class="cl">';
    for (const [cls, value] of lines[i]) {
      if (cls === null) continue;
      const text = escapeHTML(value);
      html += cls === "" ? text : '<span class="' + cls + '">' + text + "</span>";
    }
    html += "</span></span>";
  }
  return html;
}

/** Whitespace as Go's strings.TrimSpace sees it. */
export function trimSpace(s: string): string {
  return s.replace(SPACE_EDGES, "");
}

const SPACE_EDGES = new RegExp(
  "^[" + "\\t-\\r \\x85\\xa0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000" + "]+|[" +
    "\\t-\\r \\x85\\xa0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000" + "]+$",
  "g",
);

/**
 * Highlights code, returning the inner HTML of a highlighted block (the lines
 * and token spans, without the surrounding pre and code) or null when this
 * text cannot be highlighted exactly: the language is not loaded, the text is
 * longer than maxLength (HIGHLIGHT_MAX_LENGTH by default), has a line longer than
 * maxLine (HIGHLIGHT_MAX_LINE), highlighting took
 * longer than budgetMs (HIGHLIGHT_BUDGET_MS), or a fenced block names a language that is
 * not loaded. A null result means "show it as plain text".
 *
 * Output matches the markup the compiler writes for the same code.
 */
export function highlight(
  code: string,
  lang: string,
  lines?: string,
  limits: { maxLength?: number; maxLine?: number; budgetMs?: number } = {},
): string | null {
  if (code.length > (limits.maxLength ?? HIGHLIGHT_MAX_LENGTH)) return null;
  const lx = lexerFor(lang);
  if (!lx) return null;
  if (longestLine(code) > (limits.maxLine ?? HIGHLIGHT_MAX_LINE)) return null;
  const out: Tok[] = [];
  deadline = Date.now() + (limits.budgetMs ?? HIGHLIGHT_BUDGET_MS);
  try {
    lexTop(lx, code, false, out);
  } catch (e) {
    if (e instanceof Incomplete) return null;
    throw e;
  } finally {
    deadline = Infinity;
  }
  return format(out, lines ? parseLineRanges(lines).sort((a, b) => a[0] - b[0]) : []);
}

function longestLine(s: string): number {
  let best = 0;
  let run = 0;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c === 10 || c === 13) {
      if (run > best) best = run;
      run = 0;
    } else run++;
  }
  return run > best ? run : best;
}

/** Highlights text with a lexer, handling the markdown frontmatter wrapper. */
function lexTop(lx: Lexer, code: string, nested: boolean, out: Tok[]): void {
  if (lx.table.f) {
    const split = splitFrontmatter(code);
    const yaml = split && lexerFor("yaml");
    if (split && yaml) {
      lex(yaml, split[0], 0, nested, out);
      lex(lx, split[1], 0, nested, out);
      return;
    }
  }
  lex(lx, code, 0, nested, out);
}

function splitFrontmatter(text: string): [string, string] | null {
  if (!text.startsWith("---\n") && !text.startsWith("---\r\n")) return null;
  let lineEnd = text.indexOf("\n");
  if (lineEnd < 0) return null;
  if (text.slice(0, lineEnd).replace(/\r$/, "") !== "---") return null;
  for (let pos = lineEnd + 1; pos < text.length; ) {
    const next = text.indexOf("\n", pos);
    if (next < 0) break;
    lineEnd = next;
    const line = text.slice(pos, lineEnd).replace(/\r$/, "");
    if (line === "---") return [text.slice(0, lineEnd + 1), text.slice(lineEnd + 1)];
    pos = lineEnd + 1;
  }
  return null;
}

/**
 * HTML for text in a language chroma does not know: its fallback lexer writes
 * the text as line wrappers with no token spans.
 */
export function plainLines(code: string, lines?: string): string {
  const out: Tok[] = [];
  const text = ensureLF(code);
  const re = /[^\n]+|\n/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) out.push(["", m[0]]);
  return format(out, lines ? parseLineRanges(lines).sort((a, b) => a[0] - b[0]) : []);
}

/** Names of the registered languages, for tests. */
export function loadedLanguages(): string[] {
  return [...lexers.keys()];
}
