package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/check"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/deploy"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
	"github.com/kratejs/krate/packages/compiler/internal/mcp"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
	krateversion "github.com/kratejs/krate/packages/compiler/internal/version"
)

const (
	cReset  = "\033[0m"
	cRed    = "\033[31m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cCyan   = "\033[36m"
	cGray   = "\033[90m"
	cBold   = "\033[1m"
)

// version is set at build time via `-ldflags "-X main.version=<version>"`.
// Local/dev builds default to "dev".
var version = "dev"

type cliFlags struct {
	ConfigPath  string
	OutDir      string
	Watch       bool
	Verbose     bool
	Profile     bool
	Sourcemap   bool
	NoDCE       bool
	Precompress bool
	Mode        string
	Schema      bool
	Target      string
	JSON        bool
	HTTP        bool
	Port        int
	Token       string
	CPUProfile  string
	MemProfile  string
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	// Publish the build-time version to internal packages (plugins read it).
	krateversion.Value = version
	flags, args := parseFlags(os.Args[1:])
	plugin.SetVerbose(flags.Verbose)
	build.SetVerboseLogging(flags.Verbose)

	if len(args) == 0 {
		printUsage(os.Stderr)
		os.Exit(1)
	}

	// `krate <command> --help` prints command-specific usage.
	if len(args) >= 2 && (args[1] == "--help" || args[1] == "-h") {
		printCommandUsage(os.Stdout, args[0])
		return
	}

	switch args[0] {
	case "build":
		runBuild(flags, args)
	case "dev":
		runDev(flags, args)
	case "serve", "preview", "start":
		runServe(flags, args)
	case "types":
		runTypes(flags, args)
	case "go:init":
		runGoInit(flags, args)
	case "check":
		runCheck(flags, args)
	case "inspect":
		runInspect(flags, args)
	case "deploy":
		runDeploy(flags, args)
	case "clean":
		runClean(flags, args)
	case "doctor":
		runDoctor(flags, args)
	case "config":
		runConfig(flags, args)
	case "plugin":
		runPlugin(flags, args)
	case "mcp":
		runMCP(flags, args)
	case "version", "--version", "-v":
		fmt.Println("krate v" + version)
	case "help", "--help", "-h":
		printHelpCommand(os.Stdout, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "%sUnknown command:%s %s\n", cRed, cReset, args[0])
		fmt.Fprintf(os.Stderr, "Run %skrate help%s for usage.\n", cCyan, cReset)
		os.Exit(1)
	}
}

// printCommandUsage prints usage for a single command.
func printCommandUsage(w io.Writer, cmd string) {
	switch cmd {
	case "build":
		fmt.Fprintf(w, "Usage: krate build [dir] [--config <path>] [--out-dir <path>] [--watch] [--profile] [--cpuprofile <path>] [--memprofile <path>] [--precompress]\n\n")
		fmt.Fprintf(w, "Compile the project to static output (and SSR/server bundles when needed).\n")
	case "dev":
		fmt.Fprintf(w, "Usage: krate dev [dir] [--config <path>] [--verbose]\n\n")
		fmt.Fprintf(w, "Start the dev server with file watching and live reload.\n")
	case "serve", "preview", "start":
		fmt.Fprintf(w, "Usage: krate %s [dir] [--config <path>]\n\n", cmd)
		fmt.Fprintf(w, "Build, then serve the output (preview production locally).\n")
		fmt.Fprintf(w, "`preview` and `start` are aliases for `serve`.\n")
	case "types":
		fmt.Fprintf(w, "Usage: krate types [dir]\n\n")
		fmt.Fprintf(w, "Generate route/content TypeScript declarations (krate-env.d.ts, .krate/types).\n")
	case "go:init":
		fmt.Fprintf(w, "Usage: krate go:init [dir]\n\n")
		fmt.Fprintf(w, "Set up go.mod/go.work and vendored helpers for Go component intellisense.\n")
	case "check":
		fmt.Fprintf(w, "Usage: krate check [dir]\n\n")
		fmt.Fprintf(w, "Run compiler-enforced quality gates (a11y/SEO/perf) on the built output.\n")
	case "deploy":
		fmt.Fprintf(w, "Usage: krate deploy [dir] [--target netlify|vercel|cloudflare|gh-pages]\n\n")
		fmt.Fprintf(w, "Build the site and write host adapter files (e.g. _redirects, vercel.json).\n")
	case "inspect":
		fmt.Fprintf(w, "Usage: krate inspect [dir] [--json]\n\n")
		fmt.Fprintf(w, "Build the site and print its model (routes, sources, modes, deps, outputs).\n")
	case "clean":
		fmt.Fprintf(w, "Usage: krate clean [dir]\n\n")
		fmt.Fprintf(w, "Remove build output (dist) and the compiler cache (.krate/cache).\n")
	case "doctor":
		fmt.Fprintf(w, "Usage: krate doctor [dir]\n\n")
		fmt.Fprintf(w, "Print a diagnostic summary of the project and toolchain.\n")
	case "config":
		fmt.Fprintf(w, "Usage: krate config [dir] [--mode <mode>] [--config <path>] [--schema]\n\n")
		fmt.Fprintf(w, "Print the fully resolved configuration as JSON (--schema prints a JSON Schema).\n")
	case "plugin":
		fmt.Fprintf(w, "Usage: krate plugin add <package> [dir]\n\n")
		fmt.Fprintf(w, "Manage Krate plugins.\n")
	case "mcp":
		fmt.Fprintf(w, "Usage: krate mcp [dir] [--http] [--port 7331] [--token TOKEN]\n\n")
		fmt.Fprintf(w, "Run the MCP server (Model Context Protocol) over stdio by default.\n")
		fmt.Fprintf(w, "  --http        Serve Streamable HTTP at /mcp instead\n")
		fmt.Fprintf(w, "  --port N      HTTP port (default 7331, localhost only)\n")
		fmt.Fprintf(w, "  --token T     Bearer token (or KRATE_MCP_TOKEN)\n")
	default:
		printUsage(w)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, "Usage: krate [flags] <command> [dir]\n\n")
	fmt.Fprintf(w, "Commands:\n")
	fmt.Fprintf(w, "  build     Build the project for production\n")
	fmt.Fprintf(w, "  dev       Start the development server with hot reload\n")
	fmt.Fprintf(w, "  serve     Build, then serve for preview (aliases: preview, start)\n")
	fmt.Fprintf(w, "  types     Generate route/content TypeScript declarations\n")
	fmt.Fprintf(w, "  go:init   Set up go.mod/go.work for Go component intellisense\n")
	fmt.Fprintf(w, "  check     Run compiler-enforced quality gates (a11y/SEO/perf)\n")
	fmt.Fprintf(w, "  inspect   Print the compiled site model (--json for machine-readable)\n")
	fmt.Fprintf(w, "  deploy    Build and write host adapter files (netlify/vercel/...)\n")
	fmt.Fprintf(w, "  clean     Remove build output (dist) and the compiler cache (.krate/cache)\n")
	fmt.Fprintf(w, "  doctor    Print a diagnostic summary of the project and toolchain\n")
	fmt.Fprintf(w, "  plugin    Manage plugins (add <pkg>)\n")
	fmt.Fprintf(w, "  mcp       Run the MCP server (Model Context Protocol; --http for HTTP)\n")
	fmt.Fprintf(w, "  version   Print the version\n")
	fmt.Fprintf(w, "  help      Print this help, or `krate help <command>`\n\n")
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  --config <path>   Path to config file (default: project/krate.config.ts)\n")
	fmt.Fprintf(w, "  --out-dir <path>  Override output directory\n")
	fmt.Fprintf(w, "  --watch           Rebuild on file changes\n")
	fmt.Fprintf(w, "  --verbose         Print diagnostic details (e.g. reactive validation)\n")
	fmt.Fprintf(w, "  --profile         Print per-phase build timings and disk-cache hits/misses\n")
	fmt.Fprintf(w, "  --cpuprofile <p>  Write a Go CPU profile to <p> (pprof)\n")
	fmt.Fprintf(w, "  --memprofile <p>  Write a Go heap profile to <p> (pprof)\n")
	fmt.Fprintf(w, "  --sourcemap       Emit source maps for generated JS\n")
	fmt.Fprintf(w, "  --no-dce          Disable CSS/JS dead-code elimination\n")
	fmt.Fprintf(w, "  --target <name>   Deploy adapter target (netlify|vercel|cloudflare|gh-pages)\n")
	fmt.Fprintf(w, "  --json            Machine-readable output (krate inspect)\n")
}

func parseFlags(args []string) (cliFlags, []string) {
	var flags cliFlags
	var remaining []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--config" && i+1 < len(args):
			flags.ConfigPath = args[i+1]
			i++
		case args[i] == "--out-dir" && i+1 < len(args):
			flags.OutDir = args[i+1]
			i++
		case args[i] == "--target" && i+1 < len(args):
			flags.Target = args[i+1]
			i++
		case args[i] == "--watch":
			flags.Watch = true
		case args[i] == "--verbose":
			flags.Verbose = true
		case args[i] == "--profile":
			flags.Profile = true
		case args[i] == "--cpuprofile" && i+1 < len(args):
			flags.CPUProfile = args[i+1]
			i++
		case args[i] == "--memprofile" && i+1 < len(args):
			flags.MemProfile = args[i+1]
			i++
		case args[i] == "--sourcemap":
			flags.Sourcemap = true
		case args[i] == "--no-dce":
			flags.NoDCE = true
		case args[i] == "--precompress":
			flags.Precompress = true
		case args[i] == "--mode" && i+1 < len(args):
			flags.Mode = args[i+1]
			i++
		case args[i] == "--schema":
			flags.Schema = true
		case args[i] == "--json":
			flags.JSON = true
		case args[i] == "--http":
			flags.HTTP = true
		case args[i] == "--port" && i+1 < len(args):
			if n, err := strconv.Atoi(args[i+1]); err == nil {
				flags.Port = n
			}
			i++
		case args[i] == "--token" && i+1 < len(args):
			flags.Token = args[i+1]
			i++
		case args[i] == "--help" || args[i] == "-h" || args[i] == "--version":
			// Handled as commands in main(); keep them in the arg list rather
			// than rejecting them as unknown flags.
			remaining = append(remaining, args[i])
		case strings.HasPrefix(args[i], "-"):
			fmt.Fprintf(os.Stderr, "%sUnknown flag:%s %s\n", cRed, cReset, args[i])
			os.Exit(1)
		default:
			remaining = append(remaining, args[i])
		}
	}
	return flags, remaining
}

