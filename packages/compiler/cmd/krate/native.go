package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native"
)

// nativeStubs are the `krate native` subcommands that exist in the command
// surface but are not built yet.
var nativeStubs = []string{"init", "dev", "build", "profile", "sign", "notarize", "doctor"}

func printNativeUsage(w io.Writer) {
	fmt.Fprintf(w, "Usage: krate native <command> [dir]\n\n")
	fmt.Fprintf(w, "Build desktop apps from a Krate project (preview).\n\n")
	fmt.Fprintf(w, "Commands:\n")
	fmt.Fprintf(w, "  check      Check host code placement and native target restrictions\n")
	fmt.Fprintf(w, "  init       Add native support to a project (not implemented yet)\n")
	fmt.Fprintf(w, "  dev        Run the app with hot reload (not implemented yet)\n")
	fmt.Fprintf(w, "  build      Build a desktop app (not implemented yet)\n")
	fmt.Fprintf(w, "  profile    Collect a profile for optimized builds (not implemented yet)\n")
	fmt.Fprintf(w, "  sign       Sign a built app (not implemented yet)\n")
	fmt.Fprintf(w, "  notarize   Notarize a built macOS app (not implemented yet)\n")
	fmt.Fprintf(w, "  doctor     Check the native toolchain (not implemented yet)\n")
}

func runNative(flags cliFlags, args []string) {
	if len(args) < 2 {
		printNativeUsage(os.Stderr)
		exit(1)
	}
	sub := args[1]
	if sub == "check" {
		runNativeCheck(flags, append([]string{"native"}, args[2:]...))
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
