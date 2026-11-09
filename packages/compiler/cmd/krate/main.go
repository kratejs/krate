package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/check"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/deploy"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/fix"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
	"github.com/kratejs/krate/packages/compiler/internal/mcp"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
	"github.com/kratejs/krate/packages/compiler/internal/typecheck"
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
	ConfigPath    string
	OutDir        string
	Watch         bool
	Verbose       bool
	Profile       bool
	Sourcemap     bool
	NoDCE         bool
	Precompress   bool
	Mode          string
	Schema        bool
	Target        string
	StaticOnly    bool
	JSON          bool
	HTTP          bool
	Apply         bool
	Verify        bool
	Force         bool
	UpdatePackage bool
	Report        string
	Port          int
	PortSet       bool
	NoBuild       bool
	CleanDist     bool
	CleanCache    bool
	CleanAll      bool
	Global        bool
	DryRun        bool
	Fix           bool
	Open          bool
	NoSummary     bool
	QR            bool
	Host          string
	Token         string
	Roots         []string
	CPUProfile    string
	MemProfile    string
}

// exitHooks run, last registered first, before the process exits, so the run
// log is flushed on the error paths that call exit.
var exitHooks []func()

// exit runs the exit hooks and then ends the process.
func exit(code int) {
	for i := len(exitHooks) - 1; i >= 0; i-- {
		exitHooks[i]()
	}
	finishUpdateCheck()
	os.Exit(code)
}

// startRunLog saves the run's console output to the configured logs
// directory (logs.enabled) and returns the function that finishes the log.
// The returned function is safe to call more than once.
func startRunLog(root string, cfg *config.Config, command string) func() {
	stop := build.StartRunLog(root, cfg, command)
	var once sync.Once
	finish := func() {
		once.Do(func() {
			if path := stop(); path != "" {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					rel = path
				}
				fmt.Printf("  %s-%s log saved to %s\n", cGray, cReset, filepath.ToSlash(rel))
			}
		})
	}
	exitHooks = append(exitHooks, finish)
	return finish
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	// Publish the build-time version to internal packages (plugins read it).
	krateversion.Value = version
	defer finishUpdateCheck()
	flags, args := parseFlags(os.Args[1:])
	plugin.SetVerbose(flags.Verbose)
	build.SetVerboseLogging(flags.Verbose)

	if len(args) == 0 {
		printUsage(os.Stderr)
		exit(1)
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
	case "typecheck":
		runTypecheck(flags, args)
	case "go:init":
		runGoInit(flags, args)
	case "check":
		runCheck(flags, args)
	case "fix":
		runFix(flags, args)
	case "migrate":
		runMigrate(flags, args)
	case "inspect":
		runInspect(flags, args)
	case "deploy":
		runDeploy(flags, args)
	case "clean":
		runClean(flags, args)
	case "doctor":
		runDoctor(flags, args)
	case "explain":
		runExplain(flags, args)
	case "info":
		runInfo(flags, args)
	case "upgrade":
		runUpgrade(flags, args)
	case "config":
		runConfig(flags, args)
	case "plugin":
		runPlugin(flags, args)
	case "init", "create":
		// Handled by the @krate/core npm wrapper, which delegates to
		// create-krate-app so the scaffold follows the same release channel.
		fmt.Fprintf(os.Stderr, "%s`krate %s` is provided by the @krate/core npm wrapper.%s\n", cYellow, args[0], cReset)
		fmt.Fprintf(os.Stderr, "Run `npx create-krate-app@latest <dir>` or install @krate/core.\n")
		printCommandUsage(os.Stdout, "init")
		exit(1)
	case "mcp":
		runMCP(flags, args)
	case "version", "--version", "-v":
		fmt.Println("krate v" + version)
	case "help", "--help", "-h":
		printHelpCommand(os.Stdout, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "%sUnknown command:%s %s\n", cRed, cReset, args[0])
		fmt.Fprintf(os.Stderr, "Run %skrate help%s for usage.\n", cCyan, cReset)
		exit(1)
	}
}

