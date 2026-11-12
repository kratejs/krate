---
title: Files and Drag and Drop
order: 12
description: File associations in the installers, opening the files an app is launched with or handed by a second launch, and the real paths of files dropped from the desktop onto a window.
---

# Files and drag and drop

> **Preview.** Part of [Krate Native](/docs/native/). Linux (WebKitGTK) is the
> platform where this is tested, including a real drag-and-drop between two
> GTK windows and a second launch handing a file to the running app. The macOS
> (`WKWebView`) and Windows (WebView2) implementations compile and their logic
> is unit-tested, and the CI native job runs their GUI tests, but they have
> not been confirmed on those systems. The [compatibility
> matrix](/docs/guides/compatibility/) has the per-platform status.

Three things make an app a good citizen for documents:

1. the installer tells the operating system which file types the app opens
   (`native.fileAssociations`);
2. the app learns which files it was asked to open, at launch and later
   (`onOpenFile`, `openedFiles`);
3. the page learns the real paths of files dropped on it (`onFileDrop`).

A file the user opens or drops is **granted** to the app: with the `fs`
permission the page can read it, exactly as it can read a file chosen in a
[dialog](/docs/native/sdk-ui/). Nothing else outside the app directories becomes
readable.

## File associations

```ts
export default defineConfig({
  native: {
    appId: "com.acme.notes",
    productName: "Acme Notes",
    fileAssociations: [
      { ext: ["md", "markdown"], name: "Markdown document", mime: "text/markdown", role: "editor", icon: "assets/md.png" },
      { ext: ["log"], role: "viewer" },
    ],
  },
});
```

| Key | Meaning |
|-----|---------|
| `ext` | Extensions without the dot, required. Each is registered once; the match is case-insensitive |
| `name` | Display name of the type. Default: `<EXT> file` |
| `mime` | Media type. Default: `application/x-<app>-<first extension>`. Give the real one for a well-known type |
| `role` | `editor` (default) or `viewer` |
| `icon` | A PNG or JPEG for files of this type, relative to the project. Default: the app icon |

The config check rejects extensions with a dot or wildcard, duplicates across
entries, malformed media types, other roles, and the extensions of programs and
shortcuts (`exe`, `bat`, `lnk`, ...), which an app cannot claim. Each message
carries a hint. A typo inside an entry warns like any other config key.

An app with file associations also takes the [single-instance
lock](#opening-files) automatically (`app.json` gets `"singleInstance": true`
and the release build links the `app` SDK area even if no page calls it).

`krate native package` turns the list into:

| Platform | Output |
|----------|--------|
| macOS | `CFBundleDocumentTypes` in `Info.plist` (name, role, `LSHandlerRank`, extensions, media type, `CFBundleTypeIconFile`) and, for a type macOS does not declare itself, a `UTExportedTypeDeclarations` entry `<appId>.filetype.<ext>` conforming to `public.data`. About thirty common extensions (`txt`, `json`, `csv`, `pdf`, `png`, `yaml`, ...) use the system UTI and export nothing. A type with its own `icon` gets `Resources/filetype-<ext>.icns`. |
| Windows, MSIX | One `windows.fileTypeAssociation` extension per type in `AppxManifest.xml`, with the extensions (each with its `ContentType`), the display name and the logo. |
| Windows, NSIS | ProgID `<appId>.file.<ext>` with `DefaultIcon` and `shell\open\command`; `HKCR\.<ext>\OpenWithProgids`; and, for an `editor`, the default handler of an extension nobody handles yet. The uninstaller removes exactly what the installer wrote and refreshes the shell. The icon is `filetype-<ext>.ico` next to the executable. |
| Linux | `MimeType=` in the `.desktop` file (with `%F`, or `%U` when the app also has `deepLinks`) and `usr/share/mime/packages/<appId>.xml`, a shared-mime-info file with the globs, in the `.deb`, the `.rpm` and the AppImage's AppDir. |

What is not done: an installed app is not set as the *default* handler of a
type another program owns (Windows and macOS keep that choice with the user),
a `viewer` role has no Windows equivalent, and the Linux packages ship no
maintainer scripts (the distributions' triggers refresh the MIME and desktop
databases).

## Opening files

A file reaches the app when the user double-clicks it, chooses "Open with", runs
`app file.md`, or drops it on the Dock icon.

| Where | How the app hears of it |
|-------|-------------------------|
| First launch, Windows and Linux | The file is on the command line (Linux launchers pass `%F` or `%U`; `file://` URLs are converted to paths) |
| First launch, macOS | Launch Services calls the app delegate's `application:openURLs:`, even before the app finished starting; those calls are held until the SDK listens |
| App already running, Windows and Linux | The new process takes the [single-instance](/docs/native/sdk-ui/) lock, finds the first one and hands over its command line and working directory, then exits. The running app raises its window |
| App already running, macOS | The system sends `application:openURLs:` to the running app |

