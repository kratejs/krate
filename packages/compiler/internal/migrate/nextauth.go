package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// next-auth (Auth.js) v5 imports next/server, next/headers and next/navigation,
// so it cannot run under Krate. The migrator ports the config to @krate/auth
// when its shape is supported and reports everything else as a manual item.

const authDocs = "https://krate.js.org/docs/features/authentication/"

var nextAuthSpecifier = regexp.MustCompile(`["']next-auth(?:/[^"']*)?["']`)

// nextAuthImport matches one import declaration from next-auth or a subpath.
var nextAuthImport = regexp.MustCompile(`(?ms)^([ \t]*import\s+(type\s+)?([^;'"]*?)\s*from\s*)(["'])(next-auth(?:/[^"']*)?)["'][ \t]*;?[ \t]*\r?\n?`)

// authProviderExports maps Auth.js provider module names to the @krate/auth
// export. Every other provider's name is its export name.
var authProviderExports = map[string]string{
	"42-school":               "fortyTwoSchool",
	"azure-ad-b2c":            "azureAdB2c",
	"duende-identity-server6": "duendeIdentityServer6",
	"microsoft-entra-id":      "microsoftEntraId",
}

// authBuiltinProviders are the provider modules that have a @krate/auth export
// of the same name (see the catalog in packages/auth/src/providers/catalog.ts).
var authBuiltinProviders = strings.Fields(`apple asgardeo atlassian auth0 authentik bitbucket box cognito coinbase
descope discord dribbble dropbox facebook fusionauth github gitlab google hubspot huggingface kakao keycloak kinde
line linkedin logto mastodon mattermost naver netlify notion okta onelogin orcid osu patreon pinterest reddit roblox
salesforce slack spotify strava twitch twitter webex wikimedia workos yandex zitadel zoho zoom
credentials resend passkey`)

// authTypeExports maps names imported from next-auth to @krate/auth types.
var authTypeExports = map[string]string{
	"NextAuthConfig": "AuthOptions",
	"Session":        "Session",
	"User":           "User",
	"Account":        "Account",
}

// authClientExports are the next-auth/react names @krate/auth/client has.
var authClientExports = map[string]bool{"signIn": true, "signOut": true, "getSession": true, "getCsrfToken": true, "getProviders": true}

func authProviderExport(slug string) (string, bool) {
	if name, ok := authProviderExports[slug]; ok {
		return name, true
	}
	for _, id := range authBuiltinProviders {
		if id == slug {
			return id, true
		}
	}
	return "", false
}

// authConfigKeys are the NextAuth config keys @krate/auth reads.
var authConfigKeys = map[string]bool{
	"secret": true, "providers": true, "adapter": true, "basePath": true, "trustHost": true, "debug": true,
	"logger": true, "session": true, "jwt": true, "pages": true, "callbacks": true, "events": true, "cookies": true,
}

var authCallbackKeys = map[string]bool{"signIn": true, "redirect": true, "jwt": true, "session": true, "authorized": true}

var authEventKeys = map[string]bool{"signIn": true, "signOut": true, "createUser": true, "updateUser": true, "linkAccount": true, "session": true}

var authCookieOverrides = map[string]bool{"sessionToken": true, "callbackUrl": true, "csrfToken": true, "pkceCodeVerifier": true, "state": true, "nonce": true, "webauthnChallenge": true}

// authConfigAdvice explains the Auth.js keys @krate/auth has no equivalent for.
var authConfigAdvice = map[string]string{
	"theme":            "the built-in sign-in page is not themeable; build your own page at pages.signIn",
	"experimental":     "Auth.js experimental flags have no equivalent",
	"skipCSRFCheck":    "Krate always checks the CSRF token on the POST endpoints",
	"redirectProxyUrl": "the Auth.js redirect proxy is not supported; set AUTH_URL per deployment",
	"enableWebAuthn":   "enable passkeys with the passkey() provider (see the Passkeys section of the authentication docs)",
	"useSecureCookies": "set cookies.secure instead",
	"unstable_update":  "use the client updateSession() or POST /api/auth/session",
}