// printCommandUsage prints usage for a single command.
func printCommandUsage(w io.Writer, cmd string) {
	switch cmd {
	case "build":
		writeHelp(w, "Usage: krate build [dir] [--config <path>] [--out-dir <path>] [--mode <name>] [--watch] [--profile] [--cpuprofile <path>] [--memprofile <path>] [--precompress] [--no-summary]\n\n")
		fmt.Fprintf(w, "Compile the project to static output (and SSR/server bundles when needed).\n")
		fmt.Fprintf(w, "Prints a per-route size table (gzip) with the change since the last build; --no-summary hides it.\n")
	case "dev":
		fmt.Fprintf(w, "Usage: krate dev [dir] [--config <path>] [--port <n>] [--host <addr>] [--open] [--qr] [--verbose]\n\n")
		fmt.Fprintf(w, "Start the dev server with file watching and live reload.\n")
		fmt.Fprintf(w, "  --open        Open the browser once the server is up\n")
		fmt.Fprintf(w, "  --host 0.0.0.0  Listen on the network; the LAN URL is printed for phone testing\n")
		fmt.Fprintf(w, "  --qr          Print a QR code for the network URL (with --host)\n")
		fmt.Fprintf(w, "A busy port moves to the next free one. In a terminal: r + enter restarts, o opens the browser, c clears, h helps.\n")
	case "serve", "preview", "start":
		noBuildFlag := " [--no-build]"
		if cmd == "start" {
			noBuildFlag = ""
		}
		_, _ = fmt.Fprintf(w, "Usage: krate %s [dir] [--config <path>] [--port <n>] [--host <addr>]%s\n\n", cmd, noBuildFlag)
		if cmd == "start" {
			_, _ = fmt.Fprintf(w, "Serve the existing build output without rebuilding (production start).\n")
			_, _ = fmt.Fprintf(w, "Run `krate build` first. `krate serve` builds and then serves.\n")
		} else {
			_, _ = fmt.Fprintf(w, "Build, then serve the output (preview production locally).\n")
			_, _ = fmt.Fprintf(w, "`preview` is an alias for `serve`; use `krate start` to serve an existing build.\n")
		}
		_, _ = fmt.Fprintf(w, "Port precedence: --port, KRATE_SERVE_PORT, PORT, then server.port / devServer.port.\n")
	case "types":
		fmt.Fprintf(w, "Usage: krate types [dir]\n\n")
		fmt.Fprintf(w, "Generate route/content TypeScript declarations (krate-env.d.ts, .krate/types).\n")
	case "typecheck":
		fmt.Fprintf(w, "Usage: krate typecheck [dir]\n\n")
		fmt.Fprintf(w, "Generate types, then run tsc --noEmit without its errors about tier decorators.\n")
	case "go:init":
		fmt.Fprintf(w, "Usage: krate go:init [dir]\n\n")
		fmt.Fprintf(w, "Set up go.mod/go.work and vendored helpers for Go component intellisense.\n")
	case "check":
		fmt.Fprintf(w, "Usage: krate check [dir]\n\n")
		fmt.Fprintf(w, "Run compiler-enforced quality gates (a11y/SEO/perf) on the built output.\n")
	case "fix":
		fmt.Fprintf(w, "Usage: krate fix [dir] [--apply]\n\n")
		fmt.Fprintf(w, "Apply conservative, parse-gated source fixes. Dry-run by default.\n")
	case "migrate":
		fmt.Fprintf(w, "Usage: krate migrate nextjs [dir] [--apply] [--verify] [--force] [--update-package] [--report <path>]\n")
		fmt.Fprintf(w, "       krate migrate rollback <backup-dir>\n\n")
		fmt.Fprintf(w, "Plan or apply a framework migration. Dry-run by default.\n")
	case "deploy":
		writeHelp(w, "Usage: krate deploy [dir] [--target netlify|vercel|cloudflare|gh-pages|docker|node] [--static-only]\n\n")
		writeHelp(w, "Build the site and write host adapter files (_redirects/_headers, vercel.json, Dockerfile).\n")
		writeHelp(w, "Projects with SSR/ISR, API routes, actions or middleware deploy to vercel, netlify and\ncloudflare as a serverless function/worker, to --target node as a plain Node server\n(dist/.krate/server/server.mjs, no Go binary), or to --target docker. Go API routes and\nGo components need docker. --static-only publishes just the static pages.\n")
	case "inspect":
		fmt.Fprintf(w, "Usage: krate inspect [dir] [--json]\n\n")
		fmt.Fprintf(w, "Build the site and print its model (routes, sources, modes, deps, outputs).\n")
	case "clean":
		fmt.Fprintf(w, "Usage: krate clean [dir] [--dist | --cache | --all]\n\n")
		fmt.Fprintf(w, "Remove build output (dist) and the compiler cache (.krate/cache), and print the space freed.\n")
		fmt.Fprintf(w, "  --dist    Remove only the build output directory\n")
		fmt.Fprintf(w, "  --cache   Remove only the compiler cache (.krate/cache)\n")
		fmt.Fprintf(w, "  --all     Remove the build output and the whole .krate directory (types, logs, cache)\n")
	case "upgrade":
		fmt.Fprintf(w, "Usage: krate upgrade [version|tag] [dir] [--global] [--dry-run]\n\n")
		fmt.Fprintf(w, "Move the project's @krate/* packages to the latest version, or to a version or dist-tag you give.\n")
		fmt.Fprintf(w, "  --global, -g   Upgrade the globally installed krate CLI instead of the project\n")
		fmt.Fprintf(w, "  --dry-run      Print the plan and the commands without running them\n")
	case "info":
		fmt.Fprintf(w, "Usage: krate info [dir] [--config <path>] [--json]\n\n")
		fmt.Fprintf(w, "Print a paste-ready environment report (OS, Go, Node, package manager, @krate versions, config summary) for bug reports.\n")
	case "explain":
		fmt.Fprintf(w, "Usage: krate explain <code> [--json]\n\n")
		fmt.Fprintf(w, "Print the cause, the fix and a docs link for a diagnostic code, e.g. krate explain KR7110.\n")
	case "doctor":
		fmt.Fprintf(w, "Usage: krate doctor [dir] [--fix]\n\n")
		fmt.Fprintf(w, "Print a diagnostic summary of the project and toolchain.\n")
		fmt.Fprintf(w, "  --fix   Repair what it can: an older @krate/core than this binary, a missing .gitignore entry, a stale .krate cache\n")
	case "config":
		fmt.Fprintf(w, "Usage: krate config [dir] [--mode <mode>] [--config <path>] [--schema]\n\n")
		fmt.Fprintf(w, "Print the fully resolved configuration as JSON (--schema prints a JSON Schema).\n")
	case "plugin":
		fmt.Fprintf(w, "Usage: krate plugin add <package> [dir]\n\n")
		fmt.Fprintf(w, "Manage Krate plugins.\n")
	case "init", "create":
		fmt.Fprintf(w, "Usage: krate init [dir] [--use-npm|--use-pnpm|--use-yarn|--use-bun] [--skip-install] [--no-git]\n\n")
		fmt.Fprintf(w, "Scaffold a new Krate project (alias: create).\n")
		fmt.Fprintf(w, "Provided by the @krate/core npm wrapper, which delegates to create-krate-app.\n")
	case "mcp":
		fmt.Fprintf(w, "Usage: krate mcp [dir] [--http] [--host 127.0.0.1] [--port 7331] [--token TOKEN]\n\n")
		fmt.Fprintf(w, "Run the MCP server (Model Context Protocol) over stdio by default.\n")
		fmt.Fprintf(w, "  --http        Serve Streamable HTTP at /mcp instead\n")
		fmt.Fprintf(w, "  --host H      HTTP bind host (default 127.0.0.1; non-loopback needs --token)\n")
		fmt.Fprintf(w, "  --port N      HTTP port (default 7331)\n")
		fmt.Fprintf(w, "  --token T     Bearer token (or KRATE_MCP_TOKEN)\n")
		fmt.Fprintf(w, "  --root N=P    Add a read/index workspace root (repeatable; paths use N:path)\n")
	default:
		printUsage(w)
	}
}

