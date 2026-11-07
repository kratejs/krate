package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/check"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/kratedocs"
	krateversion "github.com/kratejs/krate/packages/compiler/internal/version"
)

// --- get_project_metadata ---

type packageJSON struct {
	Name             string            `json:"name"`
	Version          string            `json:"version"`
	Type             string            `json:"type"`
	Scripts          map[string]string `json:"scripts"`
	Dependencies     map[string]string `json:"dependencies"`
	DevDependencies  map[string]string `json:"devDependencies"`
	PeerDependencies map[string]string `json:"peerDependencies"`
	Engines          map[string]string `json:"engines"`
	Os               []string          `json:"os"`
	GypFile          bool              `json:"gypfile"`
	Module           string            `json:"module"`
	Main             string            `json:"main"`
	Browser          any               `json:"browser"`
	Exports          any               `json:"exports"`
}

func (s *Service) readPackageJSON(rel string) (*packageJSON, bool) {
	abs, _, err := s.resolveProjectPath(rel)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, false
	}
	var p packageJSON
	if json.Unmarshal(data, &p) != nil {
		return nil, false
	}
	return &p, true
}

func (s *Service) installedVersion(pkg string) string {
	if p, ok := s.installedPackage(pkg); ok {
		return p.Version
	}
	return ""
}

// installedPackage reads a dependency's manifest the way Node finds it, walking
// up from the project: a workspace keeps packages in a parent node_modules, or
// links them from outside the project. Only this manifest is read, so that does
// not widen what the file tools can reach.
func (s *Service) installedPackage(pkg string) (*packageJSON, bool) {
	for dir := s.root; ; dir = filepath.Dir(dir) {
		data, err := os.ReadFile(filepath.Join(dir, "node_modules", filepath.FromSlash(pkg), "package.json"))
		if err == nil {
			var p packageJSON
			if json.Unmarshal(data, &p) == nil {
				return &p, true
			}
		}
		if filepath.Dir(dir) == dir {
			return nil, false
		}
	}
}

func (s *Service) configFileName() string {
	for _, n := range []string{"krate.config.ts", "krate.config.mts", "krate.config.js", "krate.config.mjs", "krate.config.json"} {
		if _, err := os.Stat(filepath.Join(s.root, n)); err == nil {
			return n
		}
	}
	return ""
}

func (s *Service) toolProjectMetadata(ctx context.Context, _ map[string]any) (ToolResult, *rpcError) {
	out := map[string]any{
		"root":           ".",
		"rootName":       filepath.Base(s.root),
		"krateCompiler":  krateversion.Value,
		"configFile":     s.configFileName(),
		"packageManager": s.packageManager(),
	}
	if p, ok := s.readPackageJSON("package.json"); ok {
		out["package"] = map[string]any{"name": p.Name, "version": p.Version, "type": p.Type, "scripts": sortedKeys(p.Scripts)}
		krate := map[string]any{}
		all := map[string]string{}
		for k, v := range p.Dependencies {
			all[k] = v
		}
		for k, v := range p.DevDependencies {
			all[k] = v
		}
		for _, name := range sortedKeys(all) {
			if strings.HasPrefix(name, "@krate/") || name == "krate" || name == "react" || name == "preact" || name == "tailwindcss" || name == "typescript" || name == "react-dom" {
				krate[name] = map[string]any{"range": all[name], "installed": s.installedVersion(name)}
			}
		}
		out["frameworkPackages"] = krate
	}
	if node := toolVersion(ctx, "node", "--version"); node != "" {
		out["node"] = node
	}
	out["directories"] = map[string]any{
		"pages":  s.relToRoot(s.cfg.PagesDir),
		"public": s.relToRoot(s.cfg.PublicDir),
		"out":    s.relToRoot(s.outDirAbs()),
	}
	out["output"] = s.cfg.Output
	out["runtime"] = s.cfg.Runtime

	if routes, err := s.builder.RouteList(); err == nil {
		modes := map[string]int{}
		for _, r := range routes {
			modes[r.Mode]++
		}
		out["routes"] = map[string]any{"count": len(routes), "byMode": modes}
	}
	out["apiRoutes"] = len(s.scanAPIRoutes())
	if acts, err := s.builder.ListActions(); err == nil {
		out["serverActions"] = len(acts)
	}
	if mw := s.scanMiddleware(); mw != nil {
		out["middleware"] = mw.File
	}
	out["collections"] = sortedContentNames(s.contentConfig())
	plugins := []string{}
	for _, p := range s.cfg.Plugins {
		plugins = append(plugins, p.Name)
	}
	out["plugins"] = plugins

	built := map[string]any{"built": false}
	if info, err := os.Stat(build.ResolveManifestPath(s.cfg.OutDir, "manifest.json")); err == nil {
		built = map[string]any{"built": true, "at": info.ModTime().UTC().Format(time.RFC3339)}
	}
	out["build"] = built
	if t, ok := s.findDevServer(ctx); ok {
		out["devServer"] = map[string]any{"running": true, "url": t.Rec.URL, "pid": t.Rec.PID}
	} else {
		out["devServer"] = map[string]any{"running": false}
	}
	pol := map[string]any{"readOnlyMode": false}
	if s.policy != nil {
		pol = map[string]any{
			"readOnlyMode": s.policy.ReadOnlyMode, "writeRoots": s.policy.WriteRoots,
			"allowedTasks": sortedKeys(s.allowedTasks()),
		}
	}
	out["mcpPolicy"] = pol
	return JSONResult(out)
}

