// Test driver for Krate Native apps. Node only: it starts the krate-shell in
// dev mode and drives it over the dev control channel (packages/native
// PROTOCOL.md, "Dev control channel"). Pages are driven at the DOM level:
// clicks, typing and key presses are synthesized events, not operating-system
// input.
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";

/** Environment variable that names the krate-shell binary (set by `krate native test`). */
export const SHELL_ENV = "KRATE_TEST_SHELL";
/** Environment variable that names the built app directory (set by `krate native test`). */
export const APP_ENV = "KRATE_TEST_APP";

export interface LaunchOptions {
  /** Shell binary. Default: $KRATE_TEST_SHELL. */
  shell?: string;
  /** Built app directory. Default: $KRATE_TEST_APP. */
  appDir?: string;
  /** Extra environment for the shell and the host code it runs. */
  env?: Record<string, string>;
  /**
   * Give the app an empty data, config and cache directory (default true), so
   * its database, kv store and settings start fresh and never touch yours.
   */
  isolate?: boolean;
  /** How long to wait for the app to start and its main page to load, in milliseconds (default 60000). */
  startTimeout?: number;
  /** Default timeout of waitFor and the actions that wait, in milliseconds (default 5000). */
  timeout?: number;
}

export interface WaitOptions {
  /** Milliseconds (default: the launch timeout, 5000). */
  timeout?: number;
  /** Polling interval in milliseconds (default 50). */
  interval?: number;
}

/** One webview. A window's primary view has the window's id. */
export interface ViewInfo {
  id: string;
  window: string;
  primary: boolean;
}

export interface WindowInfo {
  id: string;
  views: ViewInfo[];
}

export interface PressOptions {
  /** Focus this element first. */
  selector?: string;
}

/** An error from a host function the app (or an SDK id) answered with. */
export class HostCallError extends Error {
  code: string;
  data: unknown;
  constructor(code: string, message: string, data: unknown) {
    super(`${code}: ${message}`);
    this.name = "HostCallError";
    this.code = code;
    this.data = data;
  }
}