// migrateNextAuth ports next-auth v5 to @krate/auth in every planned file that
// imports it, plans the project files the routes did not reach, and lists what it
// cannot port. It runs before unchanged modules are pruned.
func migrateNextAuth(rep *Report, root string) {
	pkgHas := packageHasNextAuth(root)
	handled := map[string]bool{}
	found := false
	ported := false
	for i := range rep.Files {
		f := &rep.Files[i]
		if len(f.Content) == 0 || f.Kind == KindPublic || f.Kind == KindPackage || f.Kind == KindConfig {
			continue
		}
		src := string(f.Content)
		if !nextAuthSpecifier.MatchString(src) && !authWrapperCall.MatchString(src) {
			continue
		}
		label := f.Source
		if label == "" {
			label = f.Target
		}
		if f.Source != "" {
			handled[filepath.ToSlash(f.Source)] = true
		}
		out, res := portNextAuthSource(src, label)
		if nextAuthSpecifier.MatchString(src) {
			found = true
		}
		ported = ported || res.ported
		if out != src {
			f.Content = []byte(out)
		}
		f.Rewrites = append(f.Rewrites, res.rewrites...)
		rep.Manual = append(rep.Manual, res.manual...)
		rep.Warnings = append(rep.Warnings, res.warnings...)
	}
	if pkgHas || found {
		for _, rel := range findNextAuthFiles(root, rep, handled) {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				continue
			}
			found = true
			if strings.HasSuffix(rel, ".d.ts") {
				rep.Manual = append(rep.Manual, ManualItem{
					Source: rel,
					Reason: "type declarations that augment next-auth are not carried over",
					Hint:   "delete the file, or declare the extra session fields on your own type; @krate/auth Session.user accepts extra fields (" + authDocs + ")",
				})
				continue
			}
			src := string(data)
			out, res := portNextAuthSource(src, rel)
			ported = ported || res.ported
			rep.Manual = append(rep.Manual, res.manual...)
			rep.Warnings = append(rep.Warnings, res.warnings...)
			if out == src {
				continue
			}
			rep.Files = append(rep.Files, PlannedFile{Kind: KindModule, Source: rel, Target: rel, Content: []byte(out), orig: data, Rewrites: res.rewrites})
		}
	}
	if pkgHas && !ported && !found {
		rep.Manual = append(rep.Manual, ManualItem{
			Source: "package.json",
			Reason: "next-auth is a dependency but no NextAuth() config was found to port; next-auth imports next/server, next/headers and next/navigation and fails at run time under Krate",
			Hint:   "move sign-in to @krate/auth (" + authDocs + ")",
		})
	}
	if pkgHas {
		rep.Manual = append(rep.Manual, ManualItem{
			Source: "package.json",
			Reason: "next-auth is still listed as a dependency",
			Hint:   "install @krate/auth (--update-package adds it) and remove next-auth once the app builds; @auth/drizzle-adapter keeps working with @krate/auth",
		})
	}
	// Anything that still imports next-auth would fail at run time.
	for _, f := range rep.Files {
		if len(f.Content) == 0 || f.Kind == KindPublic || f.Kind == KindPackage {
			continue
		}
		for _, line := range remainingNextAuthImports(string(f.Content)) {
			label := f.Source
			if label == "" {
				label = f.Target
			}
			rep.Manual = append(rep.Manual, ManualItem{
				Source: label,
				Reason: "still imports " + line + ", which needs Next.js and fails at run time",
				Hint:   "port it to @krate/auth (" + authDocs + ")",
			})
		}
	}
}

// remainingNextAuthImports lists the next-auth specifiers still imported as
// values (type-only imports are erased by the build).
func remainingNextAuthImports(src string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range nextAuthImport.FindAllStringSubmatch(src, -1) {
		if m[2] != "" || seen[m[5]] {
			continue
		}
		if clause := strings.TrimSpace(m[3]); strings.HasPrefix(clause, "{") && onlyTypeNames(clause) {
			continue
		}
		seen[m[5]] = true
		out = append(out, m[5])
	}
	return out
}

func onlyTypeNames(clause string) bool {
	body := strings.TrimSpace(strings.Trim(strings.TrimSpace(clause), "{}"))
	if body == "" {
		return true
	}
	for _, part := range strings.Split(body, ",") {
		part = strings.TrimSpace(part)
		if part != "" && !strings.HasPrefix(part, "type ") {
			return false
		}
	}
	return true
}

