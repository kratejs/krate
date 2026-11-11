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
| `startup.<platform>.coldStartMs` | process start to the first page's load event of the `native-tasks` release executable, median of three runs, in ms | `TestNativeBudgetGUI` | `KRATE_NATIVE_GUI=1`, a display and WebKitGTK |
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

Only `linux-amd64` has entries. macOS and Windows sizes are not tracked
because no CI job measures them. The start-up test builds the executable with
the `krate_devcontrol` tag, which it needs to read the page; that adds a few
kilobytes to the size, so the size entries come from a build without it.

Each entry has `measured` (what the last update saw) and `max` (`measured` plus
`headroom`, 25 percent). The tests compare against `max`.

## Running

```sh
cd packages/compiler
go test ./internal/build -run TestNativeBudgetSizes -v

# GUI budgets, on Linux under Xvfb (xvfb-run -a) or any display
KRATE_NATIVE_GUI=1 go test ./internal/build -run TestNativeBudgetGUI -v
```

`KRATE_BUDGET_SCALE=2` multiplies the cold-start limit for a slower machine.
CI sets it to 3 for the hosted runner; the limit was measured on a developer
machine and the hosted runner has not been measured yet. The memory limits are
not scaled: app minus floor does not depend on the machine the way an absolute
number does. The same measurement is available outside the tests as
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
