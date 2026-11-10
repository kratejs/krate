package main

import (
	"context"
	"encoding/json"
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
)

// infoReport is the environment summary printed by `krate info`. It holds no
// secrets: no environment variables and no config values beyond the few
// structural settings below.
type infoReport struct {
	Krate          string                  `json:"krate"`
	OS             string                  `json:"os"`
	Arch           string                  `json:"arch"`
	Go             string                  `json:"go"`
	Node           string                  `json:"node,omitempty"`
	PackageManager string                  `json:"packageManager"`
	PMVersion      string                  `json:"packageManagerVersion,omitempty"`
	Packages       []installedKratePackage `json:"packages,omitempty"`
	Project        string                  `json:"project"`
	Config         *infoConfig             `json:"config,omitempty"`
	ConfigError    string                  `json:"configError,omitempty"`
}

type infoConfig struct {
	File     string   `json:"file"`
	Output   string   `json:"output"`
	Runtime  string   `json:"runtime,omitempty"`
	PagesDir string   `json:"pagesDir"`
	OutDir   string   `json:"outDir"`
	BasePath string   `json:"basePath,omitempty"`
	Plugins  []string `json:"plugins,omitempty"`
	Logs     bool     `json:"logs,omitempty"`
}

// toolVersion runs `name args...` and returns its trimmed first output line, or
// "" when the tool is missing or fails.
func toolVersion(name string, args ...string) string {
	if _, err := exec.LookPath(name); err != nil {
		return ""
	}
	out, err := execx.Output(context.Background(), 5*time.Second, "", nil, name, args...)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line)
}

// shortPath replaces the home directory with ~ so a pasted report does not
// carry the user name.
func shortPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.ToSlash(p)
	}
	if rel, err := filepath.Rel(home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "~/" + filepath.ToSlash(rel)
	}
	return filepath.ToSlash(p)
}

func collectInfo(root string, flags cliFlags) infoReport {
	pm := packageManagerFor(root)
	rep := infoReport{
		Krate:          version,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		Go:             strings.TrimPrefix(runtime.Version(), "go"),
		Node:           strings.TrimPrefix(toolVersion("node", "--version"), "v"),
		PackageManager: pm,
		PMVersion:      toolVersion(pm, "--version"),
		Packages:       installedKratePackages(root),
		Project:        shortPath(root),
	}
	cfg, err := config.Load(root, flags.ConfigPath)
	if err != nil {
		rep.ConfigError = err.Error()
		return rep
	}
	cfg.Resolve(root)
	ic := &infoConfig{
		File:     shortPath(configPathFor(root, flags.ConfigPath)),
		Output:   cfg.Output,
		Runtime:  cfg.Runtime,
		PagesDir: relOrDash(root, cfg.PagesDir),
		OutDir:   relOrDash(root, cfg.OutDir),
		BasePath: cfg.BaseURLPath(),
		Logs:     cfg.Logs.Enabled,
	}
	if ic.Output == "" {
		ic.Output = "auto"
	}
	for _, p := range cfg.Plugins {
		name := p.Name
		if name == "" {
			name = filepath.Base(p.Module)
		}
		if name != "" {
			ic.Plugins = append(ic.Plugins, name)
		}
	}
	rep.Config = ic
	return rep
}

// writeInfo renders the report as Markdown for pasting into an issue.
func writeInfo(w io.Writer, rep infoReport) {
	val := func(s string) string {
		if s == "" {
			return "not found"
		}
		return s
	}
	fmt.Fprintf(w, "### Krate environment\n\n")
	fmt.Fprintf(w, "- krate: v%s\n", rep.Krate)
	fmt.Fprintf(w, "- OS: %s/%s\n", rep.OS, rep.Arch)
	fmt.Fprintf(w, "- Go: %s\n", rep.Go)
	fmt.Fprintf(w, "- Node: %s\n", val(rep.Node))
	fmt.Fprintf(w, "- Package manager: %s %s\n", rep.PackageManager, val(rep.PMVersion))
	if len(rep.Packages) == 0 {
		fmt.Fprintf(w, "- @krate packages: none installed in node_modules\n")
	} else {
		fmt.Fprintf(w, "- @krate packages:\n")
		for _, p := range rep.Packages {
			fmt.Fprintf(w, "  - %s %s\n", p.Name, p.Version)
		}
	}
	fmt.Fprintf(w, "- Project: %s\n", rep.Project)
	switch {
	case rep.ConfigError != "":
		fmt.Fprintf(w, "- Config: failed to load: %s\n", rep.ConfigError)
	case rep.Config != nil:
		c := rep.Config
		fmt.Fprintf(w, "- Config: %s\n", c.File)
		fmt.Fprintf(w, "  - output: %s\n", c.Output)
		if c.Runtime != "" {
			fmt.Fprintf(w, "  - runtime: %s\n", c.Runtime)
		}
		fmt.Fprintf(w, "  - pagesDir: %s, outDir: %s\n", c.PagesDir, c.OutDir)
		if c.BasePath != "" {
			fmt.Fprintf(w, "  - basePath: %s\n", c.BasePath)
		}
		if len(c.Plugins) > 0 {
			fmt.Fprintf(w, "  - plugins: %s\n", strings.Join(c.Plugins, ", "))
		}
		if c.Logs {
			fmt.Fprintf(w, "  - logs: enabled\n")
		}
	}
}

// runInfo implements `krate info [dir] [--json]`.
func runInfo(flags cliFlags, args []string) {
	root := "."
	if len(args) > 1 {
		root = args[1]
	}
	root, _ = filepath.Abs(root)
	if err := checkProjectDir(root); err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		exit(1)
	}
	requireConfigFlag(root, flags)
	rep := collectInfo(root, flags)
	if flags.JSON {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
			exit(1)
		}
		fmt.Println(string(data))
		return
	}
	writeInfo(os.Stdout, rep)
}