func packageHasNextAuth(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return false
	}
	var pkg map[string]any
	if json.Unmarshal(data, &pkg) != nil {
		return false
	}
	return hasDependency(pkg, "next-auth")
}

// findNextAuthFiles lists project source files outside the plan that mention
// next-auth. The walk is bounded and skips build output and the Next route trees
// (those are planned file by file).
func findNextAuthFiles(root string, rep *Report, handled map[string]bool) []string {
	moved := map[string]bool{}
	for _, m := range rep.MoveAside {
		moved[filepath.ToSlash(m)] = true
	}
	planned := map[string]bool{}
	for _, f := range rep.Files {
		if f.Source != "" {
			planned[filepath.ToSlash(f.Source)] = true
		}
	}
	var out []string
	count := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (skipModulePath(rel) || skipDir(d.Name()) || moved[rel]) {
				return filepath.SkipDir
			}
			return nil
		}
		if count++; count > 5000 {
			return filepath.SkipAll
		}
		if planned[rel] || handled[rel] || isTestSource(rel) {
			return nil
		}
		if !isModuleSource(rel) && !strings.HasSuffix(rel, ".d.ts") {
			return nil
		}
		if data, err := os.ReadFile(path); err == nil && nextAuthSpecifier.Match(data) {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

type nextAuthResult struct {
	rewrites []string
	manual   []ManualItem
	warnings []string
	ported   bool
}

// authWrapperCall matches auth((req) => ...) and auth(req => ...), the Auth.js
// middleware/route wrapper, and `export { auth as middleware }`.
var authWrapperCall = regexp.MustCompile(`(?m)(?:\bauth\s*\(\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_$][\w$]*)\s*=>|export\s*\{[^}]*\bauth\s+as\s+(?:middleware|proxy|default)\b|export\s+default\s+auth\b)`)

var authImportFromAuthModule = regexp.MustCompile(`(?s)import\s*\{[^}]*\bauth\b[^}]*\}\s*from\s*["'](?:[^"']*/)?auth(?:\.[a-z]+)?["']`)

// portNextAuthSource rewrites one file. It returns src unchanged when nothing
// in it needs porting.
func portNextAuthSource(src, label string) (string, nextAuthResult) {
	var res nextAuthResult
	quote := "'"
	if strings.Contains(src, `from "`) && !strings.Contains(src, `from '`) {
		quote = `"`
	}
	manual := func(reason, hint string) {
		res.manual = append(res.manual, ManualItem{Source: label, Reason: reason, Hint: hint})
	}

	if authImportFromAuthModule.MatchString(src) && authWrapperCall.MatchString(src) {
		manual("auth(handler) wraps a middleware or route handler (the Auth.js wrapper form); @krate/auth has no wrapper",
			"call `await auth()` (or `await auth(request)`) inside the handler, or export the instance and use `defineAuth(...).middleware` for route protection ("+authDocs+")")
	}

	var krateNames []string // names for the combined @krate/auth import
	addName := func(n string) {
		for _, have := range krateNames {
			if have == n {
				return
			}
		}
		krateNames = append(krateNames, n)
	}
	var typeNames []string
	nextAuthLocal := ""
	const placeholder = "\x00KRATE_AUTH_IMPORT\x00"
	placed := false

	out := nextAuthImport.ReplaceAllStringFunc(src, func(match string) string {
		sub := nextAuthImport.FindStringSubmatch(match)
		typeOnly, clause, module := sub[2] != "", strings.TrimSpace(sub[3]), sub[5]
		def, named, ns := parseImportClause(clause)
		switch {
		case module == "next-auth":
			prefix := ""
			if def != "" && !typeOnly {
				nextAuthLocal = def
				if !placed {
					placed = true
					prefix = placeholder
				}
			}
			var keep []string
			for _, n := range named {
				if k, ok := authTypeExports[n.imported]; ok && (typeOnly || n.typ) {
					if k == n.local {
						typeNames = append(typeNames, k)
					} else {
						typeNames = append(typeNames, k+" as "+n.local)
					}
					continue
				}
				keep = append(keep, n.text)
				if typeOnly || n.typ {
					res.warnings = append(res.warnings, label+": type "+n.imported+" is still imported from next-auth; the import is erased at build, but typecheck needs next-auth installed")
				} else {
					manual("imports "+n.imported+" from next-auth, which has no @krate/auth equivalent", "see "+authDocs)
				}
			}
			line := ""
			if len(keep) > 0 {
				line = "import " + sub[2] + "{ " + strings.Join(keep, ", ") + " } from " + sub[4] + "next-auth" + sub[4] + ";\n"
			}
			return prefix + line
		case strings.HasPrefix(module, "next-auth/providers/"):
			slug := strings.TrimPrefix(module, "next-auth/providers/")
			export, ok := authProviderExport(slug)
			if !ok || def == "" {
				manual("provider next-auth/providers/"+slug+" has no @krate/auth equivalent",
					"use genericOAuth({ id, authorization, token, userinfo, profile }) from @krate/auth ("+authDocs+")")
				return match
			}
			if export == def {
				addName(export)
			} else {
				addName(export + " as " + def)
			}
			if !placed {
				placed = true
				return placeholder
			}
			return ""
		case module == "next-auth/react":
			if ns != "" {
				manual("`import * as "+ns+"` from next-auth/react is not rewritten", "import signIn/signOut from @krate/auth/client")
				return match
			}
			var keep []string
			for _, n := range named {
				if authClientExports[n.imported] {
					keep = append(keep, n.text)
					continue
				}
				manual(n.imported+" from next-auth/react has no @krate/auth/client equivalent",
					"read the session with getSession() from @krate/auth/client, or in a server component with `await auth()` ("+authDocs+")")
			}
			if len(keep) == 0 {
				return ""
			}
			res.rewrites = append(res.rewrites, "rewrote next-auth/react to @krate/auth/client")
			return "import " + sub[2] + "{ " + strings.Join(keep, ", ") + " } from " + sub[4] + "@krate/auth/client" + sub[4] + ";\n"
		case module == "next-auth/jwt":
			if typeOnly || onlyTypeNames(clause) {
				return match
			}
			manual("next-auth/jwt (getToken/encode/decode) is not available; @krate/auth sessions are not Auth.js tokens",
				"read the session with `await auth(request)` instead ("+authDocs+")")
			return match
		default:
			manual("import from "+module+" is not ported", "see "+authDocs)
			return match
		}
	})
	if out != src {
		res.rewrites = append(res.rewrites, "rewrote next-auth imports to @krate/auth")
	}

	if nextAuthLocal != "" {
		var ok bool
		out, ok = portNextAuthCall(out, nextAuthLocal, quote, label, &res)
		if ok {
			addName("defineAuth")
			res.ported = true
		} else {
			manual("NextAuth("+") could not be found to port", "replace it with defineAuth(...) from @krate/auth ("+authDocs+")")
		}
	}

	if len(krateNames) > 0 || placed {
		line := ""
		if len(krateNames) > 0 {
			sort.SliceStable(krateNames, func(i, j int) bool { return krateNames[i] == "defineAuth" && krateNames[j] != "defineAuth" })
			line = "import { " + strings.Join(krateNames, ", ") + " } from " + quote + "@krate/auth" + quote + ";\n"
		}
		if placed {
			out = strings.Replace(out, placeholder, line, 1)
		} else {
			out = line + out
		}
	}
	if len(typeNames) > 0 {
		out = "import type { " + strings.Join(typeNames, ", ") + " } from " + quote + "@krate/auth" + quote + ";\n" + out
	}
	return out, res
}

type importName struct {
	imported, local string
	typ             bool
	text            string
}

// parseImportClause splits `Def, { a, type B, c as d }` or `* as ns`.
func parseImportClause(clause string) (def string, named []importName, ns string) {
	clause = strings.TrimSpace(clause)
	if strings.HasPrefix(clause, "* as ") {
		return "", nil, strings.TrimSpace(strings.TrimPrefix(clause, "* as "))
	}
	if i := strings.Index(clause, "{"); i >= 0 {
		head := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(clause[:i]), ","))
		def = head
		body := clause[i+1:]
		if j := strings.Index(body, "}"); j >= 0 {
			body = body[:j]
		}
		for _, part := range strings.Split(body, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			n := importName{text: part}
			if strings.HasPrefix(part, "type ") {
				n.typ = true
				part = strings.TrimSpace(strings.TrimPrefix(part, "type "))
			}
			n.imported, n.local = part, part
			if k := strings.Index(part, " as "); k >= 0 {
				n.imported, n.local = strings.TrimSpace(part[:k]), strings.TrimSpace(part[k+4:])
			}
			named = append(named, n)
		}
		return def, named, ""
	}
	return strings.TrimSpace(strings.TrimSuffix(clause, ",")), nil, ""
}

