package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/execx"
)

const krateScope = "@krate/"

// upgradeDep is one @krate/* dependency declared in the project's package.json.
type upgradeDep struct {
	Name      string
	Declared  string
	Dev       bool
	Installed string
}

// readKrateDeps lists the @krate/* packages the project's package.json
// declares (dependencies, optionalDependencies and devDependencies), with the
// version installed in node_modules when there is one.
func readKrateDeps(root string) ([]upgradeDep, error) {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no package.json in %s", root)
		}
		return nil, err
	}
	var pkg struct {
		Dependencies         map[string]string `json:"dependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("package.json: %w", err)
	}
	installed := map[string]string{}
	for _, p := range installedKratePackages(root) {
		installed[p.Name] = p.Version
	}
	var deps []upgradeDep
	add := func(m map[string]string, dev bool) {
		for name, declared := range m {
			if strings.HasPrefix(name, krateScope) {
				deps = append(deps, upgradeDep{Name: name, Declared: declared, Dev: dev, Installed: installed[name]})
			}
		}
	}
	add(pkg.Dependencies, false)
	add(pkg.OptionalDependencies, false)
	add(pkg.DevDependencies, true)
	sort.Slice(deps, func(i, j int) bool { return deps[i].Name < deps[j].Name })
	return deps, nil
}

// upgradeCommand is one package manager invocation.
type upgradeCommand struct {
	Name string
	Args []string
}

func (c upgradeCommand) String() string { return c.Name + " " + strings.Join(c.Args, " ") }

// projectUpgradeCommands builds the install commands that move deps to target:
// one for the regular dependencies and one for the dev dependencies, so each
// stays where package.json declares it.
func projectUpgradeCommands(pm, target string, deps []upgradeDep, pnpmWorkspaceRoot bool) []upgradeCommand {
	var prod, dev []string
	for _, d := range deps {
		spec := d.Name + "@" + target
		if d.Dev {
			dev = append(dev, spec)
		} else {
			prod = append(prod, spec)
		}
	}
	build := func(specs []string, isDev bool) []upgradeCommand {
		if len(specs) == 0 {
			return nil
		}
		var args []string
		switch pm {
		case "npm":
			args = []string{"install", "--save-prod"}
			if isDev {
				args = []string{"install", "--save-dev"}
			}
		case "yarn":
			args = []string{"add"}
			if isDev {
				args = append(args, "-D")
			}
		case "bun":
			args = []string{"add"}
			if isDev {
				args = append(args, "-d")
			}
		default:
			args = []string{"add"}
			if isDev {
				args = append(args, "-D")
			}
			if pm == "pnpm" && pnpmWorkspaceRoot {
				args = append(args, "-w")
			}
		}
		return []upgradeCommand{{Name: pm, Args: append(args, specs...)}}
	}
	return append(build(prod, false), build(dev, true)...)
}

// globalUpgradeCommand builds the command that installs the CLI package
// globally.
func globalUpgradeCommand(pm, target string) upgradeCommand {
	spec := krateScope + "core@" + target
	switch pm {
	case "pnpm":
		return upgradeCommand{Name: "pnpm", Args: []string{"add", "-g", spec}}
	case "yarn":
		return upgradeCommand{Name: "yarn", Args: []string{"global", "add", spec}}
	case "bun":
		return upgradeCommand{Name: "bun", Args: []string{"add", "-g", spec}}
	}
	return upgradeCommand{Name: "npm", Args: []string{"install", "-g", spec}}
}

// upgradeOptions drive executeUpgrade. Lookup and Run are injectable so the
// planning can be tested without a network or a package manager.
type upgradeOptions struct {
	Root   string
	Target string // a version or a dist-tag
	Global bool
	DryRun bool
	PM     string
	Lookup func(tag string) (string, error)
	Run    func(dir string, c upgradeCommand) error
}

// executeUpgrade resolves the target version, prints the plan and, unless
// DryRun, runs the package manager.
func executeUpgrade(w io.Writer, o upgradeOptions) error {
	target := o.Target
	if _, isVersion := parseSemver(target); !isVersion {
		v, err := o.Lookup(target)
		if err != nil {
			return fmt.Errorf("cannot look up %s%s@%s: %w (pass an exact version to skip the lookup)", krateScope, "core", target, err)
		}
		fmt.Fprintf(w, "  %s resolves to v%s\n", target, v)
		target = v
	}

	var cmds []upgradeCommand
	if o.Global {
		cmds = []upgradeCommand{globalUpgradeCommand(o.PM, target)}
		fmt.Fprintf(w, "  Upgrading the global krate CLI (%s @krate/core) to v%s\n", o.PM, target)
	} else {
		deps, err := readKrateDeps(o.Root)
		if err != nil {
			return err
		}
		if len(deps) == 0 {
			return fmt.Errorf("%s declares no %s* packages; use `krate upgrade --global` to upgrade the installed CLI", filepath.Join(o.Root, "package.json"), krateScope)
		}
		fmt.Fprintf(w, "  Upgrading %d %s package(s) in %s with %s\n", len(deps), strings.TrimSuffix(krateScope, "/"), o.Root, o.PM)
		changed := 0
		for _, d := range deps {
			from := d.Installed
			if from == "" {
				from = d.Declared + " (not installed)"
			}
			note := ""
			if d.Installed == target {
				note = " (already there)"
			} else {
				changed++
				if c, ok := compareSemver(target, d.Installed); ok && c < 0 {
					note = " (downgrade)"
				}
			}
			fmt.Fprintf(w, "    %-34s %s -> %s%s\n", d.Name, from, target, note)
		}
		if changed == 0 {
			fmt.Fprintf(w, "  Everything is already at v%s.\n", target)
			return nil
		}
		_, workspace := statFile(filepath.Join(o.Root, "pnpm-workspace.yaml"))
		cmds = projectUpgradeCommands(o.PM, target, deps, workspace)
	}

	for _, c := range cmds {
		fmt.Fprintf(w, "  $ %s\n", c)
	}
	if o.DryRun {
		fmt.Fprintf(w, "  Dry run: nothing was changed.\n")
		return nil
	}
	for _, c := range cmds {
		if err := o.Run(o.Root, c); err != nil {
			return fmt.Errorf("%s failed: %w", c, err)
		}
	}
	if !o.Global {
		fmt.Fprintf(w, "  Installed:\n")
		for _, p := range installedKratePackages(o.Root) {
			fmt.Fprintf(w, "    %-34s %s\n", p.Name, p.Version)
		}
	}
	fmt.Fprintf(w, "  Done. Read https://krate.js.org/docs/guides/upgrading/ for changes between versions.\n")
	return nil
}

func statFile(path string) (os.FileInfo, bool) {
	info, err := os.Stat(path)
	return info, err == nil
}

// runUpgradeCommand runs a package manager command with its output on the
// terminal.
func runUpgradeCommand(dir string, c upgradeCommand) error {
	if _, err := exec.LookPath(c.Name); err != nil {
		return fmt.Errorf("%s was not found on PATH", c.Name)
	}
	cmd := execx.Command(context.Background(), c.Name, c.Args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// runUpgrade implements `krate upgrade [version|tag] [dir] [--global] [--dry-run]`.
func runUpgrade(flags cliFlags, args []string) {
	rest := args[1:]
	targetArg, dirArg := "", "."
	if len(rest) > 0 {
		// `krate upgrade ./site` names a directory; anything else is the version.
		if info, err := os.Stat(rest[0]); err == nil && info.IsDir() {
			if _, ok := parseSemver(rest[0]); !ok {
				dirArg, rest = rest[0], rest[1:]
			}
		}
	}
	if len(rest) > 0 {
		targetArg, rest = rest[0], rest[1:]
	}
	if len(rest) > 0 {
		dirArg = rest[0]
	}
	target, err := parseUpgradeTarget(targetArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		exit(1)
	}
	root, err := filepath.Abs(dirArg)
	if err == nil {
		err = checkProjectDir(root)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		exit(1)
	}
	err = executeUpgrade(os.Stdout, upgradeOptions{
		Root:   root,
		Target: target,
		Global: flags.Global,
		DryRun: flags.DryRun,
		PM:     packageManagerFor(root),
		Lookup: func(tag string) (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			return registryVersion(ctx, registryBase(), krateScope+"core", tag)
		},
		Run: runUpgradeCommand,
	})
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			fmt.Fprintf(os.Stderr, "%sUpgrade error:%s the package manager exited with status %d\n", cRed, cReset, exitErr.ExitCode())
		} else {
			fmt.Fprintf(os.Stderr, "%sUpgrade error:%s %v\n", cRed, cReset, err)
		}
		exit(1)
	}
}
