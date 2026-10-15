package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
)

// runDoctor prints a diagnostic summary of the project and toolchain, so setup
// problems (missing config, stray warnings, absent runtimes) are obvious before
// a build.
func runDoctor(flags cliFlags, args []string) {
	root := "."
	if len(args) > 1 {
		root = args[1]
	}
	root, _ = filepath.Abs(root)

	fmt.Printf("%sKrate doctor%s\n\n", cBold, cReset)
	fmt.Printf("  krate        v%s\n", version)
	fmt.Printf("  go           %s\n", runtime.Version())
	fmt.Printf("  os/arch      %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  project      %s\n", root)

	// Config.
	cfg, err := config.Load(root, flags.ConfigPath)
	if err != nil {
		fmt.Printf("  %s✗ config%s     %v\n", cRed, cReset, err)
	} else {
		for _, w := range config.Warnings {
			fmt.Printf("  %s⚠ config%s     %s\n", cYellow, cReset, w)
		}
		fmt.Printf("  %s✓ config%s     %s\n", cGreen, cReset, configPathFor(root, flags.ConfigPath))
		fmt.Printf("  output       %s\n", displayOutput(cfg.Output))
		fmt.Printf("  pages        %s\n", relOrDash(root, cfg.PagesDir))
		fmt.Printf("  outDir       %s\n", relOrDash(root, cfg.OutDir))
		if bp := cfg.BaseURLPath(); bp != "" {
			fmt.Printf("  basePath     %s\n", bp)
		}
	}

	// Runtimes.
	checkTool("node", "API/SSR sidecar + tsx config execution")
	checkTool("npx", "tsx config bootstrap")
	if _, err := exec.LookPath("go"); err == nil {
		fmt.Printf("  %s✓ go%s         available (Go API routes + components)\n", cGreen, cReset)
		if out, err := execx.Output(context.Background(), 5*time.Second, "", nil, "go", "version"); err == nil {
			fmt.Printf("  %s   %s\n", "          ", strings.TrimSpace(string(out)))
		}
	} else {
		fmt.Printf("  %s· go%s         not found (optional; only for src/api/*.go and Go components)\n", cGray, cReset)
	}
	if cfg != nil {
		apiGo := countGoFiles(root, []string{gohost.DefaultAPIDir})
		compGo := countGoFiles(root, cfg.Go.ComponentDirList())
		if compGo > 0 {
			fmt.Printf("  go comps     %d file(s) under %s\n", compGo, strings.Join(cfg.Go.ComponentDirList(), ", "))
		}
		if apiGo+compGo > 0 {
			if v, ok := gohost.ModuleGoVersion(root); ok {
				fmt.Printf("  go.mod       go %s\n", v)
			}
			if gohost.GoextVendored(root) {
				fmt.Printf("  %s✓ goext%s      vendored (.krate/goext)\n", cGreen, cReset)
			} else {
				fmt.Printf("  %s· goext%s      not vendored (run `krate go:init`)\n", cGray, cReset)
			}
			if gohost.SetupMissing(root) {
				fmt.Printf("  %s· go:init%s    run `krate go:init` for editor intellisense\n", cGray, cReset)
			}
		}
	}

	// Node version (the sidecars need 20+).
	if _, err := exec.LookPath("node"); err == nil {
		if out, err := execx.Output(context.Background(), 5*time.Second, "", nil, "node", "--version"); err == nil {
			v := strings.TrimSpace(string(out))
			if nodeVersionAtLeast(v, 20) {
				fmt.Printf("  %s✓ node%s       %s\n", cGreen, cReset, v)
			} else {
				fmt.Printf("  %s⚠ node%s       %s (Krate sidecars need Node 20+)\n", cYellow, cReset, v)
			}
		}
	}

	// Dependencies: the config imports @krate/core.
	if _, err := os.Stat(filepath.Join(root, "node_modules", "@krate", "core", "package.json")); err == nil {
		fmt.Printf("  %s✓ deps%s       @krate/core present\n", cGreen, cReset)
	} else {
		fmt.Printf("  %s· deps%s       @krate/core not found (run your package manager install)\n", cGray, cReset)
	}

	// Cache directory writability.
	cacheDir := filepath.Join(root, ".krate", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		fmt.Printf("  %s⚠ cache%s      cannot create %s: %v\n", cYellow, cReset, cacheDir, err)
	} else {
		probe := filepath.Join(cacheDir, ".doctor-write-test")
		if err := os.WriteFile(probe, []byte("ok"), 0o644); err == nil {
			_ = os.Remove(probe)
			fmt.Printf("  %s✓ cache%s      writable (%s)\n", cGreen, cReset, cacheDir)
		} else {
			fmt.Printf("  %s⚠ cache%s      not writable: %v\n", cYellow, cReset, err)
		}
	}

	// Key files.
	reportFile(root, "tsconfig.json")
	reportFile(root, "krate.state.json")
}

// nodeVersionAtLeast reports whether a `node --version` string (e.g. "v20.11.1")
// is at least major.minor.
func nodeVersionAtLeast(v string, major int) bool {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.SplitN(v, ".", 2)
	if len(parts) == 0 {
		return false
	}
	n := 0
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n >= major
}

func configPathFor(root, explicit string) string {
	if explicit == "" {
		return filepath.Join(root, "krate.config.ts")
	}
	if filepath.IsAbs(explicit) {
		return explicit
	}
	return filepath.Join(root, explicit)
}

func displayOutput(o string) string {
	if o == "" {
		return "server (SSR/ISR/streaming enabled)"
	}
	return o
}

func relOrDash(root, p string) string {
	if p == "" {
		return "-"
	}
	if r, err := filepath.Rel(root, p); err == nil {
		return r
	}
	return p
}

func checkTool(name, purpose string) {
	if _, err := exec.LookPath(name); err == nil {
		fmt.Printf("  %s✓ %s%s%s available (%s)\n", cGreen, name, cReset, spaces(name), purpose)
		return
	}
	fmt.Printf("  %s· %s%s%s not found (%s)\n", cGray, name, cReset, spaces(name), purpose)
}

func spaces(name string) string {
	pad := 10 - len(name)
	if pad < 1 {
		pad = 1
	}
	s := ""
	for i := 0; i < pad; i++ {
		s += " "
	}
	return s
}

func reportFile(root, name string) {
	if _, err := os.Stat(filepath.Join(root, name)); err == nil {
		fmt.Printf("  %s✓ %s%s\n", cGreen, name, cReset)
	} else {
		fmt.Printf("  %s· %s%s (none)\n", cGray, name, cReset)
	}
}

// countGoFiles counts non-underscore `.go` files under the given directories.
func countGoFiles(root string, dirs []string) int {
	n := 0
	for _, d := range dirs {
		abs := d
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, d)
		}
		_ = filepath.WalkDir(abs, func(p string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return nil
			}
			if strings.HasSuffix(p, ".go") && !strings.HasPrefix(e.Name(), "_") {
				n++
			}
			return nil
		})
	}
	return n
}

// printHelpCommand handles `krate help [command]`.
func printHelpCommand(w io.Writer, args []string) {
	if len(args) == 0 {
		printUsage(w)
		return
	}
	printCommandUsage(w, args[0])
}