// portNextAuthCall renames the NextAuth(...) call to defineAuth(...), adds
// cookies.prefix "authjs" so existing sessions keep working, and reports config
// keys @krate/auth does not read.
func portNextAuthCall(src, local, quote, label string, res *nextAuthResult) (string, bool) {
	callRe := regexp.MustCompile(`(^|[^\w$.])` + regexp.QuoteMeta(local) + `\s*\(`)
	loc := callRe.FindStringIndex(src)
	if loc == nil {
		return src, false
	}
	nameStart := loc[0] + len(callRe.FindStringSubmatch(src[loc[0]:])[1])
	open := loc[1] - 1
	closeIdx, ok := matchBracket(src, open)
	if !ok {
		return src, false
	}
	manual := func(reason, hint string) {
		res.manual = append(res.manual, ManualItem{Source: label, Reason: reason, Hint: hint})
	}

	argStart, argEnd := open+1, closeIdx
	arg := strings.TrimSpace(src[argStart:argEnd])
	objOpen, objClose := -1, -1
	switch {
	case strings.HasPrefix(arg, "{"):
		objOpen = argStart + strings.Index(src[argStart:argEnd], "{")
	default:
		if m := authConfigFunction.FindStringSubmatchIndex(src[argStart:argEnd]); m != nil {
			params := strings.TrimSpace(strings.Trim(strings.TrimSpace(src[argStart+m[2]:argStart+m[3]]), "()"))
			if params != "" {
				manual("the NextAuth config function takes the request, which Auth.js passes per request",
					"@krate/auth calls the config function once with no argument; read request data with headers() inside callbacks")
			}
			objOpen = argStart + m[1] - 1
		}
	}
	if objOpen >= 0 {
		if end, ok := matchBracket(src, objOpen); ok {
			objClose = end
		} else {
			objOpen = -1
		}
	}

	insert, insertAt := "", -1
	if objOpen < 0 {
		manual("the NextAuth config is not an inline object, so cookies.prefix was not added",
			"add `cookies: { prefix: \"authjs\" }` to the defineAuth options so existing Auth.js cookies and sessions keep working ("+authDocs+")")
	} else {
		keys := objectProps(src, objOpen, objClose)
		hasSpread := false
		for _, p := range keys {
			if p.spread {
				hasSpread = true
			}
		}
		if hasSpread {
			manual("the NextAuth config spreads another object, so it was not checked", "check its keys against "+authDocs+" and make sure cookies.prefix is \"authjs\"")
		}
		byKey := map[string]objProp{}
		for _, p := range keys {
			byKey[p.key] = p
			if p.spread || authConfigKeys[p.key] {
				continue
			}
			if advice, ok := authConfigAdvice[p.key]; ok && advice != "" {
				manual("NextAuth option "+p.key+" has no @krate/auth equivalent", advice)
			} else {
				manual("NextAuth option "+p.key+" is not read by @krate/auth", "see "+authDocs)
			}
		}
		checkNested := func(key string, allowed map[string]bool) {
			p, ok := byKey[key]
			if !ok || p.valueStart < 0 || p.valueStart >= len(src) || src[p.valueStart] != '{' {
				return
			}
			end, ok := matchBracket(src, p.valueStart)
			if !ok {
				return
			}
			for _, np := range objectProps(src, p.valueStart, end) {
				if !np.spread && !allowed[np.key] {
					manual("NextAuth "+key+"."+np.key+" is not supported by @krate/auth", "see "+authDocs)
				}
			}
		}
		checkNested("callbacks", authCallbackKeys)
		checkNested("events", authEventKeys)

		if c, ok := byKey["cookies"]; ok {
			handled := false
			if c.valueStart >= 0 && c.valueStart < len(src) && src[c.valueStart] == '{' {
				if end, ok := matchBracket(src, c.valueStart); ok {
					hasPrefix, override := false, ""
					for _, np := range objectProps(src, c.valueStart, end) {
						if np.key == "prefix" {
							hasPrefix = true
						}
						if authCookieOverrides[np.key] {
							override = np.key
						}
					}
					switch {
					case override != "":
						manual("NextAuth cookies."+override+" overrides a cookie name; @krate/auth only has cookies.prefix, secure, sameSite and domain",
							"if the names are the Auth.js defaults set cookies.prefix to \"authjs\"; otherwise rename the cookie in the other places that read it")
					case !hasPrefix:
						insert, insertAt = authPrefixInsert(src, c.valueStart, "prefix: "+quote+"authjs"+quote+","), c.valueStart+1
					}
					handled = true
				}
			}
			if !handled {
				manual("NextAuth cookies is not an inline object, so cookies.prefix was not added", "set cookies.prefix to \"authjs\" so existing Auth.js cookies keep working")
			}
		} else {
			insert, insertAt = authPrefixInsert(src, objOpen, "cookies: { prefix: "+quote+"authjs"+quote+" },"), objOpen+1
		}

		_, hasAdapter := byKey["adapter"]
		strategy := authSessionStrategy(src, byKey["session"])
		if strategy == "jwt" || (strategy == "" && !hasAdapter) {
			res.warnings = append(res.warnings, label+": JWT sessions: @krate/auth session tokens are not byte-compatible with Auth.js tokens, so users sign in again once")
		}
		if hasAdapter {
			res.warnings = append(res.warnings, label+": the adapter is unchanged; @auth/drizzle-adapter implements the @krate/auth Adapter interface, so existing tables and database sessions keep working")
		}
	}

	res.rewrites = append(res.rewrites, "NextAuth(...) became defineAuth(...) from @krate/auth; handlers, auth, signIn and signOut keep their names, and cookies.prefix \"authjs\" keeps the Auth.js cookie names")
	rest := src[nameStart+len(local):]
	if insertAt >= 0 {
		cut := insertAt - (nameStart + len(local))
		rest = rest[:cut] + insert + rest[cut:]
	}
	return src[:nameStart] + "defineAuth" + rest, true
}

