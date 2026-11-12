// Package capability works out which parts of the Krate Native SDK a project
// can reach, so a release build can grant exactly the permissions the app
// needs and link only the SDK areas it uses.
//
// Three sources feed the analysis:
//
//   - UI code and TypeScript host code: imports from "@krate/app/<module>"
//     (named imports map to ids through ids.json, a namespace import to the
//     members it uses, anything it cannot narrow to the whole module) and
//     string literals "krate/<area>.<op>" (a literal with an unknown or
//     missing op counts as the whole area). Every script file under src, the
//     pages directory and hostDirs is read, reachable from an entry or not.
//     A call built at run time from a variable is invisible.
//   - Go host code: imports of github.com/kratejs/krate/packages/native/sdk/<area>
//     by any non-standard package the Go host files import, transitively
//     (go list). Go host code is trusted and never permission-gated, so these
//     imports decide which areas are linked, not which permissions the page
//     gets.
//
// ids.json is a copy of packages/native/sdk/ids.json; TestIDsJSONMatchesSDK
// compares them.
package capability

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/native/goenv"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

//go:embed ids.json
var idsJSON []byte

// Row is one SDK id.
type Row struct {
	ID         string   `json:"id"`
	Permission string   `json:"permission"`
	Area       string   `json:"area"`
	Module     string   `json:"module"`
	Exports    []string `json:"exports"`
}

// NativeModule is the import path of the native module.
const NativeModule = "github.com/kratejs/krate/packages/native"

const appPkg = "@krate/app/"

var table = func() []Row {
	var d struct {
		IDs []Row `json:"ids"`
	}
	if err := json.Unmarshal(idsJSON, &d); err != nil {
		panic("capability: ids.json: " + err.Error())
	}
	return d.IDs
}()

// Rows returns the SDK id table.
func Rows() []Row { return table }

// Shakeable lists the SDK areas a release build links only when they are
// reachable (build tag krate_no_<area>). The areas os and permission are
// always linked.
var Shakeable = []string{"fs", "db", "kv", "net", "window", "chrome", "dialog", "clipboard", "menu", "tray", "shortcut", "notify", "app", "updater", "badge", "screen", "power", "login", "crash", "page", "downloads", "cookies"}

// needs lists, for an area, the areas its package imports.
var needs = map[string][]string{"kv": {"db"}, "tray": {"menu"}, "shortcut": {"app"}}

// Site is where a capability is used.
type Site struct {
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
	// Kind is "ui" (UI code), "host" (TypeScript host module) or "go" (Go
	// host code).
	Kind string `json:"kind"`
	// Symbol is the import or id that reaches the capability.
	Symbol string `json:"symbol"`
}

// Result is the outcome of Analyze.
type Result struct {
	// Permissions are the permissions the page and TypeScript host code
	// need, sorted, without names that another entry implies.
	Permissions []string
	// Needs maps each permission (before implication pruning) to its sites.
	Needs map[string][]Site
	// UIAreas are the areas reached from UI and TypeScript host code, GoAreas
	// the ones Go host code imports.
	UIAreas, GoAreas []string
	// GoSites are the Go imports that put an area in GoAreas.
	GoSites []Site
	// PluginOps maps each plugin the code reaches to the ops it uses, sorted.
	// A plugin the code reaches only as a whole (a namespace import, a dynamic
	// import) lists every op.
	PluginOps map[string][]string
}

// PluginOp is one op of an enabled plugin.
type PluginOp struct {
	Name string
	// Permission is the full permission name ("plugin:<plugin>.<name>"), or ""
	// for an op that is always allowed.
	Permission string
}

// PluginInfo describes an enabled plugin to the analysis.
type PluginInfo struct {
	Name string
	Ops  []PluginOp
}

// Options tune Analyze.
type Options struct {
	// GoFiles are the .go files the UI imports (absolute); their packages
	// and everything those import are scanned for SDK imports. Empty for a
	// project without Go host code.
	GoFiles []string
	// Plugins are the enabled native plugins: the page code reaches their ops
	// through imports of "krate/plugin/<name>" and the ids "plugin/<name>.<op>".
	Plugins []PluginInfo
}

