package css

import (
	"fmt"
	"regexp"
	"strings"
)

// ModuleOptions configures ProcessModuleWith.
type ModuleOptions struct {
	// Composed returns the class mapping of the module named by
	// `composes: x from "spec"`. The caller is responsible for making that
	// module's CSS part of the bundle.
	Composed func(spec string) (map[string]string, error)
}

// ModuleResult is the outcome of scoping a CSS Module.
type ModuleResult struct {
	CSS     string
	Mapping map[string]string
	// ComposesFrom lists every `from "spec"` file named by a composes
	// declaration, in source order and without duplicates.
	ComposesFrom []string
	// Warnings describe constructs that could not be honoured.
	Warnings []string
}

// ProcessModule scopes a CSS Module without resolving cross-file composes.
// See ProcessModuleWith.
func ProcessModule(path, content string) (scopedCSS string, mapping map[string]string, err error) {
	r := ProcessModuleWith(path, content, ModuleOptions{})
	return r.CSS, r.Mapping, nil
}

type moduleRule struct {
	ctx     int
	classes []string // local classes of the rule selector
	single  bool     // selector is exactly one local class
}

type composeDecl struct {
	owner string
	names []string
	from  string // "" local, "global", or a module specifier
}

const (
	ctxRules = iota // selectors are scoped here
	ctxDecls        // declaration bodies (keyframe frames, nested blocks)
)

type moduleScoper struct {
	hash     string
	own      map[string]string // class -> scoped name
	keyframe map[string]string // local keyframes name -> scoped name
	composes []composeDecl
	warns    []string
}

// ProcessModuleWith scopes a CSS Module. Local class selectors are rewritten
// to `<name>_<hash>`; `:global(...)`, bare `:global` and `:local` switch
// scoping; local @keyframes names (and their references in animation and
// animation-name) are scoped; `composes:` declarations are removed and merged
// into the class mapping. Strings, comments, `url(...)` and attribute
// selectors are left untouched. The mapping also carries camelCase aliases of
// dashed class names and the scoped keyframes names.
func ProcessModuleWith(path, content string, opts ModuleOptions) *ModuleResult {
	sc := &moduleScoper{
		hash:     hashPath(path),
		own:      make(map[string]string),
		keyframe: make(map[string]string),
	}
	for _, name := range keyframeNames(content) {
		sc.keyframe[name] = name + "_" + sc.hash
	}

	stack := []moduleRule{{ctx: ctxRules}}
	var out strings.Builder
	i := 0
	n := len(content)
	preludeStart := 0

	for i < n {
		switch c := content[i]; {
		case c == '/' && i+1 < n && content[i+1] == '*':
			i = skipComment(content, i)
		case c == '"' || c == '\'':
			i = cssSkipString(content, i)
		case c == '{':
			prelude := content[preludeStart:i]
			cur := stack[len(stack)-1]
			switch {
			case cur.ctx == ctxRules:
				if at, isAt := atRuleName(prelude); isAt {
					if isKeyframesAtRule(at) {
						prelude = sc.renameKeyframes(prelude)
					}
					out.WriteString(prelude)
					out.WriteByte('{')
					if declBodyAtRule(at) {
						stack = append(stack, moduleRule{ctx: ctxDecls})
					} else {
						stack = append(stack, moduleRule{ctx: ctxRules})
					}
				} else {
					scoped, classes := sc.scopeSelector(prelude)
					out.WriteString(scoped)
					out.WriteByte('{')
					stack = append(stack, moduleRule{ctx: ctxDecls, classes: classes, single: isSingleClassSelector(prelude)})
				}
			default:
				// A nested rule inside a declaration block: the text before it may
				// hold declarations that precede the nested selector.
				cut := strings.LastIndexByte(prelude, ';') + 1
				decls := sc.scopeDecls(prelude[:cut], cur)
				out.WriteString(decls)
				out.WriteString(prelude[cut:])
				out.WriteByte('{')
				stack = append(stack, moduleRule{ctx: ctxDecls})
			}
			i++
			preludeStart = i
		case c == '}':
			body := content[preludeStart:i]
			cur := stack[len(stack)-1]
			if cur.ctx == ctxDecls {
				out.WriteString(sc.scopeDecls(body, cur))
			} else {
				out.WriteString(body)
			}
			out.WriteByte('}')
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
			i++
			preludeStart = i
		default:
			i++
		}
	}
	tail := content[preludeStart:]
	if cur := stack[len(stack)-1]; cur.ctx == ctxDecls {
		out.WriteString(sc.scopeDecls(tail, cur))
	} else {
		out.WriteString(tail)
	}

	res := &ModuleResult{CSS: out.String()}
	res.Mapping = sc.buildMapping(opts, res)
	res.Warnings = sc.warns
	return res
}