func toolVersion(ctx context.Context, name string, args ...string) string {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	b, err := execx.Command(cctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// --- get_config ---

func flattenJSON(prefix string, v any, out map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 && prefix != "" {
			out[prefix] = t
			return
		}
		for k, c := range t {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flattenJSON(key, c, out)
		}
	default:
		out[prefix] = v
	}
}

func (s *Service) toolGetConfig(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	resolved, err := json.Marshal(s.cfg)
	if err != nil {
		return ErrorResult("encoding config: " + err.Error()), nil
	}
	def := config.Default()
	def.Resolve(s.root)
	defJSON, _ := json.Marshal(def)
	var rv, dv map[string]any
	_ = json.Unmarshal(resolved, &rv)
	_ = json.Unmarshal(defJSON, &dv)
	flatR, flatD := map[string]any{}, map[string]any{}
	flattenJSON("", rv, flatR)
	flattenJSON("", dv, flatD)

	file := s.configFileName()
	prefix := argString(args, "prefix")
	onlySet := argBool(args, "onlyChanged", false)
	type entry struct {
		Key     string `json:"key"`
		Value   any    `json:"value"`
		Source  string `json:"source"`
		Default any    `json:"default,omitempty"`
	}
	var entries []entry
	for _, k := range sortedKeys(flatR) {
		if prefix != "" && !strings.HasPrefix(k, prefix) {
			continue
		}
		val := flatR[k]
		src := "default"
		a, _ := json.Marshal(val)
		b, _ := json.Marshal(flatD[k])
		if string(a) != string(b) {
			src = file
			if src == "" {
				src = "krate.config"
			}
			if strings.HasPrefix(k, "pathAliases") || k == "tsBaseDir" {
				src = "tsconfig.json or " + src
			}
		}
		if onlySet && src == "default" {
			continue
		}
		if sensitiveArgKey(k) {
			val = "[REDACTED]"
		}
		val = s.relativizeConfigValue(val)
		e := entry{Key: k, Value: val, Source: src}
		if src != "default" {
			e.Default = s.relativizeConfigValue(flatD[k])
		}
		entries = append(entries, e)
	}
	page, next, rerr := paginate(entries, args, 100, 500)
	if rerr != nil {
		return ToolResult{}, rerr
	}
	var envNames []string
	for _, n := range []string{"KRATE_ENV", "NODE_ENV", "PORT", "KRATE_SERVE_PORT"} {
		if _, ok := os.LookupEnv(n); ok {
			envNames = append(envNames, n)
		}
	}
	return JSONResult(map[string]any{
		"configFile": file, "total": len(entries), "entries": page, "nextCursor": next,
		"environmentOverrides": envNames,
		"note":                 "source is the config file when a value differs from the built-in default, otherwise default; CLI flags and environment overrides are applied on top and listed by name only",
	})
}

// relativizeConfigValue turns absolute paths under the project root into
// relative ones so no machine path leaves the server.
func (s *Service) relativizeConfigValue(v any) any {
	switch t := v.(type) {
	case string:
		if filepath.IsAbs(t) {
			if rel := s.relToRoot(t); rel != "" {
				return rel
			}
			return filepath.Base(t)
		}
		return t
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = s.relativizeConfigValue(x)
		}
		return out
	}
	return v
}

// --- list_api_routes ---

type apiRoute struct {
	Route    string   `json:"route"`
	File     string   `json:"file"`
	Language string   `json:"language"`
	Methods  []string `json:"methods"`
	Params   []string `json:"params,omitempty"`
	Shadowed bool     `json:"shadowed,omitempty"`
}

var goHandlerRe = regexp.MustCompile(`(?m)^func\s+(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|Handler)\s*\(`)
var httpMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true, "OPTIONS": true}
var paramRe = regexp.MustCompile(`\[(?:\.\.\.)?([^\]]+)\]`)

func (s *Service) scanAPIRoutes() []apiRoute {
	var out []apiRoute
	for _, dir := range s.cfg.EffectiveGo().APIDirList() {
		dir = strings.Trim(filepath.ToSlash(dir), "/")
		abs, _, err := s.resolveProjectPath(dir)
		if err != nil {
			continue
		}
		_ = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if strings.HasPrefix(name, "_") || strings.HasSuffix(name, ".d.ts") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			lang := ""
			switch ext {
			case ".ts", ".tsx", ".js", ".mjs":
				lang = "typescript"
			case ".go":
				lang = "go"
			default:
				return nil
			}
			rel := s.relToRoot(p)
			inner, _ := filepath.Rel(abs, p)
			route := "/api/" + strings.TrimSuffix(filepath.ToSlash(inner), ext)
			route = strings.TrimSuffix(route, "/index")
			if route == "/api/index" {
				route = "/api"
			}
			r := apiRoute{Route: route, File: rel, Language: lang, Methods: []string{"ALL"}}
			for _, m := range paramRe.FindAllStringSubmatch(route, -1) {
				r.Params = append(r.Params, m[1])
			}
			if data, err := os.ReadFile(p); err == nil && len(data) < 2<<20 {
				if lang == "go" {
					var ms []string
					for _, m := range goHandlerRe.FindAllStringSubmatch(string(data), -1) {
						if m[1] != "Handler" {
							ms = append(ms, m[1])
						}
					}
					if len(ms) > 0 {
						r.Methods = uniqueSorted(ms)
					}
				} else if ms := tsHTTPMethods(string(data)); len(ms) > 0 {
					r.Methods = ms
				}
			}
			out = append(out, r)
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Route < out[j].Route })
	// A Go route takes precedence over a TS route at the same path.
	goAt := map[string]bool{}
	for _, r := range out {
		if r.Language == "go" {
			goAt[r.Route] = true
		}
	}
	for i := range out {
		if out[i].Language != "go" && goAt[out[i].Route] {
			out[i].Shadowed = true
		}
	}
	return out
}

