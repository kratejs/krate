package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/native/capability"
	"github.com/kratejs/krate/packages/compiler/internal/native/scaffold"
)

// nativeStubs are the `krate native` subcommands that exist in the command
// surface but are not built yet.
var nativeStubs = []string{"sign", "notarize", "doctor"}

func printNativeUsage(w io.Writer) {
	fmt.Fprintf(w, "Usage: krate native <command> [dir] [flags]\n\n")
	fmt.Fprintf(w, "Build desktop apps from a Krate project (preview).\n\n")
	fmt.Fprintf(w, "Commands:\n")
	fmt.Fprintf(w, "  check      Check host code placement and native target restrictions\n")
	fmt.Fprintf(w, "  init       Create a native app in a directory (--go adds Go host code)\n")
	fmt.Fprintf(w, "  dev        Run the app with hot reload (--browser serves it on a loopback port)\n")
	fmt.Fprintf(w, "  build      Build the app directory (dist-native/app) the shell runs, or with --release\n")
	fmt.Fprintf(w, "             one executable per target (dist-native/<os>-<arch>/<name>)\n")
	fmt.Fprintf(w, "  profile    Run the app, collect a CPU profile and save it for optimized builds\n")
	fmt.Fprintf(w, "  sign       Sign a built app (not implemented yet)\n")
	fmt.Fprintf(w, "  notarize   Notarize a built macOS app (not implemented yet)\n")
	fmt.Fprintf(w, "  doctor     Check the native toolchain (not implemented yet)\n\n")
	fmt.Fprintf(w, "Build flags:\n")
	fmt.Fprintf(w, "  --out <dir>      Output directory (default dist-native); the app is <dir>/app\n")
	fmt.Fprintf(w, "  --target <o/a>   Platform of the Go host, for example linux/arm64\n")
	fmt.Fprintf(w, "  --shell <path>   Copy a shell binary next to the app (\"auto\" builds the monorepo shell\n")
	fmt.Fprintf(w, "                   or uses $KRATE_SHELL)\n")
	fmt.Fprintf(w, "  --release        Link one executable per target (needs Go); --target takes several,\n")
	fmt.Fprintf(w, "                   comma-separated or repeated\n")
	fmt.Fprintf(w, "\nProfile flags:\n")
	fmt.Fprintf(w, "  --out <file>     Profile path (default native.release.pgo, ./native.pgo)\n")
	fmt.Fprintf(w, "  --browser        Run the app in browser mode and print its URL\n")
	fmt.Fprintf(w, "\nDev flags:\n")
	fmt.Fprintf(w, "  --browser        No window: print a tokenized http://127.0.0.1 URL instead\n")
	fmt.Fprintf(w, "  --port <n>       Browser-mode port (default: any free port)\n")
	fmt.Fprintf(w, "  --shell <path>   Shell binary (default: $KRATE_SHELL, or a build of the monorepo shell)\n")
}

func runNative(flags cliFlags, args []string) {
	if len(args) < 2 {
		printNativeUsage(os.Stderr)
		exit(1)
	}
	sub := args[1]
	switch sub {
	case "check":
		runNativeCheck(flags, append([]string{"native"}, args[2:]...))
		return
	case "build":
		runNativeBuild(flags, append([]string{"native"}, args[2:]...))
		return
	case "init":
		runNativeInit(flags, args[2:])
		return
	case "dev":
		runNativeDev(flags, append([]string{"dev"}, args[2:]...))
		return
	case "profile":
		runNativeProfile(flags, append([]string{"native"}, args[2:]...))
		return
	}
	for _, s := range nativeStubs {
		if s == sub {
			fmt.Fprintf(os.Stderr, "%skrate native %s:%s not implemented yet\n", cYellow, sub, cReset)
			exit(2)
		}
	}
	fmt.Fprintf(os.Stderr, "%sUnknown native command:%s %s\n", cRed, cReset, sub)
	printNativeUsage(os.Stderr)
	exit(1)
}

// runNativeCheck loads the config, decides host code placement and checks the
// project against the native target's restrictions.
func runNativeCheck(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	placement, err := native.Analyze(root, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sNative check error:%s %v\n", cRed, cReset, err)
		exit(1)
	}
	target, err := native.CheckTarget(root, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sNative check error:%s %v\n", cRed, cReset, err)
		exit(1)
	}

	if cfg.Native == nil {
		fmt.Printf("%s  ! No `native` block in the config; using defaults.%s\n", cYellow, cReset)
	}
	for _, w := range placement.Warnings {
		fmt.Printf("%s  ! %s%s\n", cYellow, w, cReset)
	}

	var diags []diag.Diagnostic
	diags = append(diags, placement.Diagnostics...)
	diags = append(diags, target...)
	sort.SliceStable(diags, func(i, j int) bool {
		if diags[i].File != diags[j].File {
			return diags[i].File < diags[j].File
		}
		return diags[i].Line < diags[j].Line
	})
	if len(diags) > 0 {
		errs := make([]error, len(diags))
		for i, d := range diags {
			errs[i] = d
		}
		fmt.Fprintf(os.Stderr, "%s\n", diag.FormatDiagnostics(errs))
	}

	printPlacementSummary(placement)

	if res, pd := nativeCapabilities(root, cfg, flags); res != nil {
		fmt.Printf("\ncapabilities\n%s", indentLines(build.FormatNativeCapabilities(res, nativeAllow(cfg)), "  "))
		diags = append(diags, pd...)
		if len(pd) > 0 {
			errs := make([]error, len(pd))
			for i, d := range pd {
				errs[i] = d
			}
			fmt.Fprintf(os.Stderr, "%s\n", diag.FormatDiagnostics(errs))
		}
	}

	if len(diags) > 0 {
		fmt.Fprintf(os.Stderr, "\n%s%s  x Native check failed: %d error(s).%s\n", cBold, cRed, len(diags), cReset)
		exit(1)
	}
	fmt.Printf("\n%s%s  + Native check passed.%s\n", cBold, cGreen, cReset)
}

