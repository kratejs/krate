import { createSignal, onMount, onCleanup, useRef } from "@krate/runtime";
import { PLATFORM_PREPAINT } from "./platform";
import "./search.css";

/**
 * Docs search: a button that opens a modal search dialog. The plugin only
 * provides the index and the `window.__krateSearch` API
 * ({ search(query, { limit }) => Promise<Hit[]> }, with
 * Hit = { href, title, body, category }); this component renders the UI and
 * styles it with the `krate-docs-search*` classes in search.css.
 *
 * It works on any page of the site, not only docs pages: when the page does
 * not advertise the search module, the component reads its URL from the docs
 * index page (`docsUrl`).
 */
export interface DocsSearchProps {
  /** Text on the button, e.g. "Search docs". */
  placeholder?: string;
  maxResults?: number;
  label?: string;
  /**
   * Keyboard shortcut that opens the dialog (e.g. "mod+k", "/"). Defaults to
   * "mod+k" (Cmd+K on macOS, Ctrl+K elsewhere); pass "off" (or "none" / "")
   * to disable it.
   */
  hotkey?: string;
  /**
   * A docs page that advertises the search module, read when the current
   * page does not (a marketing home page, a blog). Default "/docs/".
   */
  docsUrl?: string;
}

/** The effective hotkey: "mod+k" unless one is configured or turned off. */
export function resolveSearchHotkey(hotkey: string | undefined): string | undefined {
  if (hotkey === undefined) return "mod+k";
  const h = hotkey.trim().toLowerCase();
  return h === "" || h === "off" || h === "none" || h === "false" ? undefined : hotkey;
}

interface Hit {
  href?: string;
  path?: string;
  title?: string;
  body?: string;
  content?: string;
  category?: string;
}

/** A result as the dialog shows it. */
interface Row {
  href: string;
  title: string;
  crumb: string;
  snippet: string;
}

const RECENT_KEY = "krate-docs-search-recent";
const RECENT_MAX = 5;

function injectSearchScript(src: string): void {
  const w = window as any;
  const el = document.createElement("script");
  el.src = src;
  el.async = true;
  el.onerror = () => {
    // Allow a retry on the next search intent.
    w.__krateSearchLoading = false;
  };
  document.head.appendChild(el);
}

/** The search module URL a page's markup advertises, if any. */
function searchSrcIn(root: Document): string | null {
  const meta = root.querySelector('meta[name="krate-search-src"]');
  const content = meta && meta.getAttribute("content");
  if (content) return content;
  const script = root.querySelector('script[src*="/search/search."]');
  return script ? script.getAttribute("src") : null;
}

/**
 * Load the search module. The docs plugin advertises it in
 * `<meta name="krate-search-src">` on docs pages; elsewhere the URL is read
 * from the docs index page. A no-op when the module is loaded or loading.
 */
function loadSearchModule(docsUrl: string): void {
  const w = window as any;
  if (w.__krateSearch || w.__krateSearchLoading) return;
  w.__krateSearchLoading = true;
  const own = document.querySelector('meta[name="krate-search-src"]');
  const ownSrc = own && own.getAttribute("content");
  if (ownSrc) {
    injectSearchScript(ownSrc);
    return;
  }
  if (document.querySelector('script[src*="/search/search."]')) {
    // Loaded eagerly by the page; it announces itself with krate:search-ready.
    return;
  }
  fetch(docsUrl, { credentials: "same-origin" })
    .then((r) => (r.ok ? r.text() : ""))
    .then((html) => {
      const src = html ? searchSrcIn(new DOMParser().parseFromString(html, "text/html")) : null;
      if (src) injectSearchScript(src);
      else w.__krateSearchLoading = false;
    })
    .catch(() => {
      w.__krateSearchLoading = false;
    });
}

function searchAPI(docsUrl: string): Promise<any> {
  const w = window as any;
  if (w.__krateSearch) return Promise.resolve(w.__krateSearch);
  return new Promise((resolve) => {
    const done = () => resolve((window as any).__krateSearch);
    w.addEventListener("krate:search-ready", done, { once: true });
    loadSearchModule(docsUrl);
    // In case the module loaded between the check and the listener.
    if ((window as any).__krateSearch) {
      w.removeEventListener("krate:search-ready", done);
      resolve((window as any).__krateSearch);
    }
  });
}

interface HotkeyCombo {
  key: string;
  meta: boolean;
  ctrl: boolean;
  alt: boolean;
}