func tsHTTPMethods(src string) []string {
	prog, perrs, _ := astprint.Parse(src)
	if prog == nil || len(perrs) > 0 {
		return nil
	}
	raw, err := astjson.EncodeProgram(prog)
	if err != nil {
		return nil
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	var ms []string
	hasDefault := false
	body, _ := doc["body"].([]any)
	for _, st := range body {
		m, _ := st.(map[string]any)
		if m["kind"] != "ExportStmt" {
			continue
		}
		if d, _ := m["default"].(bool); d {
			hasDefault = true
		}
		decl, _ := m["declaration"].(map[string]any)
		switch decl["kind"] {
		case "FnDecl":
			if n := str(decl["name"]); httpMethods[n] {
				ms = append(ms, n)
			}
		case "VarStmt":
			ds, _ := decl["decls"].([]any)
			for _, d := range ds {
				if dm, ok := d.(map[string]any); ok && httpMethods[str(dm["name"])] {
					ms = append(ms, str(dm["name"]))
				}
			}
		}
		if named, ok := m["named"].([]any); ok {
			for _, n := range named {
				if nm, ok := n.(map[string]any); ok && httpMethods[str(nm["remote"])] {
					ms = append(ms, str(nm["remote"]))
				}
			}
		}
	}
	if hasDefault && len(ms) == 0 {
		return nil
	}
	return uniqueSorted(ms)
}

func (s *Service) toolListAPIRoutes(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	routes := s.scanAPIRoutes()
	if q := argString(args, "query"); q != "" {
		var f []apiRoute
		for _, r := range routes {
			if strings.Contains(strings.ToLower(r.Route+" "+r.File), strings.ToLower(q)) {
				f = append(f, r)
			}
		}
		routes = f
	}
	page, next, rerr := paginate(routes, args, 100, 500)
	if rerr != nil {
		return ToolResult{}, rerr
	}
	return JSONResult(map[string]any{"total": len(routes), "routes": page, "nextCursor": next,
		"note": "methods are read from named exports (TypeScript) or func GET/POST/... (Go); ALL means a default export or Handler serves every method"})
}

// --- list_middleware ---

type middlewareInfo struct {
	File       string   `json:"file"`
	Handler    string   `json:"handler"`
	Matchers   []string `json:"matchers"`
	HasMatcher bool     `json:"hasMatcher"`
	Exports    []string `json:"exports"`
}

func (s *Service) scanMiddleware() *middlewareInfo {
	for _, rel := range []string{"middleware.ts", "src/middleware.ts", "middleware.js", "src/middleware.js"} {
		abs, _, err := s.resolveProjectPath(rel)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		mi := &middlewareInfo{File: rel, Handler: "none", Matchers: []string{}}
		prog, perrs, _ := astprint.Parse(string(data))
		if prog == nil || len(perrs) > 0 {
			return mi
		}
		raw, _ := astjson.EncodeProgram(prog)
		var doc map[string]any
		_ = json.Unmarshal(raw, &doc)
		exports := map[string]bool{}
		body, _ := doc["body"].([]any)
		for _, st := range body {
			m, _ := st.(map[string]any)
			if m["kind"] != "ExportStmt" {
				continue
			}
			if d, _ := m["default"].(bool); d {
				exports["default"] = true
			}
			decl, _ := m["declaration"].(map[string]any)
			switch decl["kind"] {
			case "FnDecl":
				exports[str(decl["name"])] = true
			case "VarStmt":
				ds, _ := decl["decls"].([]any)
				for _, d := range ds {
					dm, _ := d.(map[string]any)
					name := str(dm["name"])
					exports[name] = true
					init, _ := dm["init"].(map[string]any)
					switch name {
					case "config":
						if init["kind"] == "ObjectExpr" {
							props, _ := init["properties"].([]any)
							for _, p := range props {
								pm, _ := p.(map[string]any)
								if str(pm["key"]) == "matcher" {
									mi.HasMatcher = true
									mi.Matchers = append(mi.Matchers, matcherSources(pm["value"])...)
								}
							}
						}
					case "matcher":
						mi.HasMatcher = true
						mi.Matchers = append(mi.Matchers, matcherSources(init)...)
					}
				}
			}
		}
		switch {
		case exports["middleware"]:
			mi.Handler = "middleware"
		case exports["default"]:
			mi.Handler = "default"
		}
		mi.Exports = sortedKeys(exports)
		return mi
	}
	return nil
}

func matcherSources(v any) []string {
	m, _ := v.(map[string]any)
	switch m["kind"] {
	case "Literal":
		return []string{str(m["value"])}
	case "ArrayExpr":
		var out []string
		els, _ := m["elements"].([]any)
		for _, e := range els {
			out = append(out, matcherSources(e)...)
		}
		return out
	case "ObjectExpr":
		props, _ := m["properties"].([]any)
		for _, p := range props {
			pm, _ := p.(map[string]any)
			if str(pm["key"]) == "source" {
				return matcherSources(pm["value"])
			}
		}
	}
	return nil
}

func (s *Service) toolListMiddleware(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	mw := s.scanMiddleware()
	out := map[string]any{}
	if mw == nil {
		out["middleware"] = nil
		out["note"] = "no middleware.ts/js at the project root or in src/"
	} else {
		out["middleware"] = mw
		if !mw.HasMatcher {
			out["scope"] = "no matcher: runs for every request the server forwards except static asset files"
		}
		if test := argString(args, "path"); test != "" {
			ok, err := matcherMatches(mw.Matchers, test)
			if err != nil {
				return ErrorResult("matcher: " + err.Error()), nil
			}
			out["pathCheck"] = map[string]any{"path": test, "runs": ok}
		}
	}
	type redirect struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
		Permanent   bool   `json:"permanent,omitempty"`
		Status      int    `json:"status,omitempty"`
	}
	var rd []redirect
	for _, r := range s.cfg.Redirects {
		rd = append(rd, redirect{Source: r.Source, Destination: r.Destination, Permanent: r.Permanent})
	}
	rw := make([]map[string]string, 0, len(s.cfg.Rewrites))
	for _, r := range s.cfg.Rewrites {
		rw = append(rw, map[string]string{"source": r.Source, "destination": r.Destination})
	}
	hd := make([]map[string]any, 0, len(s.cfg.Headers))
	for _, h := range s.cfg.Headers {
		hd = append(hd, map[string]any{"source": h.Source, "headers": len(h.Headers)})
	}
	out["configRedirects"] = rd
	out["configRewrites"] = rw
	out["configHeaders"] = hd
	return JSONResult(out)
}

