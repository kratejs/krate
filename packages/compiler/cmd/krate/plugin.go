package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/execx"
)

// runPlugin implements `krate plugin <subcommand>`. Currently the only
// subcommand is `add <package>`, which installs an npm plugin package and
// prints the config snippet needed to register it. It deliberately does not
// edit krate.config.ts (that file is user-owned and may use arbitrary logic);
// printing the snippet keeps the operation predictable.
func runPlugin(flags cliFlags, args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "%sUsage:%s krate plugin add <package> [dir]\n", cYellow, cReset)
		os.Exit(1)
	}

	switch args[1] {
	case "add":
		if len(args) < 3 {
			fmt.Fprintf(os.Stderr, "%sUsage:%s krate plugin add <package> [dir]\n", cYellow, cReset)
			os.Exit(1)
		}
		runPluginAdd(flags, args[2], args[3:])
	default:
		fmt.Fprintf(os.Stderr, "%sUnknown plugin subcommand:%s %s\n", cRed, cReset, args[1])
		fmt.Fprintf(os.Stderr, "Available: add\n")
		os.Exit(1)
	}
}

// runPluginAdd installs an npm package into the project and prints the config
// snippet to register it. The package manager is inferred from the lockfile.
func runPluginAdd(flags cliFlags, pkg string, rest []string) {
	root := "."
	if len(rest) > 0 {
		root = rest[0]
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	pm, installArgs := detectPackageManager(absRoot, "add", pkg)
	fmt.Printf("%s%s  Installing %s with %s...%s\n", cBold, cCyan, pkg, pm, cReset)

	ctx, cancel := context.WithTimeout(context.Background(), execx.Timeout("KRATE_INSTALL_TIMEOUT", 30*time.Minute))
	defer cancel()
	cmd := execx.Command(ctx, pm, installArgs...)
	cmd.Dir = absRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "%sInstall failed:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	registerSnippet := pluginRegisterSnippet(pkg)
	fmt.Printf("\n%s%s  Installed.%s Add this to %skrate.config.ts%s:\n\n", cBold, cGreen, cReset, cCyan, cReset)
	fmt.Println(registerSnippet)
	fmt.Printf("\n  Then run %skrate build%s.\n", cCyan, cReset)
}

// pluginRegisterSnippet returns a best-effort config snippet for a plugin
// package. It cannot know the exported factory name, so it emits an import and
// a `Plugins` placeholder the user completes.
func pluginRegisterSnippet(pkg string) string {
	ident := pluginIdent(pkg)
	return fmt.Sprintf(`import { %s } from '%s';

export default defineConfig({
  // ...your existing config
  plugins: [
    %s(),
  ],
});`, ident, pkg, ident)
}

// pluginIdent derives a plausible local identifier from a package name
// (e.g. "@scope/my-plugin" -> "myPlugin").
func pluginIdent(pkg string) string {
	name := pkg
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	if len(parts) == 0 {
		return "plugin"
	}
	out := strings.ToLower(parts[0])
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		out += strings.ToUpper(p[:1]) + p[1:]
	}
	return out
}

// detectPackageManager picks the package manager from the lockfile present in
// root and returns the binary plus args to add a dependency.
func detectPackageManager(root, verb, pkg string) (string, []string) {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}

	// Read packageManager field from package.json when present.
	if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		var pj struct {
			PackageManager string `json:"packageManager"`
		}
		if json.Unmarshal(data, &pj) == nil && pj.PackageManager != "" {
			name := pj.PackageManager
			if i := strings.Index(name, "@"); i >= 0 {
				name = name[:i]
			}
			switch name {
			case "pnpm":
				return "pnpm", []string{verb, pkg}
			case "yarn":
				return "yarn", []string{verb, pkg}
			case "bun":
				return "bun", []string{verb, pkg}
			case "npm":
				return "npm", []string{"install", pkg}
			}
		}
	}

	switch {
	case has("pnpm-lock.yaml"):
		return "pnpm", []string{verb, pkg}
	case has("yarn.lock"):
		return "yarn", []string{verb, pkg}
	case has("bun.lockb"), has("bun.lock"):
		return "bun", []string{verb, pkg}
	default:
		return "npm", []string{"install", pkg}
	}
}
