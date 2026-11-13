package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
)

// ruleCtx carries what the redirect/rewrite/header readers share.
type ruleCtx struct {
	file string
	dir  string
	src  string
	prog *ast.Program
	// headersLost is set when any header rule or entry was not translated.
	headersLost bool
}

const (
	conditionHintRedirect = "Krate redirects match the request path only. Handle this one in middleware.ts (project root or src/): check new URL(request.url).searchParams, headers or cookies and return Response.redirect(...)"
	conditionHintRewrite  = "Krate rewrites match the request path only. Handle this one in middleware.ts (project root or src/): check the query, headers or cookies and return a Response with an x-middleware-rewrite header"
	conditionHintHeader   = "Krate header rules match the request path only. Set this one from middleware.ts (project root or src/): check the query, headers or cookies and return a Response with an x-middleware-next: 1 header plus the headers to add"
)

// element classifies one array element of redirects/rewrites/headers. It
// returns the object and its static source, or a manual item saying why the
// rule cannot be translated. A rule with has/missing is never returned.
func (rc *ruleCtx) element(kind string, i int, el ast.Expr, conditionHint string) (*ast.ObjectExpr, string, *ManualItem) {
	label := fmt.Sprintf("%s[%d]", kind, i)
	item, ok := el.(*ast.ObjectExpr)
	if !ok {
		return nil, "", &ManualItem{Source: rc.file, Reason: label + " is not an object literal (spread or computed); it was not translated", Hint: "write the rule into krate.config.ts by hand"}
	}
	source, ok := stringValue(prop(item, "source"))
	if !ok {
		return nil, "", &ManualItem{Source: rc.file, Reason: label + " has a non-static source; it was not translated", Hint: "write the rule into krate.config.ts by hand"}
	}
	for _, p := range item.Properties {
		if p != nil && (p.Spread || p.Computed) {
			return nil, source, &ManualItem{Source: rc.file, Reason: fmt.Sprintf("%s source %q spreads or computes keys; it was not translated", label, source), Hint: "write the rule into krate.config.ts by hand"}
		}
	}
	if cond := describeConditions(item); cond != "" {
		return nil, source, &ManualItem{
			Source: rc.file,
			Reason: fmt.Sprintf("%s source %q is conditional (%s) and was NOT translated: Krate has no has/missing, so an unconditional copy would apply to every request", label, source, cond),
			Hint:   conditionHint,
		}
	}
	for _, key := range []string{"basePath", "locale"} {
		if prop(item, key) != nil {
			return nil, source, &ManualItem{Source: rc.file, Reason: fmt.Sprintf("%s source %q sets %s, which changes how the source matches; it was not translated", label, source, key), Hint: "write the rule into krate.config.ts by hand"}
		}
	}
	return item, source, nil
}

// describeConditions renders the has/missing conditions of a rule, or "".
func describeConditions(item *ast.ObjectExpr) string {
	var parts []string
	for _, field := range []string{"has", "missing"} {
		v := prop(item, field)
		if v == nil {
			continue
		}
		arr, ok := v.(*ast.ArrayExpr)
		if !ok || len(arr.Elements) == 0 {
			parts = append(parts, field+" (computed)")
			continue
		}
		for _, el := range arr.Elements {
			c, ok := el.(*ast.ObjectExpr)
			if !ok {
				parts = append(parts, field+" (computed)")
				continue
			}
			typ, _ := stringValue(prop(c, "type"))
			key, _ := stringValue(prop(c, "key"))
			desc := field + " " + typ
			if key != "" {
				desc += " " + fmt.Sprintf("%q", key)
			}
			if val, ok := stringValue(prop(c, "value")); ok {
				desc += " = " + fmt.Sprintf("%q", val)
			}
			parts = append(parts, strings.TrimSpace(desc))
		}
	}
	return strings.Join(parts, ", ")
}