func (sc *moduleScoper) warnf(format string, args ...any) {
	sc.warns = append(sc.warns, fmt.Sprintf(format, args...))
}

// buildMapping merges own classes, composes targets, keyframes names and
// camelCase aliases into the exported mapping.
func (sc *moduleScoper) buildMapping(opts ModuleOptions, res *ModuleResult) map[string]string {
	byOwner := make(map[string][]composeDecl)
	for _, cd := range sc.composes {
		byOwner[cd.owner] = append(byOwner[cd.owner], cd)
		if cd.from != "" && cd.from != "global" {
			dup := false
			for _, f := range res.ComposesFrom {
				if f == cd.from {
					dup = true
				}
			}
			if !dup {
				res.ComposesFrom = append(res.ComposesFrom, cd.from)
			}
		}
	}
	external := make(map[string]map[string]string)
	loadExternal := func(spec string) map[string]string {
		if m, ok := external[spec]; ok {
			return m
		}
		var m map[string]string
		if opts.Composed == nil {
			sc.warnf("composes from %q: cross-file composes is not available here", spec)
		} else if got, err := opts.Composed(spec); err != nil {
			sc.warnf("composes from %q: %v", spec, err)
		} else {
			m = got
		}
		external[spec] = m
		return m
	}

	final := make(map[string]string)
	state := make(map[string]int) // 1 = resolving, 2 = done
	var resolve func(name string) string
	resolve = func(name string) string {
		if state[name] == 2 {
			return final[name]
		}
		if state[name] == 1 {
			sc.warnf("composes cycle through .%s", name)
			return sc.own[name]
		}
		state[name] = 1
		parts := []string{sc.own[name]}
		seen := map[string]bool{sc.own[name]: true}
		add := func(v string) {
			for _, tok := range strings.Fields(v) {
				if !seen[tok] {
					seen[tok] = true
					parts = append(parts, tok)
				}
			}
		}
		for _, cd := range byOwner[name] {
			for _, nm := range cd.names {
				switch cd.from {
				case "global":
					add(nm)
				case "":
					if _, ok := sc.own[nm]; !ok {
						sc.warnf(".%s composes undefined class %q", name, nm)
						continue
					}
					add(resolve(nm))
				default:
					m := loadExternal(cd.from)
					if m == nil {
						continue
					}
					v, ok := m[nm]
					if !ok {
						sc.warnf(".%s composes %q, which %q does not define", name, nm, cd.from)
						continue
					}
					add(v)
				}
			}
		}
		state[name] = 2
		final[name] = strings.Join(parts, " ")
		return final[name]
	}

	mapping := make(map[string]string, len(sc.own)+len(sc.keyframe))
	for name := range sc.own {
		mapping[name] = resolve(name)
	}
	for name, scoped := range sc.keyframe {
		if _, taken := mapping[name]; !taken {
			mapping[name] = scoped
		}
	}
	for name, v := range mapping {
		if alias := camelCaseName(name); alias != name {
			if _, taken := mapping[alias]; !taken {
				mapping[alias] = v
			}
		}
	}
	return mapping
}

