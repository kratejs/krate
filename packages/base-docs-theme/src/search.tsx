import { createSignal, onMount, onCleanup } from "@krate/runtime";

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

export function DocsSearch(props: DocsSearchProps) {
  const placeholder = props.placeholder || "Search";
  const limit = props.maxResults || 8;
  const label = props.label || "Search documentation";

  const [results, setResults] = createSignal<Hit[]>([]);
  const [open, setOpen] = createSignal(false);
  const [loading, setLoading] = createSignal(false);
  const [active, setActive] = createSignal(-1);

  let rootRef: HTMLElement | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;

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

  function choose(index: number) {
    const hit = results()[index];
    if (!hit) return;
    const href = hit.href || hit.path || "/";
    setOpen(false);
    location.assign(href);
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      setOpen(false);
      return;
    }
    const list = results();
    if (!open() || list.length === 0) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((active() + 1) % list.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((active() - 1 + list.length) % list.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      choose(active() < 0 ? 0 : active());
    }
  }

  onMount(function () {
    function onDocClick(e: MouseEvent) {
      if (rootRef && !rootRef.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener("click", onDocClick);
    onCleanup(function () {
      document.removeEventListener("click", onDocClick);
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
        placeholder={placeholder}
        aria-label={label}
        autocomplete="off"
        spellcheck="false"
        onInput={(e: any) => onInput(e.target.value)}
        onKeyDown={onKey}
        onFocus={() => {
          if (results().length > 0) setOpen(true);
        }}
      />
      {open() && (
        <div class="krate-docs-search-results" role="listbox">
          {loading() && results().length === 0 && (
            <div class="krate-docs-search-empty">Searching…</div>
          )}
          {!loading() && results().length === 0 && (
            <div class="krate-docs-search-empty">No results</div>
          )}
          {results().map((hit, i) => (
            <a
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
