package migrate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/routesource"
)

const (
	redirectHelperMarker = "krate migrate: next.config redirect helper"
	redirectBlockMarker  = "krate migrate: next.config redirects["
)

// middlewareHandlerRe finds the exported middleware function.
var middlewareHandlerRe = regexp.MustCompile(`export\s+(?:async\s+)?function\s+middleware\s*\(`)

var paramIdentRe = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// redirectHelperSource is the matcher the injected blocks call. It follows
// Next's matchHas and prepareDestination: a has/missing value is a regex
// anchored with ^(?:...)$, named groups and the key of a presence-only item
// become parameters, and the request query is merged into the destination.
const redirectHelperSource = `// ` + redirectHelperMarker + ` (begin)
type __KrateCondition = { type: string; key?: string; value?: string };
function __krateConditionsMatch(request: Request, url: URL, has: __KrateCondition[], missing: __KrateCondition[], params: Record<string, string>): boolean {
  const read = (c: __KrateCondition): string | undefined => {
    switch (c.type) {
      case "header":
        return request.headers.get(c.key || "") || undefined;
      case "cookie":
        for (const part of (request.headers.get("cookie") || "").split(";")) {
          const i = part.indexOf("=");
          if (i > 0 && part.slice(0, i).trim() === c.key) {
            const raw = part.slice(i + 1).trim();
            try { return decodeURIComponent(raw) || undefined; } catch { return raw || undefined; }
          }
        }
        return undefined;
      case "query": {
        const all = url.searchParams.getAll(c.key || "");
        return all.length ? all[all.length - 1] || undefined : undefined;
      }
      case "host":
        return (request.headers.get("host") || url.host).split(":")[0].toLowerCase() || undefined;
    }
    return undefined;
  };
  const test = (c: __KrateCondition, into: Record<string, string>): boolean => {
    const value = read(c);
    if (!value) return false;
    if (!c.value) {
      const key = c.type === "header" ? (c.key || "").toLowerCase() : c.key || "";
      into[key.replace(/[^A-Za-z]/g, "")] = value;
      return true;
    }
    const m = new RegExp("^(?:" + c.value + ")$").exec(value);
    if (!m) return false;
    if (m.groups) Object.assign(into, m.groups);
    else if (c.type === "host" && m[0]) into.host = m[0];
    return true;
  };
  for (const c of has) if (!test(c, params)) return false;
  for (const c of missing) if (test(c, {})) return false;
  return true;
}
function __krateNextRedirect(request: Request, rule: { pattern: string; names: string[]; mods: Record<string, string>; destination: string; status: number; has: __KrateCondition[]; missing: __KrateCondition[] }): Response | null {
  const url = new URL(request.url);
  const m = new RegExp(rule.pattern).exec(url.pathname);
  if (!m) return null;
  const params: Record<string, string> = {};
  rule.names.forEach((name, i) => { if (name) params[name] = m[i + 1] === undefined ? "" : m[i + 1]; });
  if (!__krateConditionsMatch(request, url, rule.has, rule.missing, params)) return null;
  const enc = (v: string) => v.split("/").map(encodeURIComponent).join("/");
  let dest = rule.destination;
  if ("splat" in params) {
    if (dest.includes(":splat")) dest = dest.split(":splat").join(enc(params.splat));
    else if (params.splat) dest = dest.replace(/\/+$/, "") + "/" + enc(params.splat);
  }
  dest = dest.replace(/:([A-Za-z_][A-Za-z0-9_]*)([*+?])?/g, (all: string, name: string, mod?: string) => {
    if (name === "splat" || !(name in params)) return all;
    return enc(params[name]) + (mod && rule.mods[name] !== mod ? mod : "");
  });
  if (rule.destination.startsWith("/") && !rule.destination.startsWith("//")) dest = dest.replace(/^[\/\\]{2,}/, "/");
  let frag = "";
  const h = dest.indexOf("#");
  if (h >= 0) { frag = dest.slice(h); dest = dest.slice(0, h); }
  let destQuery = "";
  const q = dest.indexOf("?");
  if (q >= 0) { destQuery = dest.slice(q + 1); dest = dest.slice(0, q); }
  const merged = new URLSearchParams(url.search);
  const fromDest = new URLSearchParams(destQuery);
  for (const key of new Set(fromDest.keys())) {
    merged.delete(key);
    for (const v of fromDest.getAll(key)) merged.append(key, v);
  }
  const qs = merged.toString().replace(/\+/g, "%20");
  return new Response(null, { status: rule.status, headers: { Location: dest + (qs ? "?" + qs : "") + frag } });
}
// ` + redirectHelperMarker + ` (end)
`

// jsJSON renders v as JSON without HTML escaping, so "<" stays readable in
// the generated source.
func jsJSON(v any) string {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSpace(sb.String())
}

func jsString(s string) string { return jsJSON(s) }

func jsConditions(cs []routesource.Condition) string {
	if len(cs) == 0 {
		return "[]"
	}
	return jsJSON(cs)
}