// writeHelp writes one help fragment; help output is best-effort.
func writeHelp(w io.Writer, s string) { _, _ = io.WriteString(w, s) }

func printUsage(w io.Writer) {
	fmt.Fprintf(w, "Usage: krate [flags] <command> [dir]\n\n")
	fmt.Fprintf(w, "Commands:\n")
	fmt.Fprintf(w, "  build     Build the project for production\n")
	fmt.Fprintf(w, "  dev       Start the development server with hot reload\n")
	_, _ = fmt.Fprintf(w, "  serve     Build, then serve for preview (alias: preview)\n")
	_, _ = fmt.Fprintf(w, "  start     Serve the existing build without rebuilding (production)\n")
	fmt.Fprintf(w, "  types     Generate route/content TypeScript declarations\n")
	fmt.Fprintf(w, "  typecheck Generate types and run tsc\n")
	fmt.Fprintf(w, "  go:init   Set up go.mod/go.work for Go component intellisense\n")
	fmt.Fprintf(w, "  check     Run compiler-enforced quality gates (a11y/SEO/perf)\n")
	fmt.Fprintf(w, "  fix       Apply safe source fixes (dry-run unless --apply)\n")
	fmt.Fprintf(w, "  migrate   Plan or apply a framework migration\n")
	fmt.Fprintf(w, "  inspect   Print the compiled site model (--json for machine-readable)\n")
	fmt.Fprintf(w, "  deploy    Build and write host adapter files (netlify/vercel/...)\n")
	fmt.Fprintf(w, "  clean     Remove build output (dist) and the compiler cache (.krate/cache)\n")
	fmt.Fprintf(w, "  doctor    Print a diagnostic summary of the project and toolchain\n")
	fmt.Fprintf(w, "  upgrade   Upgrade @krate/* packages to latest or a given version\n")
	fmt.Fprintf(w, "  info      Print a paste-ready environment report for bug reports\n")
	fmt.Fprintf(w, "  explain   Explain a diagnostic code (krate explain KR7110)\n")
	fmt.Fprintf(w, "  config    Print the fully resolved configuration\n")
	fmt.Fprintf(w, "  plugin    Manage plugins (add <pkg>)\n")
	fmt.Fprintf(w, "  init      Scaffold a new project (alias: create; via the npm wrapper)\n")
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
	writeHelp(w, "  --precompress     Write .br/.gz siblings for compressible assets\n")
	writeHelp(w, "  --mode <name>     Environment mode (like KRATE_ENV): loads .env.<name>\n")
	writeHelp(w, "  --target <name>   Deploy adapter target (netlify|vercel|cloudflare|gh-pages|docker)\n")
	writeHelp(w, "  --static-only     Deploy only the static pages of a project that also uses server features\n")
	fmt.Fprintf(w, "  --json            Machine-readable output (krate inspect)\n")
	writeHelp(w, "  --apply           Write changes (krate fix, krate migrate); dry-run otherwise\n")
	writeHelp(w, "  --verify, --force, --update-package, --report <path>   krate migrate options\n")
	writeHelp(w, "  --schema          Print a JSON Schema instead of the config (krate config)\n")
	writeHelp(w, "  --port <n>        Listen port for dev/serve/start\n")
	writeHelp(w, "  --host <addr>     Bind address for dev/serve/start\n")
	writeHelp(w, "  --no-build        With serve: skip the build and serve the existing output\n")
}

