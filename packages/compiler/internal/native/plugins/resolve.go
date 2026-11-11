package plugins

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Plugin is a resolved entry of native.plugins.
type Plugin struct {
	// Spec is the entry as written in the config, Kind its kind ("path",
	// "npm" or "go").
	Spec, Kind string
	// Dir is the directory with the manifest, which is also the Go package.
	Dir      string
	Manifest Manifest
	// ModulePath and ModuleDir are the Go module that holds the package;
	// ImportPath is the package's import path in it.
	ModulePath, ModuleDir, ImportPath string
	// Version is the module version of a "go" plugin (resolved from the
	// module cache); empty for the others, which are replaced by directory.
	Version string
	// Local reports that the plugin's sources are inside the project and
	// outside node_modules, so a dev session watches them.
	Local bool
}

// Resolve resolves the entries of native.plugins against the project root. cfgFile
// is the config file name for diagnostics, krateVersion the compiler version
// ("dev" or "" skips the minimum version check).
func Resolve(root string, specs []string, cfgFile, krateVersion string) ([]*Plugin, []diag.Diagnostic) {
	var out []*Plugin
	var ds []diag.Diagnostic
	byName := map[string]*Plugin{}
	have, haveVersion := ParseVersion(krateVersion)
	haveVersion = haveVersion && krateVersion != "dev"
	for _, spec := range specs {
		p, pds := resolveOne(root, spec, cfgFile)
		if len(pds) > 0 {
			ds = append(ds, pds...)
			continue
		}
		file := filepath.Join(p.Dir, ManifestFile)
		if prev, dup := byName[p.Manifest.Name]; dup {
			ds = append(ds, diag.New(cfgFile, 1, 1,
				fmt.Sprintf("the plugins %q and %q are both named %q", prev.Spec, spec, p.Manifest.Name),
				"plugin names are the namespace of their ops (plugin/<name>.<op>) and must be unique; rename one in its krate-plugin.json", "").WithCode(diag.CodePluginDuplicate))
			continue
		}
		byName[p.Manifest.Name] = p
		if need, ok := ParseVersion(p.Manifest.Krate); ok && p.Manifest.Krate != "" && haveVersion && have.Less(need) {
			ds = append(ds, diag.New(rel(root, file), 1, 1,
				fmt.Sprintf("plugin %s needs Krate %s or newer, and this is Krate %s", p.Manifest.Name, p.Manifest.Krate, krateVersion),
				"upgrade Krate (npm i @krate/core@latest), or use an older release of the plugin", "").WithCode(diag.CodePluginNeedsNewerKrate))
			continue
		}
		out = append(out, p)
	}
	return out, ds
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(p)
}

func resolveOne(root, spec, cfgFile string) (*Plugin, []diag.Diagnostic) {
	fail := func(code diag.Code, msg, hint string) (*Plugin, []diag.Diagnostic) {
		return nil, []diag.Diagnostic{diag.New(cfgFile, 1, 1, "native.plugins "+quote(spec)+": "+msg, hint, "").WithCode(code)}
	}
	p := &Plugin{Spec: spec, Kind: config.NativePluginSpecKind(spec)}
	var boundary string // the directory the governing go.mod search stops at
	switch p.Kind {
	case "path":
		if filepath.IsAbs(filepath.FromSlash(spec)) {
			p.Dir = filepath.Clean(filepath.FromSlash(spec))
		} else {
			p.Dir = filepath.Clean(filepath.Join(root, filepath.FromSlash(spec)))
		}
		boundary = root
		if r, err := filepath.Rel(root, p.Dir); err != nil || strings.HasPrefix(r, "..") {
			boundary = p.Dir
		}
		p.Local = boundary == root && !inNodeModules(root, p.Dir)
	case "npm":
		dir, why := findNPM(root, spec)
		if dir == "" {
			return fail(diag.CodePluginManifestMissing, why, "install the package (npm i "+spec+") so node_modules/"+spec+" exists")
		}
		p.Dir, boundary = dir, dir
	case "go":
		dir, ver, why := downloadGo(root, strings.TrimPrefix(spec, "go:"))
		if dir == "" {
			return fail(diag.CodePluginGoImport, why, "give a version (go:module@v1.2.3), or require the module in the app's go.mod, and check that it can be downloaded (go mod download)")
		}
		p.Dir, boundary, p.Version = dir, dir, ver
	default:
		return fail(diag.CodePluginManifestInvalid, "not a plugin reference", "use a directory, an npm package or go:<module>[@version]")
	}
	manifest := filepath.Join(p.Dir, ManifestFile)
	data, err := os.ReadFile(manifest)
	if err != nil {
		return fail(diag.CodePluginManifestMissing, "no "+ManifestFile+" in "+rel(root, p.Dir),
			"create it with krate native plugin init, or point native.plugins at the directory that holds it")
	}
	m, ds := ParseManifest(data, rel(root, manifest))
	if len(ds) > 0 {
		return nil, ds
	}
	p.Manifest = *m

	modDir, modPath := governingModule(p.Dir, boundary)
	if modDir == "" {
		return fail(diag.CodePluginGoImport, "the plugin's Go package is not inside a Go module",
			"add a go.mod next to "+ManifestFile+" (go mod init <import path>), or put the plugin inside the app's Go module")
	}
	p.ModuleDir, p.ModulePath = modDir, modPath
	r, _ := filepath.Rel(modDir, p.Dir)
	p.ImportPath = modPath
	if r != "." {
		p.ImportPath = modPath + "/" + filepath.ToSlash(r)
	}
	if p.Manifest.Go != p.ImportPath {
		return nil, []diag.Diagnostic{diag.New(rel(root, manifest), 1, 1,
			fmt.Sprintf("\"go\" is %q, but the Go package in %s has the import path %q", p.Manifest.Go, rel(root, p.Dir), p.ImportPath),
			"set \"go\" to "+quote(p.ImportPath)+", or fix the module path in "+rel(root, filepath.Join(modDir, "go.mod")), "").WithCode(diag.CodePluginGoImport)}
	}
	return p, nil
}