// --- list_collections ---

func (s *Service) toolListCollections(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	cc := s.contentConfig()
	type field struct {
		Name     string   `json:"name"`
		Type     string   `json:"type"`
		Required bool     `json:"required"`
		Enum     []string `json:"enum,omitempty"`
	}
	type col struct {
		Name    string   `json:"name"`
		Dir     string   `json:"dir"`
		Source  string   `json:"source"`
		Entries int      `json:"entries"`
		Sample  []string `json:"sampleSlugs,omitempty"`
		Schema  []field  `json:"schema,omitempty"`
		Error   string   `json:"error,omitempty"`
	}
	var cols []col
	for _, name := range sortedContentNames(cc) {
		c := cc.Collections[name]
		entry := col{Name: name, Dir: filepath.ToSlash(c.Dir), Source: "plugin"}
		if _, ok := s.cfg.Content[name]; ok {
			entry.Source = "krate.config"
		}
		for _, fname := range sortedKeys(c.Schema) {
			f := c.Schema[fname]
			entry.Schema = append(entry.Schema, field{Name: fname, Type: string(f.Type), Required: f.Required, Enum: f.Enum})
		}
		absDir, _, derr := s.contentDir(name)
		if derr != nil {
			entry.Error = derr.Error()
		} else if files, err := walkContentFiles(absDir); err == nil {
			entry.Entries = len(files)
			for i, f := range files {
				if i >= 5 {
					break
				}
				entry.Sample = append(entry.Sample, strings.TrimSuffix(f, filepath.Ext(f)))
			}
		}
		cols = append(cols, entry)
	}
	page, next, rerr := paginate(cols, args, 50, 200)
	if rerr != nil {
		return ToolResult{}, rerr
	}
	return JSONResult(map[string]any{"total": len(cols), "collections": page, "nextCursor": next})
}

// --- list_actions / get_server_action_by_id ---

func (s *Service) toolListActions(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	acts, err := s.builder.ListActions()
	if err != nil {
		return ErrorResult("scanning server actions: " + err.Error()), nil
	}
	if q := strings.ToLower(argString(args, "query")); q != "" {
		var f []build.ActionInfo
		for _, a := range acts {
			if strings.Contains(strings.ToLower(a.Export+" "+a.Source+" "+a.ID), q) {
				f = append(f, a)
			}
		}
		acts = f
	}
	page, next, rerr := paginate(acts, args, 100, 500)
	if rerr != nil {
		return ToolResult{}, rerr
	}
	return JSONResult(map[string]any{"total": len(acts), "actions": page, "nextCursor": next,
		"note": `every export of a file whose first statement is "use server" is an action, served at POST /__krate/action/<id>`})
}

func (s *Service) toolGetActionByID(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	id, rerr := requireString(args, "id")
	if rerr != nil {
		return ToolResult{}, rerr
	}
	id = strings.TrimPrefix(strings.TrimSpace(id), "/__krate/action/")
	acts, err := s.builder.ListActions()
	if err != nil {
		return ErrorResult("scanning server actions: " + err.Error()), nil
	}
	for _, a := range acts {
		if a.ID == id {
			res := map[string]any{"id": a.ID, "export": a.Export, "source": a.Source, "endpoint": "/__krate/action/" + a.ID}
			if mi, err := s.analyzeModule(a.Source); err == nil {
				for _, imp := range mi.Imports {
					if imp.Kind == "package" {
						res["usesPackages"] = append(toStrings(res["usesPackages"]), packageName(imp.Spec))
					}
				}
			}
			if line := s.exportLine(a.Source, a.Export); line > 0 {
				res["line"] = line
			}
			return JSONResult(res)
		}
	}
	return ErrorResult(fmt.Sprintf("no server action with id %q (%d actions in the project); call list_actions", id, len(acts))), nil
}

