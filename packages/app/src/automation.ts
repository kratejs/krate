// The remote debugging endpoint and DevTools protocol commands, for tests and
// debugging sessions. Needs the "automation" permission, and works only when
// the app runs in dev or was built with native.debug.
//
// WebView2 (Windows) speaks the Chrome DevTools Protocol: Playwright attaches
// with chromium.connectOverCDP(endpoint.url) and Puppeteer with
// puppeteer.connect({ browserURL: endpoint.url }), and cdp() sends one method
// in process. WebKitGTK (Linux) serves the WebKit remote inspector over HTTP:
// a browser can open it, Playwright and Puppeteer cannot drive it, and cdp()
// rejects with code "unsupported". WKWebView (macOS) is inspectable from
// Safari's Develop menu and has no network endpoint.
import { call, stream } from "./index.js";

export interface AutomationEndpoint {
  // False in a release build without native.debug.
  enabled: boolean;
  kind: "cdp" | "webkit-inspector" | "safari" | "";
  // Where to point a client; empty for "safari" and when there is none.
  url: string;
  port: number;
  // True when cdp() can send protocol methods.
  cdp: boolean;
}

export function automationEndpoint(): Promise<AutomationEndpoint> {
  return call("krate/automation.endpoint", []);
}

// cdp runs a DevTools protocol method, for example
// cdp("Page.captureScreenshot", { format: "png" }), in a window or view
// ("" is the main window) and resolves to the method's result object.
export function cdp<T = unknown>(method: string, params: Record<string, unknown> = {}, view = ""): Promise<T> {
  return call<T>("krate/automation.cdp", [method, params, view]);
}

// cdpEvents yields the parameters of each protocol event with this name, for
// example cdpEvents("Page.loadEventFired"), until the loop is left or the
// signal aborts.
export function cdpEvents<T = unknown>(event: string, view = "", signal?: AbortSignal): AsyncIterable<T> {
  return stream<T>("krate/automation.cdpEvents", [event, view], { signal });
}
