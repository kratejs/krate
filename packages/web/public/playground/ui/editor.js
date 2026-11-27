// A small code editor: a transparent <textarea> over a highlighted <pre>,
// with a line-number gutter. No dependencies.

import { highlightLines, langOf } from "./highlight.js";

const LINE_H = 20;
const PAIRS = { "{": "}", "(": ")", "[": "]" };

/**
 * @param {HTMLElement} host
 * @param {{ onChange?: (value: string) => void, onRun?: () => void, onSave?: () => void }} handlers
 */
export function createEditor(host, handlers = {}) {
  host.classList.add("ed");
  host.innerHTML =
    '<div class="ed-gutter" aria-hidden="true"><div class="ed-gutter-in"></div></div>' +
    '<div class="ed-body"><pre class="ed-pre" aria-hidden="true"></pre>' +
    '<textarea class="ed-ta" spellcheck="false" autocomplete="off" autocorrect="off" autocapitalize="off" wrap="off" aria-label="Code editor"></textarea></div>' +
    '<p class="sr-only" id="ed-hint">Press Escape, then Tab, to leave the editor. Ctrl or Command plus Enter rebuilds.</p>';
  const gutterIn = host.querySelector(".ed-gutter-in");
  const pre = host.querySelector(".ed-pre");
  const ta = host.querySelector(".ed-ta");
  ta.setAttribute("aria-describedby", "ed-hint");

  let lang = "text";
  let marks = [];
  let escaped = false;
  let lineCount = 0;

  const markByLine = () => {
    const m = new Map();
    for (const x of marks) if (!m.has(x.line) || x.severity === "error") m.set(x.line, x);
    return m;
  };

  function render() {
    const text = ta.value;
    const lines = highlightLines(text, lang);
    const byLine = markByLine();
    let html = "";
    for (let i = 0; i < lines.length; i++) {
      const mk = byLine.get(i + 1);
      html += `<span class="l${mk ? " l-" + mk.severity : ""}">${lines[i]}</span>`;
    }
    pre.innerHTML = html;
    if (lines.length !== lineCount || marks.length || gutterIn.dataset.marked) {
      let g = "";
      for (let i = 0; i < lines.length; i++) {
        const mk = byLine.get(i + 1);
        g += mk ? `<div class="n n-${mk.severity}" title="${mk.message.replace(/"/g, "&quot;")}">${i + 1}</div>` : `<div class="n">${i + 1}</div>`;
      }
      gutterIn.innerHTML = g;
      gutterIn.dataset.marked = marks.length ? "1" : "";
      lineCount = lines.length;
    }
    sync();
  }

  function sync() {
    pre.style.transform = `translate(${-ta.scrollLeft}px, ${-ta.scrollTop}px)`;
    gutterIn.style.transform = `translateY(${-ta.scrollTop}px)`;
  }

  // Replaces [start, end) through the browser's edit stack so undo works.
  function edit(start, end, text, selStart, selEnd) {
    ta.focus();
    ta.setSelectionRange(start, end);
    let ok = false;
    try {
      ok = document.execCommand("insertText", false, text);
    } catch {
      ok = false;
    }
    if (!ok || ta.value.slice(start, start + text.length) !== text) {
      ta.setRangeText(text, start, end, "end");
      ta.dispatchEvent(new Event("input", { bubbles: true }));
    }
    ta.setSelectionRange(selStart ?? start + text.length, selEnd ?? selStart ?? start + text.length);
  }

  const lineStart = (pos) => ta.value.lastIndexOf("\n", pos - 1) + 1;

  function indentSelection(outdent) {
    const v = ta.value;
    const s = ta.selectionStart;
    const e = ta.selectionEnd;
    const first = lineStart(s);
    let last = e;
    if (e > s && v[e - 1] === "\n") last = e - 1;
    const endOfLast = v.indexOf("\n", last);
    const to = endOfLast < 0 ? v.length : endOfLast;
    const block = v.slice(first, to);
    const lines = block.split("\n");
    let firstDelta = 0;
    let total = 0;
    const out = lines.map((ln, i) => {
      if (!outdent) {
        if (i === 0) firstDelta = 2;
        total += 2;
        return "  " + ln;
      }
      const m = /^( {1,2}|\t)/.exec(ln);
      const d = m ? m[0].length : 0;
      if (i === 0) firstDelta = -d;
      total -= d;
      return ln.slice(d);
    });
    edit(first, to, out.join("\n"), Math.max(first, s + firstDelta), e + total);
  }

  function onKeyDown(ev) {
    const mod = ev.ctrlKey || ev.metaKey;
    if (ev.key === "Escape") {
      escaped = true;
      return;
    }
    if (ev.key === "Tab" && !mod && !ev.altKey) {
      if (escaped) {
        escaped = false;
        return;
      }
      ev.preventDefault();
      const multi = ta.value.slice(ta.selectionStart, ta.selectionEnd).includes("\n");
      if (ev.shiftKey || multi) indentSelection(ev.shiftKey);
      else edit(ta.selectionStart, ta.selectionEnd, "  ");
      return;
    }
    escaped = false;
    if (mod && ev.key === "Enter") {
      ev.preventDefault();
      handlers.onRun?.();
    } else if (mod && (ev.key === "s" || ev.key === "S")) {
      ev.preventDefault();
      handlers.onSave?.();
    } else if (ev.key === "Enter" && !mod && !ev.shiftKey && !ev.altKey && !ev.isComposing) {
      const v = ta.value;
      const s = ta.selectionStart;
      const e = ta.selectionEnd;
      const ls = lineStart(s);
      const indent = /^[ \t]*/.exec(v.slice(ls, s))[0];
      const before = v[s - 1];
      const after = v[e];
      ev.preventDefault();
      if (before && PAIRS[before] === after) {
        edit(s, e, "\n" + indent + "  " + "\n" + indent, s + 1 + indent.length + 2);
      } else if (before && PAIRS[before]) {
        edit(s, e, "\n" + indent + "  ");
      } else {
        edit(s, e, "\n" + indent);
      }
    } else if (ev.key === "/" && mod) {
      ev.preventDefault();
      toggleComment();
    }
  }

  function toggleComment() {
    const v = ta.value;
    const s = ta.selectionStart;
    const e = ta.selectionEnd;
    const first = lineStart(s);
    const endOfLast = v.indexOf("\n", e > s && v[e - 1] === "\n" ? e - 1 : e);
    const to = endOfLast < 0 ? v.length : endOfLast;
    const lines = v.slice(first, to).split("\n");
    const css = lang === "css";
    const open = css ? "/* " : lang === "html" || lang === "md" ? "<!-- " : "// ";
    const close = css ? " */" : lang === "html" || lang === "md" ? " -->" : "";
    const all = lines.every((l) => !l.trim() || l.trim().startsWith(open.trim()));
    const out = lines.map((l) => {
      if (!l.trim()) return l;
      if (all) {
        const idx = l.indexOf(open.trim());
        let r = l.slice(0, idx) + l.slice(idx + open.trim().length).replace(/^ /, "");
        if (close && r.trimEnd().endsWith(close.trim())) r = r.trimEnd().slice(0, -close.trim().length).trimEnd();
        return r;
      }
      const ws = /^\s*/.exec(l)[0];
      return ws + open + l.slice(ws.length) + close;
    });
    edit(first, to, out.join("\n"), first, first + out.join("\n").length);
  }

  ta.addEventListener("keydown", onKeyDown);
  ta.addEventListener("blur", () => {
    escaped = false;
  });
  ta.addEventListener("input", () => {
    render();
    handlers.onChange?.(ta.value);
  });
  ta.addEventListener("scroll", sync);

  return {
    textarea: ta,
    getValue: () => ta.value,
    /** Shows a file; resets the undo stack by replacing the element's value. */
    setFile(name, text) {
      lang = langOf(name);
      ta.value = text;
      ta.scrollTop = 0;
      ta.scrollLeft = 0;
      ta.setAttribute("aria-label", "Code editor: " + name);
      render();
    },
    setMarks(list) {
      marks = list;
      render();
    },
    goto(line, col) {
      const lines = ta.value.split("\n");
      line = Math.min(Math.max(line, 1), lines.length);
      let off = 0;
      for (let i = 0; i < line - 1; i++) off += lines[i].length + 1;
      off += Math.min(Math.max((col || 1) - 1, 0), lines[line - 1].length);
      ta.focus();
      ta.setSelectionRange(off, off);
      const viewH = ta.clientHeight;
      const top = (line - 1) * LINE_H;
      if (top < ta.scrollTop || top > ta.scrollTop + viewH - LINE_H * 2) ta.scrollTop = Math.max(0, top - viewH / 3);
      sync();
    },
    focus: () => ta.focus(),
  };
}
