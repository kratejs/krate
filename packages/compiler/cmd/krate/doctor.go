package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/kratejs/krate/packages/compiler/internal/config"
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
		fmt.Printf("  %s✓ go%s         available (Go API routes)\n", cGreen, cReset)
	} else {
		fmt.Printf("  %s· go%s         not found (optional; only for src/api/*.go)\n", cGray, cReset)
	}

	// Key files.
	reportFile(root, "tsconfig.json")
	reportFile(root, "krate.state.json")
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

// printHelpCommand handles `krate help [command]`.
func printHelpCommand(w io.Writer, args []string) {
	if len(args) == 0 {
		printUsage(w)
		return
	}
	printCommandUsage(w, args[0])
}