// authConfigFunction matches the head of `() => ({`, `(req) => ({`, `async () => ({`.
// Submatch 1 is the parameter list.
var authConfigFunction = regexp.MustCompile(`^\s*(?:async\s+)?(\([^)]*\)|[A-Za-z_$][\w$]*)\s*=>\s*\(\s*\{`)

func authPrefixInsert(src string, open int, text string) string {
	// Keep the indentation of the first property when it starts on its own line.
	rest := src[open+1:]
	nl := strings.IndexByte(rest, '\n')
	if nl >= 0 && strings.TrimSpace(rest[:nl]) == "" {
		line := rest[nl+1:]
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		return "\n" + indent + text
	}
	return " " + text
}

func authSessionStrategy(src string, session objProp) string {
	if session.valueStart < 0 || session.valueStart >= len(src) || src[session.valueStart] != '{' {
		return ""
	}
	end, ok := matchBracket(src, session.valueStart)
	if !ok {
		return ""
	}
	for _, p := range objectProps(src, session.valueStart, end) {
		if p.key == "strategy" {
			v := strings.Trim(strings.TrimSpace(src[p.valueStart:p.valueEnd]), `'"`+"`")
			return v
		}
	}
	return ""
}

type objProp struct {
	key                  string
	spread               bool
	valueStart, valueEnd int
}

