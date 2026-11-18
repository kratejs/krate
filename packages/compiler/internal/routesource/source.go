// Package routesource compiles the `source` strings of redirects, rewrites
// and header rules, and the has/missing conditions on them.
//
// The syntax is the path-to-regexp 6.x subset Next.js uses: literal segments,
// :name, :name*, :name+, :name?, :name(regex), unnamed (regex) and {...}
// groups. Krate's older forms keep working: "/x/*" binds the rest of the path
// to :splat, and a header source without ":", "(" or "{" is an exact path, a
// trailing-"*" prefix or a path.Match glob.
//
// Patterns run on Go's RE2. A leading negative lookahead inside a parameter,
// as in :slug((?!compare$)[^/]+), is supported by checking the parameter value
// separately. Any other regex feature RE2 lacks (lookbehind, backreferences,
// a lookahead elsewhere) is an error at compile time, so a rule is never
// silently dead.
package routesource

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Kind selects the legacy forms a source may use.
type Kind int

const (
	KindRedirect Kind = iota
	KindRewrite
	KindHeader
)

// UnsupportedError reports a source or condition value that uses a regex
// feature Go cannot run (or a syntax error in the source).
type UnsupportedError struct {
	Msg string
}

func (e *UnsupportedError) Error() string { return e.Msg }

// IsUnsupported reports whether err is an UnsupportedError.
func IsUnsupported(err error) bool {
	_, ok := err.(*UnsupportedError)
	return ok
}

func unsupported(format string, a ...any) error {
	return &UnsupportedError{Msg: fmt.Sprintf(format, a...)}
}

type sourceMode int

const (
	modeLiteral    sourceMode = iota
	modeSplat                 // legacy "/x/*"
	modeHeaderGlob            // legacy header: exact, "prefix*" or path.Match
	modePattern
)

type paramInfo struct {
	name  string
	group int
	mod   byte
	neg   []*regexp.Regexp
}

// Source is a compiled source string.
type Source struct {
	raw        string
	mode       sourceMode
	prefix     string
	re         *regexp.Regexp
	params     []paramInfo
	splatGroup int
	jsPattern  string
}

// Params holds the values a source captured, and the modifier ('*', '+', '?'
// or 0) each named parameter was declared with.
type Params struct {
	Values map[string]string
	Mods   map[string]byte
}

// Raw returns the source as written.
func (s *Source) Raw() string { return s.raw }

// Compile compiles one source string.
func Compile(src string, kind Kind) (*Source, error) {
	s := &Source{raw: src}
	if kind == KindHeader {
		src = strings.TrimSpace(src)
		s.raw = src
	}
	if !strings.ContainsAny(src, ":({") {
		switch {
		case kind == KindHeader:
			s.mode = modeHeaderGlob
		case strings.HasSuffix(src, "/*"):
			s.mode = modeSplat
			s.prefix = strings.TrimSuffix(src, "/*")
		default:
			s.mode = modeLiteral
		}
		return s, nil
	}
	s.mode = modePattern
	pat := src
	splat := false
	if strings.HasSuffix(pat, "/*") {
		pat = strings.TrimSuffix(pat, "/*")
		splat = true
	}
	toks, err := parsePattern(pat)
	if err != nil {
		return nil, unsupported("source %q: %v", src, err)
	}
	if err := s.build(toks, splat, false); err != nil {
		return nil, unsupported("source %q: %v", src, err)
	}
	return s, nil
}

// Match reports whether path matches and returns the captured parameters.
func (s *Source) Match(p string) (Params, bool) {
	empty := Params{Values: map[string]string{}, Mods: map[string]byte{}}
	switch s.mode {
	case modeLiteral:
		return empty, s.raw == p
	case modeSplat:
		if p == s.prefix {
			empty.Values["splat"] = ""
			return empty, true
		}
		if !strings.HasPrefix(p, s.prefix+"/") {
			return Params{}, false
		}
		empty.Values["splat"] = strings.TrimPrefix(strings.TrimPrefix(p, s.prefix), "/")
		return empty, true
	case modeHeaderGlob:
		return empty, headerGlob(s.raw, p)
	}
	m := s.re.FindStringSubmatchIndex(p)
	if m == nil {
		return Params{}, false
	}
	group := func(i int) (string, bool) {
		if 2*i+1 >= len(m) || m[2*i] < 0 {
			return "", false
		}
		return p[m[2*i]:m[2*i+1]], true
	}
	for _, pi := range s.params {
		v, ok := group(pi.group)
		if ok {
			for _, n := range pi.neg {
				if n.MatchString(v) {
					return Params{}, false
				}
			}
		}
		if pi.name == "" {
			continue
		}
		empty.Mods[pi.name] = pi.mod
		if ok || pi.mod == '?' || pi.mod == '*' {
			empty.Values[pi.name] = v
		}
	}
	if s.splatGroup > 0 {
		v, _ := group(s.splatGroup)
		empty.Values["splat"] = v
	}
	return empty, true
}

// headerGlob is Krate's original header source matching: an exact path, a
// trailing "*" prefix ("/assets/*"), or a path.Match glob.
func headerGlob(pattern, p string) bool {
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "*") {
		return p == pattern
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(p, strings.TrimSuffix(pattern, "*"))
	}
	ok, _ := path.Match(pattern, p)
	return ok
}

// JSSource is a source translated for a JavaScript engine, for code the
// migrator writes into middleware when Go cannot run the original pattern.
type JSSource struct {
	// Pattern is the body of a RegExp that matches a request pathname.
	Pattern string
	// Names holds the parameter name of each capture group, in order; ""
	// marks an unnamed group. A trailing "splat" is the legacy /* tail.
	Names []string
	// Mods holds the modifier ('*', '+' or '?') of each repeated or optional
	// named parameter.
	Mods map[string]string
}

// CompileJS translates a source to a JavaScript regular expression. Patterns
// are not checked against Go's RE2; only the syntax of the source itself is.
func CompileJS(src string) (*JSSource, error) {
	out := &JSSource{Mods: map[string]string{}}
	if !strings.ContainsAny(src, ":({") {
		if strings.HasSuffix(src, "/*") {
			out.Pattern = "^" + quote(strings.TrimSuffix(src, "/*")) + `(?:/([\s\S]*))?$`
			out.Names = []string{"splat"}
			return out, nil
		}
		out.Pattern = "^" + quote(src) + "$"
		return out, nil
	}
	pat := src
	splat := false
	if strings.HasSuffix(pat, "/*") {
		pat = strings.TrimSuffix(pat, "/*")
		splat = true
	}
	toks, err := parsePattern(pat)
	if err != nil {
		return nil, unsupported("source %q: %v", src, err)
	}
	s := &Source{raw: src}
	if err := s.build(toks, splat, true); err != nil {
		return nil, unsupported("source %q: %v", src, err)
	}
	out.Pattern = s.jsPattern
	for _, p := range s.params {
		out.Names = append(out.Names, p.name)
		if p.name != "" && p.mod != 0 {
			out.Mods[p.name] = string(p.mod)
		}
	}
	if splat {
		out.Names = append(out.Names, "splat")
	}
	return out, nil
}
