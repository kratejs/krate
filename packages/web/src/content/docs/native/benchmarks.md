---
title: Benchmarks
order: 22
description: Krate Native against Electron and Tauri on one small app - cold start, idle memory, installer and disk size, with Linux numbers and a CI workflow.
---

# Benchmarks

> **Preview.** Part of [Krate Native](/docs/native/). The harness runs on Linux,
> macOS and Windows in CI. The numbers below were measured on Linux only, on a
> shared developer machine (see [Caveats](#caveats)). Read them as a first data
> point, not as a verdict.

The same small app is built three ways: a Krate Native app (TSX page plus a Go
host function), an Electron app (the same page as HTML and JavaScript,
`contextIsolation` on, a preload script exposing two functions) and a Tauri v2
app (the same page, two Rust commands). The page shows a list of 100 items and a
button that calls host code. Everything lives in `packages/benchmark/native/`,
standalone: it is not a pnpm workspace package, so installing the repository does
not install it.

## What is measured

| Metric | How |
|--------|-----|
| Cold start | The harness takes the wall clock just before it spawns the app. The page observes `first-contentful-paint` and sends `performance.timeOrigin + startTime` to the host (a host call in Krate, `invoke` in Tauri, IPC in Electron). Cold start is the paint time minus the spawn time, so it covers process start, runtime and webview start-up, page load and first paint. |
| Idle memory | Five seconds after that first paint the harness samples the whole process tree, helpers included: the shell and its WebKit web and network processes for Krate, the browser, renderer, GPU, zygote and utility processes for Electron, the app and its webview processes for Tauri. |
| Installer size | The files each tool produces for a user to download: `deb` and AppImage on Linux, `dmg` on macOS, an NSIS installer on Windows. |
| On-disk size | The app as installed, without the installer: the release directory (Krate), the unpacked app directory (Electron), the release binary or `.app` bundle (Tauri). |

Memory numbers come from `packages/native/memstat`, the code behind
`krate native profile --memory`, through the small `krate-memtree` command, so
all three apps are read the same way:

| OS | Number | Notes |
|----|--------|-------|
| Linux | PSS from `/proc/<pid>/smaps_rollup`, summed over the tree | Shared libraries are split between the processes that map them, so the sum is the real footprint. Plain RSS sums are in the JSON and overcount heavily. |
| macOS | `phys_footprint` per process (what Activity Monitor shows) | WebKit runs in XPC services that are not children of the app. For Krate and Tauri the harness adds the `com.apple.WebKit.*` services started since the launch; on a CI runner nothing else uses WebKit. |
| Windows | Private working set, summed over the tree (from PowerShell, `Win32_PerfFormattedData_PerfProc_Process`) | If that query fails the harness falls back to private bytes from `memstat`. |

The JSON also holds every run, the process names and `krate native profile
--memory` for the Krate app, as a cross-check.

## Linux numbers

Measured on **Debian 13 (Linux 6.12), AMD EPYC-Rome, 12 vCPUs, 31 GB RAM**, a
shared machine with other builds running. Display: Xvfb with openbox, so
**software rendering**. Ten runs per app, memory read 5 s after the first
paint. Versions: Krate Native at commit `eecdafa0` with WebKitGTK 2.54 (Debian
13 packages), Electron 45.3.1 (Chromium 156) packaged with electron-builder
26.18.0, Go 1.26.1. Tauri is not in this table: the machine has no WebKitGTK
development files, so the Tauri app could only be type-checked here (against the
Windows target); CI builds and measures it.

| App | Cold start, median (range) | Idle memory, PSS, median (range) | Processes | Installer | On disk |
|-----|---------------------------|----------------------------------|-----------|-----------|---------|
| Krate Native | 394 ms (337 to 718) | 135 MiB (97 to 181) | 3 | deb 3.8 MiB | 9.1 MiB |
| Electron | 274 ms (246 to 394) | 193 MiB (192 to 199) | 6 | deb 96.7 MiB, AppImage 119.8 MiB | 273.5 MiB |
| Tauri v2 | not measured here | | | | |

What this does and does not say:

- **Size** is the clear result. The Krate release executable is the Go shell with
  the app embedded; the system's WebKitGTK is a package dependency, not part of
  the download. Electron ships Chromium. Tauri also uses the system webview, and
  on Windows and macOS so does Krate (WebView2 and WKWebView are part of or
  installed beside the OS), so the comparison with Electron is about what the
  installer has to carry, not about what the machine has to hold.
- **Cold start** went the other way on this machine: Electron started faster
  than Krate under Xvfb here. The in-page first paint is 20 to 130 ms for Krate;
  most of the time is the process, the Go runtime, the WebKit process spawn and
  the page load. The first batch of runs, taken while other builds ran, had
  Krate between 346 and 1220 ms. The recorded budget for the example app
  (`packages/native/budgets.json`) is 392 ms on a quieter run. Expect this to
  move; the point of the CI job is to show it on clean runners.
- **Memory** favours Krate by about 58 MiB of PSS here, with a wide range. PSS
  divides shared pages by the number of processes that map them, so the Krate
  figure drops when other WebKit processes are running on the same host (as they
  were) and is higher on a quiet one. The same app measured with `krate native
  profile --memory` (dev shell) reads 173 MiB, 10 MiB above a blank-page floor.

## Caveats

- **Software rendering.** Under Xvfb there is no GPU. Chromium's GPU process
  fails to start and falls back to software compositing; WebKitGTK renders in
  software. On real hardware both change, and the GPU process adds memory.
- **Electron's sandbox is off on Linux** (`--no-sandbox`), because CI images and
  this machine do not give Chromium a setuid helper or user namespaces. It does
  not change the process count noticeably.
- **Cold start is not a cold disk cache.** The apps were just built or
  installed, so their files are in the page cache. The harness reports every
  run, not only the first, so you can see the first-run effect; dropping the OS
  cache needs root and is not done.
- **The clock is the spawn time.** Process start is taken as the moment before
  `spawn`, which adds well under a millisecond. Paint time comes from the
  page's own clock; both are wall clocks on one machine.
- **Same page, different toolkits.** The Krate page is TSX compiled to HTML and
  a small hydration script; Electron and Tauri load a static HTML file and a
  script that builds the list. The Krate host function is Go, linked into the
  shell executable; the Electron one runs in the main process (Node) and the
  Tauri one in Rust.
- **Tauri release profile.** The Tauri crate builds with `lto`, `opt-level = "s"`,
  `panic = "abort"` and `strip`, the settings its own documentation recommends
  for small binaries.
- **Not covered:** warm start of a second window, memory under load, update
  size, battery. Idle memory is a floor, not a typical session.

## Run it yourself

```sh
# from the repository root, after pnpm install and pnpm build
node packages/benchmark/native/bench.mjs --runs 5 --idle 5
```

It builds the `krate` CLI and `krate-memtree`, then builds, packages and runs
each app, and writes `results/native-bench-<os>-<arch>.json` and `.md`. Useful
flags: `--apps krate,electron,tauri`, `--out <dir>`, `--skip-build`,
`--no-package`.

Requirements: Go and Node 22; Rust for Tauri (and on Linux the WebKitGTK 4.1,
GTK 3 and libsoup 3 development packages); `makensis` on Windows for the Krate
NSIS installer; `appimagetool` on Linux for a Krate AppImage. On Linux run it
under a display (`xvfb-run -a node ...`). The Electron and Tauri dependencies are
pinned by their own lockfiles in `packages/benchmark/native/electron` and
`.../tauri` (`Cargo.lock` included).

## Numbers for all three systems

The `Native benchmark` workflow (`.github/workflows/native-bench.yml`) runs the
harness on `ubuntu-latest`, `macos-latest` and `windows-latest`: on demand, every
Monday, and on pull requests that touch the benchmark. It installs Rust and the
Tauri Linux packages, runs Linux under Xvfb, writes the table to the job summary
and uploads the JSON, the Markdown and the app logs as the `native-bench-<os>`
artifact. It is informational: it is not a required check, and a failed app
shows up as a failed row.

Hosted runners are shared virtual machines, so compare the apps within one run
and look at the range, not at a single number from one run to the next.
