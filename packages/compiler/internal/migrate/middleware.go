package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func plannedMiddleware(root, source string, rep *Report) PlannedFile {
	f := PlannedFile{Kind: KindMiddleware, Source: source, Target: filepath.ToSlash(source)}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source)))
	if err != nil {
		rep.Warnings = append(rep.Warnings, source+": could not read middleware: "+err.Error())
		return f
	}
	body, rewrites, manual := rewriteNextMiddleware(string(data))
	f.Content = []byte(body)
	f.Rewrites = rewrites
	for _, m := range manual {
		rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: m, Hint: "review the middleware behavior after migration"})
	}
	return f
}

var (
	nextServerImport = regexp.MustCompile(`(?m)^[ \t]*import[ \t]+(?:type[ \t]+)?\{([^}]*)\}[ \t]*from[ \t]+["']next/server["'][ \t]*;?[ \t]*\r?\n?`)
	nextRequestType  = regexp.MustCompile(`\bNextRequest\b`)
	middlewareExport = regexp.MustCompile(`export\s+(async\s+)?function\s+middleware\b`)
	requestCookies   = regexp.MustCompile(`\brequest\.cookies\b`)
	requestGeo       = regexp.MustCompile(`\brequest\.geo\b`)
	requestIP        = regexp.MustCompile(`\brequest\.ip\b`)
)

// rewriteNextMiddleware maps Next's Request/Response helpers onto the web
// Request/Response API. Matchers, edge-only request fields, and rewrites still
// need a human decision because Krate middleware runs on every request.
func rewriteNextMiddleware(src string) (string, []string, []string) {
	var rewrites, manual []string
	src = nextRequestType.ReplaceAllString(src, "Request")
	src = strings.ReplaceAll(src, "NextResponse.redirect(", "Response.redirect(")
	src = strings.ReplaceAll(src, "NextResponse.json(", "Response.json(")
	if strings.Contains(src, "NextResponse.rewrite") {
		manual = append(manual, "NextResponse.rewrite has no direct Krate middleware equivalent")
	}
	if strings.Contains(src, "NextResponse.next") {
		manual = append(manual, "NextResponse.next() was shimmed to continue, but response-header mutation on it needs manual review")
	}
	if strings.Contains(src, "nextUrl.clone") {
		manual = append(manual, "request.nextUrl.clone() needs manual migration")
	} else if strings.Contains(src, "request.nextUrl") {
		src = strings.ReplaceAll(src, "request.nextUrl", "new URL(request.url)")
		rewrites = append(rewrites, "rewrote request.nextUrl to new URL(request.url)")
	}
	needHelpers := false
	if requestCookies.MatchString(src) {
		src = requestCookies.ReplaceAllString(src, "__krateCookieJar(request)")
		rewrites = append(rewrites, "rewrote request.cookies to a Cookie header shim")
		needHelpers = true
	}
	if requestGeo.MatchString(src) {
		src = requestGeo.ReplaceAllString(src, "__krateGeo(request)")
		rewrites = append(rewrites, "rewrote request.geo to proxy-header fields")
		needHelpers = true
	}
	if requestIP.MatchString(src) {
		src = requestIP.ReplaceAllString(src, "__krateIP(request)")
		rewrites = append(rewrites, "rewrote request.ip to proxy-header fields")
		needHelpers = true
	}
	if strings.Contains(src, "export const middleware") {
		manual = append(manual, "arrow-function middleware needs a manual wrapper for matchers")
	}
	matchers, matcherFound, matcherErr := middlewareMatchers(src)
	if matcherErr != "" {
		manual = append(manual, matcherErr)
	}

	// Remove next/server imports made only of the two shimmed names.
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
	if strings.Contains(src, `from "next/server"`) || strings.Contains(src, `from 'next/server'`) {
		manual = append(manual, "next/server import could not be fully rewritten")
	}
	if strings.Contains(src, "NextResponse") && !importFailed {
		// Keep a tiny shim for NextResponse.next() or unusual helper usage.
		src = "const NextResponse = {\n  redirect: (url: string | URL, init?: ResponseInit) => Response.redirect(url, init),\n  json: (body: any, init?: ResponseInit) => Response.json(body, init),\n  next: () => undefined,\n};\n\n" + src
		rewrites = append(rewrites, "added NextResponse shim")
	}
	if !strings.Contains(src, "NextResponse") {
		rewrites = append(rewrites, "rewrote NextRequest to Request and NextResponse helpers to Response")
	}
	if needHelpers {
		src = `function __krateCookieJar(request: Request) {
  const values = new Map<string, string>();
  for (const part of (request.headers.get("cookie") || "").split(";")) {
    const i = part.indexOf("=");
    if (i > 0) values.set(part.slice(0, i).trim(), part.slice(i + 1).trim());
  }
  return {
    get: (name: string) => values.has(name) ? { name, value: values.get(name) } : undefined,
    getAll: () => [...values].map(([name, value]) => ({ name, value })),
    has: (name: string) => values.has(name),
  };
}
function __krateIP(request: Request) {
  return (request.headers.get("x-forwarded-for") || request.headers.get("x-real-ip") || "").split(",")[0].trim();
}
function __krateGeo(request: Request) {
  const h = request.headers;
  return {
    country: h.get("x-vercel-ip-country") || h.get("cf-ipcountry") || undefined,
    region: h.get("x-vercel-ip-country-region") || undefined,
    city: h.get("x-vercel-ip-city") || undefined,
  };
}

` + src
		rewrites = append(rewrites, "added cookie/IP/geo request shims")
	}
	if matcherFound {
		if rewritten, ok := wrapMiddlewareMatchers(src, matchers); ok {
			src = rewritten
			rewrites = append(rewrites, "wrapped middleware with translated matcher checks")
		} else {
			manual = append(manual, "middleware matcher could not be wrapped automatically")
		}
	} else if configExport.MatchString(src) && strings.Contains(src, "matcher") {
		if rewritten, ok := wrapMiddlewareMatchersDynamic(src); ok {
			src = rewritten
			rewrites = append(rewrites, "wrapped middleware with runtime config.matcher checks")
			manual = append(manual, "config.matcher is evaluated at runtime; if it is not an array the middleware runs for every request")
		}
	}
	p := parser.New(lexer.New(src).Tokenize())
	if prog := p.ParseProgram(); prog == nil || len(p.Errors()) > 0 {
		return string(src), rewrites, append(manual, "rewritten middleware does not parse; review it manually")
	}
	return src, rewrites, manual
}

