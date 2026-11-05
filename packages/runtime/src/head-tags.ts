// <Head> children of request-time renders.
//
// A <Head> in an async page renders its children in place, between two comment
// markers (server.ts Head). takeHead lifts them out of the rendered body, and
// mergeHead puts them into the <head> of the response, so crawlers and social
// scrapers see them in the first bytes. The Go host does the same merge
// (internal/build/headmerge.go); both must keep the same rules:
//
//   - A page tag replaces a baked tag with the same key: meta name,
//     property or http-equiv, meta charset, link rel=canonical, base. The
//     page wins over layouts and site config.
//   - Among the page's own tags a repeated name, http-equiv, charset,
//     canonical or base keeps the last one. A repeated property (og:image,
//     article:tag) keeps all of them.
//   - A tag identical to one already in the head is not added twice.
//   - Only meta, link, base, script, style and noscript are head content;
//     anything else inside <Head> is dropped.
//   - The title replaces the baked title, or is added when there is none.

export const HEAD_OPEN = "<!--krate-head-->";
export const HEAD_CLOSE = "<!--/krate-head-->";

export interface HeadSplit {
  /** Text of the last <title> in the rendered page (already HTML-escaped). */
  title: string;
  /** Head tags (meta, link, script, ...) lifted out of every <Head>. */
  head: string;
  /** The rendered html without <Head> blocks and titles. */
  html: string;
}

/**
 * Splits the document head content out of rendered page html: the <title>
 * (the last one that is not inside an <svg>, as with static <Head> hoisting
 * where a nested Head overrides its parent) and everything inside <Head>
 * markers. Both are removed from the returned html.
 */
export function takeHead(html: string): HeadSplit {
  const hasHead = html.indexOf(HEAD_OPEN) !== -1;
  const hasTitle = /<title[\s>]/i.test(html);
  if (!hasHead && !hasTitle) return { title: "", head: "", html };
  let body = html;
  let headText = "";
  if (hasHead) {
    // Head blocks may nest; the outermost pair wins and inner markers go.
    let out = "";
    let i = 0;
    for (;;) {
      const open = body.indexOf(HEAD_OPEN, i);
      if (open === -1) {
        out += body.slice(i);
        break;
      }
      out += body.slice(i, open);
      let depth = 1;
      let j = open + HEAD_OPEN.length;
      let inner = "";
      let from = j;
      while (depth > 0) {
        const nextOpen = body.indexOf(HEAD_OPEN, j);
        const nextClose = body.indexOf(HEAD_CLOSE, j);
        if (nextClose === -1) {
          // Unbalanced: keep the rest as head text.
          inner += body.slice(from);
          j = body.length;
          depth = 0;
          break;
        }
        if (nextOpen !== -1 && nextOpen < nextClose) {
          inner += body.slice(from, nextOpen);
          from = nextOpen + HEAD_OPEN.length;
          j = from;
          depth++;
        } else {
          inner += body.slice(from, nextClose);
          from = nextClose + HEAD_CLOSE.length;
          j = from;
          depth--;
        }
      }
      headText += inner;
      i = j;
    }
    body = out;
  }
  const fromHead = takeTitles(headText);
  const fromBody = takeTitles(body);
  return {
    title: fromHead.title || fromBody.title,
    head: fromHead.html,
    html: fromBody.html,
  };
}

function takeTitles(html: string): { title: string; html: string } {
  if (html.indexOf("<title") === -1 && html.indexOf("<TITLE") === -1) return { title: "", html };
  const re = /<title(?:\s[^>]*)?>([\s\S]*?)<\/title>/gi;
  const found: { index: number; length: number; text: string }[] = [];
  let m: RegExpExecArray | null;
  while ((m = re.exec(html))) {
    if (insideSVG(html, m.index)) continue;
    found.push({ index: m.index, length: m[0].length, text: m[1].trim() });
  }
  if (found.length === 0) return { title: "", html };
  let out = "";
  let last = 0;
  for (const f of found) {
    out += html.slice(last, f.index);
    last = f.index + f.length;
  }
  out += html.slice(last);
  return { title: found[found.length - 1].text, html: out };
}