func resolveConfig(flags cliFlags, args []string) (string, *config.Config) {
	root := "."
	if len(args) > 1 {
		root = args[1]
	}
	root, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	command := ""
	if len(args) > 0 {
		command = args[0]
	}
	defaultMode := "production"
	if command == "dev" {
		defaultMode = "development"
	}
	// `--mode` selects the environment mode (equivalent to KRATE_ENV) and the
	// `.env.<mode>` files. Loaded before the config so a function-form
	// defineConfig sees the resolved mode + environment variables.
	if flags.Mode != "" {
		_ = os.Setenv("KRATE_ENV", flags.Mode)
	}
	loadProjectEnv(root, defaultMode, flags.Verbose)
	mode := environ.Mode(defaultMode)

	cfg, err := config.LoadWith(root, config.LoadOptions{
		ConfigPath: flags.ConfigPath,
		Command:    command,
		Mode:       mode,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sConfig error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
	for _, w := range config.Warnings {
		fmt.Fprintf(os.Stderr, "  %s⚠ config:%s %s\n", cYellow, cReset, w)
	}
	for _, w := range plugin.ConfigWarnings(cfg) {
		fmt.Fprintf(os.Stderr, "  %s⚠ config:%s %s\n", cYellow, cReset, w)
	}
	cfg.Resolve(root)

	if flags.OutDir != "" {
		if filepath.IsAbs(flags.OutDir) {
			cfg.OutDir = flags.OutDir
		} else {
			cfg.OutDir = filepath.Join(root, flags.OutDir)
		}
	}
	if flags.Sourcemap {
		cfg.Sourcemap = true
	}
	if flags.NoDCE {
		off := false
		cfg.Dce.CSS = &off
		cfg.Dce.JS = &off
	}
	if flags.Precompress {
		cfg.Compress.Brotli = true
		cfg.Compress.Gzip = true
	}

	return root, cfg
}

// loadProjectEnv resolves the project environment mode (KRATE_ENV/NODE_ENV)
// and loads the merged .env values. The result becomes the process-wide
// environ.Current (read by serve-time JS VMs and sidecars) and the Builder's
// Env (merged into build-time npx tsx subprocesses). Loaded before any
// `<ServerComponent>` executes so setup code sees process.env.
func loadProjectEnv(root, defaultMode string, verbose bool) map[string]string {
	environ.Verbose = verbose
	mode := environ.Mode(defaultMode)
	env, err := environ.Load(root, mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sEnv error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
	environ.Current = env
	return env
}

func runBuild(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	env := loadProjectEnv(root, "production", flags.Verbose)

	fmt.Printf("%s%s  Building %s \u2192 %s%s\n", cBold, cCyan, root, cfg.OutDir, cReset)

	start := time.Now()
	builder := build.New(root, cfg)
	builder.Verbose = flags.Verbose
	builder.Env = env
	if flags.Profile {
		builder.Prof = build.NewProfiler(true)
	}
	stopCPU := func() {}
	if flags.CPUProfile != "" {
		stopCPU = startCPUProfile(flags.CPUProfile)
	}
	buildErr := builder.BuildAll()
	stopCPU()
	if flags.MemProfile != "" {
		writeHeapProfile(flags.MemProfile)
	}
	if buildErr != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %v\n", cRed, cReset, buildErr)
		os.Exit(1)
	}

	fmt.Printf("%s%s  Done! (built in %s)%s\n", cBold, cGreen, time.Since(start).Round(time.Millisecond), cReset)

	if flags.Watch {
		hub := build.NewDevHub()
		errc := make(chan error, 1)
		go func() {
			if err := build.Watch(builder, 500*time.Millisecond, hub); err != nil {
				errc <- err
			}
		}()
		go func() {
			events, cancel := hub.Subscribe()
			defer cancel()
			for ev := range events {
				if len(ev.Diagnostics) > 0 {
					msgs := make([]string, 0, len(ev.Diagnostics))
					for _, d := range ev.Diagnostics {
						msgs = append(msgs, d.Error())
					}
					fmt.Printf("\n%s%s  Build failed:%s %v (%s)\n", cBold, cRed, cReset, msgs, time.Now().Format("15:04:05"))
					continue
				}
				fmt.Printf("\n%s%s  Rebuilt:%s %v (%s)\n", cBold, cCyan, cReset, ev.Routes, time.Now().Format("15:04:05"))
			}
		}()
		err := <-errc
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
}

// startCPUProfile begins a CPU profile written to path and returns a stop
// function. If profiling cannot start it prints a warning and returns a no-op.
func startCPUProfile(path string) func() {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sWarning: cannot create cpu profile %s:%s %v\n", cYellow, path, cReset, err)
		return func() {}
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		fmt.Fprintf(os.Stderr, "%sWarning: cannot start cpu profile:%s %v\n", cYellow, cReset, err)
		return func() {}
	}
	return func() {
		pprof.StopCPUProfile()
		_ = f.Close()
		fmt.Printf("  %s·%s CPU profile written to %s\n", cGreen, cReset, path)
	}
}

// writeHeapProfile writes a heap profile to path (after a GC).
func writeHeapProfile(path string) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sWarning: cannot create mem profile %s:%s %v\n", cYellow, path, cReset, err)
		return
	}
	defer f.Close()
	runtime.GC()
	if err := pprof.WriteHeapProfile(f); err != nil {
		fmt.Fprintf(os.Stderr, "%sWarning: cannot write mem profile:%s %v\n", cYellow, cReset, err)
		return
	}
	fmt.Printf("  %s·%s heap profile written to %s\n", cGreen, cReset, path)
}