// redirects reads a static redirects array (or a function returning one).
// reason is non-empty when the value as a whole is not a static array.
func (rc *ruleCtx) redirects(v ast.Expr) (rules []nextRedirect, items []ManualItem, reason string) {
	arr, ok := returnedValue(v).(*ast.ArrayExpr)
	if !ok {
		return nil, nil, "not a static array"
	}
	for i, el := range arr.Elements {
		obj, source, item := rc.element("redirects", i, el, conditionHintRedirect)
		if item != nil {
			items = append(items, *item)
			continue
		}
		dest, ok := stringValue(prop(obj, "destination"))
		if !ok {
			items = append(items, ManualItem{Source: rc.file, Reason: fmt.Sprintf("redirects[%d] source %q has a non-static destination; it was not translated", i, source), Hint: "write the rule into krate.config.ts by hand"})
			continue
		}
		perm := false
		computed := false
		if p := prop(obj, "permanent"); p != nil {
			if b, ok := boolValue(p); ok {
				perm = b
			} else {
				computed = true
			}
		}
		if sc := prop(obj, "statusCode"); sc != nil {
			if n, ok := intValue(sc); ok {
				perm = n == 301 || n == 308
			} else {
				computed = true
			}
		}
		if computed {
			items = append(items, ManualItem{Source: rc.file, Reason: fmt.Sprintf("redirects[%d] source %q has a computed permanent/statusCode; it was not translated", i, source), Hint: "write the rule into krate.config.ts by hand"})
			continue
		}
		rules = append(rules, nextRedirect{Source: source, Destination: dest, Permanent: perm})
	}
	return rules, items, ""
}

// rewrites reads a static rewrites array or the beforeFiles/afterFiles/fallback
// object form.
func (rc *ruleCtx) rewrites(v ast.Expr) (rules []nextRewrite, items []ManualItem, reason string) {
	if arr, ok := v.(*ast.ArrayExpr); ok {
		rules, items = rc.rewriteArray(arr)
		return rules, items, ""
	}
	obj, ok := v.(*ast.ObjectExpr)
	if !ok {
		return nil, nil, "not a static array or beforeFiles/afterFiles/fallback object"
	}
	for _, key := range []string{"beforeFiles", "afterFiles", "fallback"} {
		v := prop(obj, key)
		if v == nil {
			continue
		}
		arr, ok := v.(*ast.ArrayExpr)
		if !ok {
			items = append(items, ManualItem{Source: rc.file, Reason: "rewrites." + key + " is not a static array; it was not translated", Hint: "write the rules into krate.config.ts by hand"})
			continue
		}
		rws, its := rc.rewriteArray(arr)
		rules = append(rules, rws...)
		items = append(items, its...)
	}
	return rules, items, ""
}

func (rc *ruleCtx) rewriteArray(arr *ast.ArrayExpr) ([]nextRewrite, []ManualItem) {
	var out []nextRewrite
	var items []ManualItem
	for i, el := range arr.Elements {
		obj, source, item := rc.element("rewrites", i, el, conditionHintRewrite)
		if item != nil {
			items = append(items, *item)
			continue
		}
		dest, ok := stringValue(prop(obj, "destination"))
		if !ok {
			items = append(items, ManualItem{Source: rc.file, Reason: fmt.Sprintf("rewrites[%d] source %q has a non-static destination; it was not translated", i, source), Hint: "write the rule into krate.config.ts by hand"})
			continue
		}
		out = append(out, nextRewrite{Source: source, Destination: dest})
	}
	return out, items
}

// resolveArray returns the array literal behind e, following a top-level
// `const name = [...]` once.
func (rc *ruleCtx) resolveArray(e ast.Expr) *ast.ArrayExpr {
	if arr, ok := e.(*ast.ArrayExpr); ok {
		return arr
	}
	id, ok := e.(*ast.Identifier)
	if !ok || rc.prog == nil {
		return nil
	}
	for _, stmt := range rc.prog.Body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d.Name == id.Name {
				arr, _ := d.Init.(*ast.ArrayExpr)
				return arr
			}
		}
	}
	return nil
}

// headers reads a headers array (or a function returning one). Static entries
// are kept even when a sibling entry or rule is computed.
func (rc *ruleCtx) headers(v ast.Expr) (rules []nextHeaderRule, items []ManualItem, reason string) {
	arr := rc.resolveArray(returnedValue(v))
	if arr == nil {
		return nil, nil, "not a static array"
	}
	for i, el := range arr.Elements {
		obj, source, item := rc.element("headers", i, el, conditionHintHeader)
		if item != nil {
			rc.headersLost = true
			items = append(items, *item)
			continue
		}
		harr := rc.resolveArray(prop(obj, "headers"))
		if harr == nil {
			rc.headersLost = true
			items = append(items, ManualItem{
				Source: rc.file,
				Reason: fmt.Sprintf("headers[%d] source %q: the headers value is computed (for example built by a helper call); none of its headers were translated", i, source),
				Hint:   headerHelperHint,
			})
			continue
		}
		rule := nextHeaderRule{Source: source, Headers: map[string]string{}}
		dropped := 0
		for _, hel := range harr.Elements {
			hm, ok := hel.(*ast.ObjectExpr)
			if !ok {
				dropped++
				continue
			}
			k, ok1 := stringValue(prop(hm, "key"))
			val, ok2 := stringValue(prop(hm, "value"))
			if !ok1 || !ok2 {
				dropped++
				continue
			}
			rule.Headers[k] = val
		}
		if dropped > 0 {
			rc.headersLost = true
			items = append(items, ManualItem{
				Source: rc.file,
				Reason: fmt.Sprintf("headers[%d] source %q: %d of %d header entries are computed and were not translated (%d static ones were)", i, source, dropped, len(harr.Elements), len(rule.Headers)),
				Hint:   headerHelperHint,
			})
		}
		if len(rule.Headers) > 0 {
			rules = append(rules, rule)
		}
	}
	return rules, items, ""
}

