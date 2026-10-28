package migrate

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

const nextNavigationHelper = "src/migrated/next-navigation.ts"

const nextNavigationSource = `import { useEffect, useState } from "react";
import { navigate } from "@krate/runtime";

function locationPath(): string {
  return typeof location === "undefined" ? "/" : location.pathname + location.search;
}

function subscribe(update: () => void): () => void {
  if (typeof window === "undefined") return () => {};
  window.addEventListener("krate:navigate", update);
  window.addEventListener("popstate", update);
  return () => {
    window.removeEventListener("krate:navigate", update);
    window.removeEventListener("popstate", update);
  };
}

export function usePathname(): string {
  const [path, setPath] = useState(() => (typeof location === "undefined" ? "/" : location.pathname));
  useEffect(() => {
    const update = () => setPath(location.pathname);
    return subscribe(update);
  }, []);
  return path;
}

export function useSearchParams(): URLSearchParams {
  const [search, setSearch] = useState(() => (typeof location === "undefined" ? "" : location.search));
  useEffect(() => {
    const update = () => setSearch(location.search);
    return subscribe(update);
  }, []);
  return new URLSearchParams(search);
}

export function useRouter() {
  return {
    push: (url: string) => navigate(url),
    replace: (url: string) => navigate(url, { replace: true }),
    refresh: () => { if (typeof location !== "undefined") location.reload(); },
    back: () => { if (typeof history !== "undefined") history.back(); },
    forward: () => { if (typeof history !== "undefined") history.forward(); },
    prefetch: (_url: string) => {},
  };
}

export function redirect(url: string): never {
  if (typeof location !== "undefined") navigate(url);
  throw new Error("NEXT_REDIRECT:" + url);
}
`

// rewriteNextModuleImports maps next/navigation and next/headers onto Krate
// modules. It runs after app-local assets are copied so helper paths can be
// relative to each migrated file.
func rewriteNextModuleImports(rep *Report, root string) {
	needNavigation := false
	for i := range rep.Files {
		f := &rep.Files[i]
		if len(f.Content) == 0 {
			continue
		}
		src := string(f.Content)
		if strings.Contains(src, "next/navigation") {
			needNavigation = true
			rel := relativeModulePath(f.Target, nextNavigationHelper)
			src = replaceModule(src, "next/navigation", rel)
			f.Rewrites = append(f.Rewrites, "rewrote next/navigation imports to "+rel)
			f.Dependencies = append(f.Dependencies, nextNavigationHelper)
			for _, unsupported := range []string{"notFound", "useParams", "useSelectedLayoutSegment", "useSelectedLayoutSegments"} {
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
	if !needNavigation {
		return
	}
	if fileExists(filepath.Join(root, filepath.FromSlash(nextNavigationHelper))) {
		rep.Warnings = append(rep.Warnings, nextNavigationHelper+" already exists; left unchanged")
		return
	}
	for _, f := range rep.Files {
		if f.Target == nextNavigationHelper {
			return
		}
	}
	rep.Files = append(rep.Files, PlannedFile{Kind: KindAsset, Target: nextNavigationHelper, Content: []byte(nextNavigationSource), Rewrites: []string{"generated next/navigation compatibility helpers"}})
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
