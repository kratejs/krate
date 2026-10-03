import './pagination.css';
import { createSignal, onMount, onCleanup } from '@krate/runtime';

export interface PaginationProps {
  /** Initial/fallback page (also defaults to 1). */
  page: number;
  total: number;
  /** Maximum number of page links shown around the current page (default 5). */
  window?: number;
  /** Prefix for page links (default `?page=`). */
  hrefPrefix?: string;
  /** Query-string parameter that holds the page number (default `page`). */
  param?: string;
  class?: string;
}

// Pagination highlights the page from the URL query string (`?page=N`),
// updating on load, browser back/forward, and SPA navigation. The `page` prop
// is the initial/fallback value.
export function Pagination(props: PaginationProps) {
  var total = props.total || 1;
  var size = props.window || 5;
  var prefix = props.hrefPrefix || "?page=";
  var param = props.param || "page";
  var className = "krate-pagination";
  if (props.class) className += " " + props.class;

  var [current, setCurrent] = createSignal(props.page || 1);

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
    function sync() {
      setCurrent(readFromURL());
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
    <nav class={className} aria-label="Pagination">
      <a
        class={"krate-pagination-link" + (current() <= 1 ? " krate-pagination-disabled" : "")}
        href={prefix + prevPage()}
        aria-disabled={current() <= 1 ? "true" : "false"}
      >
        Previous
      </a>
      <ul class="krate-pagination-list">
        {pages().map((p) => (
          <li>
            <a
              class={"krate-pagination-page" + (p === current() ? " krate-pagination-active" : "")}
              href={prefix + p}
              aria-current={p === current() ? "page" : "false"}
            >
              {p}
            </a>
          </li>
        ))}
      </ul>
      <a
        class={"krate-pagination-link" + (current() >= total ? " krate-pagination-disabled" : "")}
        href={prefix + nextPage()}
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
