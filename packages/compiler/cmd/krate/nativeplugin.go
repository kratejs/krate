package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native/plugins"
)

// runNativePlugin implements `krate native plugin <command>`.
func runNativePlugin(flags cliFlags, args []string) {
	if len(args) == 0 || args[0] != "init" {
		fmt.Fprintf(os.Stderr, "Usage: krate native plugin init <name> [dir]\n\n  init   create a native plugin: krate-plugin.json, a Go module with an example op and stream, a test\n")
		exit(1)
	}
	runNativePluginInit(args[1:])
}

func runNativePluginInit(args []string) {
	if len(args) == 0 || len(args) > 2 {
		fmt.Fprintf(os.Stderr, "Usage: krate native plugin init <name> [dir]\n")
		exit(1)
	}
	name := args[0]
	dir := filepath.Join("native-plugins", name)
	if len(args) == 2 {
		dir = args[1]
	}
	opts := plugins.ScaffoldOptions{Name: name}
	if abs, err := filepath.Abs(dir); err == nil {
		if mod := build.FindMonorepoNative(abs); mod != "" {
			if rel, err := filepath.Rel(abs, mod); err == nil {
				opts.NativeReplace = rel
			}
		}
	}
	files, d := plugins.Scaffold(dir, opts)
	if d != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %s\n", cRed, cReset, diag.FormatDiagnostics([]error{*d}))
		exit(1)
	}
	fmt.Printf("%s%s  Created the plugin %s in %s%s\n", cBold, cGreen, name, dir, cReset)
	for _, f := range files {
		fmt.Printf("    %s\n", f)
	}
	fmt.Printf("\n  next:\n")
	fmt.Printf("    cd %s && go mod tidy && go test ./...\n", dir)
	fmt.Printf("    add it to krate.config.ts:  native: { plugins: ['./%s'] }\n", filepath.ToSlash(dir))
	fmt.Printf("    use it in a page:           import { Hello } from 'krate/plugin/%s'\n", name)
}
