package build

import (
	"sync"
	"time"

	"github.com/dlclark/regexp2/v2"

	"github.com/kratejs/krate/packages/compiler/internal/rulepattern"
)

// Redirect and rewrite sources may constrain a parameter with a regular
// expression in path-to-regexp form, as next.config does:
// "/store/:slug((?!compare$)[^/]+)", "/p/:id(\\d+)?", or an unnamed "(a|b)".
// Such a source compiles to one anchored ECMAScript regex (lookaheads are
// allowed), matched with a timeout because the path comes from the request.

type sourceRegex struct {
	re    *regexp2.Regexp
	names []string
	mods  map[string]byte
}

var sourceRegexCache sync.Map // source -> *sourceRegex (nil when invalid)

const sourceRegexTimeout = 50 * time.Millisecond

// isRegexSource reports whether a redirect/rewrite source uses a regex
// parameter, which matchRedirectParamsMods handles with compileSourceRegex.
func isRegexSource(source string) bool {
	return rulepattern.IsRegex(source)
}

// compileSourceRegex returns the compiled form of a regex source, or nil when
// the source is not a valid pattern (it then matches nothing).
func compileSourceRegex(source string) *sourceRegex {
	if v, ok := sourceRegexCache.Load(source); ok {
		sr, _ := v.(*sourceRegex)
		return sr
	}
	pattern, names, mods, ok := rulepattern.ToRegex(source)
	var sr *sourceRegex
	if ok {
		if re, err := regexp2.Compile(pattern, regexp2.ECMAScript); err == nil {
			re.MatchTimeout = sourceRegexTimeout
			sr = &sourceRegex{re: re, names: names, mods: mods}
		}
	}
	sourceRegexCache.Store(source, sr)
	return sr
}

// matchRegexSource matches path against a regex source.
func matchRegexSource(path, source string) (map[string]string, map[string]byte, bool) {
	sr := compileSourceRegex(source)
	if sr == nil {
		return nil, nil, false
	}
	m, err := sr.re.FindStringMatch(path)
	if err != nil || m == nil {
		return nil, nil, false
	}
	params := map[string]string{}
	for i, name := range sr.names {
		if g := m.GroupByName(rulepattern.Group(i)); g != nil {
			params[name] = g.String()
		}
	}
	mods := make(map[string]byte, len(sr.mods))
	for k, v := range sr.mods {
		mods[k] = v
	}
	return params, mods, true
}