```ts
import { onOpenFile, openedFiles } from "@krate/app/app";
import { readText } from "@krate/app/fs";

const stop = onOpenFile(async (path) => {
  const text = await readText(path); // allowed: the user opened this file
  show(path, text);
});
const all = await openedFiles();    // every file opened since the app started, oldest first
```

`onOpenFile` calls the function once per file. By default it first replays the
files opened before the call, so a document that launched the app is not
missed whenever the page gets round to listening; pass `{ replay: false }` for
new files only. Both need the `app` permission. A page that reloads gets the
launch files again.

```go
import "github.com/kratejs/krate/packages/native/sdk/app"

files, _ := app.OpenedFiles()
app.OnOpenFile(func(path string) { open(path) }) // replays, then new files
```

Ids: `krate/app.openedFiles` (call) and `krate/app.openFileEvents` (stream of
`{paths, index, source}` where `source` is `launch`, `second-instance` or
`system`).

**What counts as a file.** `app.FilesFromArgs(args, dir)` is the parser used for
command lines: it drops flags, deep links and other URLs, resolves relative
paths against the other launch's working directory, converts `file://` URLs, and
keeps only existing regular files, each once. A directory argument is not an
opened file. At most 4096 files are kept per run.

**Command line of the prebuilt shell.** `krate-shell [flags] [appDir] [files...]`
treats the first argument as the app directory only when it holds an
`app.json`; otherwise every argument is a file or URL to open and the app is the
one next to the binary. The `-psn_<n>` argument older macOS versions pass is
ignored. A release build is `app file.md`.

**Access.** The same grant a dialog pick gets: the path (a file, not its folder)
becomes readable through `@krate/app/fs` and `sdk/fs`, and nothing else. The
grant lasts until the app exits. Go host code is trusted and never needs it.

**macOS deep links.** `application:openURLs:` also carries URLs of the schemes in
`deepLinks`; those reach `onOpenURL` like the ones a second launch carries.

## Drag and drop

```ts
import { onFileDrop } from "@krate/app/window";

const stop = onFileDrop(({ paths, x, y }) => {
  for (const p of paths) importFile(p); // absolute paths of files and folders
});
```

A web page never sees the path of a dropped file. The shell adds it: when the
user drops files from the desktop onto a view, `onFileDrop` fires with their
absolute paths and the drop position in CSS pixels. It needs the `window`
permission. By default it reports drops onto *this* page's view only (a window
with [several views](/docs/native/views/) has one per pane); `{ all: true }`
reports every view's drops, with `view` and `window` ids. In Go,
`window.OnFileDrop(fn)` receives every drop. Ids: `krate/window.fileDrops`
(stream) and `krate/window.thisView` (which view the caller is).

The page keeps its **normal HTML5 drop events** with the `File` objects; the
shell only watches. Dropped paths are granted like [opened files](#opening-files).

| Platform | How the paths are read |
|----------|------------------------|
| Linux | The GTK `drag-data-received`, `drag-drop` and `drag-leave` signals of the web view are watched, never stopped. WebKitGTK asks the source for the `text/uri-list` while the drag hovers; the watcher keeps that list and reports it at the drop, or requests it itself when WebKit did not. |
| macOS | The web view is a `WKWebView` subclass whose `performDragOperation:` reads the dragging pasteboard's file URLs (file URLs only) and then calls WebKit's own implementation. |
| Windows | A script injected into the app's pages posts the dropped `File` objects with `chrome.webview.postMessageWithAdditionalObjects` from a capture-phase `drop` listener; the host reads `ICoreWebView2File.Path` from the message's additional objects. Needs a WebView2 Runtime that has `ICoreWebView2WebMessageReceivedEventArgs2`; without it a log line says so and no path is reported. |

Limits: coordinates are widget pixels on Linux and CSS pixels elsewhere, which
only differ on scaled displays; a `File` made by script has no path and is
skipped; on Linux a drag from a Flatpak or Snap application can hand over
document-portal paths instead of the original ones.

## Testing

`go test` covers the packaging output (plist, manifest, registry script,
`.desktop`, MIME XML), the config checks, argument parsing, the SDK event
flow with an in-memory platform, and the injected script (under Node).
`examples/native-files` is a small app that lists the files it was opened with
and the ones dropped on it; the end-to-end test starts the real shell with a
file argument, starts it again to hand over a second file, and sends a drop
through the dev control channel's `drop` command. On Linux, a GUI test drags a
file between two GTK windows with synthesized pointer events (`xdotool`, under
a window manager) and checks that the platform reports the path and the page
still gets its drop event.

Things only a person can check are listed in `platform/darwin/MANUAL_TEST.md`
and `platform/windows/MANUAL_TEST.md`: a real Finder double click, a drag from
Explorer, an installed package's "Open with" list.
