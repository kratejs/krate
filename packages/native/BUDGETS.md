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
| `startup.<platform>.rssShellKB` | resident memory of the shell process after the page has loaded | `TestNativeBudgetGUI` | same |
| `startup.<platform>.rssTotalKB` | the shell plus its direct children (the WebKit network and web processes) | `TestNativeBudgetGUI` | same |

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

`KRATE_BUDGET_SCALE=2` multiplies the start-up limits for a slower machine.
CI sets it to 3 for the hosted runner; the limits were measured on a
developer machine and the hosted runner has not been measured yet.

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
`measuredWith` (the Go version) for the size run. Start-up numbers are noisy:
run the GUI update on an idle machine, and rerun it if one of the three
`first page load event` lines is far from the other two. Commit the new
`budgets.json` with the change that caused it and say why in the commit
message.
