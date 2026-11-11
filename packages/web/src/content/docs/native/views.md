---
title: Views and Split Panes
order: 11
description: Several webviews in one window, laid out as split panes with draggable dividers, each with its own route and permission scope, and how requests are bound to the view they came from.
---

# Views and split panes

> **Preview.** Part of [Krate Native](/docs/native/). Linux (WebKitGTK) is the
> platform where this is tested. The macOS (`NSSplitView`) and Windows (several
> WebView2 controllers in one window) implementations compile and their layout
> math is unit-tested, but they have never run on those systems.

A window normally holds one webview. A window can hold several, laid out as split
panes that the user resizes by dragging the dividers: an editor beside a
preview, a sidebar page next to the main page, a terminal under both. Each
**view** loads its own route of the app and can have its own
[permission scope](#scopes-and-identity). All views of an app share one web
process (see [Memory](#memory)).

```ts
import { main, pane, split, onViewMessage } from "@krate/app/window";

const win = main();
const sidebar = await win.addView({ path: "/sidebar", side: "left", size: 0.25, minWidth: 180 });
const logs = await win.addView({ path: "/logs", side: "bottom", size: 0.3, minHeight: 100,
                                 permissions: ["kv"] });          // a narrower scope
await sidebar.post({ open: "/tasks/3" });                          // view to view
onViewMessage(({ from, data }) => console.log(from, data));        // in the receiving page

const layout = await win.layout();                                 // ratios after a drag
await win.setLayout(split("horizontal", pane(sidebar), split("vertical", pane(win.id), pane(logs), 0.7), 0.25));
```

```go
import "github.com/kratejs/krate/packages/native/sdk/window"

w := window.Main()
side, _ := w.AddView(window.ViewOptions{Path: "/sidebar", Side: window.Left, Size: 0.25, MinWidth: 180})
side.Post(map[string]any{"open": "/tasks/3"})
l, _ := w.Layout() // *window.Layout with the ratios the user dragged to
w.SetLayout(window.Split("horizontal", 0.25, window.Pane(side.ID), window.Pane(w.ID)))
```

The operations need the `window` permission, like the rest of the window API.

## Views

Every window has a **primary view**, the one it was created with. Its id is the
window's id (`"main"`, `"w1"`). `addView` creates more; each has an id (`"v1"`,
`"v2"`, or the `id` you pass: letters, digits, `_` and `-`, at most 32, unique
among all window and view ids). Ids are never reused.

| Operation | Does |
|-----------|------|
| `window.addView(options)` | Splits a new view off an existing one. Returns the view. |
| `view.remove()` | Removes a view; the pane next to it takes its space. The primary view cannot be removed: close the window. |
| `window.views()`, `views()` | Lists views (`id`, `window`, `path`, `primary`), primary first. |
| `window.setLayout(layout)`, `window.layout()` | Replaces the layout, or reads it back with the current ratios. |
| `view.focus()` | Gives the view keyboard focus. |
| `view.navigate(path)` | Loads another route in the view. (`window.navigate` still navigates the primary view.) |
| `view.post(data)` | Posts a message to the view; see [Messages](#messages). |
| `onViewEvent(fn)` | Added, removed, navigated and closed events. |

`addView` options: `path` (default `"/"`), `beside` (the view to split; default
the primary view), `side` (`"right"` default, `"left"`, `"top"`, `"bottom"`: where the
new view goes relative to `beside`), `size` (the share of the split the new view gets,
between 0 and 1, default 0.5), `minWidth` and `minHeight` (pixels, the smallest the
pane may be dragged to), `permissions` and `id`.

## Layout

A layout is a tree of splits. A leaf is a pane showing a view; a split puts two
subtrees side by side (`"horizontal"`: first on the left) or one above the other
(`"vertical"`: first on top), with a `ratio` that is the share of the space the
**first** subtree gets (default 0.5, exclusive of the divider). Splits have two
halves; nest them for three panes or more.

```ts
split("horizontal", pane("main"), split("vertical", pane(a), pane(b), 0.6), 0.5)
//  +---------+---------+
//  |         |    a    |
//  |  main   +---------+
//  |         |    b    |
//  +---------+---------+
```

- `setLayout` must name **every** view of the window exactly once, with ratios in
  [0, 1) (zero is one half) and at most 16 nested splits. It fails with
  `bad_args` otherwise and changes nothing. The pages do not reload.
- `addView` and `view.remove()` edit the layout for you: adding replaces the pane
  of `beside` by a split of it and the new view; removing replaces the removed
  pane's split by its sibling.
- **Minimum sizes.** `minWidth`/`minHeight` belong to a pane. A divider cannot be
  dragged past them, and a split's minimum is the sum of its panes' (plus the
  divider) along its axis. If a window is made smaller than the minimums allow,
  the space is shared in proportion to them.
- **Dividers** are draggable by the user. `layout()` returns the ratios the
  dividers have now, so an app can save them and `setLayout` them on the next run.
  Ratios survive a resize (the panes grow and shrink together) and
  [release](#memory).

The layout arithmetic (tree to rectangles, divider hit-testing, drag to ratio,
minimum sizes) is plain Go in `platform/caps/layout.go` with unit tests that run on
every OS; the Windows implementation uses it directly to place its controllers,
and the Linux and macOS ones use it to edit the tree and validate layouts while
GTK's `GtkPaned` and AppKit's `NSSplitView` draw the dividers.

## Scopes and identity

Each view has a permission scope of its own, keyed by its id. A view opened with
`permissions: [...]` may use only that list, which must be a subset of the
caller's scope; without the option it inherits the scope of the view that opened
it, exactly like [`window.open`](/docs/native/release/#per-window-scopes). A
window opened with a narrow scope keeps that scope for its primary view.

**A page cannot choose its scope.** The platform layer tags every request with
the webview it came from, and the shell maps that webview to a window or view id
and its scope. The `x-krate-window` header a page sets is ignored in a native
window. So a view with `permissions: ["kv"]` cannot reach `fs` by sending the
id of a wider view or window, and a message's `from` field (below) is the real
sender. A request from a webview the shell cannot place gets a scope that allows
nothing.

Two limits remain. Every view of every window is on the same origin
(`krate://app`, `https://app.localhost`), so pages can read each other's
`localStorage` and cookies, and a scope is not an isolation boundary for content
you do not trust. And in browser mode (`krate native dev --browser`) there are no
native webviews: the header is the identity there, and a script in a tab can send
another window's id. Scopes in browser mode catch mistakes; they are not a
security boundary.

## Messages

`view.post(data)` delivers `data` (JSON-compatible, at most 1 MiB) to another view
of the app, in any window. The receiving page gets a DOM event:

```ts
window.addEventListener("krate:view-message", (e) => {
  const { from, data } = (e as CustomEvent).detail; // from: the sender's view id
});
// or: onViewMessage(({ from, data }) => ...)
```

`from` is set by the shell from the sender's native identity; it is `""` for a
message sent from host code. A message to a view that does not exist rejects with
`not_found`; one to a view of a released window rejects with an error (show the window
first, see [Memory](#memory)). Messages are not queued: a page that has not
loaded yet does not get them. Posting needs only the `window` permission on the
sender; the receiving page decides what to trust.

## Events

`onViewEvent` (stream `krate/window.viewEvents`; Go `window.OnViewEvent`) delivers
`{ view, window, kind, path }`:

| Kind | When |
|------|------|
| `added` | A view was created (`path` is its route). |
| `navigated` | A page finished loading in the view (`path`). Fires for the first load too. |
| `removed` | `view.remove()` took it out. |
| `closed` | Its page called `window.close()` (the view is removed), or its window closed. A window's views are announced closed before the window's own `closed` event. |

Events for a view are delivered after `added`, in order. The primary view of a
window gets `navigated` and `closed` events under the window's id.

## Memory

- **One web process.** On Linux every view is created with
  `webkit_web_view_new_with_related_view` of a live one, so all views of all
  windows share the app's single WebKitWebProcess; a test asserts the process
  count. Measured on Linux (WebKitGTK 2.54, `balanced` profile, blank pages): about
  **1 MB** of web process memory and **0.2 MB** of shell memory per extra view in a
  window, against about 4 to 8 MB for an extra window (the GTK window and its
  compositing surfaces). See [Measured](#measured).
- **Settings.** Every view gets the developer tools (in dev), the memory profile
  and the navigation lock when it is created, the same as a window's first view.
- **HMR and the dev inspector** work per page, so every view hot-reloads.
  `krate native dev` evaluates its `eval` command in every view of every window.
- **Release.** `hide({ release: true })` and `native.window.keepAlive` release the
  **whole window**: all its views are asked for their route and scroll position,
  then all webviews are destroyed (when every window is released the web process
  exits). `show()` recreates them all, with their routes, scroll positions, ids
  and layout ratios. While a window is released `addView`, `remove()` and `setLayout` fail
  (show it first), and so does `post` to its views. A window that is
  hidden without release gives every view the `krate:memory-pressure` event.
- **Closing** a window destroys its views.

### Measured

Linux x86-64, WebKitGTK 2.54.0, Xvfb, `balanced` profile; PSS after settling
(`TestGUIViewsMemory`):

| | web process | shell process |
|--|--|--|
| 1 view | 34 to 42 MB | 43 to 47 MB |
| 7 views in one window | 40 to 49 MB | 44 to 48 MB |
| per extra view | about 1 MB | about 0.2 MB |
| per extra window | 3.5 to 8 MB | 1 to 5 MB |

Real pages cost what their DOM and scripts cost; the figure is the per-view
overhead.

## Platform notes

| | Linux (tested) | macOS (untested) | Windows (untested) |
|--|--|--|--|
| Panes | nested `GtkPaned`; children keep their size request, which carries the minimum sizes | nested `NSSplitView`; a delegate enforces minimums | one WebView2 controller per view in the window, bounds from the Go layout |
| Divider drag | native `GtkPaned` handle | native `NSSplitView` divider | the window procedure hit-tests the divider (6 px, with a 3 px grab margin), shows the resize cursor and drags |
| Identity | `webkit_uri_scheme_request_get_web_view` | the `webView` argument of `webView:startURLSchemeTask:` | the sender of `WebResourceRequested`, one registration per controller |
| Web process | related views | one `WKProcessPool` | one `ICoreWebView2Environment` |

A divider drag changes the ratio but fires no event; read it with `layout()`.
