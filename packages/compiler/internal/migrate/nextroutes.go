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
		mods := signalModule
		if signalModule != "@krate/runtime" {
			mods += " and @krate/runtime"
		}
		rewrites = append(rewrites, "rewrote next/navigation imports to "+mods)
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

// nextResponseShim is the NextResponse subset prepended to a route handler that
// uses it. cookies mirrors Next's ResponseCookies: set/delete append
// Set-Cookie headers, get/getAll/has read them back.
const nextResponseShim = `function __krateCookieHeader(name: string, value: string, o: any = {}): string {
  let s = encodeURIComponent(name) + "=" + encodeURIComponent(value);
  s += "; Path=" + (o.path === undefined ? "/" : o.path);
  if (o.domain) s += "; Domain=" + o.domain;
  if (o.expires) s += "; Expires=" + new Date(o.expires).toUTCString();
  if (typeof o.maxAge === "number") s += "; Max-Age=" + Math.floor(o.maxAge);
  if (o.httpOnly) s += "; HttpOnly";
  if (o.secure) s += "; Secure";
  if (o.partitioned) s += "; Partitioned";
  if (o.priority) s += "; Priority=" + o.priority;
  if (o.sameSite) s += "; SameSite=" + (o.sameSite === true ? "Strict" : String(o.sameSite).charAt(0).toUpperCase() + String(o.sameSite).slice(1).toLowerCase());
  return s;
}

function __krateWithCookies<T extends Response>(res: T): T {
  const list = (): Array<{ name: string; value: string }> =>
    res.headers.getSetCookie().map((c) => {
      const first = c.split(";")[0];
      const i = first.indexOf("=");
      return { name: decodeURIComponent(first.slice(0, i)), value: decodeURIComponent(first.slice(i + 1)) };
    });
  const cookies = {
    get: (name: string) => list().filter((c) => c.name === name).pop(),
    getAll: (name?: string) => list().filter((c) => name === undefined || c.name === name),
    has: (name: string) => list().some((c) => c.name === name),
    set(name: any, value?: string, opts?: any) {
      if (typeof name === "object") res.headers.append("set-cookie", __krateCookieHeader(name.name, name.value, name));
      else res.headers.append("set-cookie", __krateCookieHeader(name, value as string, opts));
      return cookies;
    },
    delete(name: any) {
      const o = typeof name === "object" ? name : { name };
      res.headers.append("set-cookie", __krateCookieHeader(o.name, "", { path: o.path, domain: o.domain, expires: new Date(0), maxAge: 0 }));
      return cookies;
    },
  };
  Object.defineProperty(res, "cookies", { value: cookies, enumerable: false });
  return res;
}

const NextResponse = {
  redirect: (url: string | URL, init?: number | ResponseInit) => {
    const opts: ResponseInit = typeof init === "number" ? { status: init } : init || {};
    const headers = new Headers(opts.headers);
    headers.set("location", String(url));
    return __krateWithCookies(new Response(null, { ...opts, status: opts.status || 307, headers }));
  },
  json: (body: any, init?: ResponseInit) => __krateWithCookies(Response.json(body, init)),
};

`

// rewriteAPIServerSource maps the common Next route-handler helpers onto the
// web Request/Response API used by Krate API routes.
func rewriteAPIServerSource(src string) (string, []string, []string) {
	var rewrites, manual []string
	if strings.Contains(src, "new NextResponse(") {
		src = strings.ReplaceAll(src, "new NextResponse(", "new Response(")
		rewrites = append(rewrites, "rewrote new NextResponse(...) to new Response(...)")
	}
	if strings.Contains(src, "NextResponse.rewrite") {
		manual = append(manual, "NextResponse.rewrite needs a manual API response")
	}
	if strings.Contains(src, "NextResponse.next") {
		manual = append(manual, "NextResponse.next has no API-route equivalent; return a Response")
	}
	for _, prop := range []string{".ip", ".geo"} {
		if strings.Contains(src, "request"+prop) || strings.Contains(src, "req"+prop) {
			manual = append(manual, "NextRequest"+prop+" is not available on a standard Request")
		}
	}
	if requestCookieWrite.MatchString(src) {
		manual = append(manual, "request.cookies is read-only in Krate (get, getAll, has); set or delete cookies on the response")
	}
	if requestCookiesRead.MatchString(src) {
		rewrites = append(rewrites, "request.cookies (get, getAll, has) is provided by the Krate API runtime")
	}
	if requestNextURL.MatchString(src) {
		rewrites = append(rewrites, "request.nextUrl (URL with basePath and clone()) is provided by the Krate API runtime")
	}
	importFailed := false
	var runtimeImports []string
	src = nextServerImport.ReplaceAllStringFunc(src, func(line string) string {
		sub := nextServerImport.FindStringSubmatch(line)
		if len(sub) != 2 {
			return line
		}
		var unsupported, kept []string
		for _, name := range strings.Split(sub[1], ",") {
			spec := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "type "))
			base, alias, hasAlias := strings.Cut(spec, " as ")
			base = strings.TrimSpace(base)
			switch base {
			case "NextRequest", "Request", "":
			case "NextResponse":
				kept = append(kept, spec)
			case "after", "unstable_after":
				if hasAlias {
					runtimeImports = append(runtimeImports, "after as "+strings.TrimSpace(alias))
				} else if base == "unstable_after" {
					runtimeImports = append(runtimeImports, "after as unstable_after")
				} else {
					runtimeImports = append(runtimeImports, "after")
				}
			default:
				manual = append(manual, "next/server import contains unsupported helper "+base)
				unsupported = append(unsupported, spec)
				kept = append(kept, spec)
				importFailed = true
			}
		}
		if len(unsupported) > 0 {
			return "import { " + strings.Join(kept, ", ") + " } from \"next/server\";\n"
		}
		rewrites = append(rewrites, "removed next/server import")
		return ""
	})
	var header string
	if nextRequestType.MatchString(src) {
		src = nextRequestType.ReplaceAllString(src, "KrateRequest")
		header += "import type { KrateRequest } from \"@krate/runtime/server\";\n"
		rewrites = append(rewrites, "typed NextRequest as KrateRequest (cookies and nextUrl included)")
	}
	if len(runtimeImports) > 0 {
		header += "import { " + strings.Join(runtimeImports, ", ") + " } from \"@krate/runtime/server\";\n"
		rewrites = append(rewrites, "mapped next/server after() to @krate/runtime/server: it runs after the response is sent")
	}
	if strings.Contains(src, "NextResponse") && !importFailed {
		header += "\n" + nextResponseShim
		rewrites = append(rewrites, "added NextResponse API shim (json, redirect, cookies)")
	}
	src = header + src
	return src, rewrites, manual
}

var (
	requestCookieWrite = regexp.MustCompile(`\b(?:request|req)\.cookies\.(?:set|delete|clear)\s*\(`)
	requestCookiesRead = regexp.MustCompile(`\b(?:request|req)\.cookies\b`)
	requestNextURL     = regexp.MustCompile(`\b(?:request|req)\.nextUrl\b`)
)
