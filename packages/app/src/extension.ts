// The browser extensions an app packages (native.extensions). Needs the
// "extension" permission.
//
// WebView2 loads unpacked Chromium extensions into the app's profile. WKWebView
// loads them on macOS 15.4 and later. WebKitGTK has no extension API: every
// call rejects with code "unsupported". Extensions are named, not addressed by
// path, so a page cannot load code from an arbitrary place; the declared ones
// load when the app starts.
import { call } from "./index.js";

export interface ExtensionInfo {
  id: string;
  name: string;
  enabled: boolean;
}

// declaredExtensions lists the names from native.extensions.
export function declaredExtensions(): Promise<string[]> {
  return call("krate/extension.declared", []);
}

// listExtensions returns the extensions loaded now.
export function listExtensions(): Promise<ExtensionInfo[]> {
  return call("krate/extension.list", []);
}

// loadExtension loads a declared extension by name.
export function loadExtension(name: string): Promise<ExtensionInfo> {
  return call("krate/extension.load", [name]);
}

// removeExtension unloads an extension by id.
export function removeExtension(id: string): Promise<void> {
  return call("krate/extension.remove", [id]);
}