const headerHelperHint = "krate.config.ts is bundled with esbuild, so it can import a local module: import the helper that builds these headers and call it in the headers array there (it must not use Next.js or Node-only imports). Header values that depend on the request, such as a CSP nonce, must be set from middleware.ts instead"

var securityHeaderNames = []string{
	"Content-Security-Policy",
	"Strict-Transport-Security",
	"X-Frame-Options",
	"X-Content-Type-Options",
	"Permissions-Policy",
	"Referrer-Policy",
	"Cross-Origin-Opener-Policy",
	"Cross-Origin-Embedder-Policy",
	"Cross-Origin-Resource-Policy",
}

var configRelativeImport = regexp.MustCompile(`(?:from\s+|require\(\s*|import\(\s*)['"](\.{1,2}/[^'"]+)['"]`)

// securityItem reports, once, that security headers may have been lost. It
// looks for their names in next.config and in the local modules it imports.
func (rc *ruleCtx) securityItem() []ManualItem {
	if !rc.headersLost {
		return nil
	}
	text := strings.ToLower(rc.src + "\n" + rc.importedSource())
	var found []string
	for _, name := range securityHeaderNames {
		if strings.Contains(text, strings.ToLower(name)) {
			found = append(found, name)
		}
	}
	// Helpers are usually named after what they build.
	if len(found) == 0 && regexp.MustCompile(`(?i)security.?headers|\bcsp\b|\bhsts\b`).MatchString(text) {
		found = append(found, "security headers (names not visible)")
	}
	if len(found) == 0 {
		return nil
	}
	return []ManualItem{{
		Source: rc.file,
		Reason: "security headers (CSP, HSTS, ...) were NOT migrated: next.config mentions " + strings.Join(found, ", ") + " in headers that could not be translated",
		Hint:   "Until this is done, `krate serve` sends only Krate's own defaults. Set the static ones in krate.config.ts (`security`: frameOptions, referrerPolicy, permissionsPolicy, hsts; `csp`: see Content Security Policy in the config reference, or explicit `headers`), and anything per-request such as a nonce from middleware.ts",
	}}
}

// importedSource reads the relative modules next.config imports, one level
// deep, so a helper's header names can be seen.
func (rc *ruleCtx) importedSource() string {
	var sb strings.Builder
	for _, m := range configRelativeImport.FindAllStringSubmatch(rc.src, -1) {
		base := filepath.Join(rc.dir, filepath.FromSlash(m[1]))
		for _, cand := range []string{base, base + ".ts", base + ".mts", base + ".js", base + ".mjs", base + ".cjs", filepath.Join(base, "index.ts"), filepath.Join(base, "index.js")} {
			if !pluginapi.WithinRoot(rc.dir, cand) {
				continue
			}
			info, err := os.Stat(cand)
			if err != nil || info.IsDir() || info.Size() > 256<<10 {
				continue
			}
			if data, err := os.ReadFile(cand); err == nil {
				sb.Write(data)
				sb.WriteByte('\n')
				break
			}
		}
	}
	return sb.String()
}

// duplicateRuleWarnings reports identical rules in the translated output.
func duplicateRuleWarnings(d nextConfigData) []string {
	var out []string
	count := func(kind string, rules []any, name func(any) string) {
		seen := map[string]int{}
		var order []string
		for _, r := range rules {
			b, _ := json.Marshal(r)
			k := string(b)
			if seen[k] == 0 {
				order = append(order, k)
			}
			seen[k]++
		}
		for _, k := range order {
			if seen[k] > 1 {
				var r any
				_ = json.Unmarshal([]byte(k), &r)
				out = append(out, fmt.Sprintf("next.config %s contains %d identical rules for %s; the duplicates are redundant (check whether they were guarded by conditions)", kind, seen[k], name(r)))
			}
		}
	}
	src := func(r any) string {
		if m, ok := r.(map[string]any); ok {
			return fmt.Sprintf("%q", m["source"])
		}
		return "?"
	}
	var rs []any
	for _, r := range d.Redirects {
		rs = append(rs, r)
	}
	count("redirects", rs, src)
	rs = nil
	for _, r := range d.Rewrites {
		rs = append(rs, r)
	}
	count("rewrites", rs, src)
	rs = nil
	for _, r := range d.Headers {
		rs = append(rs, r)
	}
	count("headers", rs, src)
	return out
}

