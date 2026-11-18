package migrate

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// A next-auth type augmentation declares extra session fields on next-auth's
// own interfaces. @krate/auth cannot merge a field into Session.user directly
// (its built-in fields would clash with a narrower redeclaration), so it
// exposes SessionUserExtensions and UserExtensions that replace built-in
// fields of the same name.

var (
	authDeclModule = regexp.MustCompile(`declare\s+module\s*["']([^"']+)["']\s*\{`)
	authDeclImport = regexp.MustCompile(`(?m)^[ \t]*import\s[^;]*?["']([^"']+)["'][ \t]*;?[ \t]*\r?\n?|^[ \t]*import\s*["']([^"']+)["'][ \t]*;?[ \t]*\r?\n?`)
	authDeclIface  = regexp.MustCompile(`\binterface\s+(\w+)\s*(extends\s+[^{]+)?\{`)
	authDeclUser   = regexp.MustCompile(`^user\s*:\s*`)
	authDeclDefUse = regexp.MustCompile(`^\s*&\s*DefaultSession\s*\[\s*["']user["']\s*\]\s*$`)
	authDeclIdent  = regexp.MustCompile(`[\w$]+`)

	authDeclEmptyExport = regexp.MustCompile(`export\s*\{\s*\}\s*;?`)
)

// authDeclarationPath is where a converted augmentation is written.
const authDeclarationPath = "src/types/krate-auth.d.ts"

// maskComments blanks comments (keeping newlines) so offsets stay valid.
func maskComments(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '/':
			for ; i < len(b) && b[i] != '\n'; i++ {
				b[i] = ' '
			}
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			j := i + 2
			for j+1 < len(b) && !(b[j] == '*' && b[j+1] == '/') {
				j++
			}
			end := j + 2
			if end > len(b) {
				end = len(b)
			}
			for k := i; k < end; k++ {
				if b[k] != '\n' {
					b[k] = ' '
				}
			}
			i = end - 1
		case b[i] == '\'' || b[i] == '"' || b[i] == '`':
			q := b[i]
			for i++; i < len(b) && b[i] != q; i++ {
				if b[i] == '\\' {
					i++
				}
			}
		}
	}
	return string(b)
}

// matchBrace returns the index of the brace closing the one at open in masked
// text, or -1.
func matchBrace(masked string, open int) int {
	depth := 0
	for i := open; i < len(masked); i++ {
		switch masked[i] {
		case '\'', '"', '`':
			q := masked[i]
			for i++; i < len(masked) && masked[i] != q; i++ {
				if masked[i] == '\\' {
					i++
				}
			}
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

// dedentFields turns the inside of a braced body into field lines without the
// common indent. Blank lines are kept as "".
func dedentFields(body string) []string {
	lines := strings.Split(strings.Trim(strings.ReplaceAll(body, "\r\n", "\n"), "\n"), "\n")
	indent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " \t"))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.TrimRight(l[indent:], " \t"))
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// portAuthDeclaration converts a next-auth augmentation file. It returns the
// new file text, or a reason it cannot (the caller keeps a manual item).
func portAuthDeclaration(src string) (string, string) {
	masked := maskComments(src)
	mods := authDeclModule.FindAllStringSubmatchIndex(masked, -1)
	if len(mods) == 0 {
		return "", "it has no declare module block"
	}
	var sessionFields, userFields []string
	hasSession, hasUser := false, false
	rest := []byte(src)
	for _, m := range mods {
		if name := masked[m[2]:m[3]]; name != "next-auth" {
			return "", "it also augments " + name + ", which has no @krate/auth counterpart (the session token accepts extra claims without a declaration)"
		}
		open := m[1] - 1
		end := matchBrace(masked, open)
		if end < 0 {
			return "", "its braces do not balance"
		}
		for i := m[0]; i <= end; i++ {
			if rest[i] != '\n' {
				rest[i] = ' '
			}
		}
		for pos := open + 1; pos < end; {
			loc := authDeclIface.FindStringSubmatchIndex(masked[pos:end])
			if loc == nil {
				if strings.TrimSpace(masked[pos:end]) != "" {
					return "", "it has a statement other than an interface inside declare module"
				}
				break
			}
			if strings.TrimSpace(masked[pos:pos+loc[0]]) != "" {
				return "", "it has a statement other than an interface inside declare module"
			}
			iname := masked[pos+loc[2] : pos+loc[3]]
			if loc[4] >= 0 {
				return "", "interface " + iname + " extends another type"
			}
			iopen := pos + loc[1] - 1
			iend := matchBrace(masked, iopen)
			if iend < 0 || iend > end {
				return "", "its braces do not balance"
			}
			switch iname {
			case "Session":
				fields, why := sessionUserFields(src, masked, iopen+1, iend)
				if why != "" {
					return "", why
				}
				hasSession = true
				sessionFields = append(sessionFields, fields...)
			case "User":
				if strings.Contains(masked[iopen:iend], "Default") {
					return "", "interface User refers to a default type"
				}
				hasUser = true
				userFields = append(userFields, dedentFields(src[iopen+1:iend])...)
			default:
				return "", "interface " + iname + " has no @krate/auth counterpart"
			}
			pos = iend + 1
		}
	}
	if !hasSession && !hasUser {
		return "", "it declares nothing to convert"
	}

	var imports strings.Builder
	left := string(rest)
	maskedLeft := maskComments(left)
	for _, m := range authDeclImport.FindAllStringSubmatchIndex(maskedLeft, -1) {
		lo, hi := m[2], m[3]
		if lo < 0 {
			lo, hi = m[4], m[5]
		}
		spec := maskedLeft[lo:hi]
		whole := strings.TrimRight(left[m[0]:m[1]], "\r\n")
		switch {
		case spec == "next-auth":
			clause := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(whole), "import"))
			if i := strings.LastIndex(clause, "from"); i >= 0 {
				for _, n := range authDeclIdent.FindAllString(clause[:i], -1) {
					if n != "type" && n != "DefaultSession" && n != "DefaultUser" {
						return "", "it imports " + n + " from next-auth"
					}
				}
			}
		case strings.HasPrefix(spec, "next-auth/") || strings.HasPrefix(spec, "@auth/core"):
			return "", "it imports " + spec
		default:
			imports.WriteString(whole + "\n")
		}
		for i := m[0]; i < m[1]; i++ {
			if left[i] != '\n' {
				maskedLeft = maskedLeft[:i] + " " + maskedLeft[i+1:]
			}
		}
	}
	maskedLeft = authDeclEmptyExport.ReplaceAllString(maskedLeft, "")
	if strings.TrimSpace(maskedLeft) != "" {
		return "", "it declares more than the next-auth augmentation"
	}

	var b strings.Builder
	if imports.Len() > 0 {
		b.WriteString(imports.String() + "\n")
	}
	b.WriteString("declare module '@krate/auth' {\n")
	write := func(name string, fields []string) {
		fmt.Fprintf(&b, "  interface %s {\n", name)
		for _, f := range fields {
			if f == "" {
				b.WriteString("\n")
			} else {
				b.WriteString("    " + f + "\n")
			}
		}
		b.WriteString("  }\n")
	}
	if hasSession {
		write("SessionUserExtensions", sessionFields)
	}
	if hasUser {
		if hasSession {
			b.WriteString("\n")
		}
		write("UserExtensions", userFields)
	}
	b.WriteString("}\n")
	if imports.Len() == 0 {
		b.WriteString("\nexport {};\n")
	}
	return b.String(), ""
}