func runDev(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	env := loadProjectEnv(root, "development", flags.Verbose)

	fmt.Printf("%s%s  Starting dev server...%s\n", cBold, cCyan, cReset)
	start := time.Now()

	builder := build.New(root, cfg)
	builder.Verbose = flags.Verbose
	builder.DevMode = true
	builder.Env = env

	hub := build.NewDevHub()
	if err := builder.BuildAll(); err != nil {
		// A failed initial build must not kill the dev server: keep serving and
		// publish the diagnostics so the browser overlay shows them (and the
		// watcher can recover on the next save).
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %v\n", cRed, cReset, err)
		hub.Publish(build.DevEvent{Type: "reload", Diagnostics: build.DiagnosticsFromError(err)})
	}

	errc := make(chan error, 2)

	go func() {
		if err := build.ServeDev(root, cfg, hub, start); err != nil {
			errc <- err
		}
	}()

	go func() {
		if err := build.Watch(builder, 500*time.Millisecond, hub); err != nil {
			errc <- err
		}
	}()

	err := <-errc
	fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
	os.Exit(1)
}

func runServe(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	env := loadProjectEnv(root, "production", flags.Verbose)

	fmt.Printf("%s%s  Building for preview...%s\n", cBold, cCyan, cReset)
	start := time.Now()

	builder := build.New(root, cfg)
	builder.Verbose = flags.Verbose
	builder.Env = env
	if err := builder.BuildAll(); err != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	fmt.Printf("%s%s  Build complete. Starting server...%s\n", cBold, cGreen, cReset)

	if err := build.Serve(root, cfg, start); err != nil {
		fmt.Fprintf(os.Stderr, "%sServe error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
}

// runDeploy builds the site and writes host-specific adapter files so the
// output can be uploaded to a static host as-is.
func runDeploy(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	env := loadProjectEnv(root, "production", flags.Verbose)

	target := flags.Target
	if target == "" {
		target = cfg.Deploy.Target
	}
	if target == "" {
		target = "netlify"
	}

	fmt.Printf("%s%s  Building %s \u2192 %s%s\n", cBold, cCyan, root, cfg.OutDir, cReset)
	builder := build.New(root, cfg)
	builder.Verbose = flags.Verbose
	builder.Env = env
	if err := builder.BuildAll(); err != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	files, err := deploy.Emit(target, cfg, cfg.OutDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sDeploy error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
	fmt.Printf("%s%s  %s adapter ready%s (%d file(s))\n", cBold, cGreen, target, cReset, len(files))
	for _, f := range files {
		if rel, err := filepath.Rel(cfg.OutDir, f); err == nil {
			fmt.Printf("    %s%s%s\n", cGray, filepath.ToSlash(rel), cReset)
		}
	}
}

// runInspect builds the site and emits its machine-readable model (routes,
// sources, render modes, dependencies, emitted files). `--json` prints the
// model as JSON on stdout with build chatter suppressed so the output is
// pipeable.
func runInspect(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	env := loadProjectEnv(root, "production", flags.Verbose)

	builder := build.New(root, cfg)
	builder.Verbose = flags.Verbose
	builder.Env = env

	var buildErr error
	withSuppressedStdout(func() { buildErr = builder.BuildAll() })
	if buildErr != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %v\n", cRed, cReset, buildErr)
		os.Exit(1)
	}
	graph, err := builder.SiteGraph()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sInspect error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	if flags.JSON {
		data, err := json.MarshalIndent(graph, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sInspect error:%s %v\n", cRed, cReset, err)
			os.Exit(1)
		}
		fmt.Println(string(data))
		return
	}

	fmt.Printf("%s%s  %s%s\n", cBold, cCyan, root, cReset)
	fmt.Printf("  %d route(s)\n\n", len(graph.Routes))
	for _, r := range graph.Routes {
		note := ""
		if !r.Built {
			note = " (unbuilt)"
		}
		fmt.Printf("  %-7s %-44s %s%s\n", r.Mode, r.Route, r.Source, note)
	}
}

// withSuppressedStdout runs fn with os.Stdout redirected to a pipe that is
// drained to io.Discard, so library build chatter never corrupts structured
// stdout output.
func withSuppressedStdout(fn func()) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		fn()
		return
	}
	os.Stdout = w
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(drained)
	}()
	fn()
	_ = w.Close()
	<-drained
	os.Stdout = old
	_ = r.Close()
}

