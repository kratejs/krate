package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

// vendorImport is a binding imported from an npm package that client code may
// call (date-fns format, nanoid, a zod schema). The hydration scripts are
// classic scripts, so these bindings are bundled by esbuild into an IIFE
// preamble inside the page script.
type vendorImport struct {
	Source   string // bare specifier
	Kind     string // "default", "namespace" or "named"
	Imported string // exported name for Kind "named"
	Local    string // binding name in the importing module
	Dir      string // directory of the importing module (resolution base)
	File     string // importing module, for diagnostics
	Pos      ast.Pos
}

// layoutVendorImports remembers each layout's npm bindings across builds, next
// to the layout emit cache whose hydration code references them.
var layoutVendorImports sync.Map // layout path -> []vendorImport

// collectVendorImports lists the npm bindings imported by the bundled modules.
// Compat runtime specifiers, Krate packages, Node built-ins and packages that
// are Krate source (workspace links) are not vendor code.
func collectVendorImports(mods []*bundler.Module) []vendorImport {
	var out []vendorImport
	for _, m := range mods {
		if m == nil || m.Program == nil || m.IsExternal || resolver.IsCompiledDependency(m.Path) {
			continue
		}
		for _, stmt := range m.Program.Body {
			imp, ok := stmt.(*ast.ImportStmt)
			if !ok {
				continue
			}
			src := strings.Trim(imp.Source, "\"'")
			if !isBarePackageImport(src) || isCompatAliased(src) || isDataImportSpec(src) ||
				src == "server-only" || src == "client-only" {
				continue
			}
			if !strings.HasPrefix(src, "node:") {
				resolved := bundler.ResolveImport(m.Path, src)
				if resolved == "" || !resolver.IsCompiledDependency(resolved) || isStylePath(resolved) {
					continue
				}
			}
			base := vendorImport{Source: src, Dir: filepath.Dir(m.Path), File: m.Path, Pos: imp.Position}
			if imp.Default != "" {
				v := base
				v.Kind, v.Local = "default", imp.Default
				out = append(out, v)
			}
			if imp.Namespace != "" {
				v := base
				v.Kind, v.Local = "namespace", imp.Namespace
				out = append(out, v)
			}
			for _, n := range imp.Named {
				local := n.Local
				if local == "" {
					local = n.Remote
				}
				v := base
				v.Kind, v.Imported, v.Local = "named", n.Remote, local
				out = append(out, v)
			}
		}
	}
	return out
}

func isDataImportSpec(src string) bool {
	return strings.Contains(src, "?") || strings.HasSuffix(strings.ToLower(src), ".json")
}

func isStylePath(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".css", ".scss", ".sass", ".less":
		return true
	}
	return false
}

// withVendorImports prepends a bundled preamble to the hydration script for
// every npm binding the script references. Bindings used as island components
// are excluded (islands bundle their own copy).
func (b *Builder) withVendorImports(js string, candidates []vendorImport, islands []irtree.ExternalComponent) (string, error) {
	if len(candidates) == 0 {
		return js, nil
	}
	isIsland := map[string]bool{}
	for _, e := range islands {
		isIsland[e.Local] = true
		if i := strings.IndexByte(e.Local, '.'); i >= 0 {
			isIsland[e.Local[:i]] = true
		}
	}
	refs := irtree.JSIdentRefs(js)
	seen := map[string]bool{}
	var used []vendorImport
	for _, c := range candidates {
		if !refs[c.Local] || seen[c.Local] || isIsland[c.Local] {
			continue
		}
		seen[c.Local] = true
		if strings.HasPrefix(c.Source, "node:") {
			return "", diag.Diagnostic{
				File: c.File, Line: c.Pos.Line, Col: c.Pos.Col,
				Message: "`" + c.Local + "` from \"" + c.Source + "\" is used by client code, but Node built-ins do not exist in the browser",
				Hint:    "move the call to a server action or generateStaticParams, or use a browser-safe package",
			}
		}
		used = append(used, c)
	}
	if len(used) == 0 {
		return js, nil
	}
	sort.Slice(used, func(i, j int) bool { return used[i].Local < used[j].Local })
	pre, err := b.vendorPreamble(used)
	if err != nil {
		return "", err
	}
	names := make([]string, len(used))
	for i, u := range used {
		names[i] = u.Local + "=__krateVendor[" + strconv.Quote(u.Local) + "]"
	}
	return "(function(){\n" + pre + "\nvar " + strings.Join(names, ",") + ";\n" + js + "\n})();", nil
}