// printPlacementSummary lists the host modules and their functions.
func printPlacementSummary(p *native.Placement) {
	if len(p.Modules) == 0 {
		fmt.Printf("host modules: none\n")
		return
	}
	fmt.Printf("host modules: %d\n", len(p.Modules))
	for _, m := range p.Modules {
		var names []string
		for _, f := range m.Funcs {
			names = append(names, f.Name)
		}
		kind := "inline"
		if m.Whole {
			kind = string(m.Source)
		}
		line := fmt.Sprintf("  %s (%s)", filepath.ToSlash(m.Rel), kind)
		if len(names) > 0 {
			line += ": " + strings.Join(names, ", ")
		}
		fmt.Println(line)
	}
}

// nativeCapabilities runs the capability analysis for the check commands. It
// returns nil when the analysis cannot run (the error is printed).
func nativeCapabilities(root string, cfg *config.Config, flags cliFlags) (*capability.Result, []diag.Diagnostic) {
	b := build.New(root, cfg)
	b.Verbose = flags.Verbose
	res, ds, err := b.AnalyzeNativeCapabilities()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s  ! capability analysis failed: %v%s\n", cYellow, err, cReset)
		return nil, nil
	}
	return res, ds
}

func nativeAllow(cfg *config.Config) []string {
	if cfg.Native == nil {
		return nil
	}
	return cfg.Native.Permissions
}

func indentLines(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n") + "\n"
}

// runNativeBuild builds the project into an app directory for the shell.
func runNativeBuild(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	defer startRunLog(root, cfg, "native build")()
	env := loadProjectEnv(root, "production", flags.Verbose)

	outDir := flags.NativeOut
	if outDir == "" {
		outDir = "dist-native"
	}
	fmt.Printf("%s%s  Building native app %s -> %s%s\n", cBold, cCyan, root, outDir, cReset)

	start := time.Now()
	builder := build.New(root, cfg)
	builder.Verbose = flags.Verbose
	builder.Env = env
	if flags.Release {
		runNativeRelease(builder, root, outDir, flags, start)
		return
	}
	res, err := builder.BuildNativeApp(build.NativeAppOptions{
		OutDir: outDir, Target: flags.Target, Shell: flags.Shell, WantShell: flags.WantShell, Derive: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %s\n", cRed, cReset, build.BuildErrorSummary(err))
		exit(1)
	}
	for _, n := range res.Notes {
		fmt.Printf("  %s! %s%s\n", cYellow, n, cReset)
	}
	rel := func(p string) string {
		if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
		return p
	}
	m := res.Manifest
	fmt.Printf("\n  %s %s %s (%s)\n", cGreen+"+"+cReset, m.ProductName, m.Version, m.AppID)
	fmt.Printf("    app          %s\n", rel(res.AppDir))
	fmt.Printf("    permissions  %s\n", joinOrNone(m.Permissions))
	if m.HostJS != nil {
		fmt.Printf("    host js      %s\n", m.HostJS.Bundle)
	}
	if m.GoHost != nil {
		fmt.Printf("    go host      %s (%d funcs, %d streams, %d signals)\n", m.GoHost.Binary, len(m.GoHost.Funcs), len(m.GoHost.Streams), len(m.GoHost.Signals))
	}
	if len(m.Fallbacks) > 0 {
		fmt.Printf("    dynamic      %d route(s)\n", len(m.Fallbacks))
	}
	fmt.Printf("\n%s%s  Done! (built in %s)%s\n", cBold, cGreen, time.Since(start).Round(time.Millisecond), cReset)
	if res.ShellPath != "" {
		fmt.Printf("  run it with: %s %s\n", rel(res.ShellPath), rel(res.AppDir))
	} else {
		fmt.Printf("  run it with: krate-shell %s\n", rel(res.AppDir))
	}
}

func joinOrNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, ", ")
}