func parseFlags(args []string) (cliFlags, []string) {
	flags, remaining, err := parseFlagsChecked(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		exit(1)
	}
	return flags, remaining
}

// valuedFlags take a value, as `--flag value` or `--flag=value`.
var valuedFlags = map[string]bool{
	"--config": true, "--out-dir": true, "--target": true, "--mode": true,
	"--report": true, "--token": true, "--root": true, "--cpuprofile": true,
	"--memprofile": true, "--host": true, "--port": true,
}

// parseFlagsChecked parses CLI flags, returning an error for malformed values
// (a non-numeric --port, a flag missing its value).
func parseFlagsChecked(args []string) (cliFlags, []string, error) {
	var flags cliFlags
	var remaining []string
	for i := 0; i < len(args); i++ {
		if name, inline, hasInline := strings.Cut(args[i], "="); valuedFlags[name] {
			val := inline
			if !hasInline {
				if i+1 >= len(args) {
					return flags, nil, fmt.Errorf("%s requires a value", name)
				}
				i++
				val = args[i]
			}
			switch name {
			case "--config":
				flags.ConfigPath = val
			case "--out-dir":
				flags.OutDir = val
			case "--target":
				flags.Target = val
			case "--mode":
				flags.Mode = val
			case "--report":
				flags.Report = val
			case "--token":
				flags.Token = val
			case "--root":
				flags.Roots = append(flags.Roots, val)
			case "--cpuprofile":
				flags.CPUProfile = val
			case "--memprofile":
				flags.MemProfile = val
			case "--host":
				flags.Host = val
			case "--port":
				n, err := parsePortValue("--port", val)
				if err != nil {
					return flags, nil, err
				}
				flags.Port, flags.PortSet = n, true
			}
			continue
		}
		switch {
		case args[i] == "--static-only":
			flags.StaticOnly = true
		case args[i] == "--watch":
			flags.Watch = true
		case args[i] == "--verbose":
			flags.Verbose = true
		case args[i] == "--profile":
			flags.Profile = true
		case args[i] == "--sourcemap":
			flags.Sourcemap = true
		case args[i] == "--no-dce":
			flags.NoDCE = true
		case args[i] == "--precompress":
			flags.Precompress = true
		case args[i] == "--schema":
			flags.Schema = true
		case args[i] == "--json":
			flags.JSON = true
		case args[i] == "--http":
			flags.HTTP = true
		case args[i] == "--apply":
			flags.Apply = true
		case args[i] == "--verify":
			flags.Verify = true
		case args[i] == "--force":
			flags.Force = true
		case args[i] == "--update-package":
			flags.UpdatePackage = true
		case args[i] == "--no-build":
			flags.NoBuild = true
		case args[i] == "--dist":
			flags.CleanDist = true
		case args[i] == "--cache":
			flags.CleanCache = true
		case args[i] == "--all":
			flags.CleanAll = true
		case args[i] == "--global" || args[i] == "-g":
			flags.Global = true
		case args[i] == "--dry-run":
			flags.DryRun = true
		case args[i] == "--fix":
			flags.Fix = true
		case args[i] == "--open":
			flags.Open = true
		case args[i] == "--no-summary":
			flags.NoSummary = true
		case args[i] == "--qr":
			flags.QR = true
		case args[i] == "--help" || args[i] == "-h" || args[i] == "--version":
			// Handled as commands in main(); keep them in the arg list rather
			// than rejecting them as unknown flags.
			remaining = append(remaining, args[i])
		case strings.HasPrefix(args[i], "-"):
			fmt.Fprintf(os.Stderr, "%sUnknown flag:%s %s\n", cRed, cReset, args[i])
			exit(1)
		default:
			remaining = append(remaining, args[i])
		}
	}
	return flags, remaining, nil
}

func resolveConfig(flags cliFlags, args []string) (string, *config.Config) {
	root, cfg, err := loadConfig(flags, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sConfig error:%s %s\n", cRed, cReset, formatConfigError(err))
		exit(1)
	}
	if len(args) > 0 {
		startUpdateCheck(cfg, args[0], flags.JSON)
	}
	return root, cfg
}

// formatConfigError renders a config load error. A source-located diagnostic
// (a syntax error in the config file) prints with its source line, caret and
// hint; anything else prints as its message.
func formatConfigError(err error) string {
	var d diag.Diagnostic
	if errors.As(err, &d) && d.Line > 0 {
		return "\n" + diag.FormatDiagnostics([]error{d})
	}
	return err.Error()
}

// requireConfigFlag exits when --config names a file that does not exist. The
// commands that load the config get this from config.LoadWith; this covers the
// ones that accept the flag but do not read a config (fix, migrate, plugin add,
// doctor), so a mistyped path is never ignored.
func requireConfigFlag(root string, flags cliFlags) {
	if err := config.CheckExplicitConfig(root, flags.ConfigPath); err != nil {
		fmt.Fprintf(os.Stderr, "%sConfig error:%s %v\n", cRed, cReset, err)
		exit(1)
	}
}

// checkProjectDir fails with a plain message when the project directory is
// missing, so a typo is not reported as a missing pagesDir (or, worse, turned
// into a new directory by the first build step that writes to it).
func checkProjectDir(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("project directory %s does not exist (check the path; `krate init` creates a new project)", root)
		}
		return fmt.Errorf("cannot read project directory %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project path %s is not a directory", root)
	}
	return nil
}