type collector struct {
	root     string
	p        *native.Placement
	res      *Result
	perms    map[string]map[string]bool // permission -> dedupe key
	areas    map[string]bool
	byModule map[string][]Row
	byID     map[string]Row
	pluginOp map[string]map[string]bool
}

// Analyze scans the project.
func Analyze(root string, cfg *config.Config, p *native.Placement, opts Options) (*Result, error) {
	c := &collector{
		root: root, p: p,
		res:      &Result{Needs: map[string][]Site{}, PluginOps: map[string][]string{}},
		perms:    map[string]map[string]bool{},
		areas:    map[string]bool{},
		byModule: map[string][]Row{},
		byID:     map[string]Row{},
		pluginOp: map[string]map[string]bool{},
	}
	for _, r := range table {
		c.byModule[r.Module] = append(c.byModule[r.Module], r)
		c.byID[r.ID] = r
	}
	for _, pl := range opts.Plugins {
		for _, op := range pl.Ops {
			r := Row{ID: "plugin/" + pl.Name + "." + op.Name, Permission: op.Permission, Area: pluginArea + pl.Name,
				Module: pluginSpec + pl.Name, Exports: []string{op.Name}}
			c.byModule[r.Module] = append(c.byModule[r.Module], r)
			c.byID[r.ID] = r
		}
	}
	files, err := native.ProjectFiles(root, cfg)
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	for _, f := range files {
		switch strings.ToLower(filepath.Ext(f)) {
		case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".mts":
		default:
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		c.scanFile(f, string(data))
	}
	if len(opts.GoFiles) > 0 {
		sites, err := goSDKImports(root, opts.GoFiles)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, s := range sites {
			area := strings.TrimPrefix(s.Symbol, "sdk/")
			if !seen[area] {
				seen[area] = true
				c.res.GoAreas = append(c.res.GoAreas, area)
			}
			c.res.GoSites = append(c.res.GoSites, s)
		}
		sort.Strings(c.res.GoAreas)
	}
	for perm := range c.res.Needs {
		c.res.Permissions = append(c.res.Permissions, perm)
	}
	sort.Strings(c.res.Permissions)
	c.res.Permissions = pruneImplied(c.res.Permissions)
	// native.updater links the updater area even when no code calls it: its
	// start-up check restores the previous version after a failed update.
	if cfg != nil {
		if n := cfg.EffectiveNative(); n != nil && n.Updater != nil {
			c.areas["updater"] = true
		}
	}
	for a := range c.areas {
		c.res.UIAreas = append(c.res.UIAreas, a)
	}
	sort.Strings(c.res.UIAreas)
	for pl, ops := range c.pluginOp {
		for op := range ops {
			c.res.PluginOps[pl] = append(c.res.PluginOps[pl], op)
		}
		sort.Strings(c.res.PluginOps[pl])
	}
	return c.res, nil
}

const (
	pluginArea = "plugin:"
	pluginSpec = "krate/plugin/"
)

// implied lists the permissions a name grants along with itself.
var implied = map[string][]string{"fs": {"fs:read"}, "media:manage": {"media"}}

func pruneImplied(perms []string) []string {
	have := map[string]bool{}
	for _, p := range perms {
		have[p] = true
	}
	var out []string
	for _, p := range perms {
		covered := false
		for q := range have {
			for _, i := range implied[q] {
				if i == p && q != p {
					covered = true
				}
			}
		}
		if !covered {
			out = append(out, p)
		}
	}
	return out
}

// Covers reports whether the allowlist grants perm (a listed name or one it implies).
func Covers(allow []string, perm string) bool {
	for _, a := range allow {
		if a == perm {
			return true
		}
		for _, i := range implied[a] {
			if i == perm {
				return true
			}
		}
	}
	return false
}

func (c *collector) kind(path string) string {
	if c.p != nil && c.p.IsHostModule(path) {
		return "host"
	}
	return "ui"
}

func (c *collector) rel(path string) string {
	if r, err := filepath.Rel(c.root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(path)
}

// add records that id is reachable from a site.
func (c *collector) add(r Row, s Site) {
	if strings.HasPrefix(r.Area, pluginArea) {
		name := strings.TrimPrefix(r.Area, pluginArea)
		if c.pluginOp[name] == nil {
			c.pluginOp[name] = map[string]bool{}
		}
		c.pluginOp[name][r.Exports[0]] = true
	} else {
		c.areas[r.Area] = true
	}
	if r.Permission == "" {
		return
	}
	key := s.File + "|" + s.Symbol
	if c.perms[r.Permission] == nil {
		c.perms[r.Permission] = map[string]bool{}
	}
	if c.perms[r.Permission][key] {
		return
	}
	c.perms[r.Permission][key] = true
	c.res.Needs[r.Permission] = append(c.res.Needs[r.Permission], s)
}

var (
	idLitRe = regexp.MustCompile(`["'` + "`" + `]krate/([a-z]+)\.([A-Za-z]*)`)
	dynRe   = regexp.MustCompile(`(?:import|require)\s*\(\s*["']@krate/app/([a-z]+)["']\s*\)`)
	// plugin ids and dynamic imports of a plugin's module.
	pluginIDRe  = regexp.MustCompile(`["'` + "`" + `]plugin/([a-z0-9-]+)\.([A-Za-z0-9_]*)`)
	pluginDynRe = regexp.MustCompile(`(?:import|require)\s*\(\s*["']krate/plugin/([a-z0-9-]+)["']\s*\)`)
)

func (c *collector) scanFile(path, src string) {
	if !strings.Contains(src, "krate/") && !strings.Contains(src, "@krate/app") && !strings.Contains(src, "plugin/") {
		return
	}
	rel, kind := c.rel(path), c.kind(path)
	site := func(pos ast.Pos, sym string) Site {
		return Site{File: rel, Line: pos.Line, Col: pos.Col, Kind: kind, Symbol: sym}
	}

	if prog, errs := tsstrip.Parse(path, src, tsstrip.Options{}); prog != nil && len(errs) == 0 {
		for _, st := range prog.Body {
			switch d := st.(type) {
			case *ast.ImportStmt:
				mod, ok := moduleOf(d.Source)
				if !ok {
					continue
				}
				switch {
				case d.Namespace != "":
					c.namespaceUse(mod, d.Namespace, src, site(d.Position, "* as "+d.Namespace+" from "+fromText(mod)))
				default:
					for _, n := range d.Named {
						c.useExport(mod, n.Remote, site(d.Position, n.Remote+" from "+fromText(mod)))
					}
					if d.Default != "" {
						c.useModule(mod, site(d.Position, "default from "+fromText(mod)))
					}
				}
			case *ast.ExportStmt:
				mod, ok := moduleOf(d.ReexportSource)
				if !ok {
					continue
				}
				if d.StarReexport || d.Namespace != "" {
					c.useModule(mod, site(d.Position, "export * from "+fromText(mod)))
				}
				for _, n := range d.ReexportNamed {
					c.useExport(mod, n.Local, site(d.Position, n.Local+" from "+fromText(mod)))
				}
			}
		}
	}
	for _, m := range dynRe.FindAllStringSubmatchIndex(src, -1) {
		mod := src[m[2]:m[3]]
		line, col := lineCol(src, m[0])
		c.useModule(mod, Site{File: rel, Line: line, Col: col, Kind: kind, Symbol: "import(@krate/app/" + mod + ")"})
	}
	for _, m := range pluginDynRe.FindAllStringSubmatchIndex(src, -1) {
		line, col := lineCol(src, m[0])
		name := src[m[2]:m[3]]
		c.useModule(pluginSpec+name, Site{File: rel, Line: line, Col: col, Kind: kind, Symbol: "import(" + pluginSpec + name + ")"})
	}
	for _, m := range pluginIDRe.FindAllStringSubmatchIndex(src, -1) {
		name, op := src[m[2]:m[3]], src[m[4]:m[5]]
		line, col := lineCol(src, m[0])
		s := Site{File: rel, Line: line, Col: col, Kind: kind, Symbol: "plugin/" + name + "." + op}
		if r, ok := c.byID["plugin/"+name+"."+op]; ok {
			c.add(r, s)
			continue
		}
		c.useModule(pluginSpec+name, s)
	}
	for _, m := range idLitRe.FindAllStringSubmatchIndex(src, -1) {
		area, op := src[m[2]:m[3]], src[m[4]:m[5]]
		line, col := lineCol(src, m[0])
		s := Site{File: rel, Line: line, Col: col, Kind: kind, Symbol: "krate/" + area + "." + op}
		if r, ok := c.byID["krate/"+area+"."+op]; ok {
			c.add(r, s)
			continue
		}
		// An unknown or partial op: the whole area, as a dynamic call may reach any.
		for _, r := range table {
			if r.Area == area {
				c.add(r, s)
			}
		}
	}
}

func fromText(mod string) string {
	if strings.HasPrefix(mod, pluginSpec) {
		return mod
	}
	return "@krate/app/" + mod
}

func moduleOf(spec string) (string, bool) {
	spec = strings.Trim(spec, "\"'`")
	if strings.HasPrefix(spec, pluginSpec) && !strings.Contains(spec[len(pluginSpec):], "/") {
		return spec, true
	}
	if !strings.HasPrefix(spec, appPkg) {
		return "", false
	}
	mod := strings.TrimPrefix(spec, appPkg)
	if i := strings.IndexByte(mod, '/'); i >= 0 {
		mod = mod[:i]
	}
	return mod, true
}

func (c *collector) useExport(mod, name string, s Site) {
	for _, r := range c.byModule[mod] {
		for _, e := range r.Exports {
			if e == name {
				c.add(r, s)
				break
			}
		}
	}
}

func (c *collector) useModule(mod string, s Site) {
	for _, r := range c.byModule[mod] {
		c.add(r, s)
	}
}

// namespaceUse narrows `import * as ns` to the members the file accesses as
// ns.member; any other use of ns (passing it on, destructuring it) counts as
// the whole module.
func (c *collector) namespaceUse(mod, ns, src string, s Site) {
	esc := regexp.QuoteMeta(ns)
	member := regexp.MustCompile(`\b` + esc + `\s*\.\s*([A-Za-z_$][\w$]*)`)
	any := regexp.MustCompile(`\b` + esc + `\b`)
	decl := regexp.MustCompile(`import\s*\*\s*as\s+` + esc + `\s+from\s*["'][^"']*["']`)
	src = decl.ReplaceAllString(src, "")
	total := len(any.FindAllStringIndex(src, -1))
	ms := member.FindAllStringSubmatch(src, -1)
	if total != len(ms) {
		c.useModule(mod, s)
		return
	}
	for _, m := range ms {
		c.useExport(mod, m[1], Site{File: s.File, Line: s.Line, Col: s.Col, Kind: s.Kind, Symbol: ns + "." + m[1] + " from " + fromText(mod)})
	}
}

func lineCol(src string, off int) (int, int) {
	line := 1 + strings.Count(src[:off], "\n")
	col := off - (strings.LastIndex(src[:off], "\n") + 1) + 1
	return line, col
}

type listedPkg struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	Imports    []string
	Standard   bool
	Module     *struct{ Path string }
}

// goSDKImports lists the imports of native SDK area packages made by the
// packages of goFiles and their non-standard dependencies.
func goSDKImports(root string, goFiles []string) ([]Site, error) {
	dirs := map[string]bool{}
	for _, f := range goFiles {
		dirs[filepath.Dir(f)] = true
	}
	var args []string
	for d := range dirs {
		rel, err := filepath.Rel(root, d)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			args = append(args, ".")
		} else {
			args = append(args, "./"+rel)
		}
	}
	sort.Strings(args)
	return goSDKImportsIn(root, args, nil)
}

