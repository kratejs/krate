// Downloads started by the page (a link with the download attribute, a blob
// URL, a navigation to a file response). Needs the "downloads" permission.
// By default each download asks where to save with the native save dialog;
// configure({dir}) saves silently into a folder under a free file name.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

export type DownloadState = "started" | "progress" | "completed" | "cancelled" | "failed";

export interface DownloadInfo {
  id: number;
  /** The view that started it. */
  view: string;
  url: string;
  /** The file being written; the final path once completed. Empty before the destination is chosen. */
  path: string;
  state: DownloadState;
  received: number;
  /** Size in bytes, -1 when unknown. */
  total: number;
  /** Set when state is "failed". */
  error: string;
}

export type DownloadEvent = DownloadInfo;

export interface DownloadOptions {
  /** Absolute folder to save to. Empty means every download asks. */
  dir?: string;
  /** With `dir`: ask with the save dialog, starting in `dir`, instead of saving silently. */
  ask?: boolean;
}

/** configure sets where downloads are saved. */
export function configure(options: DownloadOptions): Promise<void> {
  return call("krate/downloads.configure", [{ dir: options.dir ?? "", ask: options.ask ?? false }]);
}

/** list resolves to the running downloads and the most recent finished ones. */
export function list(): Promise<DownloadInfo[]> {
  return call("krate/downloads.list");
}

/** cancel stops a running download. */
export function cancel(id: number): Promise<void> {
  return call("krate/downloads.cancel", [id]);
}

/** onEvent calls fn for every download event until the returned function is called. */
export function onEvent(fn: (e: DownloadEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<DownloadEvent>("krate/downloads.events", fn, opts);
}

/** onDone calls fn once for each download that completes, with its final path. */
export function onDone(fn: (e: DownloadInfo) => void, opts?: SubscribeOptions): () => void {
  return onEvent((e) => {
    if (e.state === "completed") fn(e);
  }, opts);
}