// loadConfig is resolveConfig without the exit, so the dev server can report a
// broken config edit and keep running.
func loadConfig(flags cliFlags, args []string) (string, *config.Config, error) {
	root := "."
	if len(args) > 1 {
		root = args[1]
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", nil, err
	}
	if err := checkProjectDir(root); err != nil {
		return "", nil, err
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

	config.Warnings = nil
	cfg, err := config.LoadWith(root, config.LoadOptions{
		ConfigPath: flags.ConfigPath,
		Command:    command,
		Mode:       mode,
	})
	if err != nil {
		return "", nil, err
	}
	for _, w := range config.Warnings {
		fmt.Fprintf(os.Stderr, "  %s! config:%s %s\n", cYellow, cReset, w)
	}
	// Validate `checks` up front so a bad failOn fails every command, not only
	// at the end of a build.
	if cfg.Checks != nil {
		if _, err := check.FromMap(cfg.Checks, root, nil); err != nil {
			return "", nil, fmt.Errorf("invalid config: %w", err)
		}
	}
	for _, w := range plugin.ConfigWarnings(cfg) {
		fmt.Fprintf(os.Stderr, "  %s! config:%s %s\n", cYellow, cReset, w)
	}
	cfg.Resolve(root)

	if flags.OutDir != "" {
		if filepath.IsAbs(flags.OutDir) {
			cfg.OutDir = flags.OutDir
		} else {
			cfg.OutDir = filepath.Join(root, flags.OutDir)
		}
	}
	if flags.Open {
		cfg.DevServer.Open = true
	}
	cfg.DevServer.QR = flags.QR
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
	if err := applyServerFlags(command, flags, cfg, os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		exit(1)
	}

	return root, cfg, nil
}

// loadProjectEnv resolves the project environment mode (KRATE_ENV/NODE_ENV)
// and loads the merged .env values. The result becomes the process-wide
// environ.Current (read by serve-time JS VMs and sidecars) and the Builder's
// Env (merged into build-time subprocesses). Loaded before any
// `<ServerComponent>` executes so setup code sees process.env.
func loadProjectEnv(root, defaultMode string, verbose bool) map[string]string {
	environ.Verbose = verbose
	mode := environ.Mode(defaultMode)
	env, err := environ.Load(root, mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sEnv error:%s %v\n", cRed, cReset, err)
		exit(1)
	}
	environ.Current = env
	return env
}

func runBuild(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	defer startRunLog(root, cfg, "build")()
	env := loadProjectEnv(root, "production", flags.Verbose)

	fmt.Printf("%s%s  Building %s -> %s%s\n", cBold, cCyan, root, cfg.OutDir, cReset)

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
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %s\n", cRed, cReset, build.BuildErrorSummary(buildErr))
		exit(1)
	}

	elapsed := time.Since(start).Round(time.Millisecond)
	if !flags.NoSummary {
		printBuildSummary(root, builder)
	}
	fmt.Printf("%s%s  Done! (built in %s)%s\n", cBold, cGreen, elapsed, cReset)

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
		exit(1)
	}
}

// printBuildSummary prints the per-route size table, compares it with the last
// build and stores it for the next one. A failure only skips the table.
func printBuildSummary(root string, builder *build.Builder) {
	s, err := builder.Summarize()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s! build summary skipped:%s %v\n", cYellow, cReset, err)
		return
	}
	table := build.FormatSummary(s, build.LoadSummary(root))
	if table == "" {
		return
	}
	fmt.Printf("\n%s\n", table)
	if err := build.SaveSummary(root, s); err != nil && builder.Verbose {
		fmt.Fprintf(os.Stderr, "  %s! could not save the build summary:%s %v\n", cYellow, cReset, err)
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
		fmt.Printf("  %s-%s CPU profile written to %s\n", cGreen, cReset, path)
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
	fmt.Printf("  %s-%s heap profile written to %s\n", cGreen, cReset, path)
}

func runDev(flags cliFlags, args []string) {
	if root, cfg, err := loadConfig(flags, args); err == nil && root != "" {
		// Mirror console output into .krate/dev.log for agents (MCP get_logs).
		defer build.TeeDevLog(root)()
		defer startRunLog(root, cfg, "dev")()
	}
	first := true
	for {
		if !runDevOnce(flags, args, first) {
			return
		}
		first = false
	}
}

