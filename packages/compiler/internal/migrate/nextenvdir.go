package migrate

import (
	"path"
	"strings"
)

// staticEnvDir evaluates the first argument of a loadEnvConfig call. src starts
// just after the opening parenthesis. The result is a slash path relative to
// the project root (where next.config lives), "" for the root itself. ok is
// false when the argument is not statically resolvable.
func staticEnvDir(src string) (dir string, ok bool) {
	args, ok := splitCallArgs(src)
	if !ok || len(args) == 0 {
		return "", false
	}
	v, ok := evalPathExpr(args[0])
	if !ok || path.IsAbs(v) {
		return "", false
	}
	v = path.Clean(v)
	if v == "." {
		return "", true
	}
	return v, true
}

// splitCallArgs splits the text after "(" into its top-level arguments, up to
// the matching ")". Quotes and nested brackets are respected.
func splitCallArgs(src string) ([]string, bool) {
	var args []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				if a := strings.TrimSpace(src[start:i]); a != "" || len(args) > 0 {
					args = append(args, a)
				}
				return args, c == ')'
			}
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(src[start:i]))
				start = i + 1
			}
		}
	}
	return nil, false
}

// evalPathExpr evaluates the path expressions next.config files use: string
// literals, process.cwd(), __dirname, import.meta.dirname, path.resolve/join
// of those, path.dirname(...) and fileURLToPath(new URL(rel, import.meta.url)).
// The project root stands in for the config directory and the cwd.
func evalPathExpr(expr string) (string, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", false
	}
	if n := len(expr); n >= 2 && (expr[0] == '\'' || expr[0] == '"' || expr[0] == '`') && expr[n-1] == expr[0] {
		inner := expr[1 : n-1]
		if strings.ContainsAny(inner, "\\$"+string(expr[0])) {
			return "", false
		}
		return inner, true
	}
	switch expr {
	case "__dirname", "import.meta.dirname", "process.cwd()":
		return ".", true
	}
	open := strings.IndexByte(expr, '(')
	if open < 0 || !strings.HasSuffix(expr, ")") {
		return "", false
	}
	callee := strings.TrimSpace(expr[:open])
	args, ok := splitCallArgs(expr[open+1:])
	if !ok {
		return "", false
	}
	callee = strings.TrimPrefix(callee, "path.")
	callee = strings.TrimPrefix(callee, "posix.")
	switch callee {
	case "resolve", "join":
		cur := "."
		for _, a := range args {
			v, ok := evalPathExpr(a)
			if !ok {
				return "", false
			}
			if path.IsAbs(v) {
				return "", false
			}
			cur = path.Join(cur, v)
		}
		return cur, true
	case "dirname":
		if len(args) != 1 {
			return "", false
		}
		if strings.Contains(args[0], "import.meta.url") && strings.HasPrefix(strings.TrimSpace(args[0]), "fileURLToPath") && strings.Count(args[0], "(") == 1 {
			return ".", true
		}
		v, ok := evalPathExpr(args[0])
		if !ok {
			return "", false
		}
		return path.Dir(v), true
	case "fileURLToPath":
		if len(args) != 1 {
			return "", false
		}
		a := strings.TrimSpace(args[0])
		if !strings.HasPrefix(a, "new URL(") {
			return "", false
		}
		ua, ok := splitCallArgs(a[len("new URL("):])
		if !ok || len(ua) != 2 || strings.TrimSpace(ua[1]) != "import.meta.url" {
			return "", false
		}
		rel, ok := evalPathExpr(ua[0])
		if !ok || path.IsAbs(rel) {
			return "", false
		}
		// The URL resolves against the config file, so "." is the file itself.
		return path.Join(".", rel), true
	}
	return "", false
}
