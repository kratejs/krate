// Small syntax highlighter for the playground: TSX/TS/JS, CSS, HTML, Markdown
// and JSON. highlightLines(text, lang) returns one HTML string per source line
// (tokens that span lines are split), so the editor can mark single lines.
// pretty(text, lang) re-indents minified HTML and CSS.

const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

const KEYWORDS = new Set(
  ("break case catch class const continue debugger default delete do else enum export extends finally for function if import in instanceof new return super switch this throw try typeof var void while with yield async await let static of as from type interface implements private public protected readonly declare namespace abstract keyof satisfies get set").split(" "),
);
const CONSTANTS = new Set(["true", "false", "null", "undefined", "NaN", "Infinity"]);

/** Language for a file name. */
export function langOf(name) {
  const ext = /\.([a-z0-9]+)$/i.exec(name || "")?.[1]?.toLowerCase();
  switch (ext) {
    case "tsx":
    case "jsx":
      return "tsx";
    case "ts":
    case "js":
    case "mjs":
    case "cjs":
      return "ts";
    case "css":
      return "css";
    case "html":
    case "htm":
      return "html";
    case "md":
    case "mdx":
      return "md";
    case "json":
      return "json";
    default:
      return "text";
  }
}

const isIdStart = (c) => /[A-Za-z_$]/.test(c);
const isIdPart = (c) => /[\w$]/.test(c);

// ---- TSX / TS ----------------------------------------------------------