// imageManualItem turns the image warnings that mean lost behavior into
// manual items. The rest (AVIF, qualities) stay warnings.
func imageManualItem(file, w string) (ManualItem, bool) {
	switch {
	case strings.Contains(w, "remotePatterns"), strings.Contains(w, "images.domains"):
		return ManualItem{
			Source: file,
			Reason: w + "; remote images from those hosts will fail the build (KR8802)",
			Hint:   "list the hosts in krate.config.ts images.remotePatterns (hostname supports *.example.com and **.example.com); krate.config.ts can read process.env, so an env-built list can be rebuilt there",
		}, true
	case strings.Contains(w, "loader"):
		return ManualItem{
			Source: file,
			Reason: w,
			Hint:   "Krate has no custom image loader hook; set images.unoptimized to pass images through, or serve the final URLs yourself",
		}, true
	}
	return ManualItem{}, false
}

type keyNote struct {
	manual bool
	reason string
	hint   string
}

// otherKeyNotes covers next.config keys the migrator does not translate.
var otherKeyNotes = map[string]keyNote{
	"transpilePackages":           {true, "transpilePackages is not translated", "Krate has no transpilePackages key; run krate build and check that each listed package resolves, and report packages that do not"},
	"serverExternalPackages":      {true, "serverExternalPackages is not translated", "the value is computed; set serverExternalPackages in krate.config.ts to the same package names (see the Config Reference)"},
	"outputFileTracingIncludes":   {true, "outputFileTracingIncludes is not translated", "the value is computed or leaves the project; set outputFileTracingIncludes in krate.config.ts with project-relative globs (see the Config Reference)"},
	"outputFileTracingExcludes":   {true, "outputFileTracingExcludes is not translated", "no Krate equivalent yet"},
	"outputFileTracingRoot":       {true, "outputFileTracingRoot is not translated", "no Krate equivalent yet; a Krate project builds from its own root"},
	"i18n":                        {true, "i18n routing is not translated", "Krate's i18n key is docs-scoped; for app pages use a [locale] dynamic segment (see Internationalization)"},
	"trailingSlash":               {true, "trailingSlash is not translated", "set deploy.options.trailingSlash in krate.config.ts for static hosts (see Deploy in the config reference)"},
	"assetPrefix":                 {true, "assetPrefix is not translated", "no Krate equivalent yet; basePath serves the site under a sub-path"},
	"distDir":                     {true, "distDir is not translated", "set outDir in krate.config.ts (default dist)"},
	"webpack":                     {true, "the webpack hook is not translated", "Krate has no webpack hook; custom build steps go in a plugin (see Plugins)"},
	"turbopack":                   {true, "turbopack settings are not translated", "Krate has no turbopack settings; custom build steps go in a plugin (see Plugins)"},
	"pageExtensions":              {true, "pageExtensions is not translated", "no Krate equivalent; rename pages that use a custom extension"},
	"cacheComponents":             {true, "cacheComponents is not translated", "see Caching and Revalidation for the Krate equivalents; \"use cache\" has none"},
	"reactStrictMode":             {false, "reactStrictMode has no effect in Krate", ""},
	"poweredByHeader":             {false, "poweredByHeader is not translated", ""},
	"compress":                    {false, "compress is not used; see Precompression in the config reference", ""},
	"generateEtags":               {false, "generateEtags is not used", ""},
	"eslint":                      {false, "eslint settings are not used; run your linter separately", ""},
	"typescript":                  {false, "typescript settings are not translated", ""},
	"devIndicators":               {false, "devIndicators is not used", ""},
	"logging":                     {false, "logging is not used", ""},
	"productionBrowserSourceMaps": {false, "productionBrowserSourceMaps is not translated; set sourcemap in krate.config.ts", ""},
	"allowedDevOrigins":           {false, "allowedDevOrigins is not used; see devServer in the config reference", ""},
	"skipTrailingSlashRedirect":   {false, "skipTrailingSlashRedirect is not used", ""},
	"skipMiddlewareUrlNormalize":  {false, "skipMiddlewareUrlNormalize is not used", ""},
	"onDemandEntries":             {false, "onDemandEntries is not used", ""},
	"sassOptions":                 {false, "sassOptions is not translated", ""},
	"compiler":                    {true, "compiler options (styled-components, removeConsole, ...) are not translated", "no Krate equivalent; remove console calls and CSS-in-JS transforms in the source"},
	"modularizeImports":           {false, "modularizeImports is not used", ""},
	"optimizePackageImports":      {false, "optimizePackageImports is not used", ""},
	"crossOrigin":                 {false, "crossOrigin is not translated; see the cors key in the config reference", ""},
	"httpAgentOptions":            {false, "httpAgentOptions is not used", ""},
	"staticPageGenerationTimeout": {false, "staticPageGenerationTimeout is not used", ""},
	"generateBuildId":             {false, "generateBuildId is not translated", ""},
	"expireTime":                  {false, "expireTime is not translated; ISR windows are set per page with revalidate", ""},
}