/**
 * The key names of a hotkey such as "mod+k" for one platform: "mod" is the
 * command key on Apple devices and Ctrl elsewhere. (Defined here, not imported:
 * the build evaluates same-module functions while it renders the page.)
 */
export function hotkeyKeys(hotkey: string, platform: "mac" | "other"): string[] {
  return hotkey.split("+").map((part) => {
    const p = part.trim().toLowerCase();
    if (p === "mod") return platform === "mac" ? "⌘" : "Ctrl";
    if (p === "cmd" || p === "meta") return platform === "mac" ? "⌘" : "Meta";
    if (p === "ctrl" || p === "control") return "Ctrl";
    if (p === "alt" || p === "option") return platform === "mac" ? "⌥" : "Alt";
    return p.toUpperCase();
  });
}

/** The hotkey as shown for one platform, e.g. "mod+k" -> "Ctrl K" or the command key and "K". */
export function hotkeyLabel(hotkey: string, platform: "mac" | "other"): string {
  return hotkeyKeys(hotkey, platform).join(" ");
}

/** Parse "mod+k", "ctrl+/", "alt+p", etc. "mod" is Cmd on macOS, Ctrl elsewhere. */
function parseHotkey(hotkey: string | undefined): HotkeyCombo | null {
  if (!hotkey) return null;
  const parts = hotkey.toLowerCase().split("+").map((p) => p.trim());
  let key = "";
  let meta = false;
  let ctrl = false;
  let alt = false;
  for (const part of parts) {
    switch (part) {
      case "mod": {
        const isMac =
          typeof navigator !== "undefined" &&
          /mac|iphone|ipad|ipod/i.test(navigator.platform || navigator.userAgent || "");
        if (isMac) meta = true;
        else ctrl = true;
        break;
      }
      case "cmd":
      case "meta":
        meta = true;
        break;
      case "ctrl":
      case "control":
        ctrl = true;
        break;
      case "alt":
      case "option":
        alt = true;
        break;
      case "shift":
        // Shift is not required (layout-friendly); allow it either way.
        break;
      default:
        if (part) key = part;
    }
  }
  return key ? { key, meta, ctrl, alt } : null;
}

/** Undo the HTML entities some engines leave in plain-text excerpts. */
export function decodeEntities(text: string): string {
  return text
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#0?39;|&apos;/g, "'")
    .replace(/&nbsp;/g, " ")
    .replace(/&amp;/g, "&");
}

/** Query words worth highlighting (two characters or more). */
export function queryTerms(query: string): string[] {
  return query
    .toLowerCase()
    .split(/\s+/)
    .filter((w) => w.length >= 2);
}

/**
 * About `size` characters of `body` around the first query term, cut at word
 * boundaries, with an ellipsis where text was dropped.
 */
export function snippetAround(body: string, terms: string[], size: number): string {
  const text = body.replace(/\s+/g, " ").trim();
  if (text.length <= size) return text;
  const lower = text.toLowerCase();
  let at = -1;
  for (const t of terms) {
    const i = lower.indexOf(t);
    if (i !== -1 && (at === -1 || i < at)) at = i;
  }
  let start = at > 40 ? at - 40 : 0;
  if (start > 0) {
    const space = text.indexOf(" ", start);
    if (space !== -1 && space < at) start = space + 1;
  }
  let end = Math.min(text.length, start + size);
  if (end < text.length) {
    const space = text.lastIndexOf(" ", end);
    if (space > start + size / 2) end = space;
  }
  return (start > 0 ? "…" : "") + text.slice(start, end) + (end < text.length ? "…" : "");
}

/** `text` split into runs that do and do not match a query term. */
export function highlightParts(text: string, terms: string[]): { text: string; hit: boolean }[] {
  if (terms.length === 0) return [{ text, hit: false }];
  const escaped = terms.map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
  const re = new RegExp("(" + escaped.join("|") + ")", "gi");
  const out: { text: string; hit: boolean }[] = [];
  let last = 0;
  text.replace(re, (m: string, _g: string, offset: number) => {
    if (offset > last) out.push({ text: text.slice(last, offset), hit: false });
    out.push({ text: m, hit: true });
    last = offset + m.length;
    return m;
  });
  if (last < text.length) out.push({ text: text.slice(last), hit: false });
  return out;
}