// runTypes generates route and content TypeScript declarations without running
// a full build. Useful in CI (`krate types && tsc --noEmit`) and for editors.
func runTypes(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	fmt.Printf("%s%s  Generating types for %s%s\n", cBold, cCyan, root, cReset)

	if errs := build.GenerateTypes(root, cfg); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "%s  Type error:%s %v\n", cRed, cReset, e)
		}
		os.Exit(1)
	}

	fmt.Printf("%s%s  Types written to %s%s\n", cBold, cGreen, filepath.Join(root, ".krate", "types"), cReset)
}

// runGoInit sets up the Go workspace (go.mod/go.work + vendored helpers) so
// editors resolve the goext import path for Go API routes and components.
func runGoInit(flags cliFlags, args []string) {
	root, _ := resolveConfig(flags, args)
	notes, err := gohost.Init(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sGo workspace error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
	fmt.Printf("%s%s  Go workspace ready for %s%s\n", cBold, cGreen, root, cReset)
	for _, n := range notes {
		fmt.Printf("  - %s\n", n)
	}
	fmt.Printf("  Run %skrate doctor%s to verify.\n", cCyan, cReset)
}

// runCheck builds the site and runs the compiler-enforced quality gates,
// exiting non-zero when findings meet or exceed the configured fail-on
// severity. Backs CI (`krate check`) and the future MCP `check` primitive.
func runCheck(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	env := loadProjectEnv(root, "production", flags.Verbose)

	fmt.Printf("%s%s  Building for checks %s \u2192 %s%s\n", cBold, cCyan, root, cfg.OutDir, cReset)
	start := time.Now()

	builder := build.New(root, cfg)
	builder.Verbose = flags.Verbose
	builder.Env = env
	// This command owns the single check pass (CheckSite re-reads dist/), so
	// suppress the in-build gates to avoid duplicate reporting.
	builder.SkipQualityChecks = true
	if err := builder.BuildAll(); err != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	findings, checkCfg, err := builder.CheckSite(true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sCheck error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	fopts := check.DefaultFormatOptions()

	if len(findings) == 0 {
		fmt.Printf("%s%s  ✓ No quality issues found.%s\n", cBold, cGreen, cReset)
		return
	}

	errs, warns := check.Counts(findings)
	fmt.Printf("\n%s%s  Quality report%s %s(%s)%s\n\n", cBold, cCyan, cReset, cGray, check.Summary(errs, warns, fopts), cReset)
	fmt.Print(check.FormatWith(findings, fopts))
	fmt.Printf("\n%s  Checks: %s in %s%s\n", cCyan, check.Summary(errs, warns, fopts), time.Since(start).Round(time.Millisecond), cReset)

	if check.Failing(findings, checkCfg.FailOn) {
		fmt.Fprintf(os.Stderr, "\n%s%s  ✗ Quality checks failed.%s\n", cBold, cRed, cReset)
		os.Exit(1)
	}

	fmt.Printf("\n%s%s  ⚠ Passed with warnings (failOn=%s).%s\n", cBold, cYellow, checkCfg.FailOn, cReset)
}

// runClean removes generated output (the build output directory and the
// compiler cache) so the next build starts from scratch. Only those paths are
// touched - generated types and sidecar manifests under .krate are left in
// place, since they are regenerated on demand.
func runClean(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	fmt.Printf("%s%s  Cleaning %s%s\n", cBold, cCyan, root, cReset)

	result, err := build.Clean(root, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sClean error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	if len(result.Removed) == 0 {
		fmt.Printf("%s%s  Nothing to clean.%s\n", cBold, cGreen, cReset)
		return
	}
	for _, dir := range result.Removed {
		rel, relErr := filepath.Rel(root, dir)
		if relErr != nil {
			rel = dir
		}
		fmt.Printf("  %s✓%s Removed %s\n", cGreen, cReset, filepath.ToSlash(rel))
	}
}

// runMCP starts the agent-native MCP server. It speaks JSON-RPC 2.0 over
// stdio: the real stdout is reserved for protocol frames, so it is captured
// before any compiler code can print to it, and os.Stdout is pointed at stderr
// for the rest of the session. An optional trailing arg selects the project
// root (defaults to the current directory).
func runMCP(flags cliFlags, args []string) {
	// Reserve the real stdout for JSON-RPC, redirect incidental output to stderr.
	protocolOut := os.Stdout
	os.Stdout = os.Stderr

	root, cfg := resolveConfig(flags, args)
	env := loadProjectEnv(root, "production", flags.Verbose)

	svc := mcp.NewService(mcp.Options{
		Root:    root,
		Cfg:     cfg,
		Env:     env,
		Verbose: flags.Verbose,
	})
	if flags.HTTP {
		port := flags.Port
		if port == 0 {
			port = 7331
		}
		token := flags.Token
		if token == "" {
			token = os.Getenv("KRATE_MCP_TOKEN")
		}
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		fmt.Fprintf(os.Stderr, "  MCP HTTP server -> http://%s/mcp (POST; GET for SSE)\n", addr)
		if err := mcp.ServeHTTP(svc, addr, token); err != nil {
			fmt.Fprintf(os.Stderr, "%sMCP HTTP error:%s %v\n", cRed, cReset, err)
			os.Exit(1)
		}
		return
	}

	srv := mcp.NewServer(protocolOut, "krate", version)
	svc.Register(srv)

	if err := srv.Serve(os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "%sMCP error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
}
