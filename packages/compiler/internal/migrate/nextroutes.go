package migrate

import (
	"path/filepath"
	"regexp"
	"strconv"
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
			if rewritten, items, ok := rewriteNextDynamic(src); ok {
				src = rewritten
				f.Rewrites = append(f.Rewrites, "rewrote next/dynamic to React lazy (ssr: false renders the loading fallback on the server and mounts the component in the browser)")
				for _, item := range items {
					rep.Manual = append(rep.Manual, ManualItem{Source: f.Source, Reason: item, Hint: "verify lazy loading behavior"})
				}
			} else {
				rep.Manual = append(rep.Manual, ManualItem{Source: f.Source, Reason: "next/dynamic import could not be rewritten automatically", Hint: "declare a lazy component with React lazy()"})
			}
		}
		if strings.Contains(src, "next/script") {
			rewritten, rewrites, items := rewriteNextScript(src)
			src = rewritten
			f.Rewrites = append(f.Rewrites, rewrites...)
			for _, item := range items {
				rep.Manual = append(rep.Manual, ManualItem{Source: f.Source, Reason: item, Hint: "see the next/script section of the migration guide"})
			}
		}
		if f.Kind != KindAPI && f.Kind != KindMiddleware && strings.Contains(src, "next/server") {
			if rewritten, ok := rewriteNextServerAfter(src); ok {
				src = rewritten
				f.Rewrites = append(f.Rewrites, "mapped next/server after() to @krate/runtime/server: it runs after the response is sent")
				if !strings.Contains(src, "next/server") {
					dropManual(rep, f.Source, "next/server request/response helpers need a Request/Response migration")
				}
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
	nextDynamicWord   = regexp.MustCompile(`\bdynamic\b`)
	nextDynamicImport = regexp.MustCompile(`(?m)^[ \t]*import[ \t]+dynamic[ \t]+from[ \t]+["']next/dynamic["'][ \t]*;?[ \t]*\r?\n?`)
	reactNamedImport  = regexp.MustCompile(`import\s*\{([^}]*)\}\s*from\s*["']react["']`)
	dynamicDecl       = regexp.MustCompile(`^(?:export[ \t]+)?(?:const|let|var)[ \t]+([A-Za-z_$][\w$]*)`)
	dynamicOptionKey  = regexp.MustCompile(`(?s)^\s*([A-Za-z_$][\w$]*)\s*(?::\s*(.*))?$`)
	arrowNoParams     = regexp.MustCompile(`(?s)^(?:async\s+)?\(\s*\)\s*=>\s*(.*)$`)
)

// dynamicCall is one dynamic(loader, options) call found in a source file.
type dynamicCall struct {
	start, end int // call span, from "dynamic" to just after ")"
	typeArgs   string
	loader     string
	options    string
	hasOptions bool
}

// findDynamicCall returns the first dynamic(...) or dynamic<T>(...) call at or
// after from. ok is false when there is none.
func findDynamicCall(src string, from int) (c dynamicCall, ok bool) {
	for _, loc := range nextDynamicWord.FindAllStringIndex(src[from:], -1) {
		start := from + loc[0]
		if start > 0 && (src[start-1] == '.' || src[start-1] == '$') {
			continue
		}
		i := start + len("dynamic")
		for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
			i++
		}
		var typeArgs string
		if i < len(src) && src[i] == '<' {
			j := skipTypeArgs(src, i)
			if j < 0 {
				continue
			}
			typeArgs = src[i:j]
			i = j
			for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
				i++
			}
		}
		if i >= len(src) || src[i] != '(' {
			continue
		}
		comma, end := topLevelArgs(src, i+1)
		c = dynamicCall{start: start, typeArgs: typeArgs}
		if comma < 0 {
			// One argument, or unbalanced: find the closing paren.
			depthEnd := matchParen(src, i)
			if depthEnd < 0 {
				continue
			}
			c.end = depthEnd + 1
			c.loader = strings.TrimSpace(src[i+1 : depthEnd])
			return c, true
		}
		c.end = end + 1
		c.loader = strings.TrimSpace(src[i+1 : comma])
		c.options = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(src[comma+1:end]), ","))
		c.hasOptions = true
		return c, true
	}
	return dynamicCall{}, false
}

// skipTypeArgs returns the index after the ">" that closes the "<" at i, or -1.
// The ">" of an arrow type does not close it.
func skipTypeArgs(src string, i int) int {
	depth := 0
	for j := i; j < len(src); j++ {
		switch src[j] {
		case '<':
			depth++
		case '>':
			if j > 0 && src[j-1] == '=' {
				continue
			}
			depth--
			if depth == 0 {
				return j + 1
			}
		case ';':
			return -1
		}
	}
	return -1
}