/** "Core concepts" for "/docs/core-concepts/routing/": the page's section. */
export function crumbFor(href: string, category: string | undefined): string {
  if (category) return category;
  const parts = href.split(/[?#]/)[0].split("/").filter(Boolean);
  // Drop the docs mount ("docs") and the page itself.
  const sections = parts.slice(1, -1);
  return sections
    .map((s) => {
      const words = s.replace(/[-_]+/g, " ");
      return words.charAt(0).toUpperCase() + words.slice(1);
    })
    .join(" / ");
}

function readRecent(): Row[] {
  try {
    const raw = window.localStorage.getItem(RECENT_KEY);
    const list = raw ? JSON.parse(raw) : [];
    return Array.isArray(list) ? list.filter((r) => r && typeof r.href === "string").slice(0, RECENT_MAX) : [];
  } catch {
    return [];
  }
}

function writeRecent(row: Row): void {
  try {
    const next = [{ href: row.href, title: row.title, crumb: row.crumb, snippet: "" }].concat(
      readRecent().filter((r) => r.href !== row.href),
    );
    window.localStorage.setItem(RECENT_KEY, JSON.stringify(next.slice(0, RECENT_MAX)));
  } catch {
    /* storage unavailable (private mode) - recent searches are optional */
  }
}

function Highlighted(props: { text: string; terms: string[] }) {
  return (
    <>
      {highlightParts(props.text, props.terms).map((part) =>
        part.hit ? <mark>{part.text}</mark> : <span>{part.text}</span>,
      )}
    </>
  );
}

export function DocsSearch(props: DocsSearchProps) {
  const placeholder = props.placeholder || "Search docs";
  const limit = props.maxResults || 8;
  const label = props.label || "Search documentation";
  const hotkey = resolveSearchHotkey(props.hotkey);
  const docsUrl = props.docsUrl || "/docs/";

  const [rows, setRows] = createSignal<Row[]>([]);
  const [terms, setTerms] = createSignal<string[]>([]);
  const [query, setQuery] = createSignal("");
  const [isOpen, setIsOpen] = createSignal(false);
  const [closing, setClosing] = createSignal(false);
  const [loading, setLoading] = createSignal(false);
  const [active, setActive] = createSignal(0);

  const listId = "krate-docs-search-results";
  const statusId = "krate-docs-search-status";
  const optionId = (index: number) => "krate-docs-search-option-" + index;

  const dialogRef = useRef<HTMLDialogElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  let timer: ReturnType<typeof setTimeout> | null = null;
  let closeTimer: ReturnType<typeof setTimeout> | null = null;
  let seq = 0;

  function showRecent() {
    setTerms([]);
    setRows(readRecent());
    setActive(0);
  }

  function run(q: string) {
    const mine = ++seq;
    if (!q.trim()) {
      setLoading(false);
      showRecent();
      return;
    }
    setLoading(true);
    searchAPI(docsUrl)
      .then((api) => api.search(q, { limit }))
      .then((hits: Hit[]) => {
        if (mine !== seq) return;
        const t = queryTerms(q);
        setTerms(t);
        setRows(
          (hits || []).map((hit) => {
            const href = hit.href || hit.path || "/";
            return {
              href,
              title: decodeEntities(hit.title || "Untitled"),
              crumb: crumbFor(href, hit.category),
              snippet: snippetAround(decodeEntities(hit.body || hit.content || ""), t, 150),
            };
          }),
        );
        setActive(0);
        setLoading(false);
      })
      .catch(() => {
        if (mine !== seq) return;
        setRows([]);
        setLoading(false);
      });
  }

  function onInput(value: string) {
    setQuery(value);
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => run(value), 90);
  }

  function warm() {
    loadSearchModule(docsUrl);
  }

  function openDialog() {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (closeTimer) {
      clearTimeout(closeTimer);
      closeTimer = null;
    }
    setClosing(false);
    if (!dialog.open) dialog.showModal();
    setIsOpen(true);
    const input = inputRef.current;
    if (query().trim() === "") showRecent();
    if (input) {
      input.focus();
      input.select();
    }
    // Start the engine (and the index download) while the user types.
    searchAPI(docsUrl).then((api) => api && api.ready && api.ready());
  }

  function closeDialog(animate: boolean) {
    const dialog = dialogRef.current;
    if (!dialog || !dialog.open) return;
    if (!animate) {
      setClosing(false);
      setIsOpen(false);
      dialog.close();
      return;
    }
    setClosing(true);
    closeTimer = setTimeout(() => {
      closeTimer = null;
      setClosing(false);
      setIsOpen(false);
      if (dialog.open) dialog.close();
    }, 140);
  }

  function remember(index: number) {
    const row = rows()[index];
    if (row) writeRecent(row);
  }

  function choose(index: number) {
    const link = document.getElementById(optionId(index));
    if (!link) return;
    // A real click, so the client router handles the navigation.
    link.click();
  }

  function scrollActiveIntoView(index: number) {
    const el = document.getElementById(optionId(index));
    if (el && el.scrollIntoView) el.scrollIntoView({ block: "nearest" });
  }

  function onKey(e: KeyboardEvent) {
    const list = rows();
    if (e.key === "ArrowDown" && list.length > 0) {
      e.preventDefault();
      const next = (active() + 1) % list.length;
      setActive(next);
      scrollActiveIntoView(next);
    } else if (e.key === "ArrowUp" && list.length > 0) {
      e.preventDefault();
      const next = (active() - 1 + list.length) % list.length;
      setActive(next);
      scrollActiveIntoView(next);
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (list.length > 0) choose(active());
    }
  }

  onMount(function () {
    const dialog = dialogRef.current;

    // Esc: animate the close instead of the browser's instant one.
    function onCancel(e: Event) {
      e.preventDefault();
      closeDialog(true);
    }
    function onClose() {
      setIsOpen(false);
      setClosing(false);
    }
    // A click on the backdrop lands on the dialog itself, outside its box.
    function onDialogClick(e: MouseEvent) {
      if (!dialog || e.target !== dialog) return;
      const r = dialog.getBoundingClientRect();
      const inside = e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom;
      if (!inside) closeDialog(true);
    }
    // Following a result: remember it and get the dialog out of the way.
    function onResultClick(e: MouseEvent) {
      const link = (e.target as HTMLElement).closest(".krate-docs-search-item") as HTMLElement | null;
      if (!link) return;
      const index = Number(link.getAttribute("data-index"));
      if (index >= 0) remember(index);
      if (!(e.metaKey || e.ctrlKey || e.shiftKey || e.button === 1)) closeDialog(false);
    }
    if (dialog) {
      dialog.addEventListener("cancel", onCancel);
      dialog.addEventListener("close", onClose);
      dialog.addEventListener("click", onDialogClick);
      dialog.addEventListener("click", onResultClick);
    }

    const combo = parseHotkey(hotkey);
    function onHotkey(e: KeyboardEvent) {
      if (!combo) return;
      if (e.key.toLowerCase() !== combo.key) return;
      if (combo.meta && !e.metaKey) return;
      if (combo.ctrl && !e.ctrlKey) return;
      if (combo.alt && !e.altKey) return;
      // A bare key ("/") must not fire while typing in another field.
      const bare = !combo.meta && !combo.ctrl && !combo.alt;
      const target = e.target as HTMLElement | null;
      if (
        bare &&
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT" ||
          target.isContentEditable)
      ) {
        return;
      }
      e.preventDefault();
      if (dialog && dialog.open && !closing()) closeDialog(true);
      else openDialog();
    }
    if (combo) document.addEventListener("keydown", onHotkey);

    // The client router swaps pages without a reload: close on navigation.
    function onNavigate() {
      closeDialog(false);
    }
    window.addEventListener("krate:navigate", onNavigate);

    onCleanup(function () {
      if (dialog) {
        dialog.removeEventListener("cancel", onCancel);
        dialog.removeEventListener("close", onClose);
        dialog.removeEventListener("click", onDialogClick);
        dialog.removeEventListener("click", onResultClick);
      }
      if (combo) document.removeEventListener("keydown", onHotkey);
      window.removeEventListener("krate:navigate", onNavigate);
      if (timer) clearTimeout(timer);
      if (closeTimer) clearTimeout(closeTimer);
    });
  });

  return (
    <div class="krate-docs-search">
      <button
        class="krate-docs-search-trigger"
        type="button"
        aria-label={label}
        aria-haspopup="dialog"
        aria-keyshortcuts={hotkey ? "Control+K Meta+K" : undefined}
        onClick={openDialog}
        onPointerEnter={warm}
        onFocus={warm}
      >
        <span class="krate-docs-search-icon" aria-hidden="true">
          <Icon name="tabler:search" width="16" height="16" />
        </span>
        <span class="krate-docs-search-placeholder">{placeholder}</span>
        {hotkey && (
          <kbd class="krate-docs-search-kbd" aria-hidden="true">
            <span class="krate-kbd-mac">{hotkeyLabel(hotkey, "mac")}</span>
            <span class="krate-kbd-other">{hotkeyLabel(hotkey, "other")}</span>
          </kbd>
        )}
      </button>
      {hotkey && (
        <Head>
          <script data-krate-once="platform-prepaint">{"(function(){" + PLATFORM_PREPAINT + "})();"}</script>
        </Head>
      )}
      <dialog
        class={`krate-docs-search-dialog${closing() ? " closing" : ""}`}
        ref={dialogRef}
        aria-label={label}
      >
        <div class="krate-docs-search-field">
          <span class="krate-docs-search-field-icon" aria-hidden="true">
            {loading() ? (
              <span class="krate-docs-search-spinner" />
            ) : (
              <Icon name="tabler:search" width="18" height="18" />
            )}
          </span>
          <input
            class="krate-docs-search-input"
            type="text"
            ref={inputRef}
            placeholder="Search the documentation"
            aria-label={label}
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={isOpen() && rows().length > 0 ? "true" : "false"}
            aria-controls={rows().length > 0 ? listId : undefined}
            aria-describedby={statusId}
            aria-activedescendant={rows().length > 0 ? optionId(active()) : undefined}
            autocomplete="off"
            spellcheck="false"
            onInput={(e: any) => onInput(e.target.value)}
            onKeyDown={onKey}
          />
          <button class="krate-docs-search-close" type="button" aria-label="Close search" onClick={() => closeDialog(true)}>
            <kbd>Esc</kbd>
            <Icon name="tabler:x" width="18" height="18" />
          </button>
        </div>
        <div class="krate-docs-search-status" id={statusId} role="status" aria-live="polite">
          {loading()
            ? "Searching"
            : query().trim() === ""
              ? ""
              : rows().length === 0
                ? "No results"
                : rows().length + (rows().length === 1 ? " result" : " results")}
        </div>
        <div class="krate-docs-search-body">
          {query().trim() === "" && rows().length > 0 && (
            <div class="krate-docs-search-group">Recent</div>
          )}
          {query().trim() === "" && rows().length === 0 && (
            <div class="krate-docs-search-empty">
              <Icon name="tabler:book-2" width="28" height="28" />
              <p>Search guides, API reference and examples.</p>
            </div>
          )}
          {query().trim() !== "" && !loading() && rows().length === 0 && (
            <div class="krate-docs-search-empty">
              <Icon name="tabler:mood-empty" width="28" height="28" />
              <p>
                No results for <strong>{query()}</strong>
              </p>
            </div>
          )}
          {rows().length > 0 && (
            <div class="krate-docs-search-results" id={listId} role="listbox" aria-label={label}>
              {rows().map((row, i) => (
                <a
                  key={row.href}
                  id={optionId(i)}
                  data-index={String(i)}
                  class={`krate-docs-search-item${i === active() ? " active" : ""}`}
                  href={row.href}
                  role="option"
                  aria-selected={i === active() ? "true" : "false"}
                  onPointerMove={() => {
                    if (active() !== i) setActive(i);
                  }}
                >
                  <span class="krate-docs-search-item-icon" aria-hidden="true">
                    {query().trim() === "" ? (
                      <Icon name="tabler:history" width="18" height="18" />
                    ) : (
                      <Icon name="tabler:file-text" width="18" height="18" />
                    )}
                  </span>
                  <span class="krate-docs-search-item-text">
                    {row.crumb && <span class="krate-docs-search-crumb">{row.crumb}</span>}
                    <span class="krate-docs-search-title">
                      <Highlighted text={row.title} terms={terms()} />
                    </span>
                    {row.snippet && (
                      <span class="krate-docs-search-snippet">
                        <Highlighted text={row.snippet} terms={terms()} />
                      </span>
                    )}
                  </span>
                  <span class="krate-docs-search-item-enter" aria-hidden="true">
                    <Icon name="tabler:corner-down-left" width="16" height="16" />
                  </span>
                </a>
              ))}
            </div>
          )}
        </div>
        <div class="krate-docs-search-footer" aria-hidden="true">
          <span>
            <kbd>
              <Icon name="tabler:arrow-up" width="12" height="12" />
            </kbd>
            <kbd>
              <Icon name="tabler:arrow-down" width="12" height="12" />
            </kbd>
            to navigate
          </span>
          <span>
            <kbd>
              <Icon name="tabler:corner-down-left" width="12" height="12" />
            </kbd>
            to open
          </span>
          <span>
            <kbd>Esc</kbd>
            to close
          </span>
        </div>
      </dialog>
    </div>
  );
}

/** Alias matching the documented `DocsSearchComponent` name. */
export const DocsSearchComponent = DocsSearch;