// pageLib is installed once per page (globalThis.__krateDriver) and holds the
// DOM-level helpers. It is plain ES2019 source, sent over the control channel.
const pageLib = String.raw`(function(){
  var sleep = function(ms){ return new Promise(function(r){ setTimeout(r, ms); }); };
  var visible = function(el){ return !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length); };
  var mods = {control:"ctrlKey", ctrl:"ctrlKey", shift:"shiftKey", alt:"altKey", meta:"metaKey", cmd:"metaKey", command:"metaKey"};
  var focusables = 'a[href],button,input,select,textarea,[tabindex],[contenteditable=""],[contenteditable="true"]';
  function usable(el){
    return !el.disabled && visible(el) && el.getAttribute("tabindex") !== "-1" && !(el.type === "hidden");
  }
  function ev(el, type, init){
    var C = type.indexOf("pointer") === 0 && typeof PointerEvent === "function" ? PointerEvent : MouseEvent;
    el.dispatchEvent(new C(type, Object.assign({bubbles:true, cancelable:true, composed:true, view:window, button:0}, init || {})));
  }
  function setValue(el, v){
    var proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype
      : el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    var d = Object.getOwnPropertyDescriptor(proto, "value");
    if (d && d.set) d.set.call(el, v); else el.value = v;
  }
  function fire(el, name, data, inputType){
    var e;
    try { e = new InputEvent(name, {bubbles:true, cancelable:false, composed:true, data:data, inputType:inputType}); }
    catch (_) { e = new Event(name, {bubbles:true}); }
    el.dispatchEvent(e);
  }
  function textual(el){
    return el instanceof HTMLTextAreaElement || (el instanceof HTMLInputElement &&
      /^(text|search|url|tel|email|password|number|)$/.test(el.type));
  }
  var lib = {
    state: function(sel){
      var el = document.querySelector(sel);
      if (!el) return {found:false, visible:false, enabled:false};
      return {found:true, visible:visible(el), enabled:!el.disabled};
    },
    exists: function(sel){ return !!document.querySelector(sel); },
    count: function(sel){ return document.querySelectorAll(sel).length; },
    text: function(sel){ var el = document.querySelector(sel); return el ? el.textContent : null; },
    value: function(sel){ var el = document.querySelector(sel); return el ? (el.value !== undefined ? el.value : null) : null; },
    click: function(sel){
      var el = document.querySelector(sel);
      if (el.scrollIntoView) el.scrollIntoView({block:"center", inline:"center"});
      if (el.focus) el.focus({preventScroll:true});
      var r = el.getBoundingClientRect(), p = {clientX:r.left + r.width / 2, clientY:r.top + r.height / 2};
      ev(el, "pointerdown", p); ev(el, "mousedown", p); ev(el, "pointerup", p); ev(el, "mouseup", p);
      el.click();
    },
    fill: function(sel, text){
      var el = document.querySelector(sel);
      if (el.focus) el.focus({preventScroll:true});
      if (el.isContentEditable) {
        el.textContent = text; fire(el, "input", text, "insertText"); return;
      }
      if (!(el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement))
        throw new Error("fill: " + sel + " is a <" + el.localName + ">, not a text field, select or editable element");
      if (el instanceof HTMLSelectElement) {
        setValue(el, text);
        if (el.value !== text) throw new Error("fill: " + sel + " has no option with value " + JSON.stringify(text));
        fire(el, "input", null, "insertReplacementText"); el.dispatchEvent(new Event("change", {bubbles:true})); return;
      }
      if (/^(checkbox|radio|file|button|submit|reset|image)$/.test(el.type))
        throw new Error("fill: " + sel + " is an input of type " + el.type + "; use click()");
      setValue(el, text);
      fire(el, "input", text, "insertText");
      el.dispatchEvent(new Event("change", {bubbles:true}));
      if (el.setSelectionRange && textual(el) && el.type !== "number" && el.type !== "email") try { el.setSelectionRange(text.length, text.length); } catch (_) {}
    },
    press: function(combo, sel){
      if (sel) { var t = document.querySelector(sel); if (t && t.focus) t.focus({preventScroll:true}); }
      var parts = combo.split("+"), key = parts.pop();
      if (key === "" && parts.length) { key = "+"; parts.pop(); }
      var m = {ctrlKey:false, shiftKey:false, altKey:false, metaKey:false};
      parts.forEach(function(p){
        var k = mods[p.toLowerCase()];
        if (p === "ControlOrMeta") k = /Mac|iPhone|iPad/.test(navigator.platform) ? "metaKey" : "ctrlKey";
        if (!k) throw new Error("press: unknown modifier " + p + " in " + combo);
        m[k] = true;
      });
      var el = document.activeElement && document.activeElement !== document.documentElement ? document.activeElement : document.body;
      if (key === "Space") key = " ";
      var init = Object.assign({key:key, code:key.length === 1 ? (/[a-z]/i.test(key) ? "Key" + key.toUpperCase() : /\d/.test(key) ? "Digit" + key : "") : key,
        bubbles:true, cancelable:true, composed:true, view:window}, m);
      var down = new KeyboardEvent("keydown", init);
      el.dispatchEvent(down);
      if (!down.defaultPrevented) {
        var printable = key.length === 1 && !m.ctrlKey && !m.metaKey;
        if (printable || key === "Enter") el.dispatchEvent(new KeyboardEvent("keypress", init));
        if (printable && textual(el)) {
          var s = el.selectionStart == null ? el.value.length : el.selectionStart, e2 = el.selectionEnd == null ? s : el.selectionEnd;
          setValue(el, el.value.slice(0, s) + key + el.value.slice(e2));
          try { el.setSelectionRange(s + 1, s + 1); } catch (_) {}
          fire(el, "input", key, "insertText");
        } else if (printable && el.isContentEditable) {
          el.textContent += key; fire(el, "input", key, "insertText");
        } else if (key === "Backspace" && textual(el)) {
          var s2 = el.selectionStart == null ? el.value.length : el.selectionStart, e3 = el.selectionEnd == null ? s2 : el.selectionEnd;
          if (s2 === e3 && s2 > 0) s2--;
          if (s2 !== e3 || e3 > 0) {
            setValue(el, el.value.slice(0, s2) + el.value.slice(e3));
            try { el.setSelectionRange(s2, s2); } catch (_) {}
            fire(el, "input", null, "deleteContentBackward");
          }
        } else if (key === "Enter") {
          if (el instanceof HTMLTextAreaElement) {
            var s3 = el.selectionStart, e4 = el.selectionEnd;
            setValue(el, el.value.slice(0, s3) + "\n" + el.value.slice(e4));
            try { el.setSelectionRange(s3 + 1, s3 + 1); } catch (_) {}
            fire(el, "input", "\n", "insertLineBreak");
          } else if (el instanceof HTMLInputElement && el.form && textual(el)) {
            try { el.form.requestSubmit(); } catch (_) { el.form.submit(); }
          } else if (el instanceof HTMLButtonElement || el instanceof HTMLAnchorElement) {
            el.click();
          }
        } else if (key === " " && (el instanceof HTMLButtonElement || (el instanceof HTMLInputElement && /^(checkbox|radio|button|submit)$/.test(el.type)))) {
          el.click();
        } else if (key === "Tab") {
          var list = Array.prototype.filter.call(document.querySelectorAll(focusables), usable);
          var i = list.indexOf(el), n = list.length;
          if (n) list[(i + (m.shiftKey ? -1 : 1) + n) % n].focus();
        }
      }
      var up = document.activeElement && document.activeElement !== document.documentElement ? document.activeElement : el;
      up.dispatchEvent(new KeyboardEvent("keyup", init));
    },
    errors: function(){
      var out = [], seen = {};
      [].concat(globalThis.__krateDriverErrs || [], globalThis.__krateErrs || []).forEach(function(e){
        var s = String(e); if (!seen[s]) { seen[s] = 1; out.push(s); }
      });
      return out;
    },
    call: async function(fn, args){
      var enc = new TextEncoder(), json = enc.encode(JSON.stringify({calls:[{id:1, fn:fn, args:args}]}));
      var body = new Uint8Array(4 + json.length + 2);
      new DataView(body.buffer).setUint32(0, json.length);
      body.set(json, 4);
      var res = await fetch("/__krate/call", {method:"POST", body:body, headers:{"content-type":"application/x-krate-frames"}});
      var buf = new Uint8Array(await res.arrayBuffer());
      if (buf.length < 4) throw new Error("host call " + fn + ": empty answer (HTTP " + res.status + ")");
      var n = new DataView(buf.buffer, buf.byteOffset).getUint32(0);
      var frame = JSON.parse(new TextDecoder().decode(buf.subarray(4, 4 + n)));
      if (frame.error) return {err:frame.error};
      if (frame.err) return {err:frame.err};
      if (frame.stream) return {err:{code:"unsupported", message:"the call returned a stream; the driver reads plain results only", data:null}};
      return {ok:frame.ok === undefined ? null : frame.ok};
    }
  };
  return lib;
})()`;