// dynamicOptions splits the top-level properties of an options object literal
// into key and value text. ok is false when it is not a plain object literal.
func dynamicOptions(text string) (props map[string]string, ok bool) {
	t := strings.TrimSpace(text)
	if len(t) < 2 || t[0] != '{' || t[len(t)-1] != '}' {
		return nil, false
	}
	body := t[1 : len(t)-1]
	props = map[string]string{}
	depth, start := 0, 0
	flush := func(end int) bool {
		part := strings.TrimSpace(body[start:end])
		if part == "" {
			return true
		}
		m := dynamicOptionKey.FindStringSubmatch(part)
		if m == nil {
			return false
		}
		val := strings.TrimSpace(m[2])
		if val == "" {
			val = m[1]
		}
		props[m[1]] = val
		return true
	}
	for i := 0; i < len(body); i++ {
		switch c := body[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				if !flush(i) {
					return nil, false
				}
				start = i + 1
			}
		case '"', '\'', '`':
			for i++; i < len(body) && body[i] != c; i++ {
				if body[i] == '\\' {
					i++
				}
			}
		}
	}
	if !flush(len(body)) {
		return nil, false
	}
	return props, true
}

// rewriteNextDynamic maps next/dynamic onto React lazy(). A call with
// `ssr: false` becomes a component that renders its loading fallback on the
// server and mounts the lazy component in the browser. manual lists options
// that were dropped without an equivalent.
func rewriteNextDynamic(src string) (out string, manual []string, ok bool) {
	if !nextDynamicImport.MatchString(src) {
		return "", nil, false
	}
	src = nextDynamicImport.ReplaceAllString(src, "")
	need := map[string]bool{"lazy": true}
	from, n := 0, 0
	for {
		c, found := findDynamicCall(src, from)
		if !found {
			break
		}
		repl, hoist, hoistAt, items, gated := buildDynamic(src, c, n)
		manual = append(manual, items...)
		if gated {
			n++
			need["useState"], need["useEffect"] = true, true
			src = src[:hoistAt] + hoist + src[hoistAt:c.start] + repl + src[c.end:]
			from = c.start + len(hoist) + len(repl)
			continue
		}
		src = src[:c.start] + repl + src[c.end:]
		from = c.start + len(repl)
	}
	var names []string
	have := map[string]bool{}
	for _, m := range reactNamedImport.FindAllStringSubmatch(src, -1) {
		for _, name := range strings.Split(m[1], ",") {
			have[strings.TrimSpace(name)] = true
		}
	}
	for _, name := range []string{"lazy", "useEffect", "useState"} {
		if need[name] && !have[name] {
			names = append(names, name)
		}
	}
	if len(names) > 0 {
		src = insertImportAfterDirective(src, "import { "+strings.Join(names, ", ")+" } from \"react\";\n")
	}
	p := parser.New(lexer.New(src).Tokenize())
	if prog := p.ParseProgram(); prog == nil || len(p.Errors()) > 0 {
		return "", nil, false
	}
	return src, manual, true
}

// buildDynamic returns the replacement for one call. For an ssr:false call it
// also returns a module-level declaration (hoist) to insert at hoistAt.
func buildDynamic(src string, c dynamicCall, n int) (repl, hoist string, hoistAt int, manual []string, gated bool) {
	lazyCall := "lazy" + c.typeArgs + "(" + c.loader + ")"
	props, isObj := dynamicOptions(c.options)
	if !c.hasOptions {
		return lazyCall, "", 0, nil, false
	}
	if !isObj {
		return lazyCall, "", 0, []string{"next/dynamic options are not an object literal and were ignored; use a client-mounted wrapper for ssr: false"}, false
	}
	if props["ssr"] != "false" {
		if _, has := props["loading"]; has {
			manual = append(manual, "next/dynamic loading option was ignored; show a fallback with Suspense")
		}
		return lazyCall, "", 0, manual, false
	}
	stmt, name, ok := dynamicStatement(src, c.start)
	if !ok {
		return lazyCall, "", 0, []string{"next/dynamic with ssr: false is not declared at the top level of the module and was converted to a plain lazy(); move it to a module-level const"}, false
	}
	lazyName := name + "Lazy"
	if name == "" {
		lazyName = "KrateLazy" + strconv.Itoa(n+1)
	}
	fallback := "null"
	if l, has := props["loading"]; has {
		var items []string
		fallback, items = dynamicFallback(l)
		manual = append(manual, items...)
	}
	propsType := "any"
	if c.typeArgs != "" {
		propsType = strings.TrimSpace(c.typeArgs[1 : len(c.typeArgs)-1])
	}
	hoist = "const " + lazyName + " = " + lazyCall + ";\n"
	repl = "(props: " + propsType + ") => {\n" +
		"  const [mounted, setMounted] = useState(false);\n" +
		"  useEffect(() => { setMounted(true); }, []);\n" +
		"  return mounted ? <" + lazyName + " {...props} /> : " + fallback + ";\n}"
	return repl, hoist, stmt, manual, true
}

