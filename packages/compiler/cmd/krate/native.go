package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/native/capability"
	"github.com/kratejs/krate/packages/compiler/internal/native/runtimes"
	"github.com/kratejs/krate/packages/compiler/internal/native/scaffold"
)

// nativeStubs are the `krate native` subcommands that exist in the command
// surface but are not built yet.
var nativeStubs = []string{}

func printNativeUsage(w io.Writer) {
	fmt.Fprintf(w, "Usage: krate native <command> [dir] [flags]\n\n")
	fmt.Fprintf(w, "Build desktop apps from a Krate project (preview).\n\n")
	fmt.Fprintf(w, "Commands:\n")
	fmt.Fprintf(w, "  check      Check host code placement and native target restrictions\n")
	fmt.Fprintf(w, "  init       Create a native app in a directory (--go adds Go host code)\n")
	fmt.Fprintf(w, "  dev        Run the app with hot reload (--browser serves it on a loopback port)\n")
	fmt.Fprintf(w, "  build      Build the app directory (dist-native/app) the shell runs, plus a runnable\n")
	fmt.Fprintf(w, "             dist-native/<os>-<arch>/<name> + app/ when a prebuilt shell is found; with --release\n")
	fmt.Fprintf(w, "             one executable per target (dist-native/<os>-<arch>/<name>)\n")
	fmt.Fprintf(w, "  profile    Run the app, collect a CPU profile and save it for optimized builds\n")
	fmt.Fprintf(w, "  package    Make installers and update files: .app/.zip/.dmg, .zip/NSIS/MSIX, .tar.gz/.deb/.rpm/AppImage\n")
	fmt.Fprintf(w, "  sign       Sign the packaged app (codesign, signtool/osslsigncode, gpg) and rebuild the archives\n")
	fmt.Fprintf(w, "  notarize   Notarize and staple the signed macOS app\n")
	fmt.Fprintf(w, "  keygen     Create the Ed25519 key pair that signs update files\n")
	fmt.Fprintf(w, "  doctor     Check the Go toolchain, shell binary, system webview, packaging tools and host runtime (--json)\n\n")
	fmt.Fprintf(w, "Build flags:\n")
	fmt.Fprintf(w, "  --out <dir>      Output directory (default dist-native); the app is <dir>/app\n")
	fmt.Fprintf(w, "  --target <o/a>   Platform of the Go host, for example linux/arm64\n")
	fmt.Fprintf(w, "  --shell <path>   Copy a shell binary next to the app. Without it the shell is $KRATE_SHELL or the\n")
	fmt.Fprintf(w, "                   krate-shell next to the krate executable (\"auto\" also builds the checkout's shell)\n")
	fmt.Fprintf(w, "  --layout <l>     \"shell\" keeps the prebuilt shell plus a Go host child; \"executable\" links one process.\n")
	fmt.Fprintf(w, "                   Default: executable when the app has Go host code and Go is installed, shell otherwise\n")
	fmt.Fprintf(w, "  --release        Link one executable per target (needs Go); --target takes several,\n")
	fmt.Fprintf(w, "                   comma-separated or repeated\n")
	fmt.Fprintf(w, "  --package        Package the build for --target; with --release, each of the targets (see: krate native package)\n")
	fmt.Fprintf(w, "\nPackage flags: --format, --sign, --notarize, --identity, --keychain-profile, --pfx, --thumbprint,\n")
	fmt.Fprintf(w, "  --gpg-key, --notes, --update-base-url, --key-out (see docs/native/packaging)\n")
	fmt.Fprintf(w, "\nProfile flags:\n")
	fmt.Fprintf(w, "  --out <file>     Profile path (default native.release.pgo, ./native.pgo)\n")
	fmt.Fprintf(w, "  --browser        Run the app in browser mode and print its URL\n")
	fmt.Fprintf(w, "  --memory         Measure memory instead: PSS and private dirty per process role next to a blank-page\n")
	fmt.Fprintf(w, "                   floor, after load, idle, a navigation storm and with the window hidden; written to\n")
	fmt.Fprintf(w, "                   <out>/report.json (--out is then the native output directory, default dist-native)\n")
	fmt.Fprintf(w, "  --idle <dur>     With --memory, the idle wait before the second sample (default 30s)\n")
	fmt.Fprintf(w, "\nCheck flags:\n")
	fmt.Fprintf(w, "  --scriptc        Also run scriptc coverage on the host code (experimental)\n")
	fmt.Fprintf(w, "  --scriptc-fail-on <level>  blockers (default), divergences, dynamic, dynamic-slots or type-claims\n")
	fmt.Fprintf(w, "\nDev flags:\n")
	fmt.Fprintf(w, "  --browser        No window: print a tokenized http://127.0.0.1 URL instead\n")
	fmt.Fprintf(w, "  --port <n>       Browser-mode port (default: any free port)\n")
	fmt.Fprintf(w, "  --shell <path>   Shell binary (default: $KRATE_SHELL, the krate-shell next to krate, or a build of the checkout shell)\n")
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
	case "package":
		runNativePackage(flags, append([]string{"native"}, args[2:]...))
		return
	case "sign":
		runNativeSign(flags, append([]string{"native"}, args[2:]...))
		return
	case "notarize":
		runNativeNotarize(flags, append([]string{"native"}, args[2:]...))
		return
	case "keygen":
		runNativeKeygen(flags)
		return
	case "doctor":
		runNativeDoctor(flags, append([]string{"native"}, args[2:]...))
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

	lean, err := native.CheckLean(root, cfg, placement)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sNative check error:%s %v\n", cRed, cReset, err)
		exit(1)
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

	if len(lean) > 0 {
		errs := make([]error, len(lean))
		for i, d := range lean {
			errs[i] = d
		}
		fmt.Fprintf(os.Stderr, "%s\n", diag.FormatDiagnostics(errs))
		fmt.Printf("%s  ! %d lean-page warning(s): memory hygiene for the webview (see docs native/memory)%s\n", cYellow, len(lean), cReset)
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

	if flags.Scriptc && len(diags) == 0 {
		diags = append(diags, runScriptcCheck(root, cfg, flags)...)
	}

	if len(diags) > 0 {
		fmt.Fprintf(os.Stderr, "\n%s%s  x Native check failed: %d error(s).%s\n", cBold, cRed, len(diags), cReset)
		exit(1)
	}
	fmt.Printf("\n%s%s  + Native check passed.%s\n", cBold, cGreen, cReset)
}

// runScriptcCheck compiles the host bundle for scriptc and runs
// `scriptc coverage --fail-on=<level>` on it. Its blockers come back as
// KR7235 diagnostics at the original file and line.
func runScriptcCheck(root string, cfg *config.Config, flags cliFlags) []diag.Diagnostic {
	level := flags.ScriptcFailOn
	if level == "" {
		level = "blockers"
	}
	if !slices.Contains(runtimes.ScriptcFailOn, level) {
		fmt.Fprintf(os.Stderr, "%sError:%s --scriptc-fail-on must be one of %s\n", cRed, cReset, strings.Join(runtimes.ScriptcFailOn, ", "))
		exit(1)
	}
	b := build.New(root, cfg)
	b.Verbose = flags.Verbose
	ds, ran, err := b.NativeScriptcCheck(level)
	if err != nil {
		if d, ok := diag.FromError(err); ok && d.Code != "" {
			return []diag.Diagnostic{d}
		}
		fmt.Fprintf(os.Stderr, "%sscriptc check error:%s %v\n", cRed, cReset, build.BuildErrorSummary(err))
		exit(1)
	}
	if !ran {
		fmt.Printf("%s  ! scriptc: no TypeScript host code to check.%s\n", cYellow, cReset)
		return nil
	}
	var errs []diag.Diagnostic
	for _, d := range ds {
		if d.IsWarning() {
			fmt.Printf("%s  ! %s%s\n", cYellow, d.Error(), cReset)
			continue
		}
		errs = append(errs, d)
	}
	if len(errs) == 0 {
		fmt.Printf("  scriptc coverage (--fail-on=%s): host code compiles.\n", level)
	}
	return errs
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
	if flags.Pkg.Package && flags.Shell == "" && !flags.Release {
		flags.WantShell = true
	}
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
	if !flags.Release && nativeWantsExecutable(flags, root, cfg) {
		fmt.Printf("  the app has Go host code: linking one executable (--layout shell keeps the shell and a Go host child)\n")
		flags.Release = true
	}
	if flags.Release {
		runNativeRelease(builder, root, outDir, flags, start)
		return
	}
	res, err := builder.BuildNativeApp(build.NativeAppOptions{
		OutDir: outDir, Target: flags.Target, Shell: flags.Shell, WantShell: flags.WantShell, Derive: true, Distribute: true,
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
	for _, c := range m.HostChildren {
		fmt.Printf("    %-12s %s (%d funcs, %d streams, %d signals)\n", c.Runtime+" host", c.Entry, len(c.Funcs), len(c.Streams), len(c.Signals))
	}
	if m.GoHost != nil {
		fmt.Printf("    go host      %s (%d funcs, %d streams, %d signals)\n", m.GoHost.Binary, len(m.GoHost.Funcs), len(m.GoHost.Streams), len(m.GoHost.Signals))
	}
	if len(m.Fallbacks) > 0 {
		fmt.Printf("    dynamic      %d route(s)\n", len(m.Fallbacks))
	}
	fmt.Printf("\n%s%s  Done! (built in %s)%s\n", cBold, cGreen, time.Since(start).Round(time.Millisecond), cReset)
	if res.DistExe != "" {
		fmt.Printf("  distributable: %s (the shell as %s, with app/ next to it)\n", rel(filepath.Dir(res.DistExe)), filepath.Base(res.DistExe))
		fmt.Printf("  run it with: %s\n", rel(res.DistExe))
	} else if res.ShellPath != "" {
		fmt.Printf("  run it with: %s %s\n", rel(res.ShellPath), rel(res.AppDir))
	} else {
		fmt.Printf("  run it with: krate-shell %s\n", rel(res.AppDir))
	}
	if flags.Pkg.Package {
		c, err := newPkgContext(flags, root, cfg)
		if err != nil {
			reportPkgError(err)
			exit(1)
		}
		packageNative(flags, c)
	}
}

// nativeWantsExecutable reports whether a plain `krate native build` should
// produce the single-process executable: the app has Go host code, Go is
// installed (it is needed to compile that code anyway) and neither --layout
// shell nor an explicit --shell asks for the shell layout. One process saves
// the Go host child's runtime (about 10 MB) and the socket hop.
func nativeWantsExecutable(flags cliFlags, root string, cfg *config.Config) bool {
	switch {
	case flags.Layout == "shell", flags.Shell != "":
	case flags.Layout == "executable":
		return true
	default:
		if _, err := exec.LookPath("go"); err != nil {
			return false
		}
		p, err := native.Analyze(root, cfg)
		if err != nil {
			return false
		}
		for _, m := range p.Modules {
			if m.Source == native.SourceGo {
				return true
			}
		}
	}
	return false
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
	} else if res.Distribution != "" {
		fmt.Printf("    app          %s (prebuilt-shell layout; run %s)\n", rel(res.Distribution), rel(res.Binaries[0].Path))
	} else {
		fmt.Printf("    app          %s (prebuilt-shell layout; run it with krate-shell)\n", rel(res.AppDir))
	}
	fmt.Printf("    report       %s\n", rel(filepath.Join(res.OutDir, "report.json")))
	fmt.Printf("\n%s%s  Done! (built in %s)%s\n", cBold, cGreen, time.Since(start).Round(time.Millisecond), cReset)
	if flags.Pkg.Package {
		// Package every executable the release produced (report.json names them).
		targets := flags.Targets
		if len(targets) == 0 {
			targets = []string{""}
		}
		for _, t := range targets {
			f := flags
			f.Target = t
			c, err := newPkgContext(f, root, builder.Cfg)
			if err != nil {
				reportPkgError(err)
				exit(1)
			}
			packageNative(f, c)
		}
	}
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
	if flags.Memory {
		runNativeMemory(builder, root, flags)
		return
	}

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

// runNativeMemory implements `krate native profile --memory`.
func runNativeMemory(builder *build.Builder, root string, flags cliFlags) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	outDir := flags.NativeOut
	if outDir == "" {
		outDir = "dist-native"
	}
	idle := 30 * time.Second
	if flags.IdleSet {
		idle = flags.Idle
	}
	fmt.Printf("%s%s  Measuring the memory of native app %s%s\n", cBold, cCyan, root, cReset)
	rep, err := builder.MemoryNative(build.NativeMemoryProfileOptions{
		OutDir: outDir, Shell: flags.Shell, Idle: idle,
		Log: os.Stdout, Stderr: os.Stderr, Ctx: ctx,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sMemory error:%s %s\n", cRed, cReset, build.BuildErrorSummary(err))
		exit(1)
	}
	fmt.Printf("\n%s\n", build.FormatNativeMemory(rep))
	p := filepath.Join(outDir, "report.json")
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		p = filepath.ToSlash(r)
	}
	fmt.Printf("%s%s  Memory written to %s%s\n", cBold, cGreen, p, cReset)
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