// middlewareMatchers extracts a static export const config.matcher array.
func middlewareMatchers(src string) ([]string, bool, string) {
	if !strings.Contains(src, "export const config") && !strings.Contains(src, "export const config ") {
		return nil, false, ""
	}
	prog := parser.New(lexer.New(src).Tokenize()).ParseProgram()
	if prog == nil {
		return nil, false, "middleware config could not be parsed"
	}
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		vs, ok := exp.Declaration.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d.Name != "config" {
				continue
			}
			obj, ok := d.Init.(*ast.ObjectExpr)
			if !ok {
				return nil, false, "middleware config is not a static object"
			}
			arr, ok := resolveMatcherArray(prop(obj, "matcher"), prog, map[string]bool{})
			if !ok {
				return nil, false, "middleware matcher is not a static array"
			}
			var out []string
			for _, el := range arr.Elements {
				s, ok := stringValue(el)
				if !ok {
					return nil, false, "middleware matcher contains a non-static entry"
				}
				out = append(out, s)
			}
			return out, true, ""
		}
	}
	return nil, false, ""
}

func resolveMatcherArray(e ast.Expr, prog *ast.Program, seen map[string]bool) (*ast.ArrayExpr, bool) {
	switch v := e.(type) {
	case *ast.ArrayExpr:
		return v, true
	case *ast.ParenExpr:
		return resolveMatcherArray(v.Expr, prog, seen)
	case *ast.Identifier:
		if seen[v.Name] {
			return nil, false
		}
		seen[v.Name] = true
		for _, stmt := range prog.Body {
			vs, ok := stmt.(*ast.VarStmt)
			if !ok {
				continue
			}
			for _, d := range vs.Decls {
				if d.Name == v.Name {
					return resolveMatcherArray(d.Init, prog, seen)
				}
			}
		}
	}
	return nil, false
}

var configExport = regexp.MustCompile(`(?s)export\s+const\s+config\s*(?::[^=]+)?=\s*\{`)

func wrapMiddlewareMatchersDynamic(src string) (string, bool) {
	if !middlewareExport.MatchString(src) {
		return "", false
	}
	src = middlewareExport.ReplaceAllString(src, "${1}function __krateNextMiddleware")
	helper := `
const __krateMatchers = (typeof config !== "undefined" && Array.isArray(config.matcher)) ? config.matcher : [];
function __krateMatcher(pattern: string, pathname: string): boolean {
  let source = pattern.replace(/:[^/]+?\*/g, ".*").replace(/:[^/]+/g, "[^/]+");
  return new RegExp("^" + source + "$").test(pathname);
}
export function middleware(request: Request) {
  const pathname = new URL(request.url).pathname;
  if (__krateMatchers.length && !__krateMatchers.some((m: string) => __krateMatcher(m, pathname))) return;
  return __krateNextMiddleware(request);
}
`
	src += helper
	return src, true
}

func wrapMiddlewareMatchers(src string, matchers []string) (string, bool) {
	if !middlewareExport.MatchString(src) {
		return "", false
	}
	loc := configExport.FindStringIndex(src)
	if loc == nil {
		return "", false
	}
	end, ok := objectEnd(src, loc[1]-1)
	if !ok {
		return "", false
	}
	// Remove the config export (and an optional semicolon/newline).
	end++
	if end < len(src) && src[end] == ';' {
		end++
	}
	if end < len(src) && src[end] == '\r' {
		end++
	}
	if end < len(src) && src[end] == '\n' {
		end++
	}
	src = src[:loc[0]] + src[end:]
	src = middlewareExport.ReplaceAllString(src, "${1}function __krateNextMiddleware")
	name := "__krateNextMiddleware"
	if strings.Contains(src, "async function __krateNextMiddleware") {
		name = "__krateNextMiddleware"
	}
	quoted, err := json.Marshal(matchers)
	if err != nil {
		return "", false
	}
	helper := `
const __krateMatchers = ` + string(quoted) + `;
function __krateMatcher(pattern: string, pathname: string): boolean {
  let source = pattern.replace(/:[^/]+?\*/g, ".*").replace(/:[^/]+/g, "[^/]+");
  return new RegExp("^" + source + "$").test(pathname);
}
export function middleware(request: Request) {
  const pathname = new URL(request.url).pathname;
  if (!__krateMatchers.some((m: string) => __krateMatcher(m, pathname))) return;
  return ` + name + `(request);
}
`
	src += helper
	return src, true
}
