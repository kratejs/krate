---
title: The App Directory and app.json
order: 5
description: The output of krate native build - the app directory layout and every field of app.json, the manifest the Krate Native shell reads.
---

# The App Directory and app.json

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.
> Field names are fixed; fields may be added.

`krate native build` writes `dist-native/app/`:

```
app/
  app.json
  dist/...            the prerendered UI
  host/host.js(.map)  only with TypeScript host code
  gohost/krate-gohost[.exe]  only with Go host code
```

The shell runs it with `krate-shell dist-native/app`. Everything the shell needs
is in the directory; paths in `app.json` are relative to it.

```json
{
  "appId": "com.acme.app",
  "productName": "Acme",
  "version": "1.0.0",
  "window": { "title": "Acme", "width": 1280, "height": 800, "resizable": true, "path": "/" },
  "assets": "dist",
  "fallbacks": [{ "pattern": "/tasks/[id]", "file": "tasks/[id]/index.html" }],
  "hostjs": { "bundle": "host/host.js", "map": "host/host.js.map" },
  "gohost": {
    "binary": "gohost/krate-gohost",
    "funcs": ["src/lib/tasks.List"],
    "streams": ["src/lib/tasks.Watch"],
    "signals": ["src/lib/tasks.Count"]
  },
  "debug": false
}
```

| Field | Meaning |
|-------|---------|
| `appId`, `productName`, `version` | From the `native` config block. Defaults when unset: an id derived from the product name (with a note), the project directory name, `0.1.0`. |
| `window` | From `native.window`. `title` is the product name. `path` is the first page. |
| `assets` | The directory with the prerendered UI, served at `/`. |
| `fallbacks` | One entry per dynamic route that accepts parameters the build did not list: the route `pattern` (`[id]`, `[...slug]`) and the shell page `file` under `assets`. The shell serves `file` for a URL that matches `pattern` and has no file of its own. |
| `hostjs` | Present with TypeScript host code: the host bundle (one script, run in the embedded JavaScript runtime) and its source map. |
| `gohost` | Present with Go host code: the binary, and the registry ids it serves by kind. An id is `<module id>.<export>`. |
| `permissions` | Optional list that gates the SDK ids the UI may call: `os:info`, `kv`, `db`, `fs`, `fs:read`, `os:secrets`, `os:secrets.read`, `shell:openExternal`, `net`, `window`, `app`, `menu`, `dialog`, `notify`, `clipboard`, `clipboard:read`, `tray`, `shortcut`, `updater`. Empty means `os:info`, `kv`, `db`, `window`, `app`, `menu`, `dialog`, `notify` and `clipboard`; unknown names are an error when the app loads. See [Native SDK](/docs/native/sdk/#permissions). |
| `updater` | Present when `native.updater` is configured: `url` (the feed URL template) and `publicKey` (base64 Ed25519). See [Updates](/docs/native/updater/). |
| `debug` | `false` for a build made by `krate native build`. |

The build manifest of the web build (`dist/.krate`) is removed from `dist/`.