// GoSDKImportsIn lists the SDK area imports of the packages named by args
// (go list patterns relative to dir) and their non-standard dependencies, with
// go run in dir under env (nil = the process environment). The per-app plugin
// shell module uses it to find the areas its plugins import.
func GoSDKImportsIn(dir string, args, env []string) ([]Site, error) {
	return goSDKImportsIn(dir, args, env)
}

func goSDKImportsIn(root string, args, env []string) ([]Site, error) {
	cmd := exec.Command("go", append([]string{"list", "-e", "-deps", "-json=ImportPath,Dir,GoFiles,Imports,Standard,Module"}, args...)...)
	cmd.Dir = root
	if env == nil {
		env = goenv.Env(root)
	} else {
		env = goenv.WithPWD(env, root)
	}
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list of the Go host packages failed: %v\n%s", err, strings.TrimSpace(errb.String()))
	}
	var sites []Site
	dec := json.NewDecoder(&out)
	prefix := NativeModule + "/sdk/"
	for dec.More() {
		var p listedPkg
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		if p.Standard || (p.Module != nil && p.Module.Path == NativeModule) {
			continue
		}
		for _, imp := range p.Imports {
			if !strings.HasPrefix(imp, prefix) {
				continue
			}
			area, _, _ := strings.Cut(strings.TrimPrefix(imp, prefix), "/")
			file, line := findImport(p, imp)
			if r, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(r, "..") {
				file = filepath.ToSlash(r)
			}
			sites = append(sites, Site{File: file, Line: line, Kind: "go", Symbol: "sdk/" + area})
		}
	}
	sort.SliceStable(sites, func(i, j int) bool {
		if sites[i].File != sites[j].File {
			return sites[i].File < sites[j].File
		}
		return sites[i].Line < sites[j].Line
	})
	return sites, nil
}