const libRef = `(globalThis.__krateDriver||(globalThis.__krateDriver=${pageLib}))`;

class Channel {
  private pending: { resolve: (s: string) => void; reject: (e: Error) => void }[] = [];
  private tail: Promise<unknown> = Promise.resolve();
  private ready: { resolve: () => void; reject: (e: Error) => void } | undefined;
  exited = false;
  exitInfo = "";
  stderr: string[] = [];

  constructor(readonly proc: ChildProcess) {
    createInterface({ input: proc.stdout! }).on("line", (l) => this.onLine(l));
    createInterface({ input: proc.stderr! }).on("line", (l) => {
      this.stderr.push(l);
      if (this.stderr.length > 500) this.stderr.shift();
    });
    proc.on("exit", (code, sig) => {
      this.exited = true;
      this.exitInfo = sig ? `killed by ${sig}` : `exited with code ${code}`;
      const err = new Error(`krate-shell ${this.exitInfo}${this.tailLog()}`);
      this.ready?.reject(err);
      this.ready = undefined;
      for (const p of this.pending.splice(0)) p.reject(err);
    });
    proc.on("error", (e) => {
      const err = new Error(`could not start krate-shell: ${e.message}`);
      this.ready?.reject(err);
      this.ready = undefined;
    });
  }

  tailLog(): string {
    const t = this.stderr.slice(-30).join("\n");
    return t ? `\nshell output:\n${t}` : "";
  }

  private onLine(l: string): void {
    if (this.ready) {
      if (l === "ready") {
        this.ready.resolve();
        this.ready = undefined;
      }
      return; // "url ..." and other startup lines
    }
    const p = this.pending.shift();
    if (!p) return;
    if (l === "ok") p.resolve("");
    else if (l.startsWith("ok ")) p.resolve(l.slice(3));
    else if (l.startsWith("error ")) p.reject(new Error(l.slice(6)));
    else p.reject(new Error(`unexpected reply from krate-shell: ${l}`));
  }