// runDevOnce builds and serves the project until the process fails or a config
// or env change asks for a restart (it then returns true).
func runDevOnce(flags cliFlags, args []string, first bool) bool {
	root, cfg := resolveConfig(flags, args)
	env := loadProjectEnv(root, "development", flags.Verbose)

	if first {
		fmt.Printf("%s%s  Starting dev server...%s\n", cBold, cCyan, cReset)
	} else {
		fmt.Printf("%s%s  Restarting dev server with the new settings...%s\n", cBold, cCyan, cReset)
	}
	start := time.Now()

	builder := build.New(root, cfg)
	builder.Verbose = flags.Verbose
	builder.DevMode = true
	builder.Env = env

	hub := build.NewDevHub()
	builder.DevReload = func(reason string) bool {
		fmt.Printf("  %s%s changed; reloading configuration...%s\n", cYellow, reason, cReset)
		if _, _, err := loadConfig(flags, args); err != nil {
			fmt.Fprintf(os.Stderr, "%sConfig error:%s %s\n", cRed, cReset, formatConfigError(err))
			hub.Publish(build.DevEvent{Type: "reload", Diagnostics: []diag.Diagnostic{{File: reason, Message: "config: " + err.Error(), Hint: "fix " + reason + "; the dev server keeps running with the previous settings"}}})
			return false
		}
		return true
	}
	if err := builder.BuildAll(); err != nil {
		// A failed initial build must not kill the dev server: keep serving and
		// publish the diagnostics so the browser overlay shows them (and the
		// watcher can recover on the next save).
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %s\n", cRed, cReset, build.BuildErrorSummary(err))
		hub.Publish(build.DevEvent{Type: "reload", Diagnostics: build.DiagnosticsFromError(err)})
	}

	errc := make(chan error, 2)

	go func() {
		// A clean shutdown (Ctrl+C / SIGTERM) returns nil.
		errc <- build.ServeDev(root, cfg, hub, start)
	}()

	go func() {
		errc <- build.Watch(builder, 75*time.Millisecond, hub)
	}()

	err := <-errc
	if errors.Is(err, build.ErrDevRestart) {
		// Wait for the other side (the server finishing its sidecar cleanup).
		if err2 := <-errc; errors.Is(err2, build.ErrDevRestart) {
			return true
		} else if err2 != nil {
			err = err2
		}
	}
	if err == nil {
		// A clean shutdown (Ctrl+C / SIGTERM).
		return false
	}
	fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
	exit(1)
	return false
}

func runServe(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	defer startRunLog(root, cfg, args[0])()
	env := loadProjectEnv(root, "production", flags.Verbose)

	// `krate start` (and `serve --no-build`) serves the existing build output
	// as-is; `serve`/`preview` build first.
	noBuild := flags.NoBuild || (len(args) > 0 && args[0] == "start")
	start := time.Now()
	if noBuild {
		if !hasBuildOutput(cfg.OutDir) {
			fmt.Fprintf(os.Stderr, "%sError:%s no build output found in %s\n", cRed, cReset, cfg.OutDir)
			fmt.Fprintf(os.Stderr, "  Run `krate build` first, or use `krate serve` to build and serve in one step.\n")
			exit(1)
		}
		fmt.Printf("%s%s  Serving existing build in %s...%s\n", cBold, cCyan, cfg.OutDir, cReset)
	} else {
		fmt.Printf("%s%s  Building for preview...%s\n", cBold, cCyan, cReset)
		builder := build.New(root, cfg)
		builder.Verbose = flags.Verbose
		builder.Env = env
		if err := builder.BuildAll(); err != nil {
			fmt.Fprintf(os.Stderr, "%sBuild error:%s %s\n", cRed, cReset, build.BuildErrorSummary(err))
			exit(1)
		}
		fmt.Printf("%s%s  Build complete. Starting server...%s\n", cBold, cGreen, cReset)
	}

	if err := build.Serve(root, cfg, start); err != nil {
		fmt.Fprintf(os.Stderr, "%sServe error:%s %v\n", cRed, cReset, err)
		exit(1)
	}
}