function insideSVG(html: string, pos: number): boolean {
  const before = html.slice(0, pos).toLowerCase();
  let depth = 0;
  const re = /<(\/?)svg[\s>\/]/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(before))) depth += m[1] ? -1 : 1;
  return depth > 0;
}

// Tag scanning

export interface HeadElement {
  name: string;
  /** Start and end offsets in the scanned text. */
  start: number;
  end: number;
  attrs: Map<string, string>;
  /** Text of a script/style/noscript element. */
  inner: string;
}

const RAW_TEXT = new Set(["script", "style", "noscript", "title", "textarea", "template"]);

function isSpace(c: string): boolean {
  return c === " " || c === "\t" || c === "\n" || c === "\r" || c === "\f";
}

/**
 * Scans the elements of head markup: tag, attributes (quoted, unquoted and
 * valueless, as the HTML tokenizer reads them) and the raw text of script,
 * style and noscript. Comments and text between elements are skipped.
 */
export function scanHeadElements(s: string): HeadElement[] {
  const out: HeadElement[] = [];
  let lower: string | null = null;
  let i = 0;
  while (i < s.length) {
    const lt = s.indexOf("<", i);
    if (lt === -1) break;
    if (s.startsWith("<!--", lt)) {
      const end = s.indexOf("-->", lt + 4);
      i = end === -1 ? s.length : end + 3;
      continue;
    }
    const c = s.charAt(lt + 1);
    if (!/[a-zA-Z]/.test(c)) {
      const gt = s.indexOf(">", lt);
      i = gt === -1 ? s.length : gt + 1;
      continue;
    }
    let j = lt + 1;
    while (j < s.length && !isSpace(s[j]) && s[j] !== "/" && s[j] !== ">") j++;
    const name = s.slice(lt + 1, j).toLowerCase();
    const attrs = new Map<string, string>();
    let closed = false;
    while (j < s.length) {
      while (j < s.length && (isSpace(s[j]) || s[j] === "/")) j++;
      if (s[j] === ">") {
        j++;
        closed = true;
        break;
      }
      let k = j;
      while (k < s.length && !isSpace(s[k]) && s[k] !== "=" && s[k] !== ">" && s[k] !== "/") k++;
      if (k === j) {
        j++;
        continue;
      }
      const an = s.slice(j, k).toLowerCase();
      j = k;
      while (j < s.length && isSpace(s[j])) j++;
      let value = "";
      if (s[j] === "=") {
        j++;
        while (j < s.length && isSpace(s[j])) j++;
        if (s[j] === '"' || s[j] === "'") {
          const q = s[j];
          const e = s.indexOf(q, j + 1);
          value = s.slice(j + 1, e === -1 ? s.length : e);
          j = e === -1 ? s.length : e + 1;
        } else {
          let e = j;
          while (e < s.length && !isSpace(s[e]) && s[e] !== ">") e++;
          value = s.slice(j, e);
          j = e;
        }
      }
      if (!attrs.has(an)) attrs.set(an, value);
    }
    if (!closed) break;
    let end = j;
    let inner = "";
    if (RAW_TEXT.has(name)) {
      if (lower === null) lower = s.toLowerCase();
      const close = lower.indexOf("</" + name, j);
      if (close === -1) {
        inner = s.slice(j);
        end = s.length;
      } else {
        inner = s.slice(j, close);
        const gt = s.indexOf(">", close);
        end = gt === -1 ? s.length : gt + 1;
      }
    }
    out.push({ name, start: lt, end, attrs, inner });
    i = end;
  }
  return out;
}

const HEAD_CONTENT = new Set(["meta", "link", "base", "script", "style", "noscript"]);