// objectProps lists the top-level properties of the object literal whose braces
// are at open and closeIdx.
func objectProps(src string, open, closeIdx int) []objProp {
	var props []objProp
	i := open + 1
	for i < closeIdx {
		i = skipSpace(src, i, closeIdx)
		if i >= closeIdx {
			break
		}
		if src[i] == ',' {
			i++
			continue
		}
		p := objProp{valueStart: -1}
		if strings.HasPrefix(src[i:closeIdx], "...") {
			p.spread = true
			p.valueStart = i + 3
			i = skipValue(src, i+3, closeIdx)
			p.valueEnd = i
			props = append(props, p)
			continue
		}
		if strings.HasPrefix(src[i:closeIdx], "async ") {
			i = skipSpace(src, i+6, closeIdx)
		}
		if c := src[i]; c == '\'' || c == '"' {
			end := skipString(src, i)
			p.key = src[i+1 : end-1]
			i = end
		} else if c == '[' {
			end, ok := matchBracket(src, i)
			if !ok {
				break
			}
			p.key = "[computed]"
			i = end + 1
		} else {
			j := i
			for j < closeIdx && (isIdentByte(src[j])) {
				j++
			}
			if j == i {
				i = skipValue(src, i, closeIdx)
				continue
			}
			p.key = src[i:j]
			i = j
		}
		i = skipSpace(src, i, closeIdx)
		switch {
		case i < closeIdx && src[i] == ':':
			p.valueStart = skipSpace(src, i+1, closeIdx)
			i = skipValue(src, p.valueStart, closeIdx)
			p.valueEnd = i
		case i < closeIdx && src[i] == '(':
			p.valueStart = i
			i = skipValue(src, i, closeIdx)
			p.valueEnd = i
		default:
			p.valueStart, p.valueEnd = -1, -1
		}
		props = append(props, p)
	}
	return props
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func skipSpace(src string, i, end int) int {
	for i < end {
		switch {
		case src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r':
			i++
		case strings.HasPrefix(src[i:end], "//"):
			for i < end && src[i] != '\n' {
				i++
			}
		case strings.HasPrefix(src[i:end], "/*"):
			if k := strings.Index(src[i+2:end], "*/"); k >= 0 {
				i += k + 4
			} else {
				return end
			}
		default:
			return i
		}
	}
	return i
}

// skipValue advances to the next top-level comma (or end).
func skipValue(src string, i, end int) int {
	for i < end {
		switch c := src[i]; {
		case c == ',':
			return i
		case c == '\'' || c == '"':
			i = skipString(src, i)
		case c == '`':
			i = skipTemplate(src, i)
		case c == '(' || c == '[' || c == '{':
			e, ok := matchBracket(src, i)
			if !ok {
				return end
			}
			i = e + 1
		case strings.HasPrefix(src[i:end], "//") || strings.HasPrefix(src[i:end], "/*"):
			i = skipSpace(src, i, end)
		default:
			i++
		}
	}
	return end
}

// skipString returns the index after the string literal starting at i.
func skipString(src string, i int) int {
	q := src[i]
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case q:
			return j + 1
		}
	}
	return len(src)
}

