---
title: Remote Content in Native Apps
order: 12
description: Show web pages in views and windows that cannot reach the app: own storage, an origin allowlist, request rules, user agent, proxy, certificates, logins.
---

# Remote content

> **Experimental.** Part of [Krate Native](/docs/native/). Linux (WebKitGTK) is tested.
> macOS (WKWebView) and Windows (WebView2) compile and have GUI tests that the CI
> native job runs; until it passes they are `untested` in the
> [compatibility matrix](/docs/guides/compatibility/).

An app sometimes has to show a page from the web: a login page of a service, a
dashboard, documentation. Krate Native shows it in a **remote view** or a **remote
window**, and gives it nothing of the app.

```ts
import { addView, openWindow, onMessage, post, setRules, setUserAgent } from "@krate/app/remote";

const view = await addView({
  url: "https://example.com/app",
  partition: "persist:example",        // its own storage; "" is the default remote one
  allow: ["https://*.example.com"],    // besides https://example.com
  messages: true,                      // opt in to the message channel
});
onMessage(({ view, origin, data }) => console.log(view, origin, data));
await post(view.id, { hello: "page" }, "https://example.com");

await setUserAgent("persist:example", "MyApp/1.0");
await setRules("persist:example", [{ urls: ["*://*.ads.example.net/*"], action: "block" }]);
const win = await openWindow({ url: "https://example.com/docs", title: "Docs", blocked: "block" });
```

```go
import "github.com/kratejs/krate/packages/native/sdk/remote"

v, _ := remote.AddView("", remote.ViewOptions{Options: remote.Options{URL: "https://example.com/app", Messages: true}})
remote.OnMessage(func(m remote.Message) { fmt.Println(m.View, m.Origin, string(m.Data)) })
remote.Post(v.ID, map[string]any{"hello": "page"}, "https://example.com")
```

## The security model

The rules, in the order a reviewer should check them:

1. **Remote content lives only in views and windows created as remote**
   (`addView`, `openWindow` of `@krate/app/remote`). An app view is never
   navigated away from the app origin: the [app-origin lock](/docs/native/views/#scopes-and-identity)
   is unchanged.
2. **A remote view has no access to Krate.** Its web context has no handler for
   the app scheme, so `fetch("krate://app/...")`, an iframe, an image or a
   navigation to it fails, and so do the host-call and dev endpoints. The bridge
   and stream shims are not injected. Requests that reach the shell anyway are
   given an empty permission scope, and the dev scripts (hot reload, queries)
   skip remote views. On Windows the app origin is `https://app.localhost`, which
   Chromium resolves to the loopback address: that host is refused as a remote
   URL, as an allowed origin and as a navigation target.
3. **Navigation is an allowlist.** A remote view may navigate to the origin it
   started on and the origins listed in `allow` (`https://*.example.com` matches
   subdomains only; `allowAnyHttps` allows any https origin). Everything else, a
   link, a redirect, `location`, a form, `window.open`, is cancelled and either
   opened in the system browser (`blocked: "external"`, the default; http, https
   and mailto only, and at most a short burst per view) or dropped
   (`blocked: "block"`). `krate:`, `file:`, `data:`, `blob:`, `javascript:` and
   every other scheme never load as a page. Frames follow the same allowlist
   (`about:`, `data:` and `blob:` documents inside a page are fine) and never open
   the browser. A popup that the allowlist accepts loads in the view itself; no
   second webview is ever created. App-initiated navigation (`navigate`) obeys the
   same list.
4. **Storage is a partition, and never the app's.** The default partition `""` is
   persistent and separate from the app's. `persist:name` is a named persistent
   one, `name` a named one that is gone when the app quits. Partitions share
   nothing: cookies, local storage, caches, logins and certificate decisions are
   per partition. The app's cookies (`@krate/app/cookies`) are not the remote
   ones. On Linux each partition is a web context with its own web process.
5. **The app and the page talk only through the message channel,** off unless the
   view is created with `messages: true`. The page calls
   `krateRemote.postMessage(data)`; the app receives `{ view, origin, data }`
   where `origin` is read from the engine, not from the page. The app calls
   `post(view, data, targetOrigin)` and the page receives a `message` event with
   `event.origin` equal to the app's origin; the message is delivered only while
   the page is on `targetOrigin` (`"*"` skips the check, like `postMessage`).
   Messages are JSON, at most 1 MiB. A message that did not come through the
   view's own script (for example from an iframe that calls the engine's handler
   directly) carries no token and is dropped.
6. **Permissions are denied** (camera, microphone, location, notifications,
   clipboard) and **downloads are cancelled** for remote content.
7. **Certificate errors end the load** unless the app decides otherwise (below).

### Where this is enforced

The enforcement points are few, and each is one function:

| Point | Where | What |
|-------|-------|------|
| Engine isolation | `NewRemoteWindow` / `AddRemoteView` in each platform (`platform/linux/remote.go`, `darwin/remote_darwin.go`, `windows/remote_windows.go`) | A separate web context, configuration or profile, created without the app's scheme handler or scripts. |
| Navigation policy | `caps.RemotePolicy.Decide` (`platform/caps/remote.go`), called by each platform's navigation, frame, popup and `navigate` paths | The allowlist, scheme rules, `app.localhost`. |
| Identity | `identify` in `shell/shell.go` and `IsRemoteView` | A request tagged with a remote view gets the empty scope. |
| Message channel | `caps.ParseRemoteMessage`, `DeliverToRemoteScript` and `PostToRemote` | Token check, engine-reported origin, target origin check. |

## Windows and views

