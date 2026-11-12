// Web pages from the internet in views and windows of the app, with no access
// to the app. Needs the "remote" permission; request rules and the request
// stream need "remote:intercept", and the user agent, proxy, certificate
// decisions, logins and stored data "remote:network".
//
// A remote view has its own storage partition, no bridge to the SDK and no way
// to fetch the app. It may navigate only to the origins you list (default: the
// origin it started on); the rest opens in the system browser or is dropped.
// You and the page talk through the message channel, which is off unless the
// view is created with `messages: true`: the page calls
// `krateRemote.postMessage(data)`, you call post(). See docs/native/remote.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";
import { Window, View } from "./window.js";

// The functions are declared under prefixed names and exported under their short ones:
// the bundler hoists top-level declarations of every module into one scope, so two modules
// that both declare a `post` or `navigate` would call each other when imported as namespaces. For the same
// reason the event subscription is `onRemoteEvent`: window.ts, which this module imports, declares `onEvent`.

export interface RemoteOptions {
  /** The first page, http or https. */
  url: string;
  /**
   * The storage partition: "" (default) is a persistent partition for remote content that is never
   * the app's, "persist:name" a named persistent one, "name" a named one that is gone when the app quits.
   */
  partition?: string;
  /** Origins the view may navigate to besides the origin of `url`: "https://example.com", "https://*.example.com". */
  allow?: string[];
  /** Let the view navigate to any https origin. */
  allowAnyHttps?: boolean;
  /** What happens to other navigations: "external" (default) opens the system browser, "block" drops them. */
  blocked?: "external" | "block";
  /** Turn on the message channel with the page. */
  messages?: boolean;
  /** A user agent for this view only. */
  userAgent?: string;
}

export interface RemoteWindowOptions extends RemoteOptions {
  id?: string;
  title?: string;
  width?: number;
  height?: number;
  fixed?: boolean;
}

export interface RemoteViewOptions extends RemoteOptions {
  id?: string;
  beside?: string;
  side?: "right" | "left" | "top" | "bottom";
  size?: number;
  minWidth?: number;
  minHeight?: number;
}

export interface RemoteMessage {
  /** The id of the view. */
  view: string;
  /** The origin of the page, read from the engine, never from the page. */
  origin: string;
  data: unknown;
}

export interface RemoteEvent {
  view: string;
  kind: "loaded" | "navBlocked";
  url: string;
  /** For navBlocked: "external" (opened in the browser) or "block". */
  verdict: "allow" | "external" | "block";
}

export interface RemoteRequest {
  view: string;
  url: string;
  method: string;
  /** What the rules did: "", "block", "redirect" or "headers". */
  action: "" | "block" | "redirect" | "headers";
}

export interface RemoteRule {
  /** Match patterns: "<all_urls>" or "https://*.example.com/ads/*" (scheme * means http and https). */
  urls: string[];
  action: "block" | "redirect" | "headers";
  /** For "redirect": an http(s) URL. */
  redirect?: string;
  /** For "headers": request headers to set. */
  headers?: Record<string, string>;
}

/** Which requests a feature covers on this platform: "" none, "navigation" the main frame, "all". */
export type RemoteLevel = "" | "navigation" | "all";

export interface RemoteCapabilities {
  observe: RemoteLevel;
  block: RemoteLevel;
  redirect: RemoteLevel;
  requestHeaders: RemoteLevel;
  userAgent: boolean;
  proxy: boolean;
  certificateErrors: boolean;
  clientCertificates: boolean;
  auth: boolean;
  inMemory: boolean;
}

export interface RemoteProxy {
  mode: "system" | "direct" | "fixed";
  /** For "fixed": http://host:port, https://host:port or socks5://host:port. */
  url?: string;
  /** Hosts that skip the proxy. */
  bypass?: string[];
}

export interface RemoteClearOptions {
  cookies?: boolean;
  cache?: boolean;
  storage?: boolean;
}

export interface RemoteCertificateError {
  id: number;
  view: string;
  url: string;
  host: string;
  /** "untrusted", "expired", "notYetValid", "mismatch", "revoked", "insecure" or "unknown". */
  errors: string[];
}

export interface RemoteAuthChallenge {
  id: number;
  view: string;
  host: string;
  realm: string;
  scheme: string;
  proxy: boolean;
  retry: boolean;
}

export interface RemoteCredentials {
  user: string;
  password: string;
}

function wireOptions(o: RemoteOptions) {
  return {
    url: o.url,
    partition: o.partition ?? "",
    allow: o.allow ?? [],
    allowAnyHttps: o.allowAnyHttps ?? false,
    blocked: o.blocked ?? "",
    messages: o.messages ?? false,
    userAgent: o.userAgent ?? "",
  };
}

/** openWindow opens a window that shows a remote page. */
async function remoteOpenWindow(o: RemoteWindowOptions): Promise<Window> {
  const id = await call<string>("krate/remote.open", [
    { ...wireOptions(o), id: o.id ?? "", title: o.title ?? "", width: o.width ?? 0, height: o.height ?? 0, fixed: o.fixed ?? false },
  ]);
  return new Window(id);
}

