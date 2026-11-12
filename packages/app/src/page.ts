// Web contents of a view: print, find in page, zoom and capture. Needs the
// "page" permission. A view is named by its id (a window id is its primary
// view); omit it for the view the call comes from.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

export type PageSize = "A3" | "A4" | "A5" | "Letter" | "Legal" | "Tabloid" | { width: number; height: number };

export interface Margins {
  top?: number;
  right?: number;
  bottom?: number;
  left?: number;
}

export interface PdfOptions {
  /** The view to print; default is the calling view. */
  view?: string;
  landscape?: boolean;
  /** Print CSS backgrounds. Default false. */
  printBackground?: boolean;
  /** A named paper size or a size in inches. Default "A4". */
  pageSize?: PageSize;
  /** Margins in inches. Default 0.4 on every side. */
  margins?: Margins;
}

export interface FindOptions {
  /** The view to search; default is the calling view. */
  view?: string;
  /** Search towards the start of the page. */
  backward?: boolean;
  matchCase?: boolean;
  /** Stop at the end of the page instead of wrapping around. */
  noWrap?: boolean;
  /** Move to the next (or, with backward, previous) match of the running search for the same text. */
  continue?: boolean;
}

export interface FindEvent {
  /** The view that was searched. */
  view: string;
  text: string;
  /** Number of matches. */
  matches: number;
  /** 1-based index of the highlighted match; 0 when unknown or without matches. */
  active: number;
  /** True when `matches` will not change for this search. */
  final: boolean;
}

/** print opens the system print dialog for the view. */
export function print(view = ""): Promise<void> {
  return call("krate/page.print", [view]);
}

/**
 * printToPdf renders the view to a PDF and resolves to its bytes. Write them
 * with @krate/app/fs.writeFile, or hand them to Go host code.
 */
export function printToPdf(options: PdfOptions = {}): Promise<Uint8Array> {
  const { view = "", pageSize, margins, landscape, printBackground } = options;
  const o: Record<string, unknown> = { landscape: landscape ?? false, printBackground: printBackground ?? false };
  if (typeof pageSize === "string") o.pageSize = pageSize;
  else if (pageSize) {
    o.pageWidth = pageSize.width;
    o.pageHeight = pageSize.height;
  }
  if (margins) {
    o.margins = { top: margins.top ?? 0, right: margins.right ?? 0, bottom: margins.bottom ?? 0, left: margins.left ?? 0 };
  }
  return call("krate/page.printToPdf", [view, o]);
}

/**
 * find searches for text, highlights the first match and reports the match
 * count through onFind. Call it again with `continue: true` to step through
 * the matches.
 */
export function find(text: string, options: FindOptions = {}): Promise<void> {
  const { view = "", backward, matchCase, noWrap } = options;
  return call("krate/page.find", [
    view,
    text,
    { backward: backward ?? false, matchCase: matchCase ?? false, noWrap: noWrap ?? false, continue: options.continue ?? false },
  ]);
}

/** stopFind clears the highlight of the search. */
export function stopFind(view = ""): Promise<void> {
  return call("krate/page.stopFind", [view]);
}

/** onFind calls fn with the result of every find until the returned function is called. */
export function onFind(fn: (e: FindEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<FindEvent>("krate/page.findEvents", fn, opts);
}

/** zoom resolves to the zoom factor of the view; 1 is 100 percent. */
export function zoom(view = ""): Promise<number> {
  return call("krate/page.zoom", [view]);
}

/** setZoom sets the zoom factor, between 0.25 and 5. Ctrl/Cmd plus, minus and 0 also change it. */
export function setZoom(factor: number, view = ""): Promise<void> {
  return call("krate/page.setZoom", [view, factor]);
}

/** capture resolves to a PNG of the visible area of the view. */
export function capture(view = ""): Promise<Uint8Array> {
  return call("krate/page.capture", [view]);
}