// camelCaseName converts dashed and underscored names to camelCase.
func camelCaseName(name string) string {
	if !strings.ContainsAny(name, "-_") {
		return name
	}
	var b strings.Builder
	up := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '-' || c == '_' {
			if b.Len() > 0 {
				up = true
			}
			continue
		}
		if up && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		up = false
		b.WriteByte(c)
	}
	if b.Len() == 0 {
		return name
	}
	return b.String()
}

var singleClassRe = regexp.MustCompile(`^\.[A-Za-z0-9_-]+$`)

func isSingleClassSelector(sel string) bool {
	return singleClassRe.MatchString(strings.TrimSpace(sel))
}

func isKeyframesAtRule(name string) bool {
	switch name {
	case "keyframes", "-webkit-keyframes", "-moz-keyframes", "-o-keyframes":
		return true
	}
	return false
}

// keyframeNames returns the names of @keyframes rules that are local to the
// module (not wrapped in :global).
func keyframeNames(content string) []string {
	var names []string
	i := 0
	for i < len(content) {
		switch c := content[i]; {
		case c == '/' && i+1 < len(content) && content[i+1] == '*':
			i = skipComment(content, i)
		case c == '"' || c == '\'':
			i = cssSkipString(content, i)
		case c == '@':
			j := i + 1
			for j < len(content) && (isIdentChar(content[j])) {
				j++
			}
			if isKeyframesAtRule(strings.ToLower(content[i+1 : j])) {
				end := strings.IndexByte(content[j:], '{')
				if end < 0 {
					return names
				}
				name := strings.TrimSpace(content[j : j+end])
				name = strings.Trim(name, `"'`)
				if name != "" && !strings.HasPrefix(name, ":global") {
					name = strings.TrimSuffix(strings.TrimPrefix(name, ":local("), ")")
					names = append(names, name)
				}
			}
			i = j
		default:
			i++
		}
	}
	return names
}

// renameKeyframes scopes the name in a `@keyframes name` prelude.
func (sc *moduleScoper) renameKeyframes(prelude string) string {
	at := strings.IndexByte(prelude, '@')
	j := at + 1
	for j < len(prelude) && isIdentChar(prelude[j]) {
		j++
	}
	head := prelude[:j]
	name := strings.TrimSpace(prelude[j:])
	trail := prelude[len(strings.TrimRight(prelude, " \t\r\n")):]
	switch {
	case strings.HasPrefix(name, ":global(") && strings.HasSuffix(name, ")"):
		name = strings.TrimSpace(name[len(":global(") : len(name)-1])
	case strings.HasPrefix(name, ":local(") && strings.HasSuffix(name, ")"):
		name = strings.TrimSpace(name[len(":local(") : len(name)-1])
		if scoped, ok := sc.keyframe[name]; ok {
			name = scoped
		}
	default:
		if scoped, ok := sc.keyframe[strings.Trim(name, `"'`)]; ok {
			name = scoped
		}
	}
	return head + " " + name + trail
}

var animationKeywords = map[string]bool{
	"none": true, "infinite": true, "normal": true, "reverse": true, "alternate": true,
	"alternate-reverse": true, "forwards": true, "backwards": true, "both": true,
	"running": true, "paused": true, "ease": true, "ease-in": true, "ease-out": true,
	"ease-in-out": true, "linear": true, "step-start": true, "step-end": true,
	"initial": true, "inherit": true, "unset": true, "revert": true, "revert-layer": true,
}

