package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// Lexical scope analysis.
//
// Component signals, props and locals are declared at the top level of a
// component function. Any nested binding with the same name (a function or
// arrow parameter, a block-scoped let/const, a catch parameter, a for-of
// binding, a nested function declaration) shadows it. The builder walkers that
// classify expressions (referencesSignal, collectSignalReads, ...) consult a
// scope chain so a shadowed name is never treated as the component binding.

// bindingScope is one lexical scope level: the names it declares plus a link
// to the enclosing scope. A nil *bindingScope is the component top level,
// where nothing is shadowed.
type bindingScope struct {
	parent *bindingScope
	names  map[string]bool
}

// declares reports whether name is bound by this scope or any enclosing
// nested scope (i.e. whether it shadows a component-level binding).
func (s *bindingScope) declares(name string) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.names[name] {
			return true
		}
	}
	return false
}

// with returns a child scope declaring names. It returns s unchanged when
// names is empty so walkers do not allocate for binding-free nodes.
func (s *bindingScope) with(names []string) *bindingScope {
	if len(names) == 0 {
		return s
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		if n != "" {
			m[n] = true
		}
	}
	return &bindingScope{parent: s, names: m}
}

// paramBindingNames returns every name bound by a parameter list, including
// the members of destructuring patterns (`{a, b: c}` binds a and c) and rest
// parameters.
func paramBindingNames(params []*ast.Param) []string {
	var names []string
	for _, p := range params {
		if p == nil {
			continue
		}
		if p.Pattern != "" {
			names = append(names, destructuredParamNames(p.Pattern)...)
			continue
		}
		if p.Name != "" && p.Name != "{...}" {
			names = append(names, p.Name)
		}
	}
	return names
}

// varDeclBindingNames returns the names bound by one declarator.
func varDeclBindingNames(d *ast.VarDecl) []string {
	if d == nil {
		return nil
	}
	var names []string
	if d.Name != "" {
		names = append(names, d.Name)
	}
	names = append(names, d.Names...)
	if d.RestName != "" {
		names = append(names, d.RestName)
	}
	if d.Pattern != "" && len(d.Names) == 0 {
		names = append(names, destructuredParamNames(d.Pattern)...)
	}
	return names
}

// blockBindingNames returns the names a statement list declares at its own
// level: let/const/var declarators, function declarations and classes. `var`
// declarations nested in inner blocks hoist to the enclosing function, so
// they are included as well (over-approximating a shadow is safe: it only
// makes a name non-reactive inside the nested function that declares it).
func blockBindingNames(stmts []ast.Stmt) []string {
	var names []string
	var hoist func(stmts []ast.Stmt, top bool)
	hoist = func(stmts []ast.Stmt, top bool) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.VarStmt:
				if top || s.Kind == ast.VarVar {
					for _, d := range s.Decls {
						names = append(names, varDeclBindingNames(d)...)
					}
				}
			case *ast.FnDecl:
				if top {
					names = append(names, s.Name)
				}
			case *ast.ClassDecl:
				if top {
					names = append(names, s.Name)
				}
			case *ast.ExportStmt:
				if s.Declaration != nil {
					hoist([]ast.Stmt{s.Declaration}, top)
				}
			case *ast.BlockStmt:
				hoist(s.Body, false)
			case *ast.IfStmt:
				hoist(s.Consequent, false)
				hoist(s.Alternate, false)
			case *ast.ForStmt:
				if s.Init != nil {
					hoist([]ast.Stmt{s.Init}, false)
				}
				hoist(s.Body, false)
			case *ast.ForInStmt:
				hoist(s.Body, false)
			case *ast.WhileStmt:
				hoist(s.Body, false)
			case *ast.DoWhileStmt:
				hoist(s.Body, false)
			case *ast.SwitchStmt:
				for _, c := range s.Cases {
					if c != nil {
						hoist(c.Body, false)
					}
				}
			case *ast.TryStmt:
				hoist(s.Body, false)
				if s.Catch != nil {
					hoist(s.Catch.Body, false)
				}
				hoist(s.Finally, false)
			}
		}
	}
	hoist(stmts, true)
	return names
}