// runNativeRelease implements `krate native build --release`.
func runNativeRelease(builder *build.Builder, root, outDir string, flags cliFlags, start time.Time) {
	res, err := builder.BuildNativeRelease(build.NativeReleaseOptions{OutDir: outDir, Targets: flags.Targets})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %s\n", cRed, cReset, build.BuildErrorSummary(err))
		exit(1)
	}
	for _, n := range res.Notes {
		fmt.Printf("  %s! %s%s\n", cYellow, n, cReset)
	}
	rel := func(p string) string {
		if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
		return p
	}
	m := res.Manifest
	fmt.Printf("\n  %s %s %s (%s)\n", cGreen+"+"+cReset, m.ProductName, m.Version, m.AppID)
	fmt.Printf("    permissions  %s\n", joinOrNone(m.Permissions))
	if res.Report != nil {
		fmt.Printf("    sdk areas    %s\n", joinOrNone(append([]string{"os", "permission"}, res.Report.Areas.Linked...)))
		if len(res.Report.Areas.Shaken) > 0 {
			fmt.Printf("    left out     %s\n", strings.Join(res.Report.Areas.Shaken, ", "))
		}
	}
	if res.PGO != "" {
		fmt.Printf("    pgo          %s\n", rel(res.PGO))
	}
	if res.Compiled {
		for _, b := range res.Binaries {
			fmt.Printf("    %-12s %s (%s)\n", b.Target, rel(b.Path), build.FormatBytes(b.Size))
		}
	} else {
		fmt.Printf("    app          %s (prebuilt-shell layout; run it with krate-shell)\n", rel(res.AppDir))
	}
	fmt.Printf("    report       %s\n", rel(filepath.Join(res.OutDir, "report.json")))
	fmt.Printf("\n%s%s  Done! (built in %s)%s\n", cBold, cGreen, time.Since(start).Round(time.Millisecond), cReset)
}

// runNativeProfile implements `krate native profile`: it runs the app and
// saves a CPU profile for profile-guided release builds.
func runNativeProfile(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	defer startRunLog(root, cfg, "native profile")()
	env := loadProjectEnv(root, "production", flags.Verbose)
	builder := build.New(root, cfg)
	builder.Verbose = flags.Verbose
	builder.Env = env

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Printf("%s%s  Profiling native app %s%s\n", cBold, cCyan, root, cReset)
	var appArgs []string
	if flags.Browser {
		appArgs = append(appArgs, "--browser")
	}
	res, err := builder.ProfileNative(build.NativeProfileOptions{
		Out: flags.NativeOut, Shell: flags.Shell, Args: appArgs,
		Stdout: os.Stdout, Stderr: os.Stderr, Ctx: ctx,
		OnStart: func(*os.Process) {
			fmt.Printf("  use the app, then close its window (or press Ctrl-C) to write the profile\n")
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sProfile error:%s %s\n", cRed, cReset, build.BuildErrorSummary(err))
		exit(1)
	}
	p := res.Path
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		p = filepath.ToSlash(r)
	}
	fmt.Printf("\n%s%s  Profile saved to %s (%s)%s\n", cBold, cGreen, p, build.FormatBytes(res.Size), cReset)
	fmt.Printf("  the next `krate native build --release` passes it to go build as -pgo\n")
}

// runNativeDev runs the app under the shell with hot reload.
func runNativeDev(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	defer startRunLog(root, cfg, "native dev")()
	defer build.TeeDevLog(root)()

	shellPath, err := build.LocateNativeShell(root, flags.Shell)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Printf("%s%s  Starting native dev for %s%s\n", cBold, cCyan, root, cReset)
	err = build.RunNativeDev(build.NativeDevOptions{
		LoadConfig: func() (string, *config.Config, map[string]string, error) {
			r, c, err := loadConfig(flags, args)
			if err != nil {
				return "", nil, nil, fmt.Errorf("%s", formatConfigError(err))
			}
			return r, c, loadProjectEnv(r, "development", flags.Verbose), nil
		},
		Browser: flags.Browser,
		Port:    flags.Port,
		Shell:   shellPath,
		Target:  flags.Target,
		Verbose: flags.Verbose,
		Ctx:     ctx,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sNative dev error:%s %v\n", cRed, cReset, err)
		exit(1)
	}
}

// runNativeInit scaffolds a minimal native app.
func runNativeInit(flags cliFlags, args []string) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	opts := scaffold.Options{Go: flags.GoHost}
	if abs, err := filepath.Abs(dir); err == nil {
		if mod := build.FindMonorepoNative(abs); mod != "" && flags.GoHost {
			if rel, err := filepath.Rel(abs, mod); err == nil {
				opts.NativeReplace = rel
			}
		}
	}
	files, d := scaffold.Init(dir, opts)
	if d != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %s\n", cRed, cReset, diag.FormatDiagnostics([]error{*d}))
		exit(1)
	}
	fmt.Printf("%s%s  Created a native app in %s%s\n", cBold, cGreen, dir, cReset)
	for _, f := range files {
		fmt.Printf("    %s\n", f)
	}
	fmt.Printf("\n  next:\n")
	if dir != "." {
		fmt.Printf("    cd %s\n", dir)
	}
	fmt.Printf("    npm install\n")
	if flags.GoHost && opts.NativeReplace == "" {
		fmt.Printf("    go get github.com/kratejs/krate/packages/native\n")
	}
	fmt.Printf("    krate native build\n")
}