// scopeDecls processes the declarations of one block: it removes and records
// `composes`, and scopes keyframes names in animation declarations.
func (sc *moduleScoper) scopeDecls(body string, rule moduleRule) string {
	if !strings.Contains(body, ":") {
		return body
	}
	segs := splitTopLevel(body, ';')
	var out strings.Builder
	for k, seg := range segs {
		colon := strings.IndexByte(seg, ':')
		last := k == len(segs)-1
		if colon < 0 {
			out.WriteString(seg)
			if !last {
				out.WriteByte(';')
			}
			continue
		}
		prop := strings.ToLower(strings.TrimSpace(seg[:colon]))
		value := seg[colon+1:]
		switch {
		case prop == "composes":
			sc.recordComposes(strings.TrimSpace(value), rule)
			// the declaration (and its ';') is dropped
			continue
		case isAnimationProp(prop) && len(sc.keyframe) > 0:
			seg = seg[:colon+1] + sc.scopeAnimation(value, prop)
		}
		out.WriteString(seg)
		if !last {
			out.WriteByte(';')
		}
	}
	return out.String()
}

func isAnimationProp(prop string) bool {
	p := strings.TrimPrefix(prop, "-webkit-")
	p = strings.TrimPrefix(p, "-moz-")
	p = strings.TrimPrefix(p, "-o-")
	return p == "animation" || p == "animation-name"
}

// scopeAnimation rewrites local keyframes names in an animation value.
func (sc *moduleScoper) scopeAnimation(value, prop string) string {
	items := splitTopLevel(value, ',')
	longhand := strings.HasSuffix(prop, "animation-name")
	for k, item := range items {
		toks := splitTopLevelFields(item)
		for _, t := range toks {
			if strings.HasPrefix(t.text, ":global(") && strings.HasSuffix(t.text, ")") {
				items[k] = strings.Replace(items[k], t.text, strings.TrimSpace(t.text[len(":global("):len(t.text)-1]), 1)
				continue
			}
			if strings.ContainsAny(t.text, "()\"'") {
				continue
			}
			if !longhand && animationKeywords[strings.ToLower(t.text)] {
				continue
			}
			if longhand && strings.EqualFold(t.text, "none") {
				continue
			}
			if scoped, ok := sc.keyframe[t.text]; ok {
				items[k] = strings.Replace(items[k], t.text, scoped, 1)
				break
			}
		}
	}
	return strings.Join(items, ",")
}

type fieldTok struct {
	text string
}

// splitTopLevelFields splits on whitespace outside parentheses and strings.
func splitTopLevelFields(s string) []fieldTok {
	var out []fieldTok
	depth := 0
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\'':
			if start < 0 {
				start = i
			}
			i = cssSkipString(s, i) - 1
		case c == '(':
			depth++
			if start < 0 {
				start = i
			}
		case c == ')':
			if depth > 0 {
				depth--
			}
		case isCSSSpace(c) && depth == 0:
			if start >= 0 {
				out = append(out, fieldTok{s[start:i]})
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		out = append(out, fieldTok{s[start:]})
	}
	return out
}

// splitTopLevel splits s on sep outside parentheses, strings and comments.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			i = skipComment(s, i) - 1
		case c == '"' || c == '\'':
			i = cssSkipString(s, i) - 1
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == sep && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// recordComposes parses a composes value (`a b`, `a from "./x.css"`,
// `a from global`) and records it for the rule's class.
func (sc *moduleScoper) recordComposes(value string, rule moduleRule) {
	if !rule.single || len(rule.classes) != 1 {
		sc.warnf("composes: %s is ignored because composes is only allowed in a rule whose selector is a single class", value)
		return
	}
	names := strings.Fields(value)
	from := ""
	for k := 0; k < len(names); k++ {
		if names[k] == "from" && k+1 < len(names) {
			spec := strings.Join(names[k+1:], " ")
			names = names[:k]
			if spec == "global" {
				from = "global"
			} else {
				from = strings.Trim(spec, `"'`)
			}
			break
		}
	}
	if len(names) == 0 {
		sc.warnf("composes: %s names no classes", value)
		return
	}
	sc.composes = append(sc.composes, composeDecl{owner: rule.classes[0], names: names, from: from})
}

