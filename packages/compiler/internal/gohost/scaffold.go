package gohost

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Result is what Scaffold produces and Build consumes.
type Result struct {
	ModulePath string
	Routes     []Route
	Components int
	TypeFiles  []ComponentFile
	// SrcMap maps a generated file under .krate/goapi back to the user's source
	// file, so Go compiler errors point at the real file.
	SrcMap map[string]string
}

// Scaffold scans the Go API + component source trees and writes the generated
// host module (.krate/goapi) and vendored helper module (.krate/goext). It
// returns nil when there is no Go source to compile.
func Scaffold(root string, cfg *config.Config) (*Result, error) {
	goCfg := cfg.Go
	if !goCfg.EnabledOrDefault() {
		return nil, nil
	}

	apiSrcDir := filepath.Join(root, filepath.FromSlash(DefaultAPIDir))
	if dirs := goCfg.APIDirList(); len(dirs) > 0 {
		if filepath.IsAbs(dirs[0]) {
			apiSrcDir = dirs[0]
		} else {
			apiSrcDir = filepath.Join(root, dirs[0])
		}
	}

	var routeFiles []string
	if goCfg.APIEnabledOrDefault() {
		if _, err := os.Stat(apiSrcDir); err == nil {
			files, err := ScanRouteFiles(apiSrcDir)
			if err != nil {
				return nil, err
			}
			routeFiles = files
		}
	}
	compPkgs, typeFiles, err := ScanComponents(goCfg.ComponentDirList(), root)
	if err != nil {
		return nil, err
	}
	totalComps := 0
	for _, p := range compPkgs {
		totalComps += len(p.Comps)
	}
	if len(routeFiles) == 0 && totalComps == 0 {
		return nil, nil
	}

	modDir := HostModuleDir(root)
	if err := os.RemoveAll(modDir); err != nil {
		return nil, fmt.Errorf("cleaning Go host module dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(modDir, "cmd", "server"), 0755); err != nil {
		return nil, err
	}

	res := &Result{TypeFiles: typeFiles, Components: totalComps, SrcMap: map[string]string{}}

	modulePath, err := WriteModule(modDir, apiSrcDir, root, goCfg)
	if err != nil {
		return nil, err
	}
	res.ModulePath = modulePath

	if err := WriteGoextModule(root); err != nil {
		return nil, err
	}
	writeGoWork(root)

	routes, err := scaffoldRoutes(modDir, apiSrcDir, modulePath, routeFiles, res.SrcMap)
	if err != nil {
		return nil, err
	}
	res.Routes = routes

	compImports, err := scaffoldComponents(modDir, modulePath, compPkgs, res.SrcMap)
	if err != nil {
		return nil, err
	}

	mainSrc, err := MainSource(modulePath, routes, totalComps > 0, compImports)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(modDir, "cmd", "server", "main.go"), []byte(mainSrc), 0644); err != nil {
		return nil, err
	}
	return res, nil
}

func scaffoldRoutes(modDir, apiSrcDir, modulePath string, files []string, srcMap map[string]string) ([]Route, error) {
	var routes []Route
	for _, file := range files {
		rel, err := filepath.Rel(apiSrcDir, file)
		if err != nil {
			return nil, err
		}
		src, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading Go API file %s: %w", file, err)
		}
		if GoFilePackage(src) == "" {
			return nil, fmt.Errorf("Go API file %s has no package clause", file)
		}
		r, hasRoute, err := ParseRoute(apiSrcDir, file, src)
		if err != nil {
			return nil, err
		}
		relDir := SanitizeRelDir(filepath.Dir(rel))
		base := SanitizeName(strings.TrimSuffix(filepath.Base(rel), ".go"))

		if !hasRoute {
			dstDir := filepath.Join(modDir, "routes", filepath.FromSlash(relDir))
			if err := os.MkdirAll(dstDir, 0755); err != nil {
				return nil, err
			}
			dst := filepath.Join(dstDir, base+".go")
			if err := os.WriteFile(dst, src, 0644); err != nil {
				return nil, err
			}
			srcMap[filepath.Clean(dst)] = file
			continue
		}

		routeDir := filepath.Join(modDir, "routes", filepath.FromSlash(relDir), base)
		if err := os.MkdirAll(routeDir, 0755); err != nil {
			return nil, err
		}
		copied := goPackageRewriteRe.ReplaceAllString(string(src), "package route")
		dst := filepath.Join(routeDir, "route.go")
		if err := os.WriteFile(dst, []byte(copied), 0644); err != nil {
			return nil, err
		}
		srcMap[filepath.Clean(dst)] = file

		r.ID = "r" + strconv.Itoa(len(routes))
		r.Register = "RegisterR" + strconv.Itoa(len(routes))
		impParts := []string{modulePath, "routes"}
		if relDir != "" {
			impParts = append(impParts, relDir)
		}
		impParts = append(impParts, base)
		r.ImportPath = strings.Join(impParts, "/")
		reg, err := registerRouteSource(r)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(routeDir, "register.go"), []byte(reg), 0644); err != nil {
			return nil, err
		}
		routes = append(routes, r)
	}
	return routes, nil
}

func scaffoldComponents(modDir, modulePath string, packages []ComponentPackage, srcMap map[string]string) ([]string, error) {
	renderSrc, err := RenderSource()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(modDir, "render"), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(modDir, "render", "render.go"), []byte(renderSrc), 0644); err != nil {
		return nil, err
	}

	var imports []string
	for _, p := range packages {
		pkgDir := filepath.Join(modDir, "components", filepath.FromSlash(p.Rel))
		if err := os.MkdirAll(pkgDir, 0755); err != nil {
			return nil, err
		}
		for _, f := range p.Files {
			src, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			dst := filepath.Join(pkgDir, filepath.Base(f))
			if err := os.WriteFile(dst, src, 0644); err != nil {
				return nil, err
			}
			srcMap[filepath.Clean(dst)] = f
		}
		imp := modulePath + "/components"
		if p.Rel != "" {
			imp += "/" + p.Rel
		}
		imports = append(imports, imp)
		pkgName := p.Pkg
		if pkgName == "" {
			pkgName = "components"
		}
		reg, err := RegisterSource(modulePath, pkgName, p.Comps)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(pkgDir, "krate_register.go"), []byte(reg), 0644); err != nil {
			return nil, err
		}
	}
	sort.Strings(imports)
	return imports, nil
}

// registerRouteSource generates a per-route register function in the generated
// `route` package.
func registerRouteSource(r Route) (string, error) {
	var b strings.Builder
	b.WriteString("// Code generated by krate. DO NOT EDIT.\n")
	b.WriteString("package route\n\n")
	b.WriteString("import \"net/http\"\n\n")
	fmt.Fprintf(&b, "func %s(mux *http.ServeMux) {\n", r.Register)
	for _, m := range r.Methods {
		if m == "Handler" {
			fmt.Fprintf(&b, "\tmux.HandleFunc(%q, Handler)\n", r.Path)
		} else {
			fmt.Fprintf(&b, "\tmux.HandleFunc(%q, %s)\n", m+" "+r.Path, m)
		}
	}
	b.WriteString("}\n")
	return b.String(), nil
}
