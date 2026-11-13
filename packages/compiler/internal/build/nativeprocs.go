package build

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native/capability"
	"github.com/kratejs/krate/packages/compiler/internal/native/goenv"
	"github.com/kratejs/krate/packages/compiler/internal/native/hostts"
	"github.com/kratejs/krate/packages/compiler/internal/native/runtimes"
)

// workersDir is where worker programs live in an app directory.
const workersDir = "host/workers"

// NativeWorkerMeta is one worker in app.json (native.workers).
type NativeWorkerMeta struct {
	Name        string                `json:"name"`
	Runtime     string                `json:"runtime"`
	Entry       string                `json:"entry"`
	Command     []string              `json:"command,omitempty"`
	Args        []string              `json:"args,omitempty"`
	Permissions []string              `json:"permissions,omitempty"`
	Restart     *NativeWorkerRestartM `json:"restart,omitempty"`
	Autostart   bool                  `json:"autostart,omitempty"`
}

// NativeWorkerRestartM is the restart policy in app.json.
type NativeWorkerRestartM struct {
	Policy    string `json:"policy,omitempty"`
	Max       int    `json:"max,omitempty"`
	BackoffMs int    `json:"backoffMs,omitempty"`
}

// NativeAutomationMeta is the automation block of app.json.
type NativeAutomationMeta struct {
	Port int `json:"port,omitempty"`
}

// addWorkerPermissions makes sure the app grants every permission a worker
// lists (the shell refuses a worker that asks for more than the app has).
func addWorkerPermissions(perms []string, eff *config.NativeConfig) []string {
	var extra []string
	for _, w := range eff.Workers {
		for _, p := range w.Permissions {
			if !capability.Covers(perms, p) {
				extra = append(extra, p)
			}
		}
	}
	if len(extra) == 0 {
		return perms
	}
	if len(perms) == 0 {
		perms = append([]string(nil), config.NativeDefaultPermissions()...)
	}
	seen := map[string]bool{}
	for _, p := range perms {
		seen[p] = true
	}
	for _, p := range extra {
		if !seen[p] {
			seen[p] = true
			perms = append(perms, p)
		}
	}
	sort.Strings(perms)
	return perms
}

func procDiag(msg, hint string) error {
	return nativeDiagError([]diag.Diagnostic{diag.New("krate.config", 1, 1, msg, hint, "")}, "native error(s)")
}

// buildNativeProcs writes workers, extensions and the automation block of the
// manifest, and copies their files into the app directory.
func (b *Builder) buildNativeProcs(eff *config.NativeConfig, appDir string, opts NativeAppOptions, m *NativeManifest, res *NativeAppResult) error {
	m.Workers, m.Extensions, m.Automation = nil, nil, nil
	names := make([]string, 0, len(eff.Workers))
	for n := range eff.Workers {
		names = append(names, n)
	}
	sort.Strings(names)
	_ = os.RemoveAll(filepath.Join(appDir, filepath.FromSlash(workersDir)))
	for _, name := range names {
		w := eff.Workers[name]
		meta := NativeWorkerMeta{Name: name, Args: w.Args, Permissions: w.Permissions, Autostart: w.Autostart}
		if r := w.Restart; r != nil {
			meta.Restart = &NativeWorkerRestartM{Policy: r.Policy, Max: r.Max, BackoffMs: r.BackoffMs}
		}
		rel := workersDir + "/" + name
		switch {
		case w.Entry != "":
			rt := runtimes.Runtime(w.Runtime)
			if rt == "" {
				rt = runtimes.Node
			}
			rel += ".mjs"
			entry := filepath.Join(b.Root, filepath.FromSlash(w.Entry))
			if _, err := os.Stat(entry); err != nil {
				return procDiag(fmt.Sprintf("native.workers.%s.entry %q does not exist", name, w.Entry), "the path is relative to the project root")
			}
			ds, err := hostts.BuildWorker(b.Root, b.Cfg, entry, rt, filepath.Join(appDir, filepath.FromSlash(rel)))
			if err != nil {
				return err
			}
			if len(ds) > 0 {
				return nativeDiagError(ds, "worker bundle error(s)")
			}
			meta.Runtime, meta.Entry, meta.Command = string(rt), rel, runtimes.Command(rt, eff.Permissions)
		case w.Go != "":
			bin := rel
			if opts.goos() == "windows" {
				bin += ".exe"
			}
			if _, err := os.Stat(filepath.Join(appDir, filepath.FromSlash(workersDir))); err != nil {
				_ = os.MkdirAll(filepath.Join(appDir, filepath.FromSlash(workersDir)), 0o755)
			}
			env := append(goenv.Env(b.Root), "CGO_ENABLED=0", "GOOS="+opts.goos(), "GOARCH="+opts.goarch())
			if msg, err := NativeGoRunner(b.Root, env, "build", "-o", filepath.Join(appDir, filepath.FromSlash(bin)), w.Go); err != nil {
				text := strings.TrimSpace(string(msg))
				if text == "" {
					text = err.Error()
				}
				return procDiag(fmt.Sprintf("building the %s worker (%s) failed: %s", name, w.Go, text), "the package must be a main package that calls worker.Serve")
			}
			meta.Runtime, meta.Entry = "go", filepath.ToSlash(strings.TrimSuffix(bin, ".exe"))
			if opts.SkipGoCompile && res != nil {
				res.Notes = append(res.Notes, "the "+name+" Go worker is compiled for "+opts.goos()+"/"+opts.goarch()+" only; build on each target for the others")
			}
		default:
			src := filepath.Join(b.Root, filepath.FromSlash(w.Exec))
			if _, err := os.Stat(src); err != nil {
				return procDiag(fmt.Sprintf("native.workers.%s.exec %q does not exist", name, w.Exec), "the path is relative to the project root")
			}
			dst := filepath.Join(appDir, filepath.FromSlash(rel))
			if err := copyNativeFile(src, dst); err != nil {
				return err
			}
			_ = os.Chmod(dst, 0o755)
			meta.Runtime, meta.Entry = "exec", rel
		}
		m.Workers = append(m.Workers, meta)
	}
	_ = os.RemoveAll(filepath.Join(appDir, "extensions"))
	for _, e := range eff.Extensions {
		src := filepath.Join(b.Root, filepath.FromSlash(e))
		if _, err := os.Stat(filepath.Join(src, "manifest.json")); err != nil {
			return procDiag(fmt.Sprintf("native.extensions entry %q has no manifest.json", e), "list the folder of an unpacked extension")
		}
		dst := filepath.Join(appDir, "extensions", filepath.Base(e))
		if err := copyTreeFiles(src, dst); err != nil {
			return err
		}
		m.Extensions = append(m.Extensions, "extensions/"+filepath.Base(e))
	}
	if a := eff.Automation; a != nil {
		if opts.Dev || eff.Debug {
			m.Automation = &NativeAutomationMeta{Port: a.Port}
		} else if res != nil {
			res.Notes = append(res.Notes, "native.automation is ignored in a release build; set native.debug to keep the debugging endpoint")
		}
	}
	return nil
}

// copyTreeFiles copies the regular files under src to dst.
func copyTreeFiles(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyNativeFile(p, filepath.Join(dst, rel))
	})
}

func (o NativeAppOptions) goos() string {
	g, _, d := parseNativeTarget(o.Target)
	if d != nil {
		return runtime.GOOS
	}
	return g
}

func (o NativeAppOptions) goarch() string {
	_, a, d := parseNativeTarget(o.Target)
	if d != nil {
		return runtime.GOARCH
	}
	return a
}