// translateOtherKeys reports every top-level key the migrator does not
// translate, with a key-specific hint.
func translateOtherKeys(file string, obj *ast.ObjectExpr, notes *nextConfigNotes, skip map[string]bool) {
	handled := map[string]bool{"basePath": true, "redirects": true, "rewrites": true, "headers": true, "images": true, "env": true}
	for k := range skip {
		handled[k] = true
	}
	var keys []string
	for _, p := range obj.Properties {
		if p == nil || p.Spread || p.Computed || handled[p.Key] {
			continue
		}
		keys = append(keys, p.Key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v := prop(obj, key)
		switch key {
		case "output":
			notes.Manual = append(notes.Manual, outputNote(file, v))
			continue
		case "experimental":
			experimentalNotes(file, v, notes)
			continue
		}
		n, known := otherKeyNotes[key]
		if !known || n.reason == "" {
			notes.Manual = append(notes.Manual, ManualItem{
				Source: file,
				Reason: "next.config key " + key + " was not translated",
				Hint:   "Krate has no mapping for this key; check whether the behavior is needed (see the Config Reference) and set it in krate.config.ts by hand",
			})
			continue
		}
		if n.manual {
			notes.Manual = append(notes.Manual, ManualItem{Source: file, Reason: n.reason, Hint: n.hint})
		} else {
			notes.Warnings = append(notes.Warnings, "next.config "+n.reason)
		}
	}
}

func outputNote(file string, v ast.Expr) ManualItem {
	mode, _ := stringValue(v)
	switch mode {
	case "standalone":
		return ManualItem{Source: file, Reason: "output: 'standalone' is not translated", Hint: "use krate deploy --target node (dist/.krate/server holds server.mjs plus a minimal node_modules for external packages) or --target docker for a self-hosted server (see the Self-hosting guide)"}
	case "export":
		return ManualItem{Source: file, Reason: "output: 'export' is not translated", Hint: "set output: \"static\" in krate.config.ts (see Output mode in the config reference)"}
	}
	return ManualItem{Source: file, Reason: "next.config output is not a static 'standalone' or 'export' string and was not translated", Hint: "use krate deploy --target node/docker for a server, or output: \"static\" in krate.config.ts for a static site"}
}

func experimentalNotes(file string, v ast.Expr, notes *nextConfigNotes) {
	eo, ok := v.(*ast.ObjectExpr)
	if !ok {
		notes.Manual = append(notes.Manual, ManualItem{Source: file, Reason: "next.config experimental is not a static object and was not translated", Hint: "check each experimental flag against the Krate docs"})
		return
	}
	for _, p := range eo.Properties {
		if p == nil || p.Spread || p.Computed {
			continue
		}
		switch p.Key {
		case "authInterrupts":
			// forbidden() and unauthorized() always work in Krate.
		case "ppr":
			notes.Manual = append(notes.Manual, ManualItem{Source: file, Reason: "experimental.ppr is not translated", Hint: "set ppr in krate.config.ts (see Partial prerendering in the config reference)"})
		case "serverComponentsExternalPackages":
			notes.Manual = append(notes.Manual, ManualItem{Source: file, Reason: "experimental.serverComponentsExternalPackages is not translated", Hint: otherKeyNotes["serverExternalPackages"].hint})
		case "outputFileTracingIncludes", "outputFileTracingExcludes":
			notes.Manual = append(notes.Manual, ManualItem{Source: file, Reason: "experimental." + p.Key + " is not translated", Hint: otherKeyNotes[p.Key].hint})
		default:
			notes.Warnings = append(notes.Warnings, "next.config experimental."+p.Key+" is not translated; Krate has no equivalent flag")
		}
	}
}