// redirectBlock renders the marked block for one redirect.
func redirectBlock(indent, param string, r middlewareRedirect) (string, error) {
	js, err := routesource.CompileJS(r.Source)
	if err != nil {
		return "", err
	}
	names := jsJSON(append([]string{}, js.Names...))
	mods := jsJSON(js.Mods)
	var sb strings.Builder
	label := fmt.Sprintf("%s%d]", redirectBlockMarker, r.Index)
	fmt.Fprintf(&sb, "%s// %s (a regex Go cannot run in krate.config.ts)\n", indent, label)
	fmt.Fprintf(&sb, "%s{\n", indent)
	fmt.Fprintf(&sb, "%s  const __krateRedirect = __krateNextRedirect(%s, { pattern: %s, names: %s, mods: %s, destination: %s, status: %d, has: %s, missing: %s });\n",
		indent, param, jsString(js.Pattern), names, mods, jsString(r.Destination), r.Status, jsConditions(r.Has), jsConditions(r.Missing))
	fmt.Fprintf(&sb, "%s  if (__krateRedirect) return __krateRedirect;\n", indent)
	fmt.Fprintf(&sb, "%s}\n", indent)
	return sb.String(), nil
}

// bodyStart finds the offset just after the "{" that opens the exported
// middleware function, and the name of its request parameter. ok is false
// when the signature is not one this can edit safely.
func bodyStart(src string) (at int, param string, ok bool) {
	locs := middlewareHandlerRe.FindAllStringIndex(src, -1)
	if len(locs) == 0 {
		return 0, "", false
	}
	open := locs[len(locs)-1][1] - 1
	depth := 0
	closeAt := -1
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				closeAt = i
			}
		}
		if closeAt >= 0 {
			break
		}
	}
	if closeAt < 0 {
		return 0, "", false
	}
	first := src[open+1 : closeAt]
	if i := strings.IndexAny(first, ",:=?"); i >= 0 {
		first = first[:i]
	}
	param = strings.TrimSpace(first)
	if !paramIdentRe.MatchString(param) {
		return 0, "", false
	}
	brace := strings.IndexByte(src[closeAt:], '{')
	if brace < 0 {
		return 0, "", false
	}
	between := src[closeAt+1 : closeAt+brace]
	if strings.TrimSpace(between) != "" && !regexp.MustCompile(`^\s*:\s*[\w\s|.\[\]]+$`).MatchString(between) {
		return 0, "", false
	}
	return closeAt + brace + 1, param, true
}

// injectMiddlewareRedirects writes the redirects the config cannot express
// into the middleware, as marked blocks at the start of the function so they
// run before the app's own checks (Next runs redirects before middleware). A
// middleware is created when the project has none.
func injectMiddlewareRedirects(abs string, rep *Report, reds []middlewareRedirect) {
	if len(reds) == 0 {
		return
	}
	idx := -1
	for i := range rep.Files {
		if rep.Files[i].Kind == KindMiddleware {
			idx = i
			break
		}
	}
	var src string
	created := false
	if idx >= 0 {
		src = string(rep.Files[idx].Content)
	} else {
		created = true
		src = "export function middleware(request: Request) {\n  return;\n}\n"
	}
	at, param, ok := bodyStart(src)
	skip := func(why string) {
		for _, r := range reds {
			rep.Manual = append(rep.Manual, ManualItem{
				Source: "next.config",
				Reason: fmt.Sprintf("redirects[%d] source %q uses a regex Go cannot run and %s; it was not translated", r.Index, r.Source, why),
				Hint:   "add the redirect to middleware.ts by hand: check new URL(request.url).searchParams, headers or cookies and return Response.redirect(...)",
			})
		}
	}
	if !ok {
		skip("the middleware's function signature could not be edited safely")
		return
	}
	var blocks strings.Builder
	var done []middlewareRedirect
	for _, r := range reds {
		b, err := redirectBlock("  ", param, r)
		if err != nil {
			rep.Manual = append(rep.Manual, ManualItem{Source: "next.config", Reason: fmt.Sprintf("redirects[%d] source %q: %v; it was not translated", r.Index, r.Source, err), Hint: "write the redirect into middleware.ts by hand"})
			continue
		}
		blocks.WriteString(b)
		done = append(done, r)
	}
	if len(done) == 0 {
		return
	}
	src = src[:at] + "\n" + blocks.String() + src[at:]
	if !strings.Contains(src, redirectHelperMarker) {
		src = insertImportAfterDirective(src, redirectHelperSource)
	}
	note := fmt.Sprintf("%d next.config redirect(s) use a regex Go cannot run; they were written as marked blocks at the start of the middleware function (they run after the redirects in krate.config.ts)", len(done))
	target := ""
	if created {
		target = "middleware.ts"
		if isDir(filepath.Join(abs, "src", "app")) || isDir(filepath.Join(abs, "src", "pages")) {
			target = "src/middleware.ts"
		}
		rep.Files = append(rep.Files, PlannedFile{Kind: KindMiddleware, Target: target, Content: []byte(src), Rewrites: []string{note}})
	} else {
		target = rep.Files[idx].Target
		rep.Files[idx].Content = []byte(src)
		rep.Files[idx].Rewrites = append(rep.Files[idx].Rewrites, note)
	}
	for _, r := range done {
		rep.Info = append(rep.Info, fmt.Sprintf("next.config redirects[%d] (%s) uses a regex krate.config.ts cannot express; it is a marked block in %s", r.Index, r.Source, target))
	}
}
