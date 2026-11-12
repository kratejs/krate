// Application lifecycle: quit, relaunch, capability checks and second-instance
// events. Needs the "app" permission.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

export interface SecondInstance {
  args: string[];
  dir: string;
  // Arguments that look like deep links (scheme://...).
  urls: string[];
}

export function quit(): Promise<void> {
  return call("krate/app.quit");
}

export function relaunch(): Promise<void> {
  return call("krate/app.relaunch");
}

// supports reports whether a capability works right now: "dialog",
// "clipboard", "menu", "tray", "notify", "shortcut", "media", "window",
// "window:alwaysOnTop", "window:fullscreen", "window:frameless",
// "window:position", "window:release", "badge", "screen", "power", "login",
// "theme", "crash".
export function supports(capability: string): Promise<boolean> {
  return call("krate/app.supports", [capability], { dedupe: true });
}

export function onSecondInstance(fn: (e: SecondInstance) => void, opts?: SubscribeOptions): () => void {
  return subscribe<SecondInstance>("krate/app.events", fn, opts);
}

export function onOpenURL(fn: (url: string) => void, opts?: SubscribeOptions): () => void {
  return onSecondInstance((e) => {
    for (const u of e.urls) fn(u);
  }, opts);
}

// Files the app was asked to open (a double click on an associated file, a
// file on the command line, "open -a", a drop on the Dock icon).
export interface OpenFileEvent {
  // Absolute paths of existing files.
  paths: string[];
  // Position of paths[0] in the list openedFiles() returns.
  index: number;
  // Where the request came from: the first launch's command line, a later
  // launch handed over by the single-instance lock, or the operating system.
  source: "launch" | "second-instance" | "system";
}

// Every file the app was asked to open since it started, oldest first: the
// ones on its command line, then later ones. The app may read them through
// @krate/app/fs when it has the fs permission: a file the user opens this way
// is granted like one chosen in a dialog.
export function openedFiles(): Promise<string[]> {
  return call("krate/app.openedFiles");
}

export interface OpenFileOptions extends SubscribeOptions {
  // Also call fn for the files already opened, such as the one the app was
  // launched with. Default true.
  replay?: boolean;
}

// Calls fn with the path of each file the app is asked to open, once each.
// By default the files opened before this call come first, so a document that
// launched the app is not missed. Returns a function that stops listening.
export function onOpenFile(fn: (path: string) => void, opts: OpenFileOptions = {}): () => void {
  const replay = opts.replay !== false;
  let stopped = false;
  let ready = false;
  let delivered = 0;
  const queue: OpenFileEvent[] = [];
  const give = (p: string) => {
    try {
      fn(p);
    } catch (e) {
      console.error(e);
    }
  };
  const handle = (e: OpenFileEvent) => {
    e.paths.forEach((p, i) => {
      if (e.index + i >= delivered) {
        delivered = e.index + i + 1;
        give(p);
      }
    });
  };
  const stop = subscribe<OpenFileEvent>(
    "krate/app.openFileEvents",
    (e) => {
      if (ready) handle(e);
      else queue.push(e);
    },
    opts,
  );
  call<string[]>("krate/app.openedFiles")
    .then((list) => {
      if (stopped) return;
      if (replay) list.forEach(give);
      delivered = list.length;
      ready = true;
      for (const e of queue.splice(0)) handle(e);
    })
    .catch((e) => {
      stop();
      opts.onError?.(e);
    });
  return () => {
    stopped = true;
    stop();
  };
}