// scopeSelector rewrites `.name` class tokens to their scoped form, skipping
// comments and strings. `:global(...)` and bare `:global` leave classes
// unscoped, `:local(...)` and bare `:local` scope them again. It also returns
// the local classes it scoped.
func (sc *moduleScoper) scopeSelector(sel string) (string, []string) {
	var out strings.Builder
	var classes []string
	global := false
	i, n := 0, len(sel)
	for i < n {
		c := sel[i]
		switch {
		case c == '/' && i+1 < n && sel[i+1] == '*':
			j := skipComment(sel, i)
			out.WriteString(sel[i:j])
			i = j
		case c == '"' || c == '\'':
			j := cssSkipString(sel, i)
			out.WriteString(sel[i:j])
			i = j
		case c == ',':
			global = false
			out.WriteByte(c)
			i++
		case c == ':' && (hasFoldAt(sel, i, ":global") || hasFoldAt(sel, i, ":local")) && isWordEnd(sel, i+wordLen(sel, i)):
			name := sel[i : i+wordLen(sel, i)]
			isGlobal := strings.EqualFold(name, ":global")
			after := i + len(name)
			if after < n && sel[after] == '(' {
				closeIdx := matchParen(sel, after)
				if closeIdx < 0 {
					out.WriteString(sel[i:])
					return out.String(), classes
				}
				inner := sel[after+1 : closeIdx]
				if isGlobal {
					out.WriteString(inner)
				} else {
					s, cl := sc.scopeSelector(inner)
					out.WriteString(s)
					classes = append(classes, cl...)
				}
				i = closeIdx + 1
				continue
			}
			global = isGlobal
			i = after
			ws := i
			for i < n && isCSSSpace(sel[i]) {
				i++
			}
			if i > ws && out.Len() > 0 && !isCSSSpace(out.String()[out.Len()-1]) {
				out.WriteByte(' ')
			}
		case c == '.':
			k := i + 1
			for k < n && isIdentChar(sel[k]) {
				k++
			}
			if k == i+1 {
				out.WriteByte('.')
				i++
				continue
			}
			if !global {
				name := sel[i+1 : k]
				scoped, ok := sc.own[name]
				if !ok {
					scoped = name + "_" + sc.hash
					sc.own[name] = scoped
				}
				classes = appendUnique(classes, name)
				out.WriteByte('.')
				out.WriteString(scoped)
				i = k
				continue
			}
			out.WriteString(sel[i:k])
			i = k
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String(), classes
}

func wordLen(s string, i int) int {
	if hasFoldAt(s, i, ":global") {
		return len(":global")
	}
	return len(":local")
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// atRuleName returns the lowercased at-rule keyword of a prelude, if it is one.
func atRuleName(prelude string) (string, bool) {
	s := strings.TrimSpace(prelude)
	if !strings.HasPrefix(s, "@") {
		return "", false
	}
	end := 1
	for end < len(s) && (isIdentChar(s[end]) || s[end] == '-') {
		end++
	}
	return strings.ToLower(s[1:end]), true
}

// declBodyAtRule reports whether an at-rule's block contains declarations
// (rather than nested rules).
func declBodyAtRule(name string) bool {
	switch name {
	case "keyframes", "-webkit-keyframes", "-moz-keyframes", "-o-keyframes",
		"font-face", "page", "counter-style", "property", "font-feature-values",
		"viewport", "-ms-viewport":
		return true
	}
	return false
}

func isIdentChar(c byte) bool {
	return c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// skipString returns the index just past a string literal starting at s[i]
// (a quote), honouring backslash escapes; unterminated strings run to the end.
func cssSkipString(s string, i int) int {
	quote := s[i]
	i++
	for i < len(s) {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == quote {
			return i + 1
		}
		i++
	}
	return len(s)
}

// matchParen returns the index of the ')' matching the '(' at s[i], honouring
// strings and nesting, or -1.
func matchParen(s string, i int) int {
	if i >= len(s) || s[i] != '(' {
		return -1
	}
	depth := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			i = cssSkipString(s, i) - 1
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
