package build

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/routesource"
)

// A compiled redirect, rewrite or header rule: the source pattern plus its
// has/missing conditions.
type routeMatcher struct {
	src  *routesource.Source
	cond *routesource.Conditions
}

func compileMatcher(source string, kind routesource.Kind, has, missing []config.RouteCondition) (routeMatcher, error) {
	src, err := routesource.Compile(source, kind)
	if err != nil {
		return routeMatcher{}, err
	}
	cond, err := routesource.CompileConditions(has, missing)
	if err != nil {
		return routeMatcher{}, err
	}
	return routeMatcher{src: src, cond: cond}, nil
}

// match reports whether the request path and conditions both match, and
// returns the captured values (conditions win over path parameters, as in
// Next) and the modifier each path parameter was declared with.
func (m routeMatcher) match(r *http.Request, p string) (map[string]string, map[string]byte, bool) {
	params, ok := m.src.Match(p)
	if !ok {
		return nil, nil, false
	}
	extra, ok := m.cond.Match(r)
	if !ok {
		return nil, nil, false
	}
	for k, v := range extra {
		params.Values[k] = v
	}
	return params.Values, params.Mods, true
}

type redirectRule struct {
	routeMatcher
	rule config.Redirect
}

// compileRedirects compiles the rules. config.Validate rejects a rule that
// does not compile; one that still fails here (a Config built in code) is
// reported and skipped.
func compileRedirects(rules []config.Redirect) []redirectRule {
	var out []redirectRule
	for _, rd := range rules {
		m, err := compileMatcher(rd.Source, routesource.KindRedirect, rd.Has, rd.Missing)
		if err != nil {
			eprintf("  %sSkipped redirect %q:%s %v\n", cYellow, rd.Source, cReset, err)
			continue
		}
		out = append(out, redirectRule{m, rd})
	}
	return out
}

type rewriteRule struct {
	routeMatcher
	rule config.Rewrite
}

func compileRewrites(rules []config.Rewrite) []rewriteRule {
	var out []rewriteRule
	for _, rw := range rules {
		m, err := compileMatcher(rw.Source, routesource.KindRewrite, rw.Has, rw.Missing)
		if err != nil {
			eprintf("  %sSkipped rewrite %q:%s %v\n", cYellow, rw.Source, cReset, err)
			continue
		}
		out = append(out, rewriteRule{m, rw})
	}
	return out
}

type headerRuleC struct {
	routeMatcher
	rule config.HeaderRule
}

func compileHeaderRules(rules []config.HeaderRule) []headerRuleC {
	var out []headerRuleC
	for _, hr := range rules {
		m, err := compileMatcher(hr.Source, routesource.KindHeader, hr.Has, hr.Missing)
		if err != nil {
			eprintf("  %sSkipped header rule %q:%s %v\n", cYellow, hr.Source, cReset, err)
			continue
		}
		out = append(out, headerRuleC{m, hr})
	}
	return out
}

// isInternalRoutePath reports paths the redirect layer leaves alone: Krate's
// own endpoints and the health probes.
func isInternalRoutePath(p string) bool {
	return strings.HasPrefix(p, "/__krate/") || p == "/healthz" || p == "/readyz"
}

