/**
 * Highlights code that changes at run time, in the browser.
 *
 * A `<Code lang="ts">{code()}</Code>` whose text is not known at build time is
 * written as plain text between slot markers, inside a
 * `<code data-krate-hl="ts">`. This module watches such elements: when the
 * text changes (a signal updates the slot) it shows the new text as is, waits
 * for a quiet moment, then swaps in highlighted markup. The tokenizer and the
 * language's tables are not part of the runtime: they are a separate chunk the
 * compiler writes only for the languages a page uses, loaded here on first use
 * (`window.__KRATE_HL` maps a language to its chunk).
 *
 * Highlighting happens in the browser only for code that is truly dynamic.
 * Text known at build time is highlighted by the compiler and ships no
 * JavaScript for it.
 */

interface HighlightChunk {
  highlight(code: string, lang: string, lines?: string): string | null;
  trimSpace(s: string): string;
}

declare global {
  interface Window {
    /** Language name to the URL of the chunk that highlights it. */
    __KRATE_HL?: Record<string, string>;
    /** Replaces dynamic import() when loading a chunk (tests). */
    __KRATE_HL_LOAD?: (url: string) => Promise<HighlightChunk>;
  }
}

/** Quiet time, in ms, before changed code is highlighted. */
const DEBOUNCE_MS = 120;

const chunks = new Map<string, Promise<HighlightChunk | null>>();

/** Loads the chunk for a language, once. Resolves to null when the page has none. */
function loadChunk(lang: string): Promise<HighlightChunk | null> {
  let p = chunks.get(lang);
  if (p === undefined) {
    const url = typeof window !== "undefined" && window.__KRATE_HL ? window.__KRATE_HL[lang] : undefined;
    const load = window.__KRATE_HL_LOAD || ((u: string) => import(/* @vite-ignore */ u) as Promise<HighlightChunk>);
    p = url ? load(url).then((m) => m, () => null) : Promise.resolve(null);
    chunks.set(lang, p);
  }
  return p;
}

/**
 * Highlights code with the chunk for its language, loading it first. Resolves
 * to the inner HTML of a highlighted block (what the compiler writes for the
 * same code), or null when the language is not available on this page or the
 * text is too large to highlight; show it as plain text then.
 */
export function krateHighlight(code: string, lang: string, lines?: string): Promise<string | null> {
  return loadChunk(lang).then((c) => (c ? c.highlight(c.trimSpace(code), lang, lines) : null));
}

const bound = new WeakSet<Element>();

/** The slot markers inside a code element, or null when it has none. */
function markersOf(code: Element): [Comment, Comment] | null {
  for (let n = code.firstChild; n; n = n.nextSibling) {
    if (n.nodeType === 8 && (n as Comment).data.startsWith("k:")) {
      const end = "/" + (n as Comment).data;
      for (let m = n.nextSibling; m; m = m.nextSibling) {
        if (m.nodeType === 8 && (m as Comment).data === end) return [n as Comment, m as Comment];
      }
    }
  }
  return null;
}

function bindCode(code: Element): void {
  if (bound.has(code)) return;
  bound.add(code);
  const lang = code.getAttribute("data-krate-hl") || "";
  if (!lang) return;

  /** The text last shown or highlighted. */
  let last: string | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let version = 0;
  let own = false;

  const region = (): { start: Node | null; end: Node | null } => {
    const m = markersOf(code);
    return m ? { start: m[0], end: m[1] } : { start: null, end: null };
  };

  const firstNode = (r: { start: Node | null }): ChildNode | null => (r.start ? r.start.nextSibling : code.firstChild);

  const textOf = (r: { start: Node | null; end: Node | null }): string => {
    let s = "";
    for (let n = firstNode(r); n && n !== r.end; n = n.nextSibling) s += n.textContent || "";
    return s;
  };

  // Server markup that is already highlighted (a request-time render) holds
  // the text as it is; remember it so unchanged code is not highlighted again.
  const init = region();
  const first = firstNode(init);
  if (first && first.nodeType === 1) last = textOf(init);

  const apply = (html: string): void => {
    const r = region();
    const tpl = document.createElement("template");
    tpl.innerHTML = html;
    own = true;
    for (let n = firstNode(r); n && n !== r.end; ) {
      const next = n.nextSibling;
      if (n.parentNode) n.parentNode.removeChild(n);
      n = next;
    }
    (r.end && r.end.parentNode ? r.end.parentNode : code).insertBefore(tpl.content, r.end);
    own = false;
    // Our own writes are not changes to the text.
    mo.takeRecords();
  };

  const schedule = (text: string, delay: number): void => {
    const mine = ++version;
    if (timer !== undefined) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = undefined;
      loadChunk(lang).then((c) => {
        if (!c || mine !== version || !code.isConnected) return;
        const html = c.highlight(c.trimSpace(text), lang);
        if (html !== null && mine === version) apply(html);
      });
    }, delay);
  };

  // Called when the slot wrote new text. The slot's text node comes first; any
  // highlighted markup after it is from the text before and is dropped.
  const sync = (): void => {
    if (own) return;
    if (!code.isConnected) {
      mo.disconnect();
      return;
    }
    const r = region();
    const node = firstNode(r);
    if (!node || node.nodeType !== 3) return;
    for (let n = node.nextSibling; n && n !== r.end; ) {
      const next = n.nextSibling;
      n.parentNode && n.parentNode.removeChild(n);
      n = next;
    }
    const text = node.textContent || "";
    if (text === last) return;
    const firstRun = last === null;
    last = text;
    schedule(text, firstRun ? 0 : DEBOUNCE_MS);
  };

  const mo = new MutationObserver(sync);
  mo.observe(code, { childList: true, characterData: true, subtree: true });
  sync();
}

let watching = false;

/** Binds code elements that are added to the page later (a conditional branch, a route change). */
function watchForNewCode(): void {
  if (watching || typeof MutationObserver === "undefined") return;
  watching = true;
  new MutationObserver((records) => {
    for (const r of records) {
      for (let i = 0; i < r.addedNodes.length; i++) {
        const n = r.addedNodes[i];
        if (n.nodeType !== 1) continue;
        const el = n as Element;
        if (el.matches("code[data-krate-hl]")) bindCode(el);
        else if (el.querySelector("code[data-krate-hl]")) krateHighlightInit(el);
      }
    }
  }).observe(document.documentElement, { childList: true, subtree: true });
}

/**
 * Starts highlighting every `<code data-krate-hl>` under root, and the ones
 * added to the page afterwards. The compiler calls this at the end of a page's
 * script, for pages that have such code.
 */
export function krateHighlightInit(root?: ParentNode): void {
  const scope: ParentNode = root || document;
  const codes = scope.querySelectorAll("code[data-krate-hl]");
  for (let i = 0; i < codes.length; i++) bindCode(codes[i]);
  if (!root) watchForNewCode();
}