// hasBuildOutput reports whether outDir looks like a finished krate build.
func hasBuildOutput(outDir string) bool {
	for _, name := range []string{"manifest.json", "index.html", ".krate"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err == nil {
			return true
		}
	}
	// A site with no root page (only /docs/..., or SSR pages and an API) has no
	// top-level index.html: any built page counts.
	found := false
	_ = filepath.WalkDir(outDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".html") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// runDeploy builds the site and writes host-specific adapter files so the
// output can be uploaded to a static host as-is.
func runDeploy(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	defer startRunLog(root, cfg, "deploy")()
	env := loadProjectEnv(root, "production", flags.Verbose)

	target := flags.Target
	if target == "" {
		target = cfg.Deploy.Target
	}
	if target == "" {
		target = "netlify"
	}
	// Fail before the build: an unknown target used to run the whole build
	// first, and with server features it was reported as a feature mismatch.
	if err := checkDeployTarget(target); err != nil {
		fmt.Fprintf(os.Stderr, "%sDeploy error:%s %v\n", cRed, cReset, err)
		exit(1)
	}

	fmt.Printf("%s%s  Building %s -> %s%s\n", cBold, cCyan, root, cfg.OutDir, cReset)
	builder := build.New(root, cfg)
	builder.Verbose = flags.Verbose
	builder.Env = env
	if err := builder.BuildAll(); err != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %s\n", cRed, cReset, build.BuildErrorSummary(err))
		exit(1)
	}

	dopts := deploy.Options{StaticOnly: flags.StaticOnly}
	if deploy.WantsServer(target, deploy.ServerFeatures(root, cfg.OutDir), flags.StaticOnly) {
		fmt.Printf("%s%s  Bundling the server for %s%s\n", cBold, cCyan, target, cReset)
		plan, err := build.BuildServerBundle(root, cfg, target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sDeploy error:%s %v\n", cRed, cReset, err)
			exit(1)
		}
		dopts.Server = plan
	}
	files, err := deploy.Emit(target, cfg, root, cfg.OutDir, dopts)
	if dopts.Server != nil {
		for _, w := range dopts.Server.Warnings {
			fmt.Fprintf(os.Stderr, "%s! warning:%s %s\n", cYellow, cReset, w)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sDeploy error:%s %v\n", cRed, cReset, err)
		exit(1)
	}
	if flags.StaticOnly {
		if features := deploy.ServerFeatures(root, cfg.OutDir); len(features) > 0 {
			fmt.Fprintf(os.Stderr, "%s! --static-only:%s publishing static pages only; %s will not run on %s\n", cYellow, cReset, strings.Join(features, ", "), target)
		}
	}
	fmt.Printf("%s%s  %s adapter ready%s (%d file(s))\n", cBold, cGreen, target, cReset, len(files))
	for _, f := range files {
		if rel, err := filepath.Rel(root, f); err == nil {
			fmt.Printf("    %s%s%s\n", cGray, filepath.ToSlash(rel), cReset)
		}
	}
}

// checkDeployTarget rejects a deploy target no adapter handles.
func checkDeployTarget(target string) error {
	for _, t := range deploy.Targets {
		if t == target {
			return nil
		}
	}
	return fmt.Errorf("unknown deploy target %q (want one of: %s; set it with --target or deploy.target in the config)", target, strings.Join(deploy.Targets, ", "))
}

// runInspect builds the site and emits its machine-readable model (routes,
// sources, render modes, dependencies, emitted files). `--json` prints the
// model as JSON on stdout with build chatter suppressed so the output is
// pipeable.
func runFix(flags cliFlags, args []string) {
	root := "."
	if len(args) > 1 {
		root = args[1]
	}
	abs, err := filepath.Abs(root)
	if err == nil {
		err = checkProjectDir(abs)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		exit(1)
	}
	requireConfigFlag(abs, flags)
	res, err := fix.Run(abs, flags.Apply)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sFix error:%s %v\n", cRed, cReset, err)
		exit(1)
	}
	if len(res.Changes) == 0 {
		fmt.Printf("fix: no safe fixes found (%d file(s) skipped as unparseable)\n", len(res.Skipped))
		return
	}
	noun := "change"
	if len(res.Changes) != 1 {
		noun = "changes"
	}
	if flags.Apply {
		fmt.Printf("fix: applied %d %s\n", len(res.Changes), noun)
	} else {
		fmt.Printf("fix: %d safe %s (dry run)\n", len(res.Changes), noun)
	}
	for _, c := range res.Changes {
		fmt.Printf("  %s:%d:%d  %s  %s -> %s\n", c.File, c.Line, c.Col, c.Code, c.From, c.To)
	}
	if !flags.Apply {
		fmt.Println("Re-run with --apply to write these changes.")
	}
	if flags.Verbose {
		for _, s := range res.Skipped {
			fmt.Fprintf(os.Stderr, "  skipped %s: %s\n", s.File, s.Reason)
		}
	}
}

func runInspect(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	env := loadProjectEnv(root, "production", flags.Verbose)

	builder := build.New(root, cfg)
	builder.Verbose = flags.Verbose
	builder.Env = env

	var buildErr error
	withSuppressedStdout(func() { buildErr = builder.BuildAll() })
	if buildErr != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %s\n", cRed, cReset, build.BuildErrorSummary(buildErr))
		exit(1)
	}
	graph, err := builder.SiteGraph()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sInspect error:%s %v\n", cRed, cReset, err)
		exit(1)
	}

	if flags.JSON {
		data, err := json.MarshalIndent(graph, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sInspect error:%s %v\n", cRed, cReset, err)
			exit(1)
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
	if len(graph.Segments) > 0 {
		fmt.Printf("\n  %d segment page dir(s)\n\n", len(graph.Segments))
		for _, sg := range graph.Segments {
			scope := ""
			if sg.Group {
				scope = " (route group)"
			}
			if sg.NotFound != "" {
				fmt.Printf("  %-9s %-36s %s%s\n", "not-found", sg.Dir, sg.NotFound, scope)
			}
			if sg.Error != "" {
				fmt.Printf("  %-9s %-36s %s%s\n", "error", sg.Dir, sg.Error, scope)
			}
		}
	}
}

// withSuppressedStdout runs fn with os.Stdout redirected to a pipe that is
// drained to io.Discard, so library build chatter never corrupts structured
// stdout output.
func withSuppressedStdout(fn func()) {
	// The build's progress writer captured os.Stdout when the package
	// initialised, so swapping os.Stdout alone does not silence it.
	build.SetOutput(io.Discard)
	defer build.SetOutput(os.Stdout)
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
		exit(1)
	}

	fmt.Printf("%s%s  Types written to %s%s\n", cBold, cGreen, filepath.Join(root, ".krate", "types"), cReset)
}