// functionScopeNames returns the names visible as locals inside a nested
// function: its parameters plus its body-level declarations.
func functionScopeNames(params []*ast.Param, body []ast.Stmt) []string {
	return append(paramBindingNames(params), blockBindingNames(body)...)
}

// forInBindingNames returns the loop binding of a for-in/for-of statement
// when it declares one (`for (const x of xs)`); a bare assignment target
// (`for (x of xs)`) binds nothing new.
func forInBindingNames(s *ast.ForInStmt) []string {
	if s == nil || s.Keyword == "" {
		return nil
	}
	return exprPatternNames(s.Left)
}

// exprPatternNames extracts identifier names from an expression used as a
// binding pattern (an identifier, or an array/object literal of them).
func exprPatternNames(e ast.Expr) []string {
	switch x := e.(type) {
	case *ast.Identifier:
		return []string{x.Name}
	case *ast.ArrayExpr:
		var names []string
		for _, el := range x.Elements {
			names = append(names, exprPatternNames(el)...)
		}
		return names
	case *ast.ObjectExpr:
		var names []string
		for _, p := range x.Properties {
			if p == nil {
				continue
			}
			if p.Value != nil {
				names = append(names, exprPatternNames(p.Value)...)
			} else if p.Key != "" {
				names = append(names, p.Key)
			}
		}
		return names
	case *ast.UnaryExpr:
		// `...rest` inside a pattern.
		return exprPatternNames(x.Arg)
	case *ast.BinaryExpr:
		// `x = default` inside a pattern binds x.
		if x.Op == "=" {
			return exprPatternNames(x.Left)
		}
	}
	return nil
}

// catchBindingNames returns the names bound by a catch clause parameter.
func catchBindingNames(c *ast.CatchClause) []string {
	if c == nil || c.Param == "" {
		return nil
	}
	if strings.HasPrefix(c.Param, "{") || strings.HasPrefix(c.Param, "[") {
		return destructuredParamNames(c.Param)
	}
	return []string{c.Param}
}

// isShadowed reports whether name is bound by a nested scope that is active
// for the walk in progress.
func (b *builder) isShadowed(name string) bool {
	return b.shadow.declares(name)
}

// pushShadow enters a nested scope declaring names and returns a function
// restoring the previous scope.
func (b *builder) pushShadow(names []string) func() {
	saved := b.shadow
	b.shadow = saved.with(names)
	return func() { b.shadow = saved }
}

// isSignalName reports whether name resolves to a component signal (or
// reactive getter) at the current walk position: it must be in the signal
// table and not shadowed by a nested binding.
func (b *builder) isSignalName(name string) bool {
	if name == "" {
		return false
	}
	if _, ok := b.sigMap()[name]; !ok {
		return false
	}
	return !b.isShadowed(name)
}

// JS identifier reference scanning.
//
// Several passes decide which helper functions, module constants or `props`
// bindings generated JS needs in scope. They operate on emitted JS text, so
// they use a small tokenizer instead of substring matching: identifiers inside
// string literals, template literal text, comments and property names
// (`x.name`, `x?.name`) are not references.

// jsIdentRefs returns the set of identifiers referenced by JS source code.
func jsIdentRefs(code string) map[string]bool {
	refs := make(map[string]bool)
	scanJSIdents(code, func(name string) { refs[name] = true })
	return refs
}