/** addView adds a remote view to a window (default: the main window). */
async function remoteAddView(o: RemoteViewOptions, window = ""): Promise<View> {
  const id = await call<string>("krate/remote.addView", [
    window,
    { ...wireOptions(o), id: o.id ?? "", beside: o.beside ?? "", side: o.side ?? "", size: o.size ?? 0, minWidth: o.minWidth ?? 0, minHeight: o.minHeight ?? 0 },
  ]);
  return new View(id);
}

/** navigate loads a URL in a remote view; the view's allowlist applies. */
function remoteNavigate(view: string, url: string): Promise<void> {
  return call("krate/remote.navigate", [view, url]);
}

/**
 * post sends a message to the page of a remote view created with `messages: true`. The page gets a
 * "message" event whose origin is the app's. It is delivered only while the page is on `targetOrigin`
 * ("*" skips the check).
 */
function remotePost(view: string, data: unknown, targetOrigin: string): Promise<void> {
  return call("krate/remote.post", [view, targetOrigin, data]);
}

/** onMessage calls fn for messages remote pages post with krateRemote.postMessage. */
function remoteOnMessage(fn: (m: RemoteMessage) => void, opts?: SubscribeOptions): () => void {
  return subscribe<RemoteMessage>("krate/remote.messages", fn, opts);
}

/** onEvent calls fn when a remote page loaded or a navigation was refused. */
function remoteOnEvent(fn: (e: RemoteEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<RemoteEvent>("krate/remote.events", fn, opts);
}

/** capabilities says what this platform can do for remote content. */
function remoteCapabilities(): Promise<RemoteCapabilities> {
  return call("krate/remote.capabilities");
}

/** setRules replaces the request rules of a partition (see RemoteRule and capabilities()). */
function remoteSetRules(partition: string, rules: RemoteRule[]): Promise<void> {
  return call("krate/remote.setRules", [
    partition,
    rules.map((r) => ({ urls: r.urls, action: r.action, redirect: r.redirect ?? "", headers: r.headers ?? {} })),
  ]);
}

/** onRequest calls fn for the requests of remote views, with what the rules did. */
function remoteOnRequest(fn: (r: RemoteRequest) => void, opts?: SubscribeOptions): () => void {
  return subscribe<RemoteRequest>("krate/remote.requests", fn, opts);
}

/** setUserAgent sets the user agent of a partition ("" restores the default). */
function remoteSetUserAgent(partition: string, userAgent: string): Promise<void> {
  return call("krate/remote.setUserAgent", [partition, userAgent]);
}

/** setProxy sets the proxy of a partition. */
function remoteSetProxy(partition: string, proxy: RemoteProxy): Promise<void> {
  return call("krate/remote.setProxy", [partition, { mode: proxy.mode, url: proxy.url ?? "", bypass: proxy.bypass ?? [] }]);
}

/** setClientCertificate makes a partition present a client certificate (PEM files) to a host. */
function remoteSetClientCertificate(partition: string, host: string, certFile: string, keyFile: string): Promise<void> {
  return call("krate/remote.setClientCertificate", [partition, host, certFile, keyFile]);
}

/** clearData removes stored data of a partition; no option set means everything. */
function remoteClearData(partition: string, options: RemoteClearOptions = {}): Promise<void> {
  return call("krate/remote.clearData", [partition, { cookies: options.cookies ?? false, cache: options.cache ?? false, storage: options.storage ?? false }]);
}

/**
 * onCertificateError decides certificates the engine refused. While a handler is subscribed fn is
 * asked for each error and returns true to accept the certificate for that host in that partition;
 * without a handler every error is final. Answer within a minute or the certificate is refused.
 */
function remoteOnCertificateError(fn: (e: RemoteCertificateError) => boolean | Promise<boolean>, opts?: SubscribeOptions): () => void {
  return subscribe<RemoteCertificateError>(
    "krate/remote.certificates",
    (e) => {
      void Promise.resolve(fn(e))
        .catch(() => false)
        .then((allow) => call<void>("krate/remote.answerCertificate", [e.id, allow === true]).catch(() => {}));
    },
    opts,
  );
}

/**
 * onAuth answers HTTP authentication challenges. Return the credentials, or null to cancel. Without a
 * handler challenges are cancelled. Answer within a minute.
 */
function remoteOnAuth(fn: (c: RemoteAuthChallenge) => RemoteCredentials | null | Promise<RemoteCredentials | null>, opts?: SubscribeOptions): () => void {
  return subscribe<RemoteAuthChallenge>(
    "krate/remote.auth",
    (c) => {
      void Promise.resolve(fn(c))
        .catch(() => null)
        .then((cr) => call<void>("krate/remote.answerAuth", [c.id, cr?.user ?? "", cr?.password ?? "", cr != null]).catch(() => {}));
    },
    opts,
  );
}

export {
  remoteOpenWindow as openWindow,
  remoteAddView as addView,
  remoteNavigate as navigate,
  remotePost as post,
  remoteOnMessage as onMessage,
  remoteOnEvent as onRemoteEvent,
  remoteCapabilities as capabilities,
  remoteSetRules as setRules,
  remoteOnRequest as onRequest,
  remoteSetUserAgent as setUserAgent,
  remoteSetProxy as setProxy,
  remoteSetClientCertificate as setClientCertificate,
  remoteClearData as clearData,
  remoteOnCertificateError as onCertificateError,
  remoteOnAuth as onAuth,
};