  waitReady(timeoutMs: number): Promise<void> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.ready = undefined;
        reject(new Error(`the app did not become ready within ${timeoutMs} ms${this.tailLog()}`));
      }, timeoutMs);
      this.ready = {
        resolve: () => (clearTimeout(timer), resolve()),
        reject: (e) => (clearTimeout(timer), reject(e)),
      };
      if (this.exited) this.ready.reject(new Error(`krate-shell ${this.exitInfo}${this.tailLog()}`));
    });
  }

  // send runs one command; commands go out one at a time, in order.
  send(cmd: string): Promise<string> {
    const run = () =>
      new Promise<string>((resolve, reject) => {
        if (this.exited) return reject(new Error(`krate-shell ${this.exitInfo}${this.tailLog()}`));
        if (/[\r\n]/.test(cmd)) return reject(new Error("a control command must be one line"));
        this.pending.push({ resolve, reject });
        this.proc.stdin!.write(cmd + "\n", (e) => {
          if (e) reject(e);
        });
      });
    const next = this.tail.then(run, run);
    this.tail = next.catch(() => undefined);
    return next;
  }
}

// transient errors come from a page that is still loading or navigating.
function transient(e: unknown): boolean {
  const m = e instanceof Error ? e.message : String(e);
  return /^query: (the page|no answer)|did not run the script|no open window|no view /.test(m);
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

/**
 * Calls fn (in the test process, not the page) until it returns a truthy value
 * and returns that value. A throw counts as "not yet"; the last one is the
 * failure's reason when the time runs out. Use it for conditions that mix
 * pages and host events.
 */
export async function poll<T>(fn: () => T | Promise<T>, opts: WaitOptions = {}): Promise<NonNullable<Awaited<T>>> {
  const timeout = opts.timeout ?? 5000;
  const deadline = Date.now() + timeout;
  let why = "it returned a falsy value";
  for (;;) {
    try {
      const v = await fn();
      if (v) return v as NonNullable<Awaited<T>>;
      why = "it returned a falsy value";
    } catch (e) {
      why = e instanceof Error ? e.message : String(e);
    }
    if (Date.now() >= deadline) throw new Error(`poll: timed out after ${timeout} ms; ${why}`);
    await sleep(opts.interval ?? 50);
  }
}

function describe(fn: unknown): string {
  return typeof fn === "function" ? "the predicate" : JSON.stringify(fn);
}

/** One webview of the app. Get one from App.page, App.window(id).view() or App.waitForView. */
export class Page {
  constructor(
    private readonly app: App,
    readonly id: string,
  ) {}

  private async run<T>(code: string): Promise<T> {
    // @json: the source is a JSON string, so the command stays one line.
    const out = await this.app.send(`query-in ${this.id} @json ${JSON.stringify(code)}`);
    return JSON.parse(out) as T;
  }

  /**
   * Runs a function (or an expression string, not statements) in the page and returns its
   * JSON value; a returned promise is awaited. The function is serialized, so
   * it cannot use variables of the test: pass them as arguments, which must be
   * JSON values.
   */
  async evaluate<R = unknown, A extends unknown[] = unknown[]>(
    fn: string | ((...args: A) => R | Promise<R>),
    ...args: A
  ): Promise<Awaited<R>> {
    // The page's content security policy forbids eval, so the source goes in
    // as code, not as a string.
    const expr = typeof fn === "function" ? `(${fn.toString()}).apply(null,${JSON.stringify(args)})` : `(${fn})`;
    return this.run<Awaited<R>>(expr);
  }

  /** The current address of the page. */
  url(): Promise<string> {
    return this.run<string>("location.href");
  }

  title(): Promise<string> {
    return this.run<string>("document.title");
  }

  /** Whether an element matches the selector right now. */
  exists(selector: string): Promise<boolean> {
    return this.run<boolean>(`${libRef}.exists(${JSON.stringify(selector)})`);
  }

  /** How many elements match the selector right now. */
  count(selector: string): Promise<number> {
    return this.run<number>(`${libRef}.count(${JSON.stringify(selector)})`);
  }

  // actionable waits for the element to exist, be visible and be enabled.
  private async actionable(selector: string, what: string, opts: WaitOptions = {}): Promise<void> {
    const timeout = opts.timeout ?? this.app.timeout;
    const deadline = Date.now() + timeout;
    let why = "no element matches it";
    for (;;) {
      try {
        const s = await this.run<{ found: boolean; visible: boolean; enabled: boolean }>(
          `${libRef}.state(${JSON.stringify(selector)})`,
        );
        if (s.found && s.visible && s.enabled) return;
        why = !s.found ? "no element matches it" : !s.visible ? "the element is not visible" : "the element is disabled";
      } catch (e) {
        if (!transient(e)) throw e;
        why = `the page is not ready (${(e as Error).message})`;
      }
      if (Date.now() >= deadline) {
        throw new Error(`${what}(${JSON.stringify(selector)}): timed out after ${timeout} ms; ${why}`);
      }
      await sleep(opts.interval ?? 50);
    }
  }

  /** Clicks the first match: pointer and mouse events, then click. Waits for it to be visible and enabled. */
  async click(selector: string, opts?: WaitOptions): Promise<void> {
    await this.actionable(selector, "click", opts);
    await this.run(`${libRef}.click(${JSON.stringify(selector)})`);
  }

  /** Sets a text field, select or editable element to text and fires input and change. */
  async fill(selector: string, text: string, opts?: WaitOptions): Promise<void> {
    await this.actionable(selector, "fill", opts);
    await this.run(`${libRef}.fill(${JSON.stringify(selector)},${JSON.stringify(text)})`);
  }

  /** Types text into an element one key at a time. */
  async type(selector: string, text: string, opts?: WaitOptions): Promise<void> {
    await this.actionable(selector, "type", opts);
    for (const ch of text) await this.press(ch, { selector });
  }

  /**
   * Sends a key to the focused element (or to options.selector): "Enter",
   * "Escape", "Tab", "a", "Control+a", "Shift+Tab". It dispatches keydown,
   * keypress and keyup, and does the default action of the common keys (text
   * insertion, Backspace, Enter submitting a form or activating a button,
   * Space on buttons, Tab moving focus). It is not real keyboard input.
   */
  async press(key: string, opts: PressOptions = {}): Promise<void> {
    if (opts.selector) await this.actionable(opts.selector, "press");
    await this.run(`${libRef}.press(${JSON.stringify(key)},${JSON.stringify(opts.selector ?? "")})`);
  }

  /** The textContent of the first match. Waits for the element to exist. */
  async text(selector: string, opts: WaitOptions = {}): Promise<string> {
    await this.waitFor(selector, opts);
    const t = await this.run<string | null>(`${libRef}.text(${JSON.stringify(selector)})`);
    if (t === null) throw new Error(`text(${JSON.stringify(selector)}): the element went away`);
    return t;
  }

  /** The value of an input, textarea or select. */
  async value(selector: string, opts: WaitOptions = {}): Promise<string> {
    await this.waitFor(selector, opts);
    const v = await this.run<string | null>(`${libRef}.value(${JSON.stringify(selector)})`);
    if (v === null) throw new Error(`value(${JSON.stringify(selector)}): the element has no value`);
    return v;
  }

  /**
   * Waits until a selector matches an element, or until a predicate function
   * (run in the page, may be async) returns a truthy value, and returns that
   * value (undefined for a selector).
   */
  async waitFor<R = unknown>(target: string | (() => R | Promise<R>), opts: WaitOptions = {}): Promise<R | undefined> {
    const timeout = opts.timeout ?? this.app.timeout;
    const deadline = Date.now() + timeout;
    let last = "";
    for (;;) {
      try {
        if (typeof target === "string") {
          if (await this.exists(target)) return undefined;
          last = "no element matches it";
        } else {
          const v = (await this.evaluate(target as () => R)) as R;
          if (v) return v;
          last = `it returned ${JSON.stringify(v ?? null)}`;
        }
      } catch (e) {
        if (!transient(e)) throw e;
        last = `the page is not ready (${(e as Error).message})`;
      }
      if (Date.now() >= deadline) {
        throw new Error(`waitFor(${describe(target)}): timed out after ${timeout} ms; ${last}`);
      }
      await sleep(opts.interval ?? 50);
    }
  }

  /** The errors the page threw so far: uncaught errors, unhandled rejections and errors the Krate runtime caught. */
  errors(): Promise<string[]> {
    return this.run<string[]>(`${libRef}.errors()`);
  }

  /**
   * Calls a host function as the page would (`<module id>.<Export>` for the
   * app's own functions, or an SDK id such as "krate/kv.get"). Arguments and
   * the result must be plain JSON values; streams are not supported. It runs
   * with this view's permissions. Rejects with a HostCallError.
   */
  async call<R = unknown>(fn: string, ...args: unknown[]): Promise<R> {
    const r = await this.run<{ ok?: R; err?: { code: string; message: string; data: unknown } }>(
      `${libRef}.call(${JSON.stringify(fn)},${JSON.stringify(args)})`,
    );
    if (r.err) throw new HostCallError(r.err.code, r.err.message, r.err.data);
    return r.ok as R;
  }

  /**
   * Not available yet: the platform layer has no webview capture API. Use
   * evaluate() to assert on the DOM instead. Rejects.
   */
  screenshot(): Promise<Uint8Array> {
    return Promise.reject(
      new Error("page.screenshot() is not available yet: the native platform layer has no webview capture API (a follow-up)"),
    );
  }
}

/** A window and its views. */
export class AppWindow {
  constructor(
    private readonly app: App,
    readonly id: string,
  ) {}

  views(): Promise<ViewInfo[]> {
    return this.app.views().then((vs) => vs.filter((v) => v.window === this.id));
  }

  /** The window's view with this id; the primary view without an id. */
  view(id: string = this.id): Page {
    return this.app.page(id);
  }
}

/** Fake native events, delivered to the app as the platform would. */
export interface Emitter {
  /** A tray icon event; kind "menu" with the item id for a tray menu item. */
  tray(kind?: "click" | "middle-click" | "right-click" | "menu", id?: string): Promise<void>;
  /** A menu item use. */
  menu(id: string, opts?: { window?: string; checked?: boolean }): Promise<void>;
  /**
   * The global shortcut the app registered for this accelerator, written as
   * the app wrote it ("CmdOrCtrl+Shift+K"). Fails if none is registered.
   */
  shortcut(accelerator: string): Promise<void>;
}

/** A running app. */
export class App {
  /** Default timeout of the waits, in milliseconds. */
  timeout: number;
  readonly emit: Emitter;
  private closed: Promise<void> | undefined;

  /** @internal use launch() */
  constructor(
    private readonly ch: Channel,
    timeout: number,
    private readonly cleanup: () => void,
  ) {
    this.timeout = timeout;
    this.emit = {
      tray: (kind = "click", id = "") => this.send(`emit tray ${JSON.stringify({ kind, id })}`).then(() => undefined),
      menu: (id, o = {}) =>
        this.send(`emit menu ${JSON.stringify({ id, window: o.window ?? "main", checked: o.checked ?? false })}`).then(
          () => undefined,
        ),
      shortcut: (accelerator) => this.send(`emit shortcut ${JSON.stringify({ accelerator })}`).then(() => undefined),
    };
  }

  /** @internal */
  send(cmd: string): Promise<string> {
    return this.ch.send(cmd);
  }

  /** Lines the shell and the host code wrote to stderr (the last 500). */
  logs(): string[] {
    return [...this.ch.stderr];
  }

  /** Every open view of every window, windows in creation order, primary views first. */
  async views(): Promise<ViewInfo[]> {
    return JSON.parse(await this.send("views")) as ViewInfo[];
  }

  /** The open windows with their views. */
  async windows(): Promise<WindowInfo[]> {
    const byWindow = new Map<string, ViewInfo[]>();
    for (const v of await this.views()) {
      const list = byWindow.get(v.window) ?? [];
      list.push(v);
      byWindow.set(v.window, list);
    }
    return [...byWindow].map(([id, views]) => ({ id, views }));
  }

  /** A window by id ("main" is the first). It does not check that it is open. */
  window(id = "main"): AppWindow {
    return new AppWindow(this, id);
  }

  /** A view by id ("main" is the first window's primary view). It does not check that it is open. */
  page(id = "main"): Page {
    return new Page(this, id);
  }

  /** Waits for a window (or view) to open, for example after the app opens a new window. */
  async waitForView(id: string, opts: WaitOptions = {}): Promise<Page> {
    const timeout = opts.timeout ?? this.timeout;
    const deadline = Date.now() + timeout;
    for (;;) {
      if ((await this.views()).some((v) => v.id === id)) return this.page(id);
      if (Date.now() >= deadline) {
        const open = (await this.views()).map((v) => v.id).join(", ") || "none";
        throw new Error(`waitForView(${JSON.stringify(id)}): timed out after ${timeout} ms; open views: ${open}`);
      }
      await sleep(opts.interval ?? 50);
    }
  }

  /** Waits until n views are open (counting every window's views). */
  async waitForViewCount(n: number, opts: WaitOptions = {}): Promise<ViewInfo[]> {
    const timeout = opts.timeout ?? this.timeout;
    const deadline = Date.now() + timeout;
    for (;;) {
      const vs = await this.views();
      if (vs.length === n) return vs;
      if (Date.now() >= deadline) throw new Error(`waitForViewCount(${n}): timed out after ${timeout} ms; ${vs.length} open`);
      await sleep(opts.interval ?? 50);
    }
  }

  /** Quits the app and removes its isolated data. Safe to call twice. */
  close(): Promise<void> {
    this.closed ??= (async () => {
      const proc = this.ch.proc;
      if (!this.ch.exited) {
        const exited = new Promise<void>((r) => proc.once("exit", () => r()));
        try {
          proc.stdin!.end();
        } catch {
          // already closed
        }
        const killer = setTimeout(() => proc.kill(), 5000);
        await exited;
        clearTimeout(killer);
      }
      this.cleanup();
    })();
    return this.closed;
  }
}

const live = new Set<ChildProcess>();
process.once("exit", () => {
  for (const p of live) p.kill();
});

function isolation(dir: string): Record<string, string> {
  switch (process.platform) {
    case "win32":
      return { APPDATA: join(dir, "roaming"), LOCALAPPDATA: join(dir, "local") };
    case "darwin":
      return { HOME: dir };
    default:
      return {
        XDG_DATA_HOME: join(dir, "data"),
        XDG_CONFIG_HOME: join(dir, "config"),
        XDG_CACHE_HOME: join(dir, "cache"),
        XDG_STATE_HOME: join(dir, "state"),
      };
  }
}

/**
 * Starts the app and resolves when it is ready. Under `krate native test` the
 * shell and the built app come from the environment; outside it pass
 * options.shell and options.appDir.
 */
export async function launch(options: LaunchOptions = {}): Promise<App> {
  const shell = options.shell ?? process.env[SHELL_ENV];
  const appDir = options.appDir ?? process.env[APP_ENV];
  if (!shell || !appDir) {
    throw new Error(
      `launch(): no app to start. Run the test with "krate native test", which builds the app and sets ${SHELL_ENV} and ${APP_ENV}, or pass { shell, appDir }.`,
    );
  }
  let dir = "";
  let env: NodeJS.ProcessEnv = { ...process.env, KRATE_TEST: "1" };
  if (options.isolate !== false) {
    dir = mkdtempSync(join(tmpdir(), "krate-test-"));
    env = { ...env, ...isolation(dir) };
  }
  Object.assign(env, options.env);
  const cleanup = () => {
    if (dir) {
      try {
        rmSync(dir, { recursive: true, force: true });
      } catch {
        // best effort
      }
    }
  };
  const proc = spawn(shell, ["--dev", appDir], { env, stdio: ["pipe", "pipe", "pipe"], windowsHide: true });
  live.add(proc);
  proc.once("exit", () => live.delete(proc));
  const ch = new Channel(proc);
  const app = new App(ch, options.timeout ?? 5000, cleanup);
  const startTimeout = options.startTimeout ?? 60000;
  const started = Date.now();
  try {
    await ch.waitReady(startTimeout);
    // "ready" means the first window exists, not that its page has loaded:
    // wait for that too, inside the start-up budget, so a test's first wait
    // is not spent on a cold start (seen on CI runners: over 5s).
    const views = await app.views();
    if (views.some((v) => v.id === "main")) {
      await app.page("main").waitFor(() => document.readyState === "complete", {
        timeout: Math.max(1000, startTimeout - (Date.now() - started)),
      });
    }
  } catch (e) {
    proc.kill();
    cleanup();
    throw e;
  }
  return app;
}
