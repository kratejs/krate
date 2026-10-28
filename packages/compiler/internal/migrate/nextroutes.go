package migrate

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

const (
	nextNavigationHelper       = "src/migrated/next-navigation.ts"
	nextNavigationServerHelper = "src/migrated/next-navigation-server.ts"
)

// nextNavigationServerSource holds the next/navigation exports that are valid
// in server components. It must stay free of hooks: a module that uses hooks
// is rejected inside a server component tree.
const nextNavigationServerSource = `// Recognized by the Krate SSR sidecar, which answers with a redirect or the 404 page.
export function redirect(url: string): never {
  throw new Error("NEXT_REDIRECT:" + url);
}

export function permanentRedirect(url: string): never {
  return redirect(url);
}

export function notFound(): never {
  const err: any = new Error("NEXT_NOT_FOUND");
  err.digest = "NEXT_NOT_FOUND";
  throw err;
}
`

const nextNavigationSource = `import { useEffect, useState } from "react";
import { getNavigationState, krateRouter, subscribeNavigation } from "@krate/runtime";

function snapshot(): { pathname: string; search: string } {
  if (typeof location === "undefined") return { pathname: "/", search: "" };
  const { pathname, search } = getNavigationState();
  return { pathname, search };
}

function useNavigation() {
  const [state, setState] = useState(snapshot);
  useEffect(() => {
    setState(snapshot());
    return subscribeNavigation(() => setState(snapshot()));
  }, []);
  return state;
}

export function usePathname(): string {
  return useNavigation().pathname;
}

export function useSearchParams(): URLSearchParams {
  return new URLSearchParams(useNavigation().search);
}

export function useRouter() {
  return {
    push: (url: string, options?: { scroll?: boolean }) => { void krateRouter.push(url, options); },
    replace: (url: string, options?: { scroll?: boolean }) => { void krateRouter.replace(url, options); },
    refresh: () => { void krateRouter.refresh(); },
    back: () => krateRouter.back(),
    forward: () => krateRouter.forward(),
    prefetch: (url: string) => { void krateRouter.prefetch(url); },
  };
}

export { notFound, permanentRedirect } from "./next-navigation-server";

// A client-side redirect navigates in place; during rendering it throws the
// signal the SSR sidecar turns into a redirect response.
export function redirect(url: string): never {
  if (typeof location !== "undefined") void krateRouter.push(url);
  throw new Error("NEXT_REDIRECT:" + url);
}
`