// runTypecheck generates types and runs the project's tsc.
func runTypecheck(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	if errs := build.GenerateTypes(root, cfg); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "%s  Type error:%s %v\n", cRed, cReset, e)
		}
		exit(1)
	}
	out, ok, err := typecheck.Run(root)
	if errors.Is(err, typecheck.ErrNoTSC) {
		fmt.Fprintf(os.Stderr, "%sType check error:%s %v; add it with `pnpm add -D typescript`\n", cRed, cReset, err)
		exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sType check error:%s %v\n", cRed, cReset, err)
		exit(1)
	}
	fmt.Print(out)
	if !ok {
		exit(1)
	}
	fmt.Printf("%s%s  No type errors%s\n", cBold, cGreen, cReset)
}

// runGoInit sets up the Go workspace (go.mod/go.work + vendored helpers) so
// editors resolve the goext import path for Go API routes and components.
func runGoInit(flags cliFlags, args []string) {
	root, _ := resolveConfig(flags, args)
	notes, err := gohost.Init(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sGo workspace error:%s %v\n", cRed, cReset, err)
		exit(1)
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
	defer startRunLog(root, cfg, "check")()
	env := loadProjectEnv(root, "production", flags.Verbose)

	fmt.Printf("%s%s  Building for checks %s -> %s%s\n", cBold, cCyan, root, cfg.OutDir, cReset)
	start := time.Now()

	builder := build.New(root, cfg)
	builder.Verbose = flags.Verbose
	builder.Env = env
	// This command owns the single check pass (CheckSite re-reads dist/), so
	// suppress the in-build gates to avoid duplicate reporting.
	builder.SkipQualityChecks = true
	if err := builder.BuildAll(); err != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %s\n", cRed, cReset, build.BuildErrorSummary(err))
		exit(1)
	}

	findings, checkCfg, err := builder.CheckSite(true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sCheck error:%s %v\n", cRed, cReset, err)
		exit(1)
	}

	fopts := check.DefaultFormatOptions()

	if len(findings) == 0 {
		fmt.Printf("%s%s  + No quality issues found.%s\n", cBold, cGreen, cReset)
		return
	}

	errs, warns := check.Counts(findings)
	fmt.Printf("\n%s%s  Quality report%s %s(%s)%s\n\n", cBold, cCyan, cReset, cGray, check.Summary(errs, warns, fopts), cReset)
	fmt.Print(check.FormatWith(findings, fopts))
	fmt.Printf("\n%s  Checks: %s in %s%s\n", cCyan, check.Summary(errs, warns, fopts), time.Since(start).Round(time.Millisecond), cReset)

	if check.Failing(findings, checkCfg.FailOn) {
		fmt.Fprintf(os.Stderr, "\n%s%s  x Quality checks failed.%s\n", cBold, cRed, cReset)
		exit(1)
	}

	fmt.Printf("\n%s%s  ! Passed with warnings (failOn=%s).%s\n", cBold, cYellow, checkCfg.FailOn, cReset)
}

// runClean removes generated output so the next build starts from scratch. By
// default that is the build output directory and the compiler cache; --dist,
// --cache and --all pick narrower or wider sets. Without --all, generated
// types and sidecar manifests under .krate are left in place, since they are
// regenerated on demand.
func runClean(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	fmt.Printf("%s%s  Cleaning %s%s\n", cBold, cCyan, root, cReset)

	result, err := build.CleanWith(root, cfg, build.CleanOptions{
		Dist:  flags.CleanDist,
		Cache: flags.CleanCache,
		All:   flags.CleanAll,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sClean error:%s %v\n", cRed, cReset, err)
		exit(1)
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
		fmt.Printf("  %s+%s Removed %s %s(%s)%s\n", cGreen, cReset, filepath.ToSlash(rel), cGray, build.FormatBytes(result.Sizes[dir]), cReset)
	}
	fmt.Printf("%s%s  Freed %s%s\n", cBold, cGreen, build.FormatBytes(result.Freed), cReset)
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

	var extraRoots []mcp.WorkspaceRoot
	for _, spec := range flags.Roots {
		name, path, ok := strings.Cut(spec, "=")
		if !ok {
			path = name
			name = ""
		}
		extraRoots = append(extraRoots, mcp.WorkspaceRoot{Name: name, Path: path})
	}
	svc := mcp.NewService(mcp.Options{
		Root:       root,
		ExtraRoots: extraRoots,
		Cfg:        cfg,
		Env:        env,
		Verbose:    flags.Verbose,
	})
	defer svc.Close()
	if flags.HTTP {
		port := flags.Port
		if port == 0 {
			port = 7331
		}
		token := flags.Token
		if token == "" {
			token = os.Getenv("KRATE_MCP_TOKEN")
		}
		host := flags.Host
		if host == "" {
			host = "127.0.0.1"
		}
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		fmt.Fprintf(os.Stderr, "  MCP HTTP server -> http://%s/mcp (POST; GET for SSE)\n", addr)
		if err := mcp.ServeHTTP(svc, addr, token); err != nil {
			fmt.Fprintf(os.Stderr, "%sMCP HTTP error:%s %v\n", cRed, cReset, err)
			exit(1)
		}
		return
	}

	srv := mcp.NewServer(protocolOut, "krate", version)
	srv.SetClient("stdio", false)
	svc.Register(srv)

	if err := srv.Serve(os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "%sMCP error:%s %v\n", cRed, cReset, err)
		exit(1)
	}
}