// findImport locates the import line of path in the Go files of p.
func findImport(p listedPkg, path string) (string, int) {
	quoted := []byte(`"` + path)
	for _, f := range p.GoFiles {
		full := filepath.Join(p.Dir, f)
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		if i := bytes.Index(data, quoted); i >= 0 {
			return full, 1 + bytes.Count(data[:i], []byte("\n"))
		}
	}
	return p.Dir, 0
}

// Linked returns the shakeable areas a binary needs: the areas the page and
// host code reach plus the ones Go host code imports, with the areas those
// import. Core areas (os, permission) are not listed.
func (r *Result) Linked() []string {
	set := map[string]bool{}
	var add func(a string)
	add = func(a string) {
		if set[a] {
			return
		}
		set[a] = true
		for _, n := range needs[a] {
			add(n)
		}
	}
	for _, a := range r.UIAreas {
		add(a)
	}
	for _, a := range r.GoAreas {
		add(a)
	}
	var out []string
	for _, a := range Shakeable {
		if set[a] {
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// Shaken returns the shakeable areas a binary can leave out.
func (r *Result) Shaken() []string {
	linked := map[string]bool{}
	for _, a := range r.Linked() {
		linked[a] = true
	}
	var out []string
	for _, a := range Shakeable {
		if !linked[a] {
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// Tags returns the go build tags of a release build: krate_no_<area> for each
// shaken area, and krate_no_hostjs without TypeScript host code.
func (r *Result) Tags(hasHostJS bool) []string {
	var tags []string
	for _, a := range r.Shaken() {
		tags = append(tags, "krate_no_"+a)
	}
	if !hasHostJS {
		tags = append(tags, "krate_no_hostjs")
	}
	return tags
}

// Require links an SDK area the shell itself uses, whatever the code reaches:
// an app with file associations needs the app area for the single-instance
// lock and the open-file events even when no page calls it.
func (r *Result) Require(area string) {
	for _, a := range r.UIAreas {
		if a == area {
			return
		}
	}
	r.UIAreas = append(r.UIAreas, area)
	sort.Strings(r.UIAreas)
}

// AddGoSites records Go imports of SDK areas found outside the project's Go
// host code (the packages of native plugins).
func (r *Result) AddGoSites(sites []Site) {
	seen := map[string]bool{}
	for _, a := range r.GoAreas {
		seen[a] = true
	}
	for _, s := range sites {
		area := strings.TrimPrefix(s.Symbol, "sdk/")
		if !seen[area] {
			seen[area] = true
			r.GoAreas = append(r.GoAreas, area)
		}
		r.GoSites = append(r.GoSites, s)
	}
	sort.Strings(r.GoAreas)
}