// rewriteNextModuleImports maps next/navigation and next/headers onto Krate
// modules. It runs after app-local assets are copied so helper paths can be
// relative to each migrated file.
func rewriteNextModuleImports(rep *Report, root string) {
	needNavigation, needNavigationServer := false, false
	for i := range rep.Files {
		f := &rep.Files[i]
		if len(f.Content) == 0 {
			continue
		}
		src := string(f.Content)
		if strings.Contains(src, "next/navigation") {
			rewritten, useShim, useServer := rewriteNextNavigationImports(src, f.Target)
			src = rewritten
			if useShim {
				needNavigation = true
				f.Rewrites = append(f.Rewrites, "rewrote next/navigation imports to "+relativeModulePath(f.Target, nextNavigationHelper))
				f.Dependencies = append(f.Dependencies, nextNavigationHelper)
			}
			if useServer {
				needNavigationServer = true
				f.Rewrites = append(f.Rewrites, "rewrote next/navigation server helpers to "+relativeModulePath(f.Target, nextNavigationServerHelper))
				f.Dependencies = append(f.Dependencies, nextNavigationServerHelper)
			}
			for _, unsupported := range []string{"useParams", "useSelectedLayoutSegment", "useSelectedLayoutSegments"} {
				if strings.Contains(src, unsupported) {
					rep.Manual = append(rep.Manual, ManualItem{Source: f.Source, Reason: unsupported + " from next/navigation needs manual migration", Hint: "use route params/404 handling explicitly"})
				}
			}
		}
		if strings.Contains(src, "next/dynamic") {
			hadOptions := strings.Contains(src, "ssr:") || strings.Contains(src, "loading:")
			if rewritten, ok := rewriteNextDynamic(src); ok {
				src = rewritten
				f.Rewrites = append(f.Rewrites, "rewrote next/dynamic to React lazy")
				if hadOptions {
					rep.Manual = append(rep.Manual, ManualItem{Source: f.Source, Reason: "next/dynamic ssr/loading options were ignored; use Suspense for a fallback", Hint: "verify lazy loading behavior"})
				}
			} else {
				rep.Manual = append(rep.Manual, ManualItem{Source: f.Source, Reason: "next/dynamic import could not be rewritten automatically", Hint: "declare a lazy component with React lazy()"})
			}
		}
		if strings.Contains(src, "next/cache") {
			rewritten, rewrites, manual := rewriteNextCache(src)
			src = rewritten
			f.Rewrites = append(f.Rewrites, rewrites...)
			for _, item := range manual {
				item.Source = f.Source
				rep.Manual = append(rep.Manual, item)
			}
		}
		if strings.Contains(src, "next/headers") {
			for _, unsupported := range []string{"draftMode"} {
				if strings.Contains(src, unsupported) {
					rep.Manual = append(rep.Manual, ManualItem{Source: f.Source, Reason: unsupported + " from next/headers is not migrated", Hint: "use the Krate action/server context"})
				}
			}
			src = replaceModule(src, "next/headers", "@krate/runtime/server")
			f.Rewrites = append(f.Rewrites, "rewrote next/headers imports to @krate/runtime/server")
		}
		f.Content = []byte(src)
	}
	addHelper := func(target, source string, note string) {
		if fileExists(filepath.Join(root, filepath.FromSlash(target))) {
			rep.Warnings = append(rep.Warnings, target+" already exists; left unchanged")
			return
		}
		for _, f := range rep.Files {
			if f.Target == target {
				return
			}
		}
		rep.Files = append(rep.Files, PlannedFile{Kind: KindAsset, Target: target, Content: []byte(source), Rewrites: []string{note}})
	}
	if needNavigationServer || needNavigation {
		// The client helper re-exports the server helper.
		addHelper(nextNavigationServerHelper, nextNavigationServerSource, "generated next/navigation server helpers")
	}
	if needNavigation {
		addHelper(nextNavigationHelper, nextNavigationSource, "generated next/navigation compatibility helpers")
	}
}

var nextNavigationImport = regexp.MustCompile(`(?m)^([ \t]*)import[ \t]+(type[ \t]+)?\{([^}]*)\}[ \t]*from[ \t]+(["'])next/navigation["'][ \t]*;?`)

var nextNavigationServerNames = map[string]bool{"notFound": true, "redirect": true, "permanentRedirect": true}

// rewriteNextNavigationImports maps next/navigation imports onto the generated
// helpers. In a file without a "use client" directive, the names that are
// valid in server components (notFound, redirect, permanentRedirect) come from
// the hook-free server helper; everything else uses the client helper.
func rewriteNextNavigationImports(src, target string) (out string, useShim, useServer bool) {
	clientFile := hasUseClientDirective(src)
	out = nextNavigationImport.ReplaceAllStringFunc(src, func(stmt string) string {
		m := nextNavigationImport.FindStringSubmatch(stmt)
		indent, typeOnly, specs, quote := m[1], strings.TrimSpace(m[2]), m[3], m[4]
		var server, client []string
		for _, spec := range strings.Split(specs, ",") {
			spec = strings.TrimSpace(spec)
			if spec == "" {
				continue
			}
			name := strings.TrimSpace(strings.TrimPrefix(spec, "type "))
			if i := strings.Index(name, " as "); i >= 0 {
				name = strings.TrimSpace(name[:i])
			}
			if !clientFile && nextNavigationServerNames[name] {
				server = append(server, spec)
			} else {
				client = append(client, spec)
			}
		}
		prefix := indent + "import "
		if typeOnly != "" {
			prefix += "type "
		}
		var lines []string
		if len(server) > 0 {
			useServer = true
			lines = append(lines, prefix+"{ "+strings.Join(server, ", ")+" } from "+quote+relativeModulePath(target, nextNavigationServerHelper)+quote+";")
		}
		if len(client) > 0 {
			useShim = true
			lines = append(lines, prefix+"{ "+strings.Join(client, ", ")+" } from "+quote+relativeModulePath(target, nextNavigationHelper)+quote+";")
		}
		return strings.Join(lines, "\n")
	})
	if strings.Contains(out, "next/navigation") {
		// Namespace or default imports: point the whole module at the client helper.
		out = replaceModule(out, "next/navigation", relativeModulePath(target, nextNavigationHelper))
		useShim = true
	}
	return out, useShim, useServer
}