func toStrings(v any) []string {
	s, _ := v.([]string)
	return s
}

// exportLine finds the line of `export ... name` in a source file.
func (s *Service) exportLine(rel, name string) int {
	text, err := s.readProjectText(rel, 2<<20)
	if err != nil {
		return 0
	}
	re := regexp.MustCompile(`(?m)^\s*export\s+(?:async\s+)?(?:function\*?|const|let|var)\s+` + regexp.QuoteMeta(name) + `\b`)
	if loc := re.FindStringIndex(text); loc != nil {
		return strings.Count(text[:loc[0]], "\n") + 1
	}
	return 0
}

// --- env_check ---

var envUseRe = regexp.MustCompile(`process\.env\.([A-Za-z_][A-Za-z0-9_]*)|process\.env\[\s*['"]([^'"]+)['"]\s*\]|import\.meta\.env\.([A-Za-z_][A-Za-z0-9_]*)`)
var envDeclRe = regexp.MustCompile(`(?m)^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=`)

var builtinEnv = map[string]bool{"NODE_ENV": true, "MODE": true, "DEV": true, "PROD": true, "BASE_URL": true, "SSR": true, "KRATE_ENV": true, "PORT": true, "HOME": true, "PATH": true, "PWD": true, "CI": true, "TZ": true, "LANG": true, "HOSTNAME": true}

func (s *Service) toolEnvCheck(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	if s.policy != nil && s.policy.invalid {
		return ErrorResult("the project policy file is malformed; fix .krate/mcp.json"), nil
	}
	type decl struct {
		Name       string   `json:"name"`
		Files      []string `json:"files"`
		Public     bool     `json:"public"`
		SecretLike bool     `json:"secretLike"`
		Used       bool     `json:"used"`
	}
	declared := map[string]*decl{}
	entries, _ := os.ReadDir(s.root)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !(n == ".env" || strings.HasPrefix(n, ".env.")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.root, n))
		if err != nil || len(data) > 1<<20 {
			continue
		}
		// Only the names are kept; values are never read into a result.
		for _, m := range envDeclRe.FindAllStringSubmatch(string(data), -1) {
			d := declared[m[1]]
			if d == nil {
				d = &decl{Name: m[1], Public: strings.HasPrefix(m[1], "KRATE_PUBLIC_"), SecretLike: secretEnvKey(m[1])}
				declared[m[1]] = d
			}
			d.Files = append(d.Files, n)
		}
	}

	type loc struct {
		File string `json:"file"`
		Line int    `json:"line"`
	}
	type use struct {
		Name       string `json:"name"`
		Locations  []loc  `json:"locations"`
		Declared   bool   `json:"declared"`
		SetInShell bool   `json:"setInShell"`
		ClientTier bool   `json:"inClientTierCode,omitempty"`
		Public     bool   `json:"public"`
		Issue      string `json:"issue,omitempty"`
	}
	uses := map[string]*use{}
	roots := []string{"src", "api", "server", "lib", "app", "pages"}
	roots = append(roots, s.cfg.EffectiveGo().APIDirList()...)
	files := []string{"middleware.ts", "middleware.js", "krate.config.ts", "krate.config.js", "krate.config.mjs", "tailwind.config.ts", "tailwind.config.js"}
	seenFile := map[string]bool{}
	for _, r := range roots {
		abs, _, err := s.resolveProjectPath(r)
		if err != nil {
			continue
		}
		_ = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				switch d.Name() {
				case "node_modules", ".git", ".krate", "dist":
					return filepath.SkipDir
				}
				return nil
			}
			if isScriptFile(p) {
				if rel := s.relToRoot(p); rel != "" && !seenFile[rel] {
					seenFile[rel] = true
					files = append(files, rel)
				}
			}
			return nil
		})
	}
	scanned := 0
	for _, rel := range files {
		if s.policy != nil && s.policy.denied(rel) {
			continue
		}
		abs, _, err := s.resolveProjectPath(rel)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.Size() > 1<<20 {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		text := string(data)
		matches := envUseRe.FindAllStringSubmatchIndex(text, -1)
		if len(matches) == 0 {
			continue
		}
		scanned++
		clientTier := false
		if strings.HasPrefix(rel, "src/") && !strings.HasPrefix(rel, "src/api/") {
			if mi, err := s.analyzeModule(rel); err == nil && mi.Tier == "client" {
				clientTier = true
			}
		}
		for _, m := range matches {
			name := ""
			for g := 1; g <= 3; g++ {
				if m[2*g] >= 0 {
					name = text[m[2*g]:m[2*g+1]]
				}
			}
			if name == "" || builtinEnv[name] {
				continue
			}
			u := uses[name]
			if u == nil {
				_, inShell := os.LookupEnv(name)
				u = &use{Name: name, Declared: declared[name] != nil, SetInShell: inShell, Public: strings.HasPrefix(name, "KRATE_PUBLIC_")}
				uses[name] = u
			}
			if len(u.Locations) < 10 {
				u.Locations = append(u.Locations, loc{File: rel, Line: strings.Count(text[:m[0]], "\n") + 1})
			}
			if clientTier {
				u.ClientTier = true
			}
		}
	}
	var missing, exposure, clientRead []string
	var useList []*use
	for _, name := range sortedKeys(uses) {
		u := uses[name]
		if d := declared[name]; d != nil {
			d.Used = true
		}
		switch {
		case !u.Declared && !u.SetInShell:
			u.Issue = "used but not declared in any .env file and not set in the shell"
			missing = append(missing, name)
		case u.Public && secretEnvKey(name):
			u.Issue = "secret-like name with the KRATE_PUBLIC_ prefix: its value is inlined into client JavaScript"
			exposure = append(exposure, name)
		case u.ClientTier && !u.Public:
			u.Issue = "read in client-tier code without the KRATE_PUBLIC_ prefix: undefined in the browser (fine only for build-time use)"
			clientRead = append(clientRead, name)
		}
		useList = append(useList, u)
	}
	var unused []string
	var decls []*decl
	for _, name := range sortedKeys(declared) {
		d := declared[name]
		d.Files = uniqueSorted(d.Files)
		if !d.Used {
			unused = append(unused, name)
		}
		decls = append(decls, d)
	}
	declPage, declNext, rerr := paginate(decls, args, 100, 500)
	if rerr != nil {
		return ToolResult{}, rerr
	}
	return JSONResult(map[string]any{
		"filesScanned":    scanned,
		"declared":        declPage,
		"declaredNext":    declNext,
		"used":            useList,
		"missing":         missing,
		"unused":          unused,
		"secretExposure":  exposure,
		"clientTierReads": clientRead,
		"note":            "only variable names are reported; values are never read into the result. Vars set in the shell count as provided.",
	})
}

