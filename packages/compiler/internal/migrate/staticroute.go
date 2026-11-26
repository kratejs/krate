package migrate

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

type routeHandlerInfo struct {
	hasGET       bool
	hasNonGET    bool
	forceStatic  bool
	forceDynamic bool
	hasGSP       bool
}

var (
	routeHandlerDynamicRe = regexp.MustCompile(`(?m)^\s*export\s+const\s+dynamic\s*(?::[^=]+)?=\s*["'](auto|force-dynamic|force-static|error)["']`)
	routeHandlerGSPRe     = regexp.MustCompile(`(?m)^\s*export\s+(?:async\s+)?(?:function\s*\*?|const|let|var)\s+generateStaticParams\b`)
)

// classifyNextRouteHandler inspects a Next.js App Router route.ts/route.tsx
// source file to determine whether it is a GET-only handler, whether it opts
// into force-static or force-dynamic, and whether it exports generateStaticParams.
func classifyNextRouteHandler(src string) routeHandlerInfo {
	var info routeHandlerInfo
	if m := routeHandlerDynamicRe.FindStringSubmatch(src); m != nil {
		switch m[1] {
		case "force-static":
			info.forceStatic = true
		case "force-dynamic":
			info.forceDynamic = true
		}
	}
	info.hasGSP = routeHandlerGSPRe.MatchString(src)

	prog := parser.New(lexer.New(src).Tokenize()).ParseProgram()
	if prog == nil {
		return info
	}
	record := func(name string) {
		switch name {
		case "GET":
			info.hasGET = true
		case "POST", "PUT", "PATCH", "DELETE":
			info.hasNonGET = true
		case "generateStaticParams":
			info.hasGSP = true
		}
	}
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		if exp.Default {
			info.hasNonGET = true
			continue
		}
		if exp.Declaration != nil {
			switch d := exp.Declaration.(type) {
			case *ast.FnDecl:
				record(d.Name)
			case *ast.VarStmt:
				for _, vd := range d.Decls {
					if vd.Name != "" {
						record(vd.Name)
					}
					for _, n := range vd.Names {
						record(n)
					}
					if vd.RestName != "" {
						record(vd.RestName)
					}
				}
			}
		}
		for _, spec := range exp.Named {
			name := spec.Remote
			if name == "" {
				name = spec.Local
			}
			record(name)
		}
		for _, spec := range exp.ReexportNamed {
			name := spec.Remote
			if name == "" {
				name = spec.Local
			}
			record(name)
		}
	}
	return info
}

// staticHandlerRoute turns a migrated src/pages/**/route.* target path into
// its public URL path (e.g. "src/pages/llms.txt/route.ts" -> "/llms.txt").
func staticHandlerRoute(target string) string {
	rel := strings.TrimPrefix(filepath.ToSlash(target), "src/pages/")
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	var parts []string
	for _, seg := range strings.Split(rel, "/") {
		switch {
		case seg == "" || strings.HasPrefix(seg, "@"):
		case strings.HasPrefix(seg, "(") && strings.HasSuffix(seg, ")") && !isInterceptingSegment(seg):
		default:
			parts = append(parts, seg)
		}
	}
	if len(parts) > 0 && parts[len(parts)-1] == "route" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return "/"
	}
	return "/" + strings.Join(parts, "/")
}
