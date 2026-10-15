package gohost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/gotsgen"
	krateversion "github.com/kratejs/krate/packages/compiler/internal/version"
)

// BuildInfo reports what Build produced.
type BuildInfo struct {
	Routes     []Route
	Components int
	BinPath    string
	SrcMap     map[string]string
}

// Build scaffolds and compiles the Go host. Returns (nil, nil) when there is no
// Go source. It always writes the route manifest (even when empty) so
// `krate serve` starts the host for runtime Go components.
func Build(root string, cfg *config.Config) (*BuildInfo, error) {
	res, err := Scaffold(root, cfg)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	binPath := HostBinaryPath(root)
	if err := compile(root, cfg, binPath, res); err != nil {
		return nil, err
	}
	if err := WriteManifest(root, res.Routes); err != nil {
		return nil, err
	}
	WriteTypes(root, res.TypeFiles)
	return &BuildInfo{Routes: res.Routes, Components: res.Components, BinPath: binPath, SrcMap: res.SrcMap}, nil
}

// BuildError carries structured Go compiler diagnostics so the dev overlay can
// show file:line:col.
type BuildError struct {
	Summary string
	Diags   []diag.Diagnostic
}

func (e *BuildError) Error() string { return e.Summary }
func (e *BuildError) Unwrap() []error {
	out := make([]error, len(e.Diags))
	for i, d := range e.Diags {
		out[i] = d
	}
	return out
}

// goErrLineRe matches a Go compiler error line `path:line:col: message`.
var goErrLineRe = regexp.MustCompile(`^(.+?\.go):(\d+):(?:(\d+):)?\s*(.*)$`)

func compile(root string, cfg *config.Config, binPath string, res *Result) error {
	modDir := HostModuleDir(root)
	key := dirHash(modDir) + "|" + dirHash(GoextVendorDir(root)) + "|" + configHash(cfg) + "|" + buildStamp()
	cacheDir := filepath.Join(root, KrateDirName, "cache", "goapi")
	keyPath := filepath.Join(cacheDir, "key")
	if _, err := os.Stat(binPath); err == nil {
		if prev, err := os.ReadFile(keyPath); err == nil && strings.TrimSpace(string(prev)) == key {
			return nil
		}
	}

	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("Go routes/components require the Go toolchain: %w", err)
	}
	if cfg.Go.TidyOrDefault() {
		out, stderr, err := execx.Run(context.Background(), execx.Timeout("KRATE_GOAPI_TIDY_TIMEOUT", 10*time.Minute), modDir, nil, "go", "mod", "tidy")
		if err != nil {
			return fmt.Errorf("go mod tidy failed:\n%s%s%w", string(out), string(stderr), err)
		}
	}
	out, stderr, err := execx.Run(context.Background(), execx.Timeout("KRATE_GOAPI_TIMEOUT", 15*time.Minute), modDir, nil, "go", "build", "-o", binPath, "./cmd/server")
	if err != nil {
		summary := "compiling Go host failed (add go.deps/go.replaces or go.tidy for third-party modules):\n" +
			remap(string(out), res.SrcMap) + remap(string(stderr), res.SrcMap)
		if diags := buildDiagnostics(string(stderr), res.SrcMap); len(diags) > 0 {
			return &BuildError{Summary: strings.TrimRight(summary, "\n"), Diags: diags}
		}
		return fmt.Errorf("%s%w", summary, err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err == nil {
		_ = os.WriteFile(keyPath, []byte(key), 0644)
	}
	return nil
}

func remap(s string, srcMap map[string]string) string {
	for gen, orig := range srcMap {
		s = strings.ReplaceAll(s, gen, orig)
	}
	return s
}

func buildDiagnostics(output string, srcMap map[string]string) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, line := range strings.Split(output, "\n") {
		m := goErrLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		lineNo, _ := strconv.Atoi(m[2])
		col := 0
		if m[3] != "" {
			col, _ = strconv.Atoi(m[3])
		}
		out = append(out, diag.Diagnostic{File: remap(m[1], srcMap), Line: lineNo, Col: col, Message: m[4], Hint: "Go host build"})
	}
	return out
}

func configHash(cfg *config.Config) string {
	data, _ := json.Marshal(cfg.Go)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func dirHash(dir string) string {
	h := sha256.New()
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		h.Write([]byte(filepath.ToSlash(rel)))
		h.Write([]byte{0})
		if data, err := os.ReadFile(path); err == nil {
			h.Write(data)
		}
		h.Write([]byte{0})
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

var (
	stampOnce  sync.Once
	stampValue string
)

func buildStamp() string {
	stampOnce.Do(func() {
		if exe, err := os.Executable(); err == nil {
			if data, rerr := os.ReadFile(exe); rerr == nil {
				sum := sha256.Sum256(data)
				stampValue = hex.EncodeToString(sum[:])
			}
		}
		if stampValue == "" {
			stampValue = krateversion.Value
		}
	})
	return stampValue
}

// WriteManifest writes the route manifest read by `krate serve`.
func WriteManifest(root string, routes []Route) error {
	manifest := Manifest{}
	seen := map[string]bool{}
	for _, r := range routes {
		for _, m := range r.Methods {
			key := m + " " + r.Path
			if seen[key] {
				return fmt.Errorf("duplicate Go API route %q (files map to the same path)", r.Path)
			}
			seen[key] = true
			method := m
			if method == "Handler" {
				method = ""
			}
			manifest.Routes = append(manifest.Routes, RouteEntry{Method: method, Path: r.Path})
		}
	}
	sort.Slice(manifest.Routes, func(i, j int) bool {
		if manifest.Routes[i].Path == manifest.Routes[j].Path {
			return manifest.Routes[i].Method < manifest.Routes[j].Method
		}
		return manifest.Routes[i].Path < manifest.Routes[j].Path
	})
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling Go API manifest: %w", err)
	}
	p := RoutesManifestPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

// LoadManifest reads the route manifest and its modification time.
func LoadManifest(path string) ([]RouteEntry, time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, time.Time{}, err
	}
	return m.Routes, fi.ModTime(), nil
}

// WriteTypes writes ambient module declarations for each component file under
// .krate/types and prunes stale ones.
func WriteTypes(root string, files []ComponentFile) {
	baseCount := map[string]int{}
	for _, f := range files {
		baseCount[filepath.Base(f.Path)]++
	}
	written := map[string]bool{}
	for _, f := range files {
		src, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		base := filepath.Base(f.Path)
		pattern := "*" + base
		if baseCount[base] > 1 {
			pattern = "*" + filepath.Base(filepath.Dir(f.Path)) + "/" + base
		}
		ts, err := gotsgen.GenerateAmbientModule(pattern, f.Path, src, gotsgen.CollectStructs(filepath.Dir(f.Path)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "  krate: Go component types for %s: %v\n", f.Path, err)
			continue
		}
		rel, err := filepath.Rel(root, f.Path)
		if err != nil {
			rel = base
		}
		outBase := filepath.Join(root, KrateDirName, "types", filepath.FromSlash(filepath.ToSlash(rel)))
		_ = os.Remove(outBase + ".d.ts")
		out := outBase + ".krate.d.ts"
		written[filepath.Clean(out)] = true
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			continue
		}
		_ = fsutil.WriteFileIfChanged(out, []byte(ts), 0644)
	}
	typesDir := filepath.Join(root, KrateDirName, "types")
	_ = filepath.WalkDir(typesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".krate.d.ts") && !written[filepath.Clean(path)] {
			_ = os.Remove(path)
		}
		return nil
	})
}