// --- bundle_report ---

func (s *Service) toolBundleReport(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	out := s.outDirAbs()
	if _, err := os.Stat(out); err != nil {
		return ErrorResult("no build output; run the build tool first"), nil
	}
	type asset struct {
		File   string `json:"file"`
		Type   string `json:"type"`
		Bytes  int64  `json:"bytes"`
		Gzip   int64  `json:"gzipBytes"`
		Routes int    `json:"routes"`
	}
	var assets []asset
	totals := map[string]int64{}
	_ = filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(out, p)
		if d.IsDir() {
			if rel == ".krate" {
				return filepath.SkipDir
			}
			return nil
		}
		typ := ""
		switch strings.ToLower(filepath.Ext(p)) {
		case ".js", ".mjs":
			typ = "js"
		case ".css":
			typ = "css"
		case ".html":
			typ = "html"
		default:
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		totals[typ+"Bytes"] += info.Size()
		assets = append(assets, asset{File: filepath.ToSlash(rel), Type: typ, Bytes: info.Size()})
		return nil
	})

	// Per-route cost from the built documents.
	routes, _ := s.builder.RouteList()
	type routeCost struct {
		Route     string   `json:"route"`
		Mode      string   `json:"mode"`
		JSBytes   int64    `json:"jsBytes"`
		JSGzip    int64    `json:"jsGzipBytes"`
		CSSBytes  int64    `json:"cssBytes"`
		HTMLBytes int64    `json:"htmlBytes"`
		Total     int64    `json:"totalBytes"`
		Scripts   int      `json:"scripts"`
		Over      []string `json:"overBudget,omitempty"`
	}
	chk, _ := check.FromMap(s.cfg.Checks, s.root, s.env)
	usage := map[string]int{}
	var costs []routeCost
	for _, r := range routes {
		data, _, err := s.readBuilt(r.Route, 4<<20)
		if err != nil {
			continue
		}
		info := parseHTMLInfo(data)
		as, t := s.pageAssets(info, len(data))
		rc := routeCost{Route: r.Route, Mode: r.Mode, JSBytes: t["jsBytes"], JSGzip: t["jsGzip"], CSSBytes: t["cssBytes"], HTMLBytes: t["htmlBytes"], Scripts: len(info.Scripts)}
		rc.Total = rc.JSBytes + rc.CSSBytes + rc.HTMLBytes
		if b := int64(chk.JSBudgetBytes); b > 0 && rc.JSBytes > b {
			rc.Over = append(rc.Over, fmt.Sprintf("js %d > budget %d", rc.JSBytes, b))
		}
		if b := int64(chk.RouteBudgetBytes); b > 0 && rc.Total > b {
			rc.Over = append(rc.Over, fmt.Sprintf("route total %d > budget %d", rc.Total, b))
		}
		for _, a := range as {
			usage[a.Path]++
		}
		costs = append(costs, rc)
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Bytes > assets[j].Bytes })
	topN := clampInt(args, "topAssets", 15, 100)
	var top []asset
	for _, a := range assets {
		if a.Type == "html" {
			continue
		}
		if len(top) >= topN {
			break
		}
		if a.Bytes <= maxAssetRead {
			if data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(a.File))); err == nil {
				a.Gzip = gzipSize(data)
			}
		}
		a.Routes = usage[s.relToRoot(filepath.Join(out, filepath.FromSlash(a.File)))]
		top = append(top, a)
	}
	for _, a := range assets {
		if a.Type != "html" {
			totals[a.Type+"Count"]++
		}
	}
	sort.Slice(costs, func(i, j int) bool { return costs[i].JSGzip > costs[j].JSGzip })
	page, next, rerr := paginate(costs, args, 25, 500)
	if rerr != nil {
		return ToolResult{}, rerr
	}
	overAll := []string{}
	for _, c := range costs {
		if len(c.Over) > 0 {
			overAll = append(overAll, c.Route)
		}
	}
	return JSONResult(map[string]any{
		"totals":           totals,
		"largestAssets":    top,
		"routes":           page,
		"routeCount":       len(costs),
		"nextCursor":       next,
		"budgets":          map[string]any{"jsBytes": chk.JSBudgetBytes, "hydrationBytes": chk.HydrationBudgetBytes, "islandBytes": chk.IslandBudgetBytes, "routeBytes": chk.RouteBudgetBytes},
		"routesOverBudget": overAll,
		"note":             "routes are sorted by gzip JS size; budgets come from checks.budget in the config (0 = off). Hydration and island budgets are enforced by the check tool.",
	})
}