var vendorMemo sync.Map // key -> string

// vendorPreamble bundles the bindings into `var __krateVendor=(()=>{...})();`.
func (b *Builder) vendorPreamble(used []vendorImport) (string, error) {
	var src strings.Builder
	var exports []string
	for i, u := range used {
		tag := strconv.Quote(fmt.Sprintf("krate-vendor:%d:%s", i, u.Source))
		v := "__v" + strconv.Itoa(i)
		switch u.Kind {
		case "default":
			fmt.Fprintf(&src, "import %s from %s;\n", v, tag)
		case "namespace":
			fmt.Fprintf(&src, "import * as %s from %s;\n", v, tag)
		default:
			fmt.Fprintf(&src, "import { %s as %s } from %s;\n", u.Imported, v, tag)
		}
		exports = append(exports, v+" as "+strconv.Quote(u.Local))
	}
	fmt.Fprintf(&src, "export { %s };\n", strings.Join(exports, ", "))

	minify := b.shouldMinifyJS()
	var keySrc strings.Builder
	fmt.Fprintf(&keySrc, "%v|%v|%s\n", minify, b.DevMode, src.String())
	for _, u := range used {
		fmt.Fprintf(&keySrc, "%s|%s\n", u.Dir, u.Source)
	}
	sum := sha256.Sum256([]byte(keySrc.String()))
	key := hex.EncodeToString(sum[:])
	if v, ok := vendorMemo.Load(key); ok {
		return v.(string), nil
	}

	mode := "production"
	if b.DevMode {
		mode = "development"
	}
	res := api.Build(api.BuildOptions{
		Stdin:             &api.StdinOptions{Contents: src.String(), ResolveDir: b.Root, Sourcefile: "krate-vendor.js", Loader: api.LoaderJS},
		Bundle:            true,
		Write:             false,
		Format:            api.FormatIIFE,
		GlobalName:        "__krateVendor",
		Platform:          api.PlatformBrowser,
		Target:            api.ES2020,
		Conditions:        []string{mode},
		MinifyWhitespace:  minify,
		MinifyIdentifiers: minify,
		MinifySyntax:      minify,
		Define:            map[string]string{"process.env.NODE_ENV": strconv.Quote(mode)},
		LogLevel:          api.LogLevelSilent,
		Charset:           api.CharsetUTF8,
		Plugins: []api.Plugin{{
			Name: "krate-vendor-resolve",
			Setup: func(pb api.PluginBuild) {
				pb.OnResolve(api.OnResolveOptions{Filter: `^krate-vendor:`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
					rest := strings.TrimPrefix(args.Path, "krate-vendor:")
					colon := strings.IndexByte(rest, ':')
					idx, _ := strconv.Atoi(rest[:colon])
					spec := rest[colon+1:]
					r := pb.Resolve(spec, api.ResolveOptions{ResolveDir: used[idx].Dir, Kind: api.ResolveJSImportStatement})
					if len(r.Errors) > 0 {
						return api.OnResolveResult{}, fmt.Errorf("%s", r.Errors[0].Text)
					}
					return api.OnResolveResult{Path: r.Path, Namespace: r.Namespace, External: r.External}, nil
				})
			},
		}},
	})
	if len(res.Errors) > 0 {
		u := used[0]
		msg := res.Errors[0].Text
		for _, c := range used {
			if strings.Contains(msg, c.Source) {
				u = c
				break
			}
		}
		return "", diag.Diagnostic{
			File: u.File, Line: u.Pos.Line, Col: u.Pos.Col,
			Message: "cannot bundle \"" + u.Source + "\" for the browser: " + msg,
			Hint:    "the package is used by client code; make sure it ships a browser-compatible build, or call it in a server action instead",
		}
	}
	if len(res.OutputFiles) == 0 {
		return "", fmt.Errorf("vendor bundle produced no output")
	}
	out := strings.TrimSpace(string(res.OutputFiles[0].Contents))
	vendorMemo.Store(key, out)
	return out, nil
}
