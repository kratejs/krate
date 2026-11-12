---
title: The App Directory and app.json
order: 7
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
  host/host.js(.map)  only with TypeScript host code on the embedded runtime
  host/<runtime>/host.mjs(.map)  host code that runs on Node, Bun or Deno
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
  "memory": "balanced",
  "features": { "media": false, "webaudio": false, "webgl": false, "webrtc": false, "spellcheck": false },
  "hostchildren": [{
    "runtime": "node",
    "entry": "host/node/host.mjs",
    "map": "host/node/host.mjs.map",
    "command": ["node", "--enable-source-maps", "{entry}"],
    "funcs": ["src/lib/img.host.resize"],
    "streams": [],
    "signals": []
  }],
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
| `window` | From `native.window`. `title` is the product name. `path` is the first page. `permissions`, when present, narrows the SDK permissions of the main window's page ([per-window scopes](/docs/native/release/#per-window-scopes)). `keepAlive` (default `false`) keeps the app running with no window open: closing the last window releases its webview instead of quitting ([background mode](/docs/native/memory/#windows-and-background-mode)). |
| `assets` | The directory with the prerendered UI, served at `/`. |
| `fallbacks` | One entry per dynamic route that accepts parameters the build did not list: the route `pattern` (`[id]`, `[...slug]`) and the shell page `file` under `assets`. The shell serves `file` for a URL that matches `pattern` and has no file of its own. |
| `hostjs` | Present with TypeScript host code: the host bundle (one script, run in the embedded JavaScript runtime) and its source map. |
| `hostchildren` | One entry per TypeScript host program that runs in a child process on `node`, `bun` or `deno` (see [Host runtimes](/docs/native/runtimes/)): the `entry` script (and its `map`), the `command` that starts it and the ids it serves. In `command`, `{entry}` is the absolute path of the entry and `{sockdir}` the shell's socket directory; a first element without a path separator is looked up on `PATH`. A release replaces the script by an executable: `entry` names it and `command` is absent. The child may also report ids at run time, which the shell registers too. A reload restarts the child. |
| `gohost` | Present with Go host code: the binary, and the registry ids it serves by kind. An id is `<module id>.<export>`. |
| `strictPermissions` | `true` in a build that derived `permissions` from the code: an empty list then means nothing instead of the defaults. |
| `permissions` | Optional list that gates the SDK ids the UI may call: `os:info`, `kv`, `db`, `fs`, `fs:read`, `os:secrets`, `os:secrets.read`, `shell:openExternal`, `net`, `window`, `app`, `menu`, `dialog`, `notify`, `clipboard`, `clipboard:read`, `tray`, `shortcut`, `updater`. Empty means `os:info`, `kv`, `db`, `window`, `app`, `menu`, `dialog`, `notify` and `clipboard`; unknown names are an error when the app loads. See [Native SDK](/docs/native/sdk/#permissions). |
| `plugins` | The names of the [native plugins](/docs/native/plugins/) the app uses. The shell that runs the app has to link them (the one a build makes for an app with plugins does); the prebuilt shell refuses such an app. Their permissions are listed in `permissions` as `plugin:<plugin>.<permission>`. |
| `singleInstance` | `true` when `native.fileAssociations` is set: the shell takes the single-instance lock at start, so a file opened while the app runs reaches the running instance ([Files](/docs/native/files/#opening-files)). |
| `updater` | Present when `native.updater` is configured: `url` (the feed URL template), `publicKey` (base64 Ed25519) and the optional `channel`, `maxStarts`, `healthTimeout` and `confirm`. See [Updates](/docs/native/updater/). |
| `memory` | The memory profile, from `native.memory`: `balanced` (default), `minimal` or `performance`. See [Memory](/docs/native/memory/). |
| `features` | `{media, webaudio, webgl, webrtc, spellcheck}`: which optional webview features the UI uses, found by scanning the built UI and adjusted by `native.features`. The `balanced` and `minimal` profiles switch the others off. An `app.json` without it keeps every feature on. |
| `media` | From `native.media`: `{policy: {camera, microphone, screen, location, notifications, clipboardRead}, noRemember, uses}`. Each policy is `prompt`, `allow` or `deny`. `uses` lists what the built UI asks the webview for (`camera`, `microphone`, `screen`, `location`); packaging reads it, the shell does not. See [Camera, microphone and location](/docs/native/media/). |
| `hostjs.memoryMB` | From `native.host.memoryMB`: the QuickJS heap limit in MiB. Absent means the profile default (64, 32 or 256). |
| `debug` | `true` for a build made by `krate native dev` and for a release build with `native.debug: true`; `false` otherwise. It turns the webview's developer tools on; see [Developer tools](/docs/native/dev/#developer-tools). |

The build manifest of the web build (`dist/.krate`) is removed from `dist/`.
