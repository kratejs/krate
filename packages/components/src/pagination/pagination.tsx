import './pagination.css';
import { createSignal, onMount, onCleanup } from '@krate/runtime';

export interface PaginationProps {
  /** Initial/fallback page (also defaults to 1). */
  page: number;
  total: number;
  /** Maximum number of page links shown around the current page (default 5). */
  window?: number;
  /**
   * Prefix for page links, e.g. `/blog/page/`. Without it, links keep the other
   * query parameters of the current URL and set `param` to the page number.
   */
  hrefPrefix?: string;
  /** Query-string parameter that holds the page number (default `page`). Links use it too. */
  param?: string;
  class?: string;
  id?: string;
  className?: string;
}

// Pagination highlights the page from the URL query string (`?page=N`),
// updating on load, browser back/forward, and SPA navigation. The `page` prop
// is the initial/fallback value.
export function Pagination(props: PaginationProps) {
  var total = props.total || 1;
  var size = props.window || 5;
  var param = props.param || "page";
  var className = "krate-pagination";
  if (props.class) className += " " + props.class;
  if (props.className) className += " " + props.className;

  var [current, setCurrent] = createSignal(props.page || 1);
  // The other query parameters of the current URL, kept on every link.
  var [others, setOthers] = createSignal("");

  function otherParams(): string {
    if (typeof window === "undefined" || !window.location) return "";
    var kept: string[] = [];
    var parts = window.location.search.replace(/^\?/, "").split("&");
    for (var i = 0; i < parts.length; i++) {
      if (parts[i] === "") continue;
      var name = parts[i].split("=")[0];
      if (name !== param) kept.push(parts[i]);
    }
    return kept.join("&");
  }

  // The href of page `n`; `rest` is the preserved query (passed in so the link
  // is recomputed when it changes).
  function hrefFor(n: number, rest: string): string {
    if (props.hrefPrefix) return props.hrefPrefix + n;
    return "?" + (rest ? rest + "&" : "") + param + "=" + n;
  }

  function readFromURL(): number {
    if (typeof window === "undefined" || !window.location) {
      return clampPage(props.page || 1, total);
    }
    var match = new RegExp("[?&]" + param + "=(\\d+)").exec(window.location.search);
    if (!match) {
      return clampPage(props.page || 1, total);
    }
    return clampPage(parseInt(match[1], 10), total);
  }

  onMount(function () {
    setCurrent(readFromURL());
    setOthers(otherParams());
    function sync() {
      setCurrent(readFromURL());
      setOthers(otherParams());
    }
    window.addEventListener("popstate", sync);
    window.addEventListener("krate:navigate", sync);
    onCleanup(function () {
      window.removeEventListener("popstate", sync);
      window.removeEventListener("krate:navigate", sync);
    });
  });

  function clampPage(value: number, max: number): number {
    if (!isFinite(value) || value < 1) return 1;
    if (value > max) return max;
    return value;
  }

  function pages(): number[] {
    var page = current();
    var half = (size - 1) / 2;
    var start = page - half;
    if (start < 1) start = 1;
    var end = start + size - 1;
    if (end > total) end = total;
    var first = end - size + 1;
    if (first > start) start = first;
    if (start < 1) start = 1;
    var out = [];
    for (var i = start; i <= end; i++) {
      out.push(i);
    }
    return out;
  }

  function prevPage(): number {
    return current() > 1 ? current() - 1 : 1;
  }

  function nextPage(): number {
    return current() < total ? current() + 1 : total;
  }

  return (
    <nav class={className} id={props.id} aria-label="Pagination">
      <a
        class={"krate-pagination-link" + (current() <= 1 ? " krate-pagination-disabled" : "")}
        href={hrefFor(prevPage(), others())}
        aria-disabled={current() <= 1 ? "true" : "false"}
      >
        Previous
      </a>
      <ul class="krate-pagination-list">
        {pages().map((p) => (
          <li key={p}>
            <a
              class={"krate-pagination-page" + (p === current() ? " krate-pagination-active" : "")}
              href={hrefFor(p, others())}
              aria-current={p === current() ? "page" : "false"}
            >
              {p}
            </a>
          </li>
        ))}
      </ul>
      <a
        class={"krate-pagination-link" + (current() >= total ? " krate-pagination-disabled" : "")}
        href={hrefFor(nextPage(), others())}
        aria-disabled={current() >= total ? "true" : "false"}
      >
        Next
      </a>
    </nav>
  );
}

// paginationRange returns up to `window` page numbers centered on `page`. Kept
// for callers that want the numbers themselves (e.g. building custom markup).
export function paginationRange(page: number, total: number, window?: number): number[] {
  var size = window || 5;
  var half = (size - 1) / 2;
  var start = page - half;
  if (start < 1) start = 1;
  var end = start + size - 1;
  if (end > total) end = total;
  var first = end - size + 1;
  if (first > start) start = first;
  if (start < 1) start = 1;
  var out: number[] = [];
  for (var i = start; i <= end; i++) {
    out.push(i);
  }
  return out;
}