// sessionUserFields reads `user: { ... } & DefaultSession['user']` out of the
// body of interface Session (offsets into src and its masked copy).
func sessionUserFields(src, masked string, from, to int) ([]string, string) {
	body := masked[from:to]
	lead := len(body) - len(strings.TrimLeft(body, " \t\r\n"))
	m := authDeclUser.FindStringIndex(body[lead:])
	if m == nil {
		return nil, "interface Session declares something other than user"
	}
	start := from + lead + m[1]
	for start < to && (masked[start] == ' ' || masked[start] == '\t' || masked[start] == '\n' || masked[start] == '\r') {
		start++
	}
	if start >= to || masked[start] != '{' {
		return nil, "Session.user is not an object type"
	}
	close := matchBrace(masked, start)
	if close < 0 || close > to {
		return nil, "its braces do not balance"
	}
	tail := strings.TrimRight(strings.TrimSpace(masked[close+1:to]), ";")
	if tail != "" && !authDeclDefUse.MatchString(tail) {
		return nil, "Session.user combines the object with " + strings.TrimSpace(tail)
	}
	return dedentFields(src[start+1 : close]), ""
}

// planAuthDeclaration plans the converted augmentation at authDeclarationPath
// and moves the original aside, or leaves a manual item that quotes it.
func planAuthDeclaration(rep *Report, rel string, data []byte) {
	out, why := portAuthDeclaration(string(data))
	if why != "" {
		quote := strings.Join(strings.Fields(string(data)), " ")
		if len(quote) > 600 {
			quote = quote[:600] + "..."
		}
		rep.Manual = append(rep.Manual, ManualItem{
			Source: rel,
			Reason: "type declarations that augment next-auth were not converted (" + why + "): " + quote,
			Hint:   "declare the extra fields on SessionUserExtensions in `declare module '@krate/auth'`, or delete the file (" + authDocs + ")",
		})
		return
	}
	target := authDeclarationPath
	if dir := path.Dir(rel); dir != "." && dir != "src/types" {
		target = dir + "/krate-auth.d.ts"
	}
	rep.Files = append(rep.Files, PlannedFile{
		Kind: KindModule, Source: rel, Target: target, Content: []byte(out),
		Rewrites: []string{"next-auth augmentation became @krate/auth SessionUserExtensions/UserExtensions (" + authDocs + ")"},
	})
	rep.MoveAside = append(rep.MoveAside, rel)
}