// dynamicFallback turns the loading option into a JSX expression.
func dynamicFallback(loading string) (expr string, manual []string) {
	l := strings.TrimSpace(loading)
	if m := arrowNoParams.FindStringSubmatch(l); m != nil {
		body := strings.TrimSpace(m[1])
		if !strings.HasPrefix(body, "{") {
			return body, nil
		}
	}
	if regexp.MustCompile(`^[A-Z][\w$]*$`).MatchString(l) {
		return "<" + l + " />", nil
	}
	return "null", []string{"next/dynamic loading is not an arrow function returning JSX or a component name and was replaced by null; render your fallback in the wrapper"}
}

// dynamicStatement finds the start of the top-level declaration that holds the
// call at idx (above its leading comment) and the declared name.
func dynamicStatement(src string, idx int) (at int, name string, ok bool) {
	lineStart := strings.LastIndexByte(src[:idx], '\n') + 1
	for !isLetter(src[lineStart]) {
		if lineStart == 0 {
			return 0, "", false
		}
		lineStart = strings.LastIndexByte(src[:lineStart-1], '\n') + 1
	}
	rest := src[lineStart:idx]
	m := dynamicDecl.FindStringSubmatch(rest)
	isDefault := strings.HasPrefix(rest, "export default")
	if m == nil && !isDefault {
		return 0, "", false
	}
	depth := 0
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}
	}
	if depth != 0 {
		return 0, "", false
	}
	if m != nil {
		name = m[1]
	}
	at = lineStart
	for at > 0 {
		prevStart := strings.LastIndexByte(src[:at-1], '\n') + 1
		prev := strings.TrimSpace(src[prevStart:at])
		if strings.HasPrefix(prev, "*") || strings.HasPrefix(prev, "/*") || strings.HasPrefix(prev, "//") {
			at = prevStart
			continue
		}
		break
	}
	return at, name, true
}

func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// topLevelArgs scans the arguments of a call whose "(" ends at start. It
// returns the index of the comma that starts the second argument and of the
// closing ")", or -1 when the call has a single argument or is unbalanced.
func topLevelArgs(src string, start int) (comma, end int) {
	comma = -1
	depth := 0
	for i := start; i < len(src); i++ {
		switch c := src[i]; c {
		case '(', '[', '{':
			depth++
		case ']', '}':
			depth--
		case ')':
			if depth == 0 {
				if comma < 0 {
					return -1, -1
				}
				return comma, i
			}
			depth--
		case ',':
			if depth == 0 && comma < 0 {
				comma = i
			}
		case '"', '\'', '`':
			// Skip the literal; an unmatched quote (JSX text) is plain text.
			for j := i + 1; j < len(src) && (c == '`' || src[j] != '\n'); j++ {
				if src[j] == '\\' {
					j++
				} else if src[j] == c {
					i = j
					break
				}
			}
		}
	}
	return -1, -1
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
	src = insertImportAfterDirective(src, header)
	return src, rewrites, manual
}

var (
	requestCookieWrite = regexp.MustCompile(`\b(?:request|req)\.cookies\.(?:set|delete|clear)\s*\(`)
	requestCookiesRead = regexp.MustCompile(`\b(?:request|req)\.cookies\b`)
	requestNextURL     = regexp.MustCompile(`\b(?:request|req)\.nextUrl\b`)
)

// rewriteNextServerAfter moves after (and unstable_after) out of a value
// import from next/server into @krate/runtime/server, keeping every other
// name where it was. Type-only imports are left alone.
func rewriteNextServerAfter(src string) (string, bool) {
	changed := false
	out := nextServerImport.ReplaceAllStringFunc(src, func(line string) string {
		sub := nextServerImport.FindStringSubmatch(line)
		if len(sub) != 2 || strings.HasPrefix(strings.TrimSpace(line), "import type") {
			return line
		}
		var kept, moved []string
		for _, name := range strings.Split(sub[1], ",") {
			spec := strings.TrimSpace(name)
			if spec == "" {
				continue
			}
			base, alias, hasAlias := strings.Cut(strings.TrimSpace(strings.TrimPrefix(spec, "type ")), " as ")
			base = strings.TrimSpace(base)
			if base != "after" && base != "unstable_after" || strings.HasPrefix(spec, "type ") {
				kept = append(kept, spec)
				continue
			}
			switch {
			case hasAlias:
				moved = append(moved, "after as "+strings.TrimSpace(alias))
			case base == "unstable_after":
				moved = append(moved, "after as unstable_after")
			default:
				moved = append(moved, "after")
			}
		}
		if len(moved) == 0 {
			return line
		}
		changed = true
		res := "import { " + strings.Join(moved, ", ") + " } from \"@krate/runtime/server\";\n"
		if len(kept) > 0 {
			res = "import { " + strings.Join(kept, ", ") + " } from \"next/server\";\n" + res
		}
		return res
	})
	return out, changed
}

// dropManual removes the manual items of source with exactly this reason.
func dropManual(rep *Report, source, reason string) {
	kept := rep.Manual[:0]
	for _, m := range rep.Manual {
		if m.Source == source && m.Reason == reason {
			continue
		}
		kept = append(kept, m)
	}
	rep.Manual = kept
}