func quote(s string) string { return "\"" + s + "\"" }

func inNodeModules(root, dir string) bool {
	r, err := filepath.Rel(root, dir)
	if err != nil {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(r), "/") {
		if part == "node_modules" {
			return true
		}
	}
	return false
}

// findNPM finds node_modules/<name> from root upward; the manifest is in the
// package root, or where the "krate.plugin" field of its package.json says.
func findNPM(root, name string) (string, string) {
	dir := root
	for {
		cand := filepath.Join(dir, "node_modules", filepath.FromSlash(name))
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			if real, err := filepath.EvalSymlinks(cand); err == nil {
				cand = real
			}
			if _, err := os.Stat(filepath.Join(cand, ManifestFile)); err == nil {
				return cand, ""
			}
			if b, err := os.ReadFile(filepath.Join(cand, "package.json")); err == nil {
				var pj struct {
					Krate struct {
						Plugin string `json:"plugin"`
					} `json:"krate"`
				}
				if json.Unmarshal(b, &pj) == nil && pj.Krate.Plugin != "" {
					sub := filepath.Join(cand, filepath.FromSlash(pj.Krate.Plugin))
					if st, err := os.Stat(sub); err == nil && !st.IsDir() {
						sub = filepath.Dir(sub)
					}
					if _, err := os.Stat(filepath.Join(sub, ManifestFile)); err == nil {
						return sub, ""
					}
				}
			}
			return "", "the package " + name + " is installed but has no " + ManifestFile
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "the package " + name + " is not installed"
		}
		dir = parent
	}
}

var goModRe = regexp.MustCompile(`(?m)^\s*module\s+("?)([^\s"]+)("?)`)

// governingModule returns the directory and path of the Go module that holds
// dir, searching upward as far as boundary.
func governingModule(dir, boundary string) (string, string) {
	d := dir
	for {
		if b, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			if m := goModRe.FindSubmatch(b); m != nil {
				return d, string(m[2])
			}
		}
		if d == boundary || filepath.Dir(d) == d {
			return "", ""
		}
		d = filepath.Dir(d)
	}
}

// downloadGo resolves "module[@version]" to its directory in the module cache.
func downloadGo(root, spec string) (dir, version, why string) {
	mod, ver, _ := strings.Cut(spec, "@")
	run := func(args ...string) ([]byte, string) {
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(errb.String())
			if msg == "" {
				// "go mod download -json" reports its error on stdout.
				var info struct{ Error string }
				if json.Unmarshal(out.Bytes(), &info) == nil && info.Error != "" {
					msg = info.Error
				} else {
					msg = err.Error()
				}
			}
			return nil, msg
		}
		return out.Bytes(), ""
	}
	if _, err := exec.LookPath("go"); err != nil {
		return "", "", "the go command was not found on PATH; a Go module plugin is resolved with it"
	}
	var out []byte
	var msg string
	if ver == "" {
		out, msg = run("list", "-m", "-json", mod)
	} else {
		out, msg = run("mod", "download", "-json", mod+"@"+ver)
	}
	if msg != "" {
		return "", "", "cannot resolve the Go module " + mod + ": " + firstLine(msg)
	}
	var info struct {
		Dir     string
		Version string
		Error   string
	}
	if err := json.Unmarshal(out, &info); err != nil || info.Dir == "" {
		if info.Error != "" {
			return "", "", "cannot resolve the Go module " + mod + ": " + firstLine(info.Error)
		}
		return "", "", "the Go module " + mod + " has no source directory (not downloaded)"
	}
	return info.Dir, info.Version, ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Digest hashes everything a plugin build depends on: the manifest and the Go
// sources and module files of the plugin's module (build caches key on it).
func (p *Plugin) Digest() string {
	h := sha256.New()
	fmt.Fprintln(h, p.Spec, p.Version, p.ImportPath)
	var files []string
	_ = filepath.WalkDir(p.ModuleDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n := d.Name()
			if path != p.ModuleDir && (n == "node_modules" || n == "testdata" || n == "dist" || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "dist-")) {
				return filepath.SkipDir
			}
			return nil
		}
		n := d.Name()
		if (strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go")) || n == "go.mod" || n == "go.sum" || n == ManifestFile {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		r, _ := filepath.Rel(p.ModuleDir, f)
		fmt.Fprintln(h, filepath.ToSlash(r), len(b))
		h.Write(b)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