// configRedirectMiddleware answers config redirects before anything else
// routes the request (middleware, API routes, pages, files), as Next does.
func configRedirectMiddleware(rules []config.Redirect, next http.Handler) http.Handler {
	compiled := compileRedirects(rules)
	if len(compiled) == 0 {
		return next
	}
	hit := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveConfigRedirect(w, r, r.URL.Path, compiled)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isInternalRoutePath(r.URL.Path) {
			for _, rd := range compiled {
				if _, _, ok := rd.match(r, r.URL.Path); ok {
					hit.ServeHTTP(w, r)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// serveConfigRedirect answers the first rule whose source and conditions
// match and reports whether it did. The request's query string is carried to
// the destination; a key the destination sets itself wins.
func serveConfigRedirect(w http.ResponseWriter, r *http.Request, p string, rules []redirectRule) bool {
	for _, rd := range rules {
		vals, mods, ok := rd.match(r, p)
		if !ok {
			continue
		}
		dest := buildDestination(rd.rule.Destination, vals, mods)
		http.Redirect(w, r, mergeRequestQuery(dest, r.URL.RawQuery), rd.rule.Status())
		return true
	}
	return false
}

// queryPair is one key of a query string with its values, in first-seen order.
type queryPair struct {
	key  string
	vals []string
}

func parseQueryOrdered(raw string) []queryPair {
	var out []queryPair
	idx := map[string]int{}
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		dk, err1 := url.QueryUnescape(k)
		dv, err2 := url.QueryUnescape(v)
		if err1 != nil || err2 != nil {
			continue
		}
		i, ok := idx[dk]
		if !ok {
			i = len(out)
			idx[dk] = i
			out = append(out, queryPair{key: dk})
		}
		out[i].vals = append(out[i].vals, dv)
	}
	return out
}

func encodeQueryPart(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// mergeRequestQuery adds the request's query to the destination's own. The
// destination's keys replace the request's values for the same key.
func mergeRequestQuery(dest, rawQuery string) string {
	if rawQuery == "" {
		return dest
	}
	frag := ""
	if i := strings.IndexByte(dest, '#'); i >= 0 {
		dest, frag = dest[:i], dest[i:]
	}
	destQuery := ""
	if i := strings.IndexByte(dest, '?'); i >= 0 {
		dest, destQuery = dest[:i], dest[i+1:]
	}
	pairs := parseQueryOrdered(rawQuery)
	idx := map[string]int{}
	for i, p := range pairs {
		idx[p.key] = i
	}
	for _, dp := range parseQueryOrdered(destQuery) {
		if i, ok := idx[dp.key]; ok {
			pairs[i].vals = dp.vals
			continue
		}
		idx[dp.key] = len(pairs)
		pairs = append(pairs, dp)
	}
	var sb strings.Builder
	for _, p := range pairs {
		for _, v := range p.vals {
			if sb.Len() > 0 {
				sb.WriteByte('&')
			}
			sb.WriteString(encodeQueryPart(p.key))
			sb.WriteByte('=')
			sb.WriteString(encodeQueryPart(v))
		}
	}
	if sb.Len() == 0 {
		return dest + frag
	}
	return dest + "?" + sb.String() + frag
}

var destParamRe = regexp.MustCompile(`:([A-Za-z_][A-Za-z0-9_]*)`)

// escapeRedirectValue percent-encodes a matched (already decoded) path value
// for use inside a destination: each segment is escaped on its own so a "\"
// or an encoded "/" can never turn into a scheme-relative URL, while "/"
// between segments of a multi-segment value is kept.
func escapeRedirectValue(v string) string {
	if v == "" {
		return ""
	}
	segs := strings.Split(v, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return strings.Join(segs, "/")
}

// escapeQueryValue is escapeRedirectValue for a value placed after the "?" of
// a destination, where "&", "=" and "+" must not be able to add a parameter.
func escapeQueryValue(v string) string {
	return strings.ReplaceAll(encodeQueryPart(v), "%2F", "/")
}

// buildDestination fills ":splat" and ":name" (also ":name*", ":name+" and
// ":name?") placeholders in destination from the matched values. Values are
// percent-encoded, and a path destination never starts with "//" or "/\" (an
// open redirect), whatever the request contained.
func buildDestination(destination string, vals map[string]string, mods map[string]byte) string {
	var out string
	if splat, ok := vals["splat"]; ok {
		switch {
		case strings.Contains(destination, ":splat"):
			out = strings.ReplaceAll(destination, ":splat", escapeRedirectValue(splat))
		case splat != "":
			out = strings.TrimRight(destination, "/") + "/" + escapeRedirectValue(splat)
		default:
			out = destination
		}
		if len(vals) > 1 {
			out = replaceDestParams(out, vals, mods)
		}
	} else {
		out = replaceDestParams(destination, vals, mods)
	}
	return sanitizeRedirectTarget(destination, out)
}

// replaceDestParams substitutes ":name" placeholders. A '*', '+' or '?' right
// after the name is part of the placeholder only when the source declared the
// parameter with that modifier, so "/x/:id?ref=1" keeps its query string.
func replaceDestParams(destination string, params map[string]string, mods map[string]byte) string {
	queryAt := strings.IndexByte(destination, '?')
	var sb strings.Builder
	last := 0
	for _, m := range destParamRe.FindAllStringSubmatchIndex(destination, -1) {
		name := destination[m[2]:m[3]]
		val, ok := params[name]
		if !ok || name == "splat" {
			continue
		}
		end := m[1]
		if mod := mods[name]; mod != 0 && end < len(destination) && destination[end] == mod {
			end++
		}
		prefix := destination[last:m[0]]
		// An optional parameter that matched nothing takes its "/" with it,
		// like path-to-regexp: /a/:rest* at /a gives /a, not /a/.
		if val == "" && (queryAt < 0 || m[0] < queryAt) && (mods[name] == '*' || mods[name] == '?') && strings.HasSuffix(prefix, "/") && sb.Len()+len(prefix) > 1 {
			prefix = prefix[:len(prefix)-1]
		}
		sb.WriteString(prefix)
		if queryAt >= 0 && m[0] > queryAt {
			sb.WriteString(escapeQueryValue(val))
		} else {
			sb.WriteString(escapeRedirectValue(val))
		}
		last = end
	}
	sb.WriteString(destination[last:])
	return sb.String()
}

type cachedSource struct {
	src *routesource.Source
	err error
}

var sourceCache sync.Map

// rewriteDestination matches source against path and fills destination from
// the match; with no match the destination is returned unchanged. It compiles
// the source once and caches it, for callers that hold no compiled rule.
func rewriteDestination(path, source, destination string) string {
	v, ok := sourceCache.Load(source)
	if !ok {
		src, err := routesource.Compile(source, routesource.KindRedirect)
		v, _ = sourceCache.LoadOrStore(source, cachedSource{src, err})
	}
	cs := v.(cachedSource)
	if cs.err != nil {
		return destination
	}
	params, ok := cs.src.Match(path)
	if !ok {
		return destination
	}
	return buildDestination(destination, params.Values, params.Mods)
}