`openWindow(options)` opens a window whose only view is remote; `addView(options, window?)`
adds a remote view beside a pane of an existing window (`side`, `size`, `beside`,
`minWidth`, `minHeight` as in [Views](/docs/native/views/)). Both return the usual
handles, so `view.remove()`, `focus()` and the layout calls work. A remote view and
window have an **empty permission scope**: calls from them to the SDK fail with
`permission_denied` whatever the app holds. `navigate(view, url)` loads a URL the
allowlist accepts. `onRemoteEvent` reports `loaded` and `navBlocked` (with the
verdict). Opening needs the `remote` permission.

## Request rules

`setRules(partition, rules)` replaces the declarative rules of a partition (needs
`remote:intercept`); `onRequest` reports the requests of its views with what the
rules did. A rule has `urls` (match patterns: `<all_urls>`, or
`scheme://host/path` where the scheme `*` means http and https, a host `*.x.com`
means subdomains, and `*` in the path matches anything) and an `action`:
`block`, `redirect` (with `redirect`) or `headers` (with `headers`).

The engines differ, so `capabilities()` reports what works:

| | Linux | macOS | Windows |
|--|-------|-------|---------|
| Observe requests | every request | navigations | every request |
| Block | every request (content filter) | every request (content rule list) | every request |
| Redirect | navigations | navigations | every request |
| Request headers | navigations | navigations | every request |

Honest limits:

- WebKitGTK's `resource-load-started` only observes. Blocking uses a
  `WebKitUserContentFilter` (compiled from the block rules, so it follows the
  content-blocker syntax); redirects and header changes are done for main-frame
  navigations by cancelling the navigation and loading a rewritten request. A
  subresource cannot be redirected or given headers.
- WKWebView has no hook for subresources either: blocking is a
  `WKContentRuleList`, navigations are decided in `decidePolicyForNavigationAction`.
- WebView2 gets all of it from `WebResourceRequested` on the remote view. A blocked
  request is answered with a 403.
- Rules are per partition and apply to its views at once (WebKit: when the filter
  is compiled, which `setRules` waits for).

## User agent, proxy, certificates, logins

These need `remote:network`.

- `setUserAgent(partition, ua)` and the per-view `userAgent` option. `""` restores
  the engine's.
- `setProxy(partition, { mode, url, bypass })`, modes `system`, `direct` and
  `fixed` (`http://`, `https://` or `socks5://`). WebKitGTK sets the proxy per
  partition (`WebKitNetworkProxySettings`), macOS 14 and later through
  `WKWebsiteDataStore.proxyConfigurations`. **Windows does not support it:**
  WebView2 takes `--proxy-server` as a browser argument of the whole environment,
  which the app shares; the call answers `unsupported`.
- `onCertificateError(fn)` decides certificates the engine refused. Without a
  handler the load fails. `fn` gets `{ view, url, host, errors }` and returns
  whether to accept the certificate for that host **in that partition** (WebKitGTK
  `allow_tls_certificate_for_host`, WKWebView a server-trust credential, WebView2
  `ALWAYS_ALLOW`). On macOS the system evaluates the trust first, and only a failure
  is passed on; the error list there is just `untrusted`. Answer within a minute or
  the certificate is refused.
- `onAuth(fn)` answers HTTP authentication (basic, digest, NTLM): return the
  credentials, or `null` to cancel; nothing is saved. `retry` tells that a former
  answer was refused.
- `setClientCertificate(partition, host, certFile, keyFile)` makes a partition
  present a client certificate (PEM files) to a host. **Linux only:** macOS needs a
  keychain identity and Windows takes certificates from the Windows store, so both
  answer `unsupported` (`capabilities().clientCertificates` says which).
- `clearData(partition, options)` removes cookies, cache and storage of a partition.
  On Windows a view of the partition must be open (WebView2 clears a profile through
  one of its views).

## Platform notes

| | Linux | macOS | Windows |
|--|-------|-------|---------|
| Isolation | a `WebKitWebContext` and website data manager per partition | a `WKWebsiteDataStore` and `WKProcessPool` per partition | a WebView2 profile per partition (`ICoreWebView2ControllerOptions`; needs a runtime with `ICoreWebView2Environment10`) |
| Persistent partition | a directory under the app data directory | `dataStoreForIdentifier:` (macOS 14); before that only in-memory stores exist, and the default partition is then in-memory too | a named profile |
| In-memory partition | a private temporary directory removed at exit (WebKit's ephemeral managers share certificate exceptions with each other, so they are not used) | `nonPersistentDataStore` | an InPrivate profile |
| Popups and `window.open` | policy plus the `create` signal | `decidePolicyForNavigationAction` plus the UI delegate | `NewWindowRequested` |
| Message origin | the view's URL at the time (the page script is injected in the top frame, and frames cannot sign) | `WKFrameInfo.securityOrigin` of the main frame | `WebMessageReceived` source |

Remote views use more memory than app views: a separate partition means a
separate web process (Linux, macOS) or profile (Windows). Views of the same
partition share one.

## Testing

`platform/caps` holds the policy, partition, pattern and rule logic with unit
tests that run everywhere. The Linux GUI tests (`TestGUIRemote*`) load pages from
local servers, an https server with a self-signed certificate, a login server, a
client-certificate server and a proxy, and try every rule above: fetches,
iframes, `window.open` and navigations to the app, storage isolation, request
rules, the user agent, certificates, logins, the proxy and the message channel.
`examples/native-remote` and `TestNativeE2ERemoteGUI` drive the same through the
real shell. `krate native test` can use `@krate/app/remote`; note that a remote
page cannot answer the dev control channel (it cannot reach the app), so a test
reads a remote page through the message channel.