// --- compat_check ---

func (s *Service) toolCompatCheck(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	pkg, rerr := requireString(args, "package")
	if rerr != nil {
		return ToolResult{}, rerr
	}
	pkg = strings.TrimSpace(pkg)
	if strings.ContainsAny(pkg, " \t\\") || strings.Contains(pkg, "..") || strings.HasPrefix(pkg, "/") {
		return ErrorResult("package must be an npm package name"), nil
	}
	out := map[string]any{"package": pkg}
	var evidence []string
	verdict, confidence := "unknown", "low"

	pj, installed := s.installedPackage(pkg)
	if installed {
		out["installed"] = pj.Version
		info := map[string]any{"module": pj.Type == "module" || pj.Module != "", "hasExports": pj.Exports != nil}
		if len(pj.PeerDependencies) > 0 {
			info["peerDependencies"] = pj.PeerDependencies
		}
		out["package_json"] = info
		_, peerReact := pj.PeerDependencies["react"]
		_, depReact := pj.Dependencies["react"]
		switch {
		case pj.GypFile || len(pj.Os) > 0:
			verdict, confidence = "unsupported-in-browser", "medium"
			evidence = append(evidence, "native addon or OS-restricted package (gypfile/os field): it cannot run in the browser or the embedded runtime; use it only from a Node API route or action")
		case peerReact || depReact:
			verdict, confidence = "island", "medium"
			evidence = append(evidence, "depends on React: Krate bundles compiled React packages into an isolated Preact island (see react-compatibility, Compiled React packages)")
			if v := s.installedVersion("preact"); v != "" {
				evidence = append(evidence, "preact "+v+" is installed; islands need >= 10.20.0")
				out["preact"] = v
			} else {
				evidence = append(evidence, "preact is not installed in the project; islands resolve it from the Krate runtime, add preact only if the build asks for it")
			}
		default:
			verdict, confidence = "plain-package", "medium"
			evidence = append(evidence, "no React dependency: bundles as ordinary JavaScript where it is imported")
		}
		if pj.Type != "module" && pj.Module == "" && pj.Exports == nil {
			evidence = append(evidence, "CommonJS-only entry: usable, but not tree-shakeable")
		}
	} else {
		out["installed"] = nil
		evidence = append(evidence, "not found in node_modules; install it to inspect its metadata")
	}

	// Documented mentions in the embedded framework docs.
	needle := strings.ToLower(pkg)
	type mention struct {
		Slug    string `json:"slug"`
		Title   string `json:"title"`
		Excerpt string `json:"excerpt"`
	}
	var mentions []mention
	for _, d := range kratedocs.Docs() {
		low := strings.ToLower(d.Markdown)
		idx := strings.Index(low, needle)
		if idx < 0 {
			continue
		}
		// Whole-token check to avoid "react" matching "react-hook-form" text.
		for idx >= 0 {
			before := idx == 0 || !isPkgChar(low[idx-1])
			end := idx + len(needle)
			after := end >= len(low) || !isPkgChar(low[end])
			if before && after {
				break
			}
			next := strings.Index(low[idx+1:], needle)
			if next < 0 {
				idx = -1
				break
			}
			idx += 1 + next
		}
		if idx < 0 {
			continue
		}
		mentions = append(mentions, mention{Slug: d.Slug, Title: d.Page.Title, Excerpt: excerpt(d.Markdown, pkg, 220)})
		if len(mentions) >= 6 {
			break
		}
	}
	if len(mentions) > 0 {
		evidence = append(evidence, fmt.Sprintf("documented in %d framework doc page(s); read them with krate://docs/<slug>", len(mentions)))
		if verdict == "unknown" {
			verdict, confidence = "documented", "medium"
		} else {
			confidence = "high"
		}
	}
	out["verdict"] = verdict
	out["confidence"] = confidence
	out["evidence"] = evidence
	out["docs"] = emptyIfNil(mentions)
	out["note"] = "advisory: derived from the package's metadata and the compatibility docs, not from running it. Verify by importing it in a page and running build."
	return JSONResult(out)
}

func isPkgChar(c byte) bool {
	return c == '-' || c == '_' || c == '/' || c == '@' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')
}

