package renderer

import (
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// Props registrations carry only what client code reads.
//
// A client child's call-site props are registered in its parent's hydration
// scope (`__krate_props["<id>"]={...}`) and the child's own scope binds them
// with `var props=__krate_props["<id>"]`. That scope is the only client code
// that reads the registration: a remounted conditional branch re-runs the
// same scope, SPA navigation and HMR re-run the whole page script (the
// registrations included), keyed list rows render their components through
// h(Component, props) with the row's own props, and lazy chunks hold the same
// scope text. Everything else the props fed was rendered into the HTML at
// build time.
//
// The rule: an entry is emitted when the child's emitted scope reads it
// (`props.x`, `props?.x`, `props["x"]`, or a destructured parameter key),
// counting the entries the child itself forwards to its own children
// (a kept `n={props.n}` getter reads props.n). Every entry is emitted when
// `props` escapes (passed whole, spread, aliased, indexed by a non-literal
// key, shadowed), when a scope uses the registry in a way this analysis does
// not model, or when the child's scope cannot be found. Renderer boundaries
// hand their whole props object to the runtime and are never filtered (their
// registrations have no structured form).
type propsPlan struct {
	// keep maps a registration id to the entry names to emit. An id that is
	// absent keeps every entry.
	keep map[string]map[string]bool
}

// render returns the ExtraVars renderer for sig's scope.
func (p *propsPlan) render(sig irtree.ComponentSignature) func(string) string {
	if p == nil || len(p.keep) == 0 || len(sig.PropsRegs) == 0 {
		return identityVar
	}
	regs := regsByJS(sig)
	return func(v string) string {
		reg, ok := regs[v]
		if !ok {
			return v
		}
		names, ok := p.keep[reg.ID]
		if !ok {
			return v
		}
		return reg.JS(func(name string) bool { return names[name] })
	}
}

func identityVar(v string) string { return v }

func regsByJS(sig irtree.ComponentSignature) map[string]irtree.PropsReg {
	m := make(map[string]irtree.PropsReg, len(sig.PropsRegs))
	for _, r := range sig.PropsRegs {
		m[r.JS(nil)] = r
	}
	return m
}

// propsReads is what one piece of client code reads from `props`.
type propsReads struct {
	names map[string]bool
	all   bool
	// unused maps a local initialised with exactly `props.<member>` and not
	// referenced anywhere else in the code to that member. The member is read
	// only if code outside (a kept registration entry) uses the local.
	unused map[string][]string
}

func (r *propsReads) merge(o propsReads) {
	if o.all {
		r.all = true
	}
	for n := range o.names {
		r.add(n)
	}
	for local, members := range o.unused {
		if r.unused == nil {
			r.unused = make(map[string][]string)
		}
		r.unused[local] = append(r.unused[local], members...)
	}
}

// use records that code outside the analysed text references local.
func (r *propsReads) use(local string) {
	for _, m := range r.unused[local] {
		r.add(m)
	}
	delete(r.unused, local)
}

func (r *propsReads) add(name string) {
	if r.names == nil {
		r.names = make(map[string]bool)
	}
	r.names[name] = true
}

// scopeInfo is one client scope's analysis input.
type scopeInfo struct {
	sig   irtree.ComponentSignature
	ownID string     // registry id the scope binds as `props` ("" when none)
	base  propsReads // reads outside the registrations it makes
}

// planPropsRegs analyses every client scope of the page and decides which
// entries of each structured props registration to emit.
func planPropsRegs(result *EmitResult) *propsPlan {
	hasRegs := false
	for _, sig := range result.Signatures {
		if sig.Tier == irtree.TierClient && len(sig.PropsRegs) > 0 {
			hasRegs = true
			break
		}
	}
	if !hasRegs {
		return nil
	}
	// Page-level code outside the component scopes must not touch the
	// registry; if it does, nothing is filtered.
	outside := append(append([]string(nil), result.SharedModuleVars...), result.ModuleInit...)
	outside = append(outside, result.IslandBootJS)
	for _, fn := range result.ListComponents {
		outside = append(outside, irtree.RenderComponentFnJS(fn))
	}
	for _, code := range outside {
		if strings.Contains(code, "__krate_props") {
			return nil
		}
	}

	var scopes []*scopeInfo
	byID := map[string][]*scopeInfo{}
	for _, sig := range result.Signatures {
		if sig.Tier != irtree.TierClient {
			continue
		}
		sc := &scopeInfo{sig: sig}
		regs := regsByJS(sig)
		var buf strings.Builder
		writeScopeBody(&buf, result, sig, func(v string) string {
			if _, ok := regs[v]; ok {
				return ""
			}
			if strings.HasPrefix(v, "var props=__krate_props[") {
				id, binding, ok := parsePropsDecl(v)
				if !ok || sc.ownID != "" {
					sc.base.all = true
					return ""
				}
				sc.ownID = id
				sc.base.merge(bindingReads(binding))
				return ""
			}
			return v
		}, false)
		reads, foreign := analyzePropsReads(buf.String())
		if foreign {
			return nil
		}
		sc.base.merge(reads)
		scopes = append(scopes, sc)
		if sc.ownID != "" {
			byID[sc.ownID] = append(byID[sc.ownID], sc)
		}
	}

	plan := &propsPlan{keep: map[string]map[string]bool{}}
	const (
		pending = 1
		done    = 2
	)
	state := map[*scopeInfo]int{}
	total := map[*scopeInfo]propsReads{}
	// keepFor returns the entry names of registration id to emit, or nil to
	// keep all of them.
	var keepFor func(id string) map[string]bool
	resolve := func(sc *scopeInfo) propsReads {
		switch state[sc] {
		case done:
			return total[sc]
		case pending:
			return propsReads{all: true}
		}
		state[sc] = pending
		r := propsReads{}
		r.merge(sc.base)
		for _, reg := range sc.sig.PropsRegs {
			names := keepFor(reg.ID)
			for _, e := range reg.Entries {
				if names != nil && !names[e.Name] {
					continue
				}
				er, foreign := analyzePropsReads(e.Value)
				if foreign {
					er.all = true
				}
				r.merge(er)
				irtree.ScanJSIdentsAt(e.Value, func(name string, _ int) { r.use(name) })
			}
		}
		state[sc] = done
		total[sc] = r
		return r
	}
	keepFor = func(id string) map[string]bool {
		if names, ok := plan.keep[id]; ok {
			return names
		}
		readers := byID[id]
		if len(readers) == 0 {
			return nil
		}
		names := map[string]bool{}
		for _, sc := range readers {
			r := resolve(sc)
			if r.all {
				return nil
			}
			for n := range r.names {
				names[n] = true
			}
		}
		plan.keep[id] = names
		return names
	}
	for _, sc := range scopes {
		for _, reg := range sc.sig.PropsRegs {
			keepFor(reg.ID)
		}
	}
	return plan
}

// parsePropsDecl splits `var props=__krate_props["<id>"]` (optionally
// followed by `;var <binding>=props`) into the id and the binding.
func parsePropsDecl(v string) (id, binding string, ok bool) {
	rest := strings.TrimPrefix(v, "var props=__krate_props[")
	end := strings.Index(rest, "]")
	if end < 0 {
		return "", "", false
	}
	id, err := strconv.Unquote(rest[:end])
	if err != nil {
		return "", "", false
	}
	rest = rest[end+1:]
	if rest == "" {
		return id, "", true
	}
	b, found := strings.CutPrefix(rest, ";var ")
	if !found {
		return "", "", false
	}
	b, found = strings.CutSuffix(b, "=props")
	if !found {
		return "", "", false
	}
	return id, strings.TrimSpace(b), true
}

// bindingReads returns the keys a parameter binding (`{ a, b: c, d = 1 }`)
// reads from props. A plain name, an array pattern, a rest element or a
// computed key reads everything.
func bindingReads(binding string) propsReads {
	r := propsReads{}
	if binding == "" {
		return r
	}
	if !strings.HasPrefix(binding, "{") || !strings.HasSuffix(binding, "}") {
		return propsReads{all: true}
	}
	for _, el := range splitTopLevel(binding[1 : len(binding)-1]) {
		el = strings.TrimSpace(el)
		switch {
		case el == "":
			continue
		case strings.HasPrefix(el, "..."), strings.HasPrefix(el, "["):
			return propsReads{all: true}
		case el[0] == '"' || el[0] == '\'':
			end := strings.IndexByte(el[1:], el[0])
			if end < 0 || strings.Contains(el[1:end+1], "\\") {
				return propsReads{all: true}
			}
			r.add(el[1 : end+1])
		default:
			i := 0
			for i < len(el) && isJSIdentByte(el[i]) {
				i++
			}
			if i == 0 {
				return propsReads{all: true}
			}
			r.add(el[:i])
		}
	}
	return r
}

// splitTopLevel splits s at commas outside brackets, braces, parens and
// string or template literals.
func splitTopLevel(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'', '`':
			for i++; i < len(s) && s[i] != c; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

func isJSIdentByte(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c >= 0x80
}

// analyzePropsReads finds the `props` members code reads. foreign reports a
// use of the registry other than a context overlay, which the plan does not
// model. A local declared as exactly `props.x` that nothing else in code
// references is replayed body code the client never uses (its value was
// rendered at build time), so x is recorded in r.unused instead of read.
func analyzePropsReads(code string) (r propsReads, foreign bool) {
	type ident struct {
		name string
		at   int
	}
	var idents []ident
	refs := map[string]int{}
	irtree.ScanJSIdentsAt(code, func(name string, at int) {
		idents = append(idents, ident{name, at})
		refs[name]++
	})
	for _, id := range idents {
		switch id.name {
		case "props":
			member, ok := propsMemberAt(code, id.at+len(id.name))
			if !ok {
				r.all = true
				continue
			}
			if local := declaredLocal(code, id.at); local != "" && refs[local] == 1 && endsInit(code, id.at, member) {
				if r.unused == nil {
					r.unused = make(map[string][]string)
				}
				r.unused[local] = append(r.unused[local], member)
				continue
			}
			r.add(member)
		case "eval":
			r.all = true
		case "__krate_props":
			rest := strings.TrimLeft(code[id.at+len(id.name):], " \t\n")
			if !strings.HasPrefix(rest, "[") {
				foreign = true
				continue
			}
			key, ok := literalKey(rest[1:])
			if !ok || !strings.Contains(key, "#ctx:") {
				foreign = true
			}
		}
	}
	return r, foreign
}

// declaredLocal returns NAME when code[:at] ends with `var|let|const NAME=`.
func declaredLocal(code string, at int) string {
	s := strings.TrimRight(code[:at], " \t")
	if !strings.HasSuffix(s, "=") {
		return ""
	}
	s = s[:len(s)-1]
	if s != "" && strings.ContainsRune("=!<>+-*/%&|^?", rune(s[len(s)-1])) {
		return ""
	}
	s = strings.TrimRight(s, " \t")
	i := len(s)
	for i > 0 && isJSIdentByte(s[i-1]) {
		i--
	}
	name := s[i:]
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		return ""
	}
	kw := strings.TrimRight(s[:i], " \t\n")
	for _, k := range []string{"var", "let", "const"} {
		if strings.HasSuffix(kw, k) {
			before := kw[:len(kw)-len(k)]
			if before == "" || !isJSIdentByte(before[len(before)-1]) {
				return name
			}
		}
	}
	return ""
}

// endsInit reports whether the `props.<member>` read at code[at:] is the
// whole initializer: the statement or declarator ends right after it.
func endsInit(code string, at int, member string) bool {
	rest := code[at+len("props"):]
	i := strings.Index(rest, member)
	if i < 0 {
		return false
	}
	rest = strings.TrimLeft(rest[i+len(member):], " \t")
	if rest == "" {
		return true
	}
	if rest[0] == ']' {
		rest = strings.TrimLeft(rest[1:], " \t")
		if rest == "" {
			return true
		}
	}
	switch rest[0] {
	case ';', '\n', ',':
		return true
	}
	return false
}

// propsMemberAt returns the member name read at code[i:] right after a
// `props` identifier: `.x`, `?.x`, `["x"]` or `?.["x"]`.
func propsMemberAt(code string, i int) (string, bool) {
	rest := strings.TrimLeft(code[i:], " \t\n")
	switch {
	case strings.HasPrefix(rest, "..."):
		return "", false
	case strings.HasPrefix(rest, "?.["):
		return literalKey(rest[3:])
	case strings.HasPrefix(rest, "?."):
		return leadingIdent(rest[2:])
	case strings.HasPrefix(rest, "."):
		return leadingIdent(rest[1:])
	case strings.HasPrefix(rest, "["):
		return literalKey(rest[1:])
	}
	return "", false
}

func leadingIdent(s string) (string, bool) {
	s = strings.TrimLeft(s, " \t\n")
	i := 0
	for i < len(s) && isJSIdentByte(s[i]) {
		i++
	}
	if i == 0 || (s[0] >= '0' && s[0] <= '9') {
		return "", false
	}
	return s[:i], true
}

// literalKey parses a string literal key followed by `]`.
func literalKey(s string) (string, bool) {
	s = strings.TrimLeft(s, " \t\n")
	if s == "" || (s[0] != '"' && s[0] != '\'') {
		return "", false
	}
	end := strings.IndexByte(s[1:], s[0])
	if end < 0 {
		return "", false
	}
	key := s[1 : end+1]
	if strings.Contains(key, "\\") {
		return "", false
	}
	if !strings.HasPrefix(strings.TrimLeft(s[end+2:], " \t\n"), "]") {
		return "", false
	}
	return key, true
}
