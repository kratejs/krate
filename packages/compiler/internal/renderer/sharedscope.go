package renderer

import (
	"crypto/sha256"
	"encoding/binary"
	"regexp"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// Shared component scopes.
//
// A client component's hydration scope is the same code on every page that
// renders it, apart from the instance's slot and component ids and the build
// data folded into it. When sharing is on, each eligible scope is compiled
// into a function of those values (its template); the page script keeps one
// __ku(id, args) call per instance and the build writes every template once,
// to a shared chunk. Templates are content-addressed, so two instances (on
// one page or on different pages) share a template only when their code is
// identical, and the arguments recreate exactly the values the inline scope
// had. A scope the compiler cannot prove equivalent this way stays inline.

// SharedScope is one component scope template.
type SharedScope struct {
	// ID is the content hash of JS.
	ID string
	// Source is the module that defines the component.
	Source string
	// JS is the template: `function(__ka){...}`.
	JS string
	// Marks are the source positions JS's markers (/*@k:N*/) index, when
	// the page is built with sourcemaps.
	Marks []SrcMark `json:",omitempty"`
}

// ShareOptions turns on shared scopes for a page.
type ShareOptions struct {
	// PageFile is the page's own module. Components defined there stay in the
	// page script unless All is set.
	PageFile string
	// All shares page-local components too.
	All bool
	// Reserved are names the build declares around the page script (npm
	// bindings, island components); a scope reading one stays inline.
	Reserved map[string]bool
}

// SharedScopeArg is the parameter name of a template.
const SharedScopeArg = "__ka"

// sharedScopeCall is the page-script call that runs a template.
const sharedScopeCall = "__ku"

var sharedVarNameRe = regexp.MustCompile(`^(?:/\*[^*]*\*/)?var ([A-Za-z_$][\w$]*)=`)

// pageLevelNames are the names the page script declares around the
// component scopes. A template runs outside the page script, so a scope that
// reads one of them cannot be shared.
func pageLevelNames(result *EmitResult) map[string]bool {
	names := map[string]bool{
		"__ko": true, "__km": true, "__krateOwnF": true, SharedScopeArg: true,
		// Code a template must not move away from its call site.
		"arguments": true, "eval": true, "this": true,
	}
	for _, decl := range result.SharedModuleVars {
		if m := sharedVarNameRe.FindStringSubmatch(decl); m != nil {
			names[m[1]] = true
		} else {
			// An unknown declaration shape: every name it mentions counts.
			irtree.ScanJSIdentsAt(decl, func(n string, _ int) { names[n] = true })
		}
	}
	for _, fn := range result.ListComponents {
		if fn != nil && fn.Name != "" {
			names[fn.Name] = true
		}
	}
	if result.Share != nil {
		for n := range result.Share.Reserved {
			names[n] = true
		}
	}
	return names
}

// shareBodies replaces the bodies of eligible scopes with template calls and
// records the templates on result.
func shareBodies(result *EmitResult, bodies []string, skip map[int]bool) {
	var lazyRoots []string
	for _, sig := range result.Signatures {
		if sig.Lazy {
			lazyRoots = append(lazyRoots, string(sig.ComponentID))
		}
	}
	reserved := pageLevelNames(result)
	ids := instanceIDs(result.Signatures)
	seen := map[string]bool{}
	for i, sig := range result.Signatures {
		if skip[i] || bodies[i] == "" || !shareEligible(result, sig, lazyRoots) {
			continue
		}
		// Unused build-data declarations are dropped by the scope alone, as
		// the page script would have dropped them.
		body, marks := localMarks(dropUnusedPureVars(bodies[i]), result.SrcMarks)
		call, sc, ok := shareScope(body, marks, reserved, ids, sig.Source)
		if !ok {
			continue
		}
		bodies[i] = call
		if sc.ID != "" && !seen[sc.ID] {
			seen[sc.ID] = true
			result.SharedScopes = append(result.SharedScopes, sc)
		}
	}
}

var srcMarkRe = regexp.MustCompile(`/\*@k:(\d+)\*/`)

// localMarks renumbers the source markers of a scope body from the page's
// SrcMarks to the scope's own list, so a template does not depend on where
// its page put the marks.
func localMarks(body string, page []SrcMark) (string, []SrcMark) {
	if !strings.Contains(body, SrcMarkPrefix) {
		return body, nil
	}
	var marks []SrcMark
	body = srcMarkRe.ReplaceAllStringFunc(body, func(m string) string {
		n, err := strconv.Atoi(m[len(SrcMarkPrefix) : len(m)-2])
		if err != nil || n < 0 || n >= len(page) {
			return ""
		}
		marks = append(marks, page[n])
		return SrcMarkPrefix + strconv.Itoa(len(marks)-1) + "*/"
	})
	return body, marks
}

// instanceIDs are the slot and component ids of a page's scopes: the
// strings that differ between two instances of the same component.
func instanceIDs(sigs []irtree.ComponentSignature) map[string]bool {
	ids := map[string]bool{}
	add := func(id irtree.SlotID) {
		if id != "" {
			ids[string(id)] = true
		}
	}
	for _, sig := range sigs {
		add(sig.ComponentID)
		add(sig.ParentID)
		add(sig.ListSlot)
		for _, c := range sig.Children {
			add(c)
		}
		for _, c := range sig.Conds {
			add(c.SlotID)
		}
		for _, sb := range sig.SlotBindings {
			add(sb.SlotID)
		}
		for _, h := range sig.Handlers {
			add(h.ElementSlotID)
		}
		for _, a := range sig.AttrBindings {
			add(a.ElementSlotID)
		}
		for _, r := range sig.RefBindings {
			add(r.ElementSlotID)
		}
		for _, l := range sig.InLists {
			add(l)
		}
	}
	return ids
}

// shareEligible reports whether sig's scope may become a template. Lazy
// components and their descendants move to lazy chunks, boundary content
// runs through the boundary, and island prop providers read page state;
// those stay inline.
func shareEligible(result *EmitResult, sig irtree.ComponentSignature, lazyRoots []string) bool {
	opts := result.Share
	if opts == nil || sig.Tier != irtree.TierClient || sig.Source == "" {
		return false
	}
	if !opts.All && sig.Source == opts.PageFile {
		return false
	}
	if sig.Lazy || sig.Boundary != "" || len(sig.IslandBoot) > 0 || sigCodeSuspends(&sig) {
		return false
	}
	id := string(sig.ComponentID)
	for _, root := range lazyRoots {
		if strings.HasPrefix(id, root+".") {
			return false
		}
	}
	return true
}

// shareScope turns a scope body into a template call. It returns the call
// statement that replaces the body in the page script and the template.
func shareScope(body string, marks []SrcMark, reserved, ids map[string]bool, source string) (string, SharedScope, bool) {
	tpl, args, ok := templateScope(body, reserved, ids)
	if !ok {
		return "", SharedScope{}, false
	}
	if strings.TrimSpace(tpl) == "" {
		// Only unread data: the scope does nothing.
		return "", SharedScope{}, true
	}
	fn := "function(" + SharedScopeArg + "){\n" + tpl + "}"
	key := fn
	for _, m := range marks {
		key += "\n" + m.File + ":" + strconv.Itoa(m.Line) + ":" + strconv.Itoa(m.Col)
	}
	id := sharedScopeID(key)
	var call strings.Builder
	call.WriteString(sharedScopeCall)
	call.WriteString("(")
	call.WriteString(strconv.Quote(id))
	if len(args) > 0 {
		call.WriteString(",[")
		call.WriteString(strings.Join(args, ","))
		call.WriteString("]")
	}
	call.WriteString(");\n")
	return call.String(), SharedScope{ID: id, Source: source, JS: fn, Marks: marks}, true
}

// sharedScopeID is a short content hash of a template.
func sharedScopeID(js string) string {
	sum := sha256.Sum256([]byte(js))
	s := strconv.FormatUint(binary.BigEndian.Uint64(sum[:8]), 36)
	for len(s) < 9 {
		s = "0" + s
	}
	return s[:9]
}

// templateScope rewrites a scope body into a template body and the source of
// its arguments. Two kinds of values become arguments:
//
//   - string literals in expression position whose value is one of the
//     page's slot or component ids (ids): they differ per instance;
//   - array and object literals made only of literals on the right of a
//     top-level assignment that contains no function: folded build data
//     (tags, a table of contents) that differs per page. The argument list
//     is evaluated on every run of the scope, so each run still gets fresh
//     objects, as the inline literals did.
//
// A top-level declaration of such data that nothing reads is dropped. The
// body is rejected when it reads a name in reserved (declared by the page
// script around the scopes, or one whose meaning depends on the enclosing
// function), uses with or a class, or does not tokenize.
func templateScope(body string, reserved, ids map[string]bool) (string, []string, bool) {
	toks := lexer.NewTS(body).Tokenize()
	var sig []lexer.Token
	for _, t := range toks {
		switch t.Kind {
		case lexer.EOF:
			continue
		case lexer.Error:
			return "", nil, false
		}
		sig = append(sig, t)
	}
	kind := func(i int) lexer.Kind {
		if i < 0 || i >= len(sig) {
			return lexer.EOF
		}
		return sig[i].Kind
	}
	uses := map[string]int{}
	for i, t := range sig {
		if !isWordToken(t) || kind(i-1) == lexer.DOT || kind(i-1) == lexer.QUESTION_DOT {
			continue
		}
		if kind(i+1) == lexer.COLON && (kind(i-1) == lexer.LBRACE || kind(i-1) == lexer.COMMA) {
			continue // an object key or a label
		}
		if reserved[t.Value] || t.Kind == lexer.With_ || t.Kind == lexer.Class_ {
			return "", nil, false
		}
		uses[t.Value]++
	}

	// Literal groups to hoist and dead declarations to drop, as token index
	// ranges [start, end].
	hoist := map[int]int{}
	drop := map[int]int{}
	depth := 0
	stmtStart := 0
	for i := 0; i < len(sig); i++ {
		switch sig[i].Kind {
		case lexer.LBRACE, lexer.LPAREN, lexer.LBRACKET, lexer.TEMPLATE_START:
			depth++
		case lexer.RBRACE, lexer.RPAREN, lexer.RBRACKET, lexer.TEMPLATE_END:
			if sig[i].Kind != lexer.TEMPLATE_END || sig[i].Value[0] == '}' {
				depth--
			}
			if depth == 0 && sig[i].Kind == lexer.RBRACE {
				stmtStart = i + 1
			}
		case lexer.TEMPLATE_MID:
			// `}...${` closes one substitution and opens the next.
		case lexer.SEMI:
			if depth == 0 {
				stmtStart = i + 1
			}
		case lexer.ASSIGN:
			if depth != 0 || !hoistableStatement(sig[stmtStart:i]) {
				continue
			}
			end := rhsEnd(sig, i+1)
			if end < 0 {
				continue
			}
			// A declaration of pure data nothing reads is dropped, as the
			// minifier drops it from an inline scope; as an argument it would
			// still ship in the page script.
			if lhs := sig[stmtStart:i]; len(lhs) == 2 && lhs[0].Kind != lexer.Identifier && uses[lhs[1].Value] == 1 &&
				sig[end].Kind == lexer.SEMI && pureValue(sig, i+1, end) {
				drop[stmtStart] = end
				i = end - 1
				continue
			}
			findPureGroups(sig, i, end, hoist)
			i = end - 1
		}
	}

	var out strings.Builder
	var args []string
	argIndex := map[string]int{}
	// Equal strings share an argument; every array or object literal gets its
	// own, since each one evaluates to a distinct object.
	arg := func(src string, shared bool) string {
		n, ok := argIndex[src]
		if !ok || !shared {
			n = len(args)
			args = append(args, src)
			if shared {
				argIndex[src] = n
			}
		}
		return SharedScopeArg + "[" + strconv.Itoa(n) + "]"
	}
	pos := 0
	put := func(from, to int, repl string) {
		out.WriteString(body[pos:from])
		if from > 0 && isIdentByte(body[from-1]) {
			out.WriteByte(' ')
		}
		out.WriteString(repl)
		pos = to
	}
	for i := 0; i < len(sig); i++ {
		t := sig[i]
		if end, ok := drop[i]; ok {
			put(t.Offset, sig[end].End, "")
			i = end
			continue
		}
		if end, ok := hoist[i]; ok {
			put(t.Offset, sig[end].End, arg(body[t.Offset:sig[end].End], false))
			i = end
			continue
		}
		if t.Kind != lexer.String || !strings.HasPrefix(t.Value, `"`) || !stringIsValue(sig, i) {
			continue
		}
		if v, err := strconv.Unquote(t.Value); err != nil || !ids[v] {
			continue
		}
		put(t.Offset, t.End, arg(t.Value, true))
	}
	out.WriteString(body[pos:])
	return out.String(), args, true
}

// stringIsValue reports whether the string token at i is an expression that
// may be replaced by another expression: not an object key, method or class
// member name, assignment target, module specifier or directive.
func stringIsValue(sig []lexer.Token, i int) bool {
	prev, next := lexer.EOF, lexer.EOF
	if i > 0 {
		prev = sig[i-1].Kind
	}
	if i+1 < len(sig) {
		next = sig[i+1].Kind
	}
	switch next {
	case lexer.COLON:
		if prev == lexer.LBRACE || prev == lexer.COMMA {
			return false
		}
	case lexer.LPAREN, lexer.ASSIGN:
		return false
	}
	switch prev {
	case lexer.From, lexer.Import, lexer.Export:
		return false
	}
	return !strings.HasPrefix(sig[i].Value, `"use `)
}

// hoistableStatement reports whether the tokens before a top-level `=` are
// a plain declaration or assignment target (var x, a.b, a["k"]) rather than
// a control statement, a function or a destructuring pattern.
func hoistableStatement(lhs []lexer.Token) bool {
	if len(lhs) == 0 {
		return false
	}
	switch lhs[0].Kind {
	case lexer.Var_, lexer.Let_, lexer.Const_:
		return len(lhs) == 2 && lhs[1].Kind == lexer.Identifier
	case lexer.Identifier:
	default:
		return false
	}
	for _, t := range lhs[1:] {
		switch t.Kind {
		case lexer.Identifier, lexer.DOT, lexer.LBRACKET, lexer.RBRACKET, lexer.String, lexer.Number:
		default:
			return false
		}
	}
	return true
}

// rhsEnd returns the index of the `;` or `,` that ends the right-hand side
// starting at from, or -1 when it contains a function, a class or a nested
// assignment (the hoisted literals must be evaluated once per scope run).
func rhsEnd(sig []lexer.Token, from int) int {
	depth := 0
	for i := from; i < len(sig); i++ {
		switch sig[i].Kind {
		case lexer.Function, lexer.ARROW, lexer.Class_, lexer.ASSIGN, lexer.Yield_, lexer.Await_,
			lexer.TEMPLATE_START, lexer.TEMPLATE_MID:
			return -1
		case lexer.LBRACE, lexer.LPAREN, lexer.LBRACKET:
			depth++
		case lexer.RBRACE, lexer.RPAREN, lexer.RBRACKET:
			depth--
			if depth < 0 {
				return -1
			}
		case lexer.SEMI, lexer.COMMA:
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// pureValue reports whether the tokens [from, end) are one literal: a
// scalar or an array or object literal of pure data.
func pureValue(sig []lexer.Token, from, end int) bool {
	switch {
	case end == from+1:
		switch sig[from].Kind {
		case lexer.String, lexer.Number, lexer.True, lexer.False, lexer.Null_:
			return true
		}
		return false
	case end > from+1 && (sig[from].Kind == lexer.LBRACKET || sig[from].Kind == lexer.LBRACE):
		return pureLiteralEnd(sig, from) == end-1
	}
	return false
}

// findPureGroups records the maximal non-empty array and object literals of
// pure data in the right-hand side (eq, end) whose bracket opens a value.
func findPureGroups(sig []lexer.Token, eq, end int, hoist map[int]int) {
	for i := eq + 1; i < end; i++ {
		k := sig[i].Kind
		if k != lexer.LBRACKET && k != lexer.LBRACE {
			continue
		}
		switch sig[i-1].Kind {
		case lexer.ASSIGN, lexer.COLON, lexer.COMMA, lexer.LBRACKET, lexer.LPAREN:
		default:
			continue
		}
		if close := pureLiteralEnd(sig, i); close > i+1 && close < end {
			hoist[i] = close
			i = close
		}
	}
}

// pureLiteralEnd returns the index of the bracket closing the array or object
// literal that opens at i when it holds only literal values, else -1.
func pureLiteralEnd(sig []lexer.Token, i int) int {
	type frame struct{ object bool }
	var stack []frame
	for j := i; j < len(sig); j++ {
		t := sig[j]
		prev := sig[j-1].Kind
		inObject := len(stack) > 0 && stack[len(stack)-1].object
		switch t.Kind {
		case lexer.LBRACKET, lexer.LBRACE:
			if j > i && inObject && prev != lexer.COLON {
				return -1 // a computed key
			}
			stack = append(stack, frame{object: t.Kind == lexer.LBRACE})
		case lexer.RBRACKET, lexer.RBRACE:
			if len(stack) == 0 || stack[len(stack)-1].object != (t.Kind == lexer.RBRACE) {
				return -1
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return j
			}
		case lexer.COMMA, lexer.COLON:
		case lexer.String, lexer.Number, lexer.True, lexer.False, lexer.Null_:
			if inObject && (prev == lexer.LBRACE || prev == lexer.COMMA) {
				if j+1 >= len(sig) || sig[j+1].Kind != lexer.COLON {
					return -1
				}
			}
		case lexer.TEMPLATE_END:
			// A template without substitutions.
			if !strings.HasPrefix(t.Value, "`") || inObject && prev != lexer.COLON {
				return -1
			}
		case lexer.MINUS:
			if j+1 >= len(sig) || sig[j+1].Kind != lexer.Number || (prev != lexer.LBRACKET && prev != lexer.COMMA && prev != lexer.COLON) {
				return -1
			}
		default:
			// A word is allowed only as an object key (`{a: 1}`).
			if !isWordToken(t) || !inObject || (prev != lexer.LBRACE && prev != lexer.COMMA) ||
				j+1 >= len(sig) || sig[j+1].Kind != lexer.COLON {
				return -1
			}
		}
	}
	return -1
}

// isWordToken reports an identifier or keyword token.
func isWordToken(t lexer.Token) bool {
	if t.Value == "" {
		return false
	}
	c := t.Value[0]
	if !(c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80) {
		return false
	}
	switch t.Kind {
	case lexer.String, lexer.Number, lexer.Regexp:
		return false
	}
	return true
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}