function tokenizeJS(src, jsx) {
  const out = [];
  const stack = ["js"];
  const n = src.length;
  let i = 0;
  let prevSig = ""; // last significant source text, for the JSX-vs-less-than call
  const push = (cls, text) => {
    if (!text) return;
    out.push([cls, text]);
    if (cls !== "m" && /\S/.test(text)) prevSig = text.trim();
  };
  // Scans a template literal body from i (just after the opening ` or }).
  const template = () => {
    let j = i;
    while (j < n) {
      const c = src[j];
      if (c === "\\") j += 2;
      else if (c === "`") {
        push("s", src.slice(i, j + 1));
        i = j + 1;
        stack.pop();
        return;
      } else if (c === "$" && src[j + 1] === "{") {
        push("s", src.slice(i, j));
        push("", "${");
        i = j + 2;
        stack.push("js");
        return;
      } else j++;
    }
    push("s", src.slice(i));
    i = n;
  };
  const jsxStart = () => {
    if (!jsx) return false;
    if (!/^<\/?([A-Za-z][\w.:-]*)?[\s/>]/.test(src.slice(i, i + 80))) return false;
    if (stack[stack.length - 1] === "jsx") return true;
    if (!prevSig) return true;
    const last = prevSig[prevSig.length - 1];
    if (/[(,={}>;:?&|[!~+\-*%]/.test(last)) return true;
    return prevSig === "return" || prevSig === "=>" || prevSig === "default";
  };

  while (i < n) {
    const ctx = stack[stack.length - 1];
    const c = src[i];

    if (ctx === "tpl") {
      template();
      continue;
    }

    if (ctx === "jsx") {
      if (c === "{") {
        push("", "{");
        i++;
        stack.push("js");
      } else if (c === "<") {
        // fall through to the tag handling below
        if (src[i + 1] === "/") {
          const m = /^<\/([A-Za-z][\w.:-]*)?\s*>/.exec(src.slice(i, i + 120));
          if (m) {
            push("g", "</" + (m[1] || ""));
            push("g", m[0].slice(2 + (m[1] || "").length));
            i += m[0].length;
            stack.pop();
            continue;
          }
        }
        const m = /^<([A-Za-z][\w.:-]*)?/.exec(src.slice(i, i + 120));
        push("g", m[0]);
        i += m[0].length;
        stack.push("tag");
      } else {
        let j = i + 1;
        while (j < n && src[j] !== "{" && src[j] !== "<") j++;
        push("", src.slice(i, j));
        i = j;
      }
      continue;
    }

    if (ctx === "tag") {
      if (/\s/.test(c)) {
        let j = i + 1;
        while (j < n && /\s/.test(src[j])) j++;
        push("", src.slice(i, j));
        i = j;
      } else if (c === "/" && src[i + 1] === ">") {
        push("g", "/>");
        i += 2;
        stack.pop();
      } else if (c === ">") {
        push("g", ">");
        i++;
        stack.pop();
        stack.push("jsx");
      } else if (c === "{") {
        push("", "{");
        i++;
        stack.push("js");
      } else if (c === '"' || c === "'") {
        let j = i + 1;
        while (j < n && src[j] !== c) j++;
        push("s", src.slice(i, j + 1));
        i = j + 1;
      } else if (isIdStart(c)) {
        let j = i + 1;
        while (j < n && /[\w$:.-]/.test(src[j])) j++;
        push("a", src.slice(i, j));
        i = j;
      } else {
        push("", c);
        i++;
      }
      continue;
    }

    // ctx === "js"
    if (c === "/" && src[i + 1] === "/") {
      let j = src.indexOf("\n", i);
      if (j < 0) j = n;
      push("m", src.slice(i, j));
      i = j;
    } else if (c === "/" && src[i + 1] === "*") {
      let j = src.indexOf("*/", i + 2);
      j = j < 0 ? n : j + 2;
      push("m", src.slice(i, j));
      i = j;
    } else if (c === '"' || c === "'") {
      let j = i + 1;
      while (j < n && src[j] !== c && src[j] !== "\n") j += src[j] === "\\" ? 2 : 1;
      push("s", src.slice(i, Math.min(j + 1, n)));
      i = Math.min(j + 1, n);
    } else if (c === "`") {
      push("s", "`");
      i++;
      stack.push("tpl");
    } else if (c === "{") {
      push("", "{");
      i++;
      stack.push("js");
    } else if (c === "}") {
      push("", "}");
      i++;
      if (stack.length > 1) stack.pop();
    } else if (c === "<" && jsxStart()) {
      const m = /^<\/?([A-Za-z][\w.:-]*)?/.exec(src.slice(i, i + 120));
      push("g", m[0]);
      i += m[0].length;
      stack.push("tag");
    } else if (/\d/.test(c) || (c === "." && /\d/.test(src[i + 1] || ""))) {
      const m = /^(0[xX][\da-fA-F_]+|0[bB][01_]+|\d[\d_]*\.?[\d_]*(?:[eE][+-]?\d+)?n?|\.\d+)/.exec(src.slice(i, i + 40));
      const text = m ? m[0] : c;
      push("n", text);
      i += text.length;
    } else if (isIdStart(c)) {
      let j = i + 1;
      while (j < n && isIdPart(src[j])) j++;
      const word = src.slice(i, j);
      let k = j;
      while (k < n && (src[k] === " " || src[k] === "\t")) k++;
      const afterDot = prevSig === ".";
      let cls = "";
      if (!afterDot && KEYWORDS.has(word)) cls = "k";
      else if (!afterDot && CONSTANTS.has(word)) cls = "n";
      else if (src[k] === "(" || (src[k] === "<" && /^<[\w\s,.[\]|&]*>\(/.test(src.slice(k, k + 60)))) cls = "f";
      else if (/^[A-Z]/.test(word) && word.length > 1) cls = "t";
      push(cls, word);
      i = j;
    } else {
      // Punctuation and whitespace: group runs of whitespace, keep others single.
      if (/\s/.test(c)) {
        let j = i + 1;
        while (j < n && /\s/.test(src[j])) j++;
        out.push(["", src.slice(i, j)]);
        i = j;
      } else {
        push("", c === "=" && src[i + 1] === ">" ? "=>" : c);
        i += c === "=" && src[i + 1] === ">" ? 2 : 1;
      }
    }
  }
  return out;
}

// ---- CSS -----------------------------------------------------------------

function tokenizeCSS(src) {
  const out = [];
  const n = src.length;
  let i = 0;
  const push = (cls, text) => text && out.push([cls, text]);
  const stmtKind = (from) => {
    // A statement is a rule header if "{" comes before ";" or "}".
    let paren = 0;
    for (let j = from; j < n; j++) {
      const c = src[j];
      if (c === "(") paren++;
      else if (c === ")") paren--;
      else if (c === "/" && src[j + 1] === "*") {
        const e = src.indexOf("*/", j + 2);
        if (e === -1) return "decl";
        j = e + 1;
      } else if (c === '"' || c === "'") {
        j = src.indexOf(c, j + 1);
        if (j < 0) return "decl";
      } else if (paren <= 0 && c === "{") return "rule";
      else if (paren <= 0 && (c === ";" || c === "}")) return "decl";
    }
    return "decl";
  };
  const comment = () => {
    let j = src.indexOf("*/", i + 2);
    j = j < 0 ? n : j + 2;
    push("m", src.slice(i, j));
    i = j;
  };
  const str = () => {
    const q = src[i];
    let j = i + 1;
    while (j < n && src[j] !== q && src[j] !== "\n") j += src[j] === "\\" ? 2 : 1;
    push("s", src.slice(i, Math.min(j + 1, n)));
    i = Math.min(j + 1, n);
  };
  const value = () => {
    // After the colon of a declaration, until ; or }.
    while (i < n && src[i] !== ";" && src[i] !== "}") {
      const c = src[i];
      if (c === "/" && src[i + 1] === "*") comment();
      else if (c === '"' || c === "'") str();
      else if (c === "#" && /[\da-fA-F]/.test(src[i + 1] || "")) {
        const m = /^#[\da-fA-F]+/.exec(src.slice(i, i + 12));
        push("n", m[0]);
        i += m[0].length;
      } else if (/[\d.]/.test(c) || (c === "-" && /[\d.]/.test(src[i + 1] || ""))) {
        const m = /^-?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?(?:%|[a-zA-Z]+)?/.exec(src.slice(i, i + 30));
        if (m) {
          push("n", m[0]);
          i += m[0].length;
        } else {
          push("", c);
          i++;
        }
      } else if (c === "-" && src[i + 1] === "-") {
        const m = /^--[\w-]*/.exec(src.slice(i, i + 80));
        push("t", m[0]);
        i += m[0].length;
      } else if (isIdStart(c)) {
        const m = /^[\w-]+/.exec(src.slice(i, i + 80));
        const next = src[i + m[0].length];
        push(next === "(" ? "f" : "", m[0]);
        i += m[0].length;
      } else {
        push("", c);
        i++;
      }
    }
  };

  while (i < n) {
    const c = src[i];
    if (/\s/.test(c)) {
      let j = i + 1;
      while (j < n && /\s/.test(src[j])) j++;
      push("", src.slice(i, j));
      i = j;
    } else if (c === "/" && src[i + 1] === "*") comment();
    else if (c === "{" || c === "}" || c === ";") {
      push("", c);
      i++;
    } else if (c === "@") {
      const m = /^@[\w-]+/.exec(src.slice(i, i + 60));
      push("k", m ? m[0] : "@");
      i += m ? m[0].length : 1;
      // The prelude up to { or ; (media queries, import paths).
      while (i < n && src[i] !== "{" && src[i] !== ";") {
        if (src[i] === '"' || src[i] === "'") str();
        else if (src[i] === "/" && src[i + 1] === "*") comment();
        else if (/\d/.test(src[i])) {
          const mm = /^\d+\.?\d*(?:%|[a-zA-Z]+)?/.exec(src.slice(i, i + 20));
          push("n", mm[0]);
          i += mm[0].length;
        } else if (isIdStart(src[i])) {
          const mm = /^[\w-]+/.exec(src.slice(i, i + 60));
          push(src[i + mm[0].length] === "(" ? "f" : "", mm[0]);
          i += mm[0].length;
        } else {
          push("", src[i]);
          i++;
        }
      }
    } else if (stmtKind(i) === "rule") {
      // Selector up to {.
      while (i < n && src[i] !== "{") {
        const s = src[i];
        if (s === "/" && src[i + 1] === "*") comment();
        else if (s === '"' || s === "'") str();
        else if (s === "." || s === "#") {
          const m = /^[.#][\w-]+/.exec(src.slice(i, i + 80));
          if (m) {
            push(s === "." ? "t" : "n", m[0]);
            i += m[0].length;
          } else {
            push("", s);
            i++;
          }
        } else if (s === ":") {
          const m = /^::?[\w-]+/.exec(src.slice(i, i + 60));
          push("f", m ? m[0] : ":");
          i += m ? m[0].length : 1;
        } else if (s === "[") {
          const e = src.indexOf("]", i);
          const j = e === -1 ? n : e + 1;
          push("a", src.slice(i, j));
          i = j;
        } else if (isIdStart(s) || s === "*" || s === "&") {
          const m = /^[\w-]+|^[*&]/.exec(src.slice(i, i + 80));
          push("g", m[0]);
          i += m[0].length;
        } else {
          push("", s);
          i++;
        }
      }
    } else {
      // Declaration: property, colon, value.
      const m = /^(--[\w-]*|-?[A-Za-z_][\w-]*)/.exec(src.slice(i, i + 80));
      if (m) {
        push(m[0].startsWith("--") ? "t" : "a", m[0]);
        i += m[0].length;
        while (i < n && /\s/.test(src[i])) {
          push("", src[i]);
          i++;
        }
        if (src[i] === ":") {
          push("", ":");
          i++;
          value();
        }
      } else {
        push("", c);
        i++;
      }
    }
  }
  return out;
}

// ---- HTML ----------------------------------------------------------------

function tokenizeHTML(src) {
  const out = [];
  const n = src.length;
  let i = 0;
  const push = (cls, text) => text && out.push([cls, text]);
  while (i < n) {
    if (src.startsWith("<!--", i)) {
      let j = src.indexOf("-->", i + 4);
      j = j < 0 ? n : j + 3;
      push("m", src.slice(i, j));
      i = j;
    } else if (src[i] === "<" && /[A-Za-z!/]/.test(src[i + 1] || "")) {
      const m = /^<(!?\/?)([\w:-]*)/.exec(src.slice(i, i + 80));
      push("g", "<" + m[1] + m[2]);
      i += m[0].length;
      // Attributes up to >.
      while (i < n && src[i] !== ">") {
        const c = src[i];
        if (/\s/.test(c)) {
          let j = i + 1;
          while (j < n && /\s/.test(src[j])) j++;
          push("", src.slice(i, j));
          i = j;
        } else if (c === '"' || c === "'") {
          let j = src.indexOf(c, i + 1);
          j = j < 0 ? n : j + 1;
          push("s", src.slice(i, j));
          i = j;
        } else if (c === "=" || c === "/") {
          push("", c);
          i++;
        } else {
          const mm = /^[^\s=/>"']+/.exec(src.slice(i, i + 200));
          push("a", mm ? mm[0] : c);
          i += mm ? mm[0].length : 1;
        }
      }
      if (i < n) {
        push("g", ">");
        i++;
      }
      const name = m[2].toLowerCase();
      if (!m[1].includes("/") && (name === "script" || name === "style")) {
        const close = src.toLowerCase().indexOf("</" + name, i);
        const end = close === -1 ? n : close;
        const body = src.slice(i, end);
        const inner = name === "style" ? tokenizeCSS(body) : tokenizeJS(body, false);
        for (const t of inner) out.push(t);
        i = end;
      }
    } else {
      let j = src.indexOf("<", i + 1);
      if (j < 0) j = n;
      push("", src.slice(i, j));
      i = j;
    }
  }
  return out;
}

// ---- Markdown ------------------------------------------------------------

function tokenizeMD(src) {
  const out = [];
  const lines = src.split("\n");
  let fence = null;
  let front = lines[0] === "---";
  lines.forEach((line, idx) => {
    const nl = idx < lines.length - 1 ? "\n" : "";
    const push = (cls, text) => text && out.push([cls, text]);
    if (front) {
      if (idx > 0 && line === "---") front = false;
      if (idx === 0 || line === "---") push("m", line);
      else {
        const m = /^([\w-]+)(:)(.*)$/.exec(line);
        if (m) {
          push("a", m[1]);
          push("", m[2]);
          push("s", m[3]);
        } else push("", line);
      }
      push("", nl);
      return;
    }
    if (fence) {
      if (line.startsWith(fence)) fence = null;
      push(fence === null ? "m" : "s", line);
      push("", nl);
      return;
    }
    const f = /^(\s*)(```+|~~~+)/.exec(line);
    if (f) {
      fence = f[2];
      push("m", line);
      push("", nl);
      return;
    }
    const h = /^(#{1,6}\s)(.*)$/.exec(line);
    if (h) {
      push("k", h[1]);
      push("t", h[2]);
      push("", nl);
      return;
    }
    let rest = line;
    const li = /^(\s*(?:[-*+]|\d+\.)\s(?:\[[ xX]\]\s)?|>\s?)/.exec(rest);
    if (li) {
      push("k", li[1]);
      rest = rest.slice(li[1].length);
    }
    const re = /`[^`]*`|\*\*[^*]+\*\*|\*[^*\s][^*]*\*|\[[^\]]*\]\([^)]*\)/g;
    let last = 0;
    let m;
    while ((m = re.exec(rest))) {
      push("", rest.slice(last, m.index));
      const t = m[0];
      push(t[0] === "`" ? "s" : t[0] === "[" ? "f" : "t", t);
      last = m.index + t.length;
    }
    push("", rest.slice(last));
    push("", nl);
  });
  return out;
}

function tokenizeJSON(src) {
  const out = [];
  const re = /("(?:\\.|[^"\\])*")(\s*:)?|(-?\d+\.?\d*(?:[eE][+-]?\d+)?)|\b(true|false|null)\b|([\s\S])/g;
  let m;
  while ((m = re.exec(src))) {
    if (m[1]) {
      out.push([m[2] ? "a" : "s", m[1]]);
      if (m[2]) out.push(["", m[2]]);
    } else if (m[3]) out.push(["n", m[3]]);
    else if (m[4]) out.push(["n", m[4]]);
    else out.push(["", m[5]]);
  }
  return out;
}

export function tokenize(text, lang) {
  switch (lang) {
    case "tsx":
      return tokenizeJS(text, true);
    case "ts":
      return tokenizeJS(text, false);
    case "css":
      return tokenizeCSS(text);
    case "html":
      return tokenizeHTML(text);
    case "md":
      return tokenizeMD(text);
    case "json":
      return tokenizeJSON(text);
    default:
      return [["", text]];
  }
}

/** One HTML string per source line. */
export function highlightLines(text, lang) {
  const lines = [""];
  for (const [cls, t] of tokenize(text, lang)) {
    const parts = t.split("\n");
    parts.forEach((part, k) => {
      if (k > 0) lines.push("");
      if (!part) return;
      lines[lines.length - 1] += cls ? `<span class="tk-${cls}">${esc(part)}</span>` : esc(part);
    });
  }
  return lines;
}

// ---- Prettifiers (for minified output) ---------------------------------------

const BLOCK = new Set(
  "html head body main div section nav header footer article aside ul ol li p h1 h2 h3 h4 h5 h6 table thead tbody tr td th form fieldset dl dt dd figure blockquote details summary dialog title meta link script style template noscript base".split(" "),
);
const VOID = new Set("meta link base br hr img input area col embed source track wbr".split(" "));

export function prettyHTML(html) {
  if (html.split("\n").length > 8 + html.length / 400) return html;
  const out = [];
  let depth = 0;
  let cur = "";
  const flush = () => {
    if (cur.trim()) out.push("  ".repeat(Math.max(depth, 0)) + cur.trim());
    cur = "";
  };
  const re = /<!--[\s\S]*?-->|<!doctype[^>]*>|<\/?[a-zA-Z][^>]*>|[^<]+|</gi;
  let m;
  let raw = null;
  let lastOpen = null;
  while ((m = re.exec(html))) {
    const t = m[0];
    if (raw) {
      if (new RegExp("^</" + raw, "i").test(t)) {
        flush();
        depth--;
        out.push("  ".repeat(depth) + t);
        raw = null;
      } else cur += t;
      continue;
    }
    const tag = /^<(\/?)([a-zA-Z][\w:-]*)/.exec(t);
    if (!tag) {
      if (t.startsWith("<!--") || /^<!doctype/i.test(t)) {
        flush();
        out.push("  ".repeat(depth) + t);
      } else cur += t;
      continue;
    }
    const name = tag[2].toLowerCase();
    const closing = tag[1] === "/";
    if (!BLOCK.has(name)) {
      cur += t;
      continue;
    }
    if (closing) {
      if (lastOpen && lastOpen.name === name && lastOpen.idx === out.length - 1 && cur.trim()) {
        out[lastOpen.idx] += cur.trim() + t;
        cur = "";
        depth--;
        lastOpen = null;
        continue;
      }
      flush();
      depth--;
      out.push("  ".repeat(Math.max(depth, 0)) + t);
    } else if (VOID.has(name) || t.endsWith("/>")) {
      flush();
      out.push("  ".repeat(depth) + t);
    } else if (name === "script" || name === "style") {
      flush();
      out.push("  ".repeat(depth) + t);
      depth++;
      raw = name;
    } else {
      flush();
      out.push("  ".repeat(depth) + t);
      lastOpen = { name, idx: out.length - 1 };
      depth++;
    }
  }
  flush();
  return out.join("\n") + "\n";
}

export function prettyCSS(css) {
  if (css.split("\n").length > 6 + css.length / 300) return css;
  let out = "";
  let depth = 0;
  let paren = 0;
  const n = css.length;
  const indent = () => "  ".repeat(Math.max(depth, 0));
  for (let i = 0; i < n; i++) {
    const c = css[i];
    if (c === '"' || c === "'") {
      let j = i + 1;
      while (j < n && css[j] !== c) j += css[j] === "\\" ? 2 : 1;
      out += css.slice(i, j + 1);
      i = j;
    } else if (c === "/" && css[i + 1] === "*") {
      let j = css.indexOf("*/", i + 2);
      j = j < 0 ? n : j + 2;
      out += css.slice(i, j) + "\n" + indent();
      i = j - 1;
    } else if (c === "(") {
      paren++;
      out += c;
    } else if (c === ")") {
      paren--;
      out += c;
    } else if (c === "{" && paren <= 0) {
      out = out.trimEnd() + " {\n";
      depth++;
      out += indent();
    } else if (c === ";" && paren <= 0) {
      out += ";\n" + indent();
    } else if (c === "}" && paren <= 0) {
      depth--;
      out = out.trimEnd() + "\n" + indent() + "}\n" + indent();
    } else if (/\s/.test(c) && /\s$/.test(out)) {
      continue;
    } else out += c;
  }
  return out.replace(/[ \t]+\n/g, "\n").replace(/\n{3,}/g, "\n\n").trim() + "\n";
}

export function pretty(text, lang) {
  if (lang === "html") return prettyHTML(text);
  if (lang === "css") return prettyCSS(text);
  return text;
}