// hasUseClientDirective reports whether src starts with a "use client"
// directive (comments and blank lines may precede it).
func hasUseClientDirective(src string) bool {
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*"):
			continue
		case t == `"use client";` || t == `"use client"` || t == `'use client';` || t == `'use client'`:
			return true
		}
		return false
	}
	return false
}

var (
	nextDynamicCall   = regexp.MustCompile(`\bdynamic\s*\(`)
	nextDynamicArgs   = regexp.MustCompile(`(?s)\bdynamic\s*\(\s*((?:\(\)\s*=>\s*)?import\([^)]*\)[^,]*?)\s*,\s*\{[^}]*\}\s*\)`)
	nextDynamicImport = regexp.MustCompile(`(?m)^[ \t]*import[ \t]+dynamic[ \t]+from[ \t]+["']next/dynamic["'][ \t]*;?[ \t]*\r?\n?`)
	lazyImport        = regexp.MustCompile(`import\s*\{[^}]*\blazy\b[^}]*\}\s*from\s*["']react["']`)
)

func rewriteNextDynamic(src string) (string, bool) {
	if !nextDynamicImport.MatchString(src) {
		return "", false
	}
	src = nextDynamicImport.ReplaceAllString(src, "")
	src = nextDynamicArgs.ReplaceAllString(src, "lazy($1)")
	src = nextDynamicCall.ReplaceAllString(src, "lazy(")
	if !lazyImport.MatchString(src) {
		src = "import { lazy } from \"react\";\n" + src
	}
	p := parser.New(lexer.New(src).Tokenize())
	if prog := p.ParseProgram(); prog == nil || len(p.Errors()) > 0 {
		return "", false
	}
	return src, true
}

func replaceModule(src, module, replacement string) string {
	for _, quote := range []string{`"`, `'`} {
		src = strings.ReplaceAll(src, quote+module+quote, quote+replacement+quote)
	}
	return src
}

func relativeModulePath(fromTarget, toTarget string) string {
	rel, err := filepath.Rel(filepath.Dir(filepath.FromSlash(fromTarget)), filepath.FromSlash(toTarget))
	if err != nil {
		return toTarget
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, ".ts")
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

// rewriteAPIServerSource maps the common Next route-handler helpers onto the
// web Request/Response API used by Krate API routes.
func rewriteAPIServerSource(src string) (string, []string, []string) {
	var rewrites, manual []string
	src = nextRequestType.ReplaceAllString(src, "Request")
	src = strings.ReplaceAll(src, "NextResponse.redirect(", "Response.redirect(")
	src = strings.ReplaceAll(src, "NextResponse.json(", "Response.json(")
	if strings.Contains(src, "NextResponse.rewrite") {
		manual = append(manual, "NextResponse.rewrite needs a manual API response")
	}
	importFailed := false
	src = nextServerImport.ReplaceAllStringFunc(src, func(line string) string {
		sub := nextServerImport.FindStringSubmatch(line)
		if len(sub) != 2 {
			return line
		}
		for _, name := range strings.Split(sub[1], ",") {
			n := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "type "))
			if n != "NextRequest" && n != "NextResponse" && n != "Request" && n != "" {
				manual = append(manual, "next/server import contains unsupported helper "+n)
				importFailed = true
				return line
			}
		}
		rewrites = append(rewrites, "removed next/server import")
		return ""
	})
	if strings.Contains(src, "NextResponse") && !importFailed {
		src = "const NextResponse = {\n  redirect: (url: string | URL, init?: ResponseInit) => Response.redirect(url, init),\n  json: (body: any, init?: ResponseInit) => Response.json(body, init),\n};\n\n" + src
		rewrites = append(rewrites, "added NextResponse API shim")
	}
	return src, rewrites, manual
}
