package migrate

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// rewriteNextModuleImports maps next/navigation and next/headers onto Krate
// modules. It runs after app-local assets are copied so helper paths can be
// relative to each migrated file.
func rewriteNextModuleImports(rep *Report, root string) {
	for i := range rep.Files {
		f := &rep.Files[i]
		if len(f.Content) == 0 || f.Kind == KindPublic {
			continue
		}
		src := string(f.Content)
		if strings.Contains(src, "next/navigation") {
			rewritten, rewrites, manual := rewriteNextNavigationImports(src)
			src = rewritten
			f.Rewrites = append(f.Rewrites, rewrites...)
			for _, item := range manual {
				rep.Manual = append(rep.Manual, ManualItem{Source: f.Source, Reason: item.Reason, Hint: item.Hint})
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
		if strings.Contains(src, "next/og") {
			src = replaceModule(src, "next/og", "@krate/runtime/og")
			f.Rewrites = append(f.Rewrites, "rewrote next/og to @krate/runtime/og")
		}
		f.Content = []byte(src)
	}
}

var nextNavigationImport = regexp.MustCompile(`(?m)^([ \t]*)import[ \t]+(type[ \t]+)?\{([^}]*)\}[ \t]*from[ \t]+(["'])next/navigation["'][ \t]*;?`)

// nextNavigationHooks are the next/navigation hooks @krate/runtime implements.
var nextNavigationHooks = map[string]bool{
	"usePathname": true, "useSearchParams": true, "useParams": true, "useRouter": true,
	"useSelectedLayoutSegment": true, "useSelectedLayoutSegments": true,
}

// nextNavigationSignals are the next/navigation functions that end a render or
// a request. They come from @krate/runtime/server in a server file (hook-free)
// and from @krate/runtime in a client file.
var nextNavigationSignals = map[string]bool{
	"notFound": true, "redirect": true, "permanentRedirect": true, "forbidden": true, "unauthorized": true,
}

// nextNavigationUnsupported are next/navigation names with no Krate equivalent.
var nextNavigationUnsupported = map[string]string{
	"RedirectType":            "RedirectType is not available; redirect(url) always replaces the document",
	"unstable_rethrow":        "unstable_rethrow is not available; Krate's signals pass through try/catch blocks that rethrow",
	"useServerInsertedHTML":   "useServerInsertedHTML is not available; render <Head> tags instead",
	"useLinkStatus":           "useLinkStatus is not available; read getNavigationState().status from @krate/runtime",
	"ReadonlyURLSearchParams": "ReadonlyURLSearchParams is a type only; use URLSearchParams",
}

// rewriteNextNavigationImports points next/navigation imports at @krate/runtime
// (hooks, and the signals of a client file) and @krate/runtime/server (the
// signals of a server file). Names with no equivalent stay on next/navigation
// and are reported.
func rewriteNextNavigationImports(src string) (out string, rewrites []string, manual []ManualItem) {
	clientFile := hasUseClientDirective(src)
	signalModule := "@krate/runtime/server"
	if clientFile {
		signalModule = "@krate/runtime"
	}
	out = nextNavigationImport.ReplaceAllStringFunc(src, func(stmt string) string {
		m := nextNavigationImport.FindStringSubmatch(stmt)
		indent, typeOnly, specs, quote := m[1], strings.TrimSpace(m[2]), m[3], m[4]
		var hooks, signals, rest []string
		for _, spec := range strings.Split(specs, ",") {
			spec = strings.TrimSpace(spec)
			if spec == "" {
				continue
			}
			name := strings.TrimSpace(strings.TrimPrefix(spec, "type "))
			if i := strings.Index(name, " as "); i >= 0 {
				name = strings.TrimSpace(name[:i])
			}
			switch {
			case nextNavigationHooks[name]:
				hooks = append(hooks, spec)
			case nextNavigationSignals[name]:
				signals = append(signals, spec)
			default:
				rest = append(rest, spec)
				reason := name + " from next/navigation has no Krate equivalent"
				if msg, ok := nextNavigationUnsupported[name]; ok {
					reason = msg
				}
				manual = append(manual, ManualItem{Reason: reason, Hint: "see the Next.js migration guide"})
			}
		}
		prefix := indent + "import "
		if typeOnly != "" {
			prefix += "type "
		}
		var lines []string
		if len(hooks) > 0 {
			lines = append(lines, prefix+"{ "+strings.Join(hooks, ", ")+" } from "+quote+"@krate/runtime"+quote+";")
		}
		if len(signals) > 0 {
			lines = append(lines, prefix+"{ "+strings.Join(signals, ", ")+" } from "+quote+signalModule+quote+";")
		}
		if len(rest) > 0 {
			lines = append(lines, prefix+"{ "+strings.Join(rest, ", ")+" } from "+quote+"next/navigation"+quote+";")
		}
		return strings.Join(lines, "\n")
	})
	if out != src {
		rewrites = append(rewrites, "rewrote next/navigation imports to "+signalModule+" and @krate/runtime")
	}
	if strings.Contains(out, "next/navigation") && nextNavigationImport.FindString(out) == "" {
		// Namespace or default imports: point the whole module at the runtime.
		out = replaceModule(out, "next/navigation", "@krate/runtime")
		rewrites = append(rewrites, "rewrote the next/navigation namespace import to @krate/runtime")
	}
	return out, rewrites, manual
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
	if strings.Contains(src, "NextResponse.rewrite") {
		manual = append(manual, "NextResponse.rewrite needs a manual API response")
	}
	if strings.Contains(src, "NextResponse.next") {
		manual = append(manual, "NextResponse.next has no API-route equivalent; return a Response")
	}
	for _, prop := range []string{".nextUrl", ".ip", ".geo", ".cookies"} {
		if strings.Contains(src, "request"+prop) || strings.Contains(src, "req"+prop) {
			manual = append(manual, "NextRequest"+prop+" is not available on a standard Request")
		}
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
		src = "const NextResponse = {\n  redirect: (url: string | URL, init?: ResponseInit) => {\n    const headers = new Headers(init && init.headers);\n    headers.set(\"location\", String(url));\n    return new Response(null, { status: (init && init.status) || 307, headers });\n  },\n  json: (body: any, init?: ResponseInit) => Response.json(body, init),\n};\n\n" + src
		rewrites = append(rewrites, "added NextResponse API shim")
	}
	return src, rewrites, manual
}