/** The dedupe key of an element ("" when it never replaces another). */
export function headKey(el: HeadElement): string {
  const a = el.attrs;
  if (el.name === "meta") {
    if (a.has("charset")) return "meta:charset";
    if (a.has("name")) return "meta:name:" + (a.get("name") || "").toLowerCase();
    if (a.has("property")) return "meta:property:" + (a.get("property") || "").toLowerCase();
    if (a.has("http-equiv")) return "meta:http-equiv:" + (a.get("http-equiv") || "").toLowerCase();
    return "";
  }
  if (el.name === "link") {
    const rel = (a.get("rel") || "").toLowerCase().split(/\s+/);
    if (rel.includes("canonical")) return "link:canonical";
    return "";
  }
  if (el.name === "base") return "base";
  return "";
}

// A repeated property (og:image) is legitimate; every other key is single.
function singleKey(key: string): boolean {
  return key !== "" && !key.startsWith("meta:property:");
}

function signature(el: HeadElement): string {
  const attrs = [...el.attrs.entries()].sort((x, y) => (x[0] < y[0] ? -1 : x[0] > y[0] ? 1 : 0));
  return el.name + "|" + attrs.map(([k, v]) => k + "=" + v).join("&") + "|" + el.inner;
}

export interface HeadTag {
  html: string;
  key: string;
  el: HeadElement;
}

/** The head tags of a <Head> fragment: only head content, last single key wins. */
export function parseHeadTags(fragment: string): HeadTag[] {
  const tags: HeadTag[] = [];
  for (const el of scanHeadElements(fragment)) {
    if (!HEAD_CONTENT.has(el.name)) continue;
    tags.push({ html: fragment.slice(el.start, el.end), key: headKey(el), el });
  }
  const last = new Map<string, number>();
  tags.forEach((t, i) => {
    if (singleKey(t.key)) last.set(t.key, i);
  });
  return tags.filter((t, i) => !singleKey(t.key) || last.get(t.key) === i);
}

/**
 * Merges page head content into the head section of a document: `head` is the
 * text from the start of the document up to (not including) </head>. Returns
 * the new head section.
 */
export function mergeHeadSection(head: string, title: string, fragment: string): string {
  const tags = fragment ? parseHeadTags(fragment) : [];
  if (title === "" && tags.length === 0) return head;
  const els = scanHeadElements(head);
  // Edits are collected as [start, end, replacement] over the original text.
  const edits: Array<[number, number, string]> = [];
  const pageKeys = new Set(tags.map((t) => t.key).filter((k) => k !== ""));
  const kept = new Set<string>();
  let titleDone = false;
  for (const el of els) {
    if (el.name === "title" && title !== "" && !titleDone) {
      edits.push([el.start, el.end, "<title>" + title + "</title>"]);
      titleDone = true;
      continue;
    }
    if (el.name === "title" && title !== "") {
      edits.push([el.start, el.end, ""]);
      continue;
    }
    const key = headKey(el);
    if (key !== "" && pageKeys.has(key)) {
      edits.push([el.start, el.end, ""]);
      continue;
    }
    kept.add(signature(el));
  }
  let add = "";
  if (title !== "" && !titleDone) add += "<title>" + title + "</title>";
  for (const t of tags) {
    const sig = signature(t.el);
    if (kept.has(sig)) continue;
    kept.add(sig);
    add += t.html;
  }
  let out = "";
  let at = 0;
  for (const [s, e, r] of edits.sort((x, y) => x[0] - y[0])) {
    out += head.slice(at, s) + r;
    at = e;
  }
  out += head.slice(at);
  return out + add;
}

/** Index where the head section of a document ends (</head>, else <body), or -1. */
export function headEndOf(doc: string): number {
  const lower = doc.toLowerCase();
  const h = lower.indexOf("</head");
  if (h !== -1) return h;
  return lower.indexOf("<body");
}

/** Merges the title and head tags into the head of a whole document. */
export function mergeHead(doc: string, title: string, fragment: string): string {
  if (title === "" && fragment === "") return doc;
  const end = headEndOf(doc);
  if (end === -1) return doc;
  return mergeHeadSection(doc.slice(0, end), title, fragment) + doc.slice(end);
}