// skipTemplate returns the index after the template literal starting at i,
// including nested ${...} expressions.
func skipTemplate(src string, i int) int {
	for j := i + 1; j < len(src); j++ {
		switch {
		case src[j] == '\\':
			j++
		case src[j] == '`':
			return j + 1
		case src[j] == '$' && j+1 < len(src) && src[j+1] == '{':
			e, ok := matchBracket(src, j+1)
			if !ok {
				return len(src)
			}
			j = e
		}
	}
	return len(src)
}

// matchBracket returns the index of the bracket that closes the one at open,
// skipping strings, templates, comments and nested brackets.
func matchBracket(src string, open int) (int, bool) {
	if open < 0 || open >= len(src) {
		return 0, false
	}
	var want byte
	switch src[open] {
	case '(':
		want = ')'
	case '[':
		want = ']'
	case '{':
		want = '}'
	default:
		return 0, false
	}
	i := open + 1
	for i < len(src) {
		c := src[i]
		switch {
		case c == want:
			return i, true
		case c == '\'' || c == '"':
			i = skipString(src, i)
		case c == '`':
			i = skipTemplate(src, i)
		case c == '(' || c == '[' || c == '{':
			e, ok := matchBracket(src, i)
			if !ok {
				return 0, false
			}
			i = e + 1
		case strings.HasPrefix(src[i:], "//") || strings.HasPrefix(src[i:], "/*"):
			i = skipSpace(src, i, len(src))
		default:
			i++
		}
	}
	return 0, false
}