func (s *Service) registerProjectTools(srv *Server) {
	s.reg(srv, Tool{
		Name:        "get_project_metadata",
		Description: "Project overview in one call: package name/version, framework package versions (declared and installed), Node and package manager, directories, route counts by render mode, API routes, server actions, middleware, collections, plugins, build state, dev server state and the MCP policy in force. Paths are project-relative. Equivalent of Next.js get_project_metadata, with far more context.",
		InputSchema: objSchema(nil),
		Annotations: readOnlyAnnotations("Get Project Metadata"),
		Handler:     s.toolProjectMetadata,
	})
	s.reg(srv, Tool{
		Name:        "get_config",
		Description: "The resolved Krate configuration as flat dotted keys, each tagged with where it came from (the config file, tsconfig, or the built-in default) and its default when overridden. Filter with prefix (e.g. ssr, checks) or onlyChanged. Secrets are redacted and paths are project-relative.",
		InputSchema: objSchema(map[string]any{
			"prefix":      strSchema("Only keys starting with this prefix, e.g. ssr or checks.budget"),
			"onlyChanged": boolSchema("Only values that differ from the default (default false)"),
			"limit":       numSchema("Keys per page (default 100, max 500)"),
			"cursor":      strSchema("Opaque cursor from a previous result"),
		}),
		Annotations: readOnlyAnnotations("Get Config"),
		Handler:     s.toolGetConfig,
	})
	s.reg(srv, Tool{
		Name:        "list_api_routes",
		Description: "List API routes under src/api (and configured Go API dirs): URL pattern, source file, language (TypeScript or Go), HTTP methods read from the exports, dynamic params, and whether a Go route shadows a TypeScript one.",
		InputSchema: objSchema(map[string]any{
			"query":  strSchema("Substring filter on route or file"),
			"limit":  numSchema("Routes per page (default 100, max 500)"),
			"cursor": strSchema("Opaque cursor from a previous result"),
		}),
		Annotations: readOnlyAnnotations("List API Routes"),
		Handler:     s.toolListAPIRoutes,
	})
	s.reg(srv, Tool{
		Name:        "list_middleware",
		Description: "Show the request-interception layers: middleware.ts (handler, exports, matcher patterns) plus config redirects, rewrites and header rules. Pass path to test whether the middleware runs for a URL using the same matcher engine as the server.",
		InputSchema: objSchema(map[string]any{
			"path": strSchema("Optional URL path to test against the matcher, e.g. /admin/users"),
		}),
		Annotations: readOnlyAnnotations("List Middleware"),
		Handler:     s.toolListMiddleware,
	})
	s.reg(srv, Tool{
		Name:        "list_collections",
		Description: "List content collections (from content: in the config and from plugins such as docs) with directory, entry count, sample slugs and the frontmatter schema (field types, required, enums). Use read_content to read entries and create_content to add one.",
		InputSchema: objSchema(map[string]any{
			"limit":  numSchema("Collections per page (default 50, max 200)"),
			"cursor": strSchema("Opaque cursor from a previous result"),
		}),
		Annotations: readOnlyAnnotations("List Collections"),
		Handler:     s.toolListCollections,
	})
	s.reg(srv, Tool{
		Name:        "list_actions",
		Description: "List server actions: every export of a \"use server\" module with its stable action id, source file and endpoint. Scans sources, no build needed.",
		InputSchema: objSchema(map[string]any{
			"query":  strSchema("Substring filter on export name, file or id"),
			"limit":  numSchema("Actions per page (default 100, max 500)"),
			"cursor": strSchema("Opaque cursor from a previous result"),
		}),
		Annotations: readOnlyAnnotations("List Server Actions"),
		Handler:     s.toolListActions,
	})
	s.reg(srv, Tool{
		Name:        "get_server_action_by_id",
		Description: "Look up a server action by the id seen in a request (POST /__krate/action/<id>), error log or form: returns its export name, source file and line, endpoint and the npm packages its module imports. Equivalent of Next.js get_server_action_by_id.",
		InputSchema: objSchema(map[string]any{
			"id": strSchema("Action id, e.g. a1b2c3d4e5f6a7b8c9, or the full /__krate/action/<id> path"),
		}, "id"),
		Annotations: readOnlyAnnotations("Get Server Action"),
		Handler:     s.toolGetActionByID,
	})
	s.reg(srv, Tool{
		Name:        "env_check",
		Description: "Audit environment variables without ever returning a value: names declared in .env files vs names the code reads (process.env / import.meta.env), variables used but missing, declared but unused, secret-like names with the KRATE_PUBLIC_ prefix (inlined into client JS), and non-public reads in client-tier code. Only names and file:line locations are reported.",
		InputSchema: objSchema(map[string]any{
			"limit":  numSchema("Declared variables per page (default 100, max 500)"),
			"cursor": strSchema("Opaque cursor for the declared list"),
		}),
		Annotations: readOnlyAnnotations("Env Check"),
		Handler:     s.toolEnvCheck,
	})
	s.reg(srv, Tool{
		Name:        "bundle_report",
		Description: "Size report of the build output: totals by type, the largest JS/CSS assets with gzip size and how many routes load each, and per-route JS/CSS/HTML cost sorted by gzip JS, flagged against the performance budgets (checks.budget) in the config. Run build first. Use explain_page or why_in_bundle to dig into a route.",
		InputSchema: objSchema(map[string]any{
			"topAssets": numSchema("Largest assets to list (default 15, max 100)"),
			"limit":     numSchema("Routes per page (default 25, max 500)"),
			"cursor":    strSchema("Opaque cursor from a previous result"),
		}),
		Annotations: readOnlyAnnotations("Bundle Report"),
		Handler:     s.toolBundleReport,
	})
	s.reg(srv, Tool{
		Name:        "compat_check",
		Description: "Check whether an npm package fits Krate: reads its installed package.json (React dependency means a Preact island, native addon or OS restriction means Node-only) and searches the embedded compatibility docs for documented support. Returns a verdict with confidence and the evidence behind it.",
		InputSchema: objSchema(map[string]any{
			"package": strSchema("npm package name, e.g. zustand or @radix-ui/react-dialog"),
		}, "package"),
		Annotations: readOnlyAnnotations("Compat Check"),
		Handler:     s.toolCompatCheck,
	})
}
