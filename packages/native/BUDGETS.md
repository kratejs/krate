# Budgets

`budgets.json` holds the size, start-up and memory limits of Krate Native.
Two Go tests in `packages/compiler/internal/build` (`native_budget_test.go`)
fail when a measurement is over its limit.

| Entry | Measures | Test | Needs |
| --- | --- | --- | --- |
| `sizes.<platform>.shell` | `krate-shell`, built the way the platform packages build it (`CGO_ENABLED=0 -trimpath -ldflags "-s -w"`), in bytes | `TestNativeBudgetSizes` | Go |
| `sizes.<platform>.goOnlyRelease` | release executable of an app with Go host code only | `TestNativeBudgetSizes` | Go |
| `sizes.<platform>.tsOnlyRelease` | release executable of an app with TypeScript host code only (QuickJS linked) | `TestNativeBudgetSizes` | Go |
| `sizes.<platform>.nativeTasksRelease` | release executable of `examples/native-tasks` | `TestNativeBudgetSizes` | Go, `pnpm install && pnpm build` |
| `startup.<platform>.coldStartMs` | process start to the first page's load event of the `native-tasks` release executable, fastest of three runs, in ms | `TestNativeBudgetGUI` | `KRATE_NATIVE_GUI=1`, a display and WebKitGTK, or a macOS or Windows desktop |
| `memory.<platform>.appMinusFloor.<role>.<metric>` | memory of the `native-tasks` release executable minus the floor, 5 s after the page loaded; **enforced** | `TestNativeBudgetGUI` | same |
| `memory.<platform>.app.<role>.<metric>`, `...floor...` | the absolute values of the app and of the floor; **warn only** | `TestNativeBudgetGUI` | same |

Memory is measured with `packages/native/memstat` through the shell's `memory`
dev control command (the same code behind `krate native profile --memory`). The
metrics are `pss` and `privateDirty`, in bytes: PSS counts each shared page once,
split between the processes that map it; private dirty is memory only that
process holds. Summed resident size is not used because it counts the shared
WebKit libraries once per process. `role` is `total`, `shell`, `gohost`,
`webview:web`, `webview:network`, `webview:gpu` and so on. The **floor** is the
same shell on a generated blank one-line page in the same window size with no
host code; that is what the system webview costs by itself, and it changes with
the WebKit version and the runner. `appMinusFloor` is what Krate and the app add
on top, so it is the part a budget can hold steady; it can be negative for one
role. The test runs app and floor three times and compares the median of each
value.

Memory limits are `measured` plus the headroom, but never closer than 8 MiB
(private dirty) or 16 MiB (PSS) to it: single runs on a loaded machine vary by
more than 25 percent of a small delta. An absolute value over its limit prints a
warning (and a GitHub Actions `::warning::` line); an `appMinusFloor` value over
its limit fails the test. A role that was not measured counts 0.

Only `linux-amd64` has entries so far, but the machinery accepts any
`<goos>-<goarch>` key (`darwin-arm64`, `windows-amd64`, `windows-arm64`,
`linux-arm64`, ...) with the same rule: once a platform has entries in a
section, each measured value is compared with its `max` and a value over it
fails (absolute memory values only warn, as above). A platform with **no**
entries in a section is not enforced yet: it runs the same tests, never fails
for a missing entry, and prints what it measured in the form to paste (see
"Recording a new platform" below). `linux-amd64` is the exception and keeps
failing on a missing entry, so a renamed metric cannot go unnoticed there. The
`native` job also still measures macOS and Windows memory with
`krate native profile --memory`, and `scripts/native-budget-compare.mjs` prints
it next to the linux-amd64 limits. The start-up test builds the executable with
the `krate_devcontrol` tag, which it needs to read the page; that adds a few
kilobytes to the size, so the size entries come from a build without it.

Each entry has `measured` (what the last update saw) and `max` (`measured` plus
`headroom`, 25 percent). The tests compare against `max`.

## Running

```sh
cd packages/compiler
KRATE_NATIVE_BUDGETS=1 go test ./internal/build -run TestNativeBudgetSizes -v

# GUI budgets, on Linux under Xvfb (xvfb-run -a) or any display
KRATE_NATIVE_GUI=1 go test ./internal/build -run TestNativeBudgetGUI -v
```

`KRATE_BUDGET_SCALE=2` multiplies the cold-start limit for a slower machine.
CI sets it to 3 for the hosted runner; the limit was measured on a developer
machine and the hosted runner has not been measured yet. The memory limits are
not scaled; they are measured where they are enforced. The `linux-amd64` memory
entries come from CI's GitHub `ubuntu-24.04` runner (WebKitGTK 2.52.6, Mesa
software GL with compositing on, run 43117981305). App minus floor still
depends on the rendering stack: on the private WebKitGTK 2.54 sysroot used for
local runs (compositing off) it is about a third of the runner's, so local runs
stay well under these limits and only the runner catches growth. The same measurement is available outside the tests as
`krate native profile --memory` (see the release page of the docs).

## Updating a budget

Raise a limit only for growth you intend (a new SDK area, a bigger engine, a
Go toolchain upgrade). Lower it after a real reduction.

```sh
cd packages/compiler
KRATE_UPDATE_BUDGETS=1 go test ./internal/build -run TestNativeBudgetSizes -v
KRATE_NATIVE_GUI=1 KRATE_UPDATE_BUDGETS=1 go test ./internal/build -run TestNativeBudgetGUI -v
git diff ../native/budgets.json
```

Update mode rewrites `measured` and `max` for every entry the run measured and
`measuredWith` (the Go version) for the size run; the GUI update replaces all of
the platform's `memory` entries, so a role that is gone disappears. Start-up and
memory numbers are noisy: run the GUI update on an idle machine, and rerun it if
one of the three `first page load event` lines is far from the other two or the
three memory runs disagree by more than a few MB. Commit the new
`budgets.json` with the change that caused it and say why in the commit
message.

## Recording a new platform

`darwin-arm64`, `windows-amd64`, `windows-arm64` and `linux-arm64` run
`TestNativeBudgetSizes` and `TestNativeBudgetGUI` in the CI `native` job and
report instead of enforcing. Each run ends the test with a block like

```
BUDGET-RECORD-BEGIN windows-amd64
{
  "sizes": { "windows-amd64": { "shell": { "measured": 15990784, "max": 19988480 } } },
  "startup": { "windows-amd64": { "coldStartMs": { "measured": 640, "max": 800 } } },
  "memory": { "windows-amd64": { "appMinusFloor.total.pss": { ... } } }
}
BUDGET-RECORD-END windows-amd64
```

on stdout (find it with the markers in the step log) and in the job summary.
Memory values and sizes use the same headroom and slack rules as update mode,
so the fragment can be merged into `budgets.json` by hand: copy each platform
key under the matching `sizes`, `startup` and `memory` section. Run the job
more than once and take the larger measured values when they differ. From then
on the platform is enforced. `KRATE_UPDATE_BUDGETS=1` still rewrites the entries
of the platform the run is on.
