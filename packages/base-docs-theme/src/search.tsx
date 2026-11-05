import { createSignal, onMount, onCleanup, useRef } from "@krate/runtime";
import { PLATFORM_PREPAINT } from "./platform";

/**
 * Headless docs search - the theme places this wherever it wants and styles it
 * entirely with its own CSS. The plugin only provides the index + the
 * `window.__krateSearch` API ({ search(query, { limit }) => Promise<Hit[]> },
 * with Hit = { href, title, body, category }). No markup is injected by the
 * plugin and this component ships NO default styles: every class below
 * (`krate-docs-search*`) is yours to style.
 */
export interface DocsSearchProps {
  placeholder?: string;
  maxResults?: number;
  label?: string;
  /**
   * Keyboard shortcut that focuses the input (e.g. "mod+k", "/"). Defaults to
   * "mod+k" (Cmd+K on macOS, Ctrl+K elsewhere); pass "off" (or "none" / "")
   * to disable it.
   */
  hotkey?: string;
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

function searchAPI(): Promise<any> {
  const w = window as any;
  if (w.__krateSearch) return Promise.resolve(w.__krateSearch);
  return new Promise((resolve) => {
    const done = () => resolve((window as any).__krateSearch);
    w.addEventListener("krate:search-ready", done, { once: true });
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
    if (p === "mod") return platform === "mac" ? "\u2318" : "Ctrl";
    if (p === "cmd" || p === "meta") return platform === "mac" ? "\u2318" : "Meta";
    if (p === "ctrl" || p === "control") return "Ctrl";
    if (p === "alt" || p === "option") return platform === "mac" ? "\u2325" : "Alt";
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

export function DocsSearch(props: DocsSearchProps) {
  const placeholder = props.placeholder || "Search";
  const limit = props.maxResults || 8;
  const label = props.label || "Search documentation";
  const hotkey = resolveSearchHotkey(props.hotkey);

  const [results, setResults] = createSignal<Hit[]>([]);
  const [open, setOpen] = createSignal(false);
  const [loading, setLoading] = createSignal(false);
  const [active, setActive] = createSignal(-1);

  const listId = "krate-docs-search-results";
  const statusId = "krate-docs-search-status";
  const optionId = (index: number) => "krate-docs-search-option-" + index;

  let rootRef: HTMLElement | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let restoreFocus: HTMLElement | null = null;
  const inputRef = useRef<HTMLInputElement | null>(null);

  function run(q: string) {
    if (!q.trim()) {
      setResults([]);
      setOpen(false);
      return;
    }
    setLoading(true);
    searchAPI()
      .then((api) => api.search(q, { limit }))
      .then((hits: Hit[]) => {
        setResults(hits || []);
        setOpen(true);
        setActive(-1);
        setLoading(false);
      })
      .catch(() => {
        setResults([]);
        setLoading(false);
      });
  }

  function onInput(value: string) {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => run(value), 120);
  }

  function close(restore: boolean) {
    setOpen(false);
    setActive(-1);
    if (restore && restoreFocus && inputRef.current === document.activeElement) {
      restoreFocus.focus();
      restoreFocus = null;
    }
  }

  function choose(index: number) {
    const hit = results()[index];
    if (!hit) return;
    const href = hit.href || hit.path || "/";
    setOpen(false);
    location.assign(href);
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      close(true);
      return;
    }
    const list = results();
    if (!open() || list.length === 0) return;
    if (e.key === "Tab" && rootRef) {
      // Keep keyboard focus inside the open search popover.
      const focusables = Array.from(
        rootRef.querySelectorAll<HTMLElement>('input, a[href]'),
      );
      if (focusables.length > 1) {
        const current = focusables.indexOf(document.activeElement as HTMLElement);
        if (e.shiftKey && current <= 0) {
          e.preventDefault();
          focusables[focusables.length - 1].focus();
        } else if (!e.shiftKey && current === focusables.length - 1) {
          e.preventDefault();
          focusables[0].focus();
        }
      }
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((active() + 1) % list.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((active() - 1 + list.length) % list.length);
    } else if (e.key === "Home") {
      e.preventDefault();
      setActive(0);
    } else if (e.key === "End") {
      e.preventDefault();
      setActive(list.length - 1);
    } else if (e.key === "Enter") {
      e.preventDefault();
      choose(active() < 0 ? 0 : active());
    }
  }

  onMount(function () {
    function onDocClick(e: MouseEvent) {
      if (rootRef && !rootRef.contains(e.target as Node)) close(false);
    }
    document.addEventListener("click", onDocClick);

    const combo = parseHotkey(hotkey);
    function onHotkey(e: KeyboardEvent) {
      if (!combo) return;
      if (e.key.toLowerCase() !== combo.key) return;
      if (combo.meta && !e.metaKey) return;
      if (combo.ctrl && !e.ctrlKey) return;
      if (combo.alt && !e.altKey) return;
      // Don't steal the key while typing in another field.
      const target = e.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT" ||
          target.isContentEditable)
      ) {
        return;
      }
      e.preventDefault();
      const input = inputRef.current;
      if (input) {
        restoreFocus = document.activeElement as HTMLElement | null;
        input.focus();
        input.select();
      }
    }
    if (combo) document.addEventListener("keydown", onHotkey);

    onCleanup(function () {
      document.removeEventListener("click", onDocClick);
      if (combo) document.removeEventListener("keydown", onHotkey);
    });
  });

  return (
    <div class="krate-docs-search" ref={rootRef}>
      <span class="krate-docs-search-icon" aria-hidden="true">
        <Icon name="tabler:search" width="15" height="15" />
      </span>
      <input
        class="krate-docs-search-input"
        type="search"
        ref={inputRef}
        placeholder={placeholder}
        aria-label={label}
        role="combobox"
        aria-haspopup="listbox"
        aria-autocomplete="list"
        aria-expanded={open() ? "true" : "false"}
        aria-controls={listId}
        aria-describedby={statusId}
        aria-activedescendant={open() && active() >= 0 ? optionId(active()) : undefined}
        autocomplete="off"
        spellcheck="false"
        onInput={(e: any) => onInput(e.target.value)}
        onKeyDown={onKey}
        onFocus={() => {
          if (results().length > 0) setOpen(true);
        }}
        onBlur={() => {
          // Keep the list open while focus moves into a result with the mouse.
          setTimeout(() => {
            if (rootRef && !rootRef.contains(document.activeElement)) close(false);
          }, 0);
        }}
      />
      {hotkey && (
        <kbd class="krate-docs-search-kbd" aria-hidden="true">
          <span class="krate-kbd-mac">{hotkeyLabel(hotkey, "mac")}</span>
          <span class="krate-kbd-other">{hotkeyLabel(hotkey, "other")}</span>
        </kbd>
      )}
      {hotkey && (
        <Head>
          <script>{"(function(){" + PLATFORM_PREPAINT + "})();"}</script>
        </Head>
      )}
      <div class="krate-docs-search-status" id={statusId} role="status" aria-live="polite">
        {loading()
          ? "Searching"
          : open()
            ? results().length === 0
              ? "No results"
              : results().length + (results().length === 1 ? " result" : " results")
            : ""}
      </div>
      {open() && (
        <div class="krate-docs-search-results" id={listId} role="listbox" aria-label={label}>
          {loading() && results().length === 0 && (
            <div class="krate-docs-search-empty">Searching...</div>
          )}
          {!loading() && results().length === 0 && (
            <div class="krate-docs-search-empty">No results</div>
          )}
          {results().map((hit, i) => (
            <a
              key={hit.href || hit.path || i}
              id={optionId(i)}
              class={`krate-docs-search-item${i === active() ? " active" : ""}`}
              href={hit.href || hit.path || "/"}
              role="option"
              aria-selected={i === active() ? "true" : "false"}
            >
              <span class="krate-docs-search-title">{hit.title || "Untitled"}</span>
              {(hit.body || hit.content) && (
                <span class="krate-docs-search-snippet">
                  {(hit.body || hit.content || "").slice(0, 150)}
                </span>
              )}
            </a>
          ))}
        </div>
      )}
    </div>
  );
}

/** Alias matching the documented `DocsSearchComponent` name. */
export const DocsSearchComponent = DocsSearch;