// scanJSIdents calls fn for every identifier token in code that is not a
// property name. Regex literals are recognized with the usual
// previous-token heuristic; a misclassification only ever causes a spurious
// reference (safe: the caller includes an extra declaration).
func scanJSIdents(code string, fn func(name string)) {
	n := len(code)
	// braceStack tracks template literal `${` nesting: true entries are
	// template substitutions, false entries are ordinary braces.
	var braceStack []bool
	// prev is the last significant character; prevWord the last word token.
	prev := byte(0)
	prevWord := ""
	afterDot := false
	inTemplate := false
	i := 0
	for i < n {
		if inTemplate {
			// Template literal text: skip until the closing backtick or a
			// `${` substitution.
			for i < n {
				c := code[i]
				if c == '\\' {
					i += 2
					continue
				}
				if c == '`' {
					i++
					inTemplate = false
					prev, prevWord, afterDot = ')', "", false
					break
				}
				if c == '$' && i+1 < n && code[i+1] == '{' {
					i += 2
					braceStack = append(braceStack, true)
					inTemplate = false
					prev, prevWord, afterDot = '{', "", false
					break
				}
				i++
			}
			continue
		}
		c := code[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '/' && i+1 < n && code[i+1] == '/':
			for i < n && code[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && code[i+1] == '*':
			end := strings.Index(code[i+2:], "*/")
			if end < 0 {
				return
			}
			i += end + 4
		case c == '"' || c == '\'':
			i++
			for i < n && code[i] != c {
				if code[i] == '\\' {
					i++
				}
				i++
			}
			i++
			prev, prevWord, afterDot = '"', "", false
		case c == '`':
			i++
			inTemplate = true
		case c == '/' && regexAllowedAfter(prev, prevWord):
			i++
			inClass := false
			for i < n {
				ch := code[i]
				if ch == '\\' {
					i += 2
					continue
				}
				if ch == '\n' {
					break
				}
				if ch == '[' {
					inClass = true
				} else if ch == ']' {
					inClass = false
				} else if ch == '/' && !inClass {
					i++
					break
				}
				i++
			}
			for i < n && isIdentChar(code[i]) {
				i++
			}
			prev, prevWord, afterDot = ')', "", false
		case isIdentStart(c):
			start := i
			for i < n && (isIdentChar(code[i]) || code[i] >= 0x80) {
				i++
			}
			word := code[start:i]
			if !afterDot {
				fn(word)
			}
			prev, prevWord, afterDot = 'a', word, false
		case c >= '0' && c <= '9':
			for i < n && (isIdentChar(code[i]) || code[i] == '.') {
				i++
			}
			prev, prevWord, afterDot = '0', "", false
		case c == '.':
			if i+2 < n && code[i+1] == '.' && code[i+2] == '.' {
				i += 3
				prev, prevWord, afterDot = '.', "", false
				continue
			}
			i++
			afterDot = true
			prev, prevWord = '.', ""
		case c == '?' && i+1 < n && code[i+1] == '.' && !(i+2 < n && code[i+2] >= '0' && code[i+2] <= '9'):
			i += 2
			// `a?.(x)` and `a?.[x]` are calls/indexing, not property names.
			afterDot = i < n && code[i] != '(' && code[i] != '['
			prev, prevWord = '.', ""
		case c == '{':
			braceStack = append(braceStack, false)
			i++
			prev, prevWord, afterDot = c, "", false
		case c == '}':
			i++
			if len(braceStack) > 0 {
				top := braceStack[len(braceStack)-1]
				braceStack = braceStack[:len(braceStack)-1]
				if top {
					inTemplate = true
					continue
				}
			}
			prev, prevWord, afterDot = c, "", false
		default:
			i++
			prev, prevWord, afterDot = c, "", false
		}
	}
}

func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '$' || c >= 0x80
}

// regexAllowedAfter reports whether a `/` following prev/prevWord starts a
// regular expression literal rather than a division.
func regexAllowedAfter(prev byte, prevWord string) bool {
	if prevWord != "" {
		switch prevWord {
		case "return", "typeof", "instanceof", "in", "of", "new", "delete", "void", "throw", "case", "do", "else", "yield", "await":
			return true
		}
		return false
	}
	switch prev {
	case 0, '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '+', '-', '*', '%', '<', '>', '~', '^':
		return true
	}
	return false
}
