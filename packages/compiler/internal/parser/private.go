package parser

import (
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// privateScope tracks the private names (`#x`) declared in one class body and
// the references made inside it, so undeclared and duplicate names are
// reported.
type privateScope struct {
	declared map[string]string // name -> "field", "get", "set", "get+set"
	static   map[string]bool
	refs     []lexer.Token
}

func (p *Parser) declarePrivate(t lexer.Token, name, kind string, static bool) {
	if len(p.classes) == 0 {
		return
	}
	cs := p.classes[len(p.classes)-1]
	if cs.static == nil {
		cs.static = map[string]bool{}
	}
	prev, exists := cs.declared[name]
	switch {
	case !exists:
		cs.declared[name] = kind
		cs.static[name] = static
	case (prev == "get" && kind == "set" || prev == "set" && kind == "get") && cs.static[name] == static:
		cs.declared[name] = "get+set"
	default:
		p.errorAt(t, "duplicate private name `"+name+"`", "each private member needs a unique name (a getter and setter pair may share one)")
	}
}

// refPrivate records a reference to a private name.
func (p *Parser) refPrivate(t lexer.Token) {
	if len(p.classes) == 0 {
		p.errorAt(t, "private name `"+t.Value+"` is not defined", "private names can only be used inside the class that declares them")
		return
	}
	cs := p.classes[len(p.classes)-1]
	cs.refs = append(cs.refs, t)
}

// closePrivateScope resolves the innermost class body's references; names
// not declared there are resolved against the enclosing class.
func (p *Parser) closePrivateScope() {
	n := len(p.classes)
	if n == 0 {
		return
	}
	cs := p.classes[n-1]
	p.classes = p.classes[:n-1]
	for _, r := range cs.refs {
		if _, ok := cs.declared[r.Value]; ok {
			continue
		}
		if n > 1 {
			parent := p.classes[n-2]
			parent.refs = append(parent.refs, r)
			continue
		}
		p.errorAt(r, "private name `"+r.Value+"` is not defined", "declare it in the class body, e.g. `"+r.Value+" = 0;`")
	}
}

// reservedWords are the words that may never be identifiers.
var reservedWords = map[string]bool{
	"break": true, "case": true, "catch": true, "class": true, "const": true, "continue": true,
	"debugger": true, "default": true, "delete": true, "do": true, "else": true, "enum": true,
	"export": true, "extends": true, "false": true, "finally": true, "for": true, "function": true,
	"if": true, "import": true, "in": true, "instanceof": true, "new": true, "null": true,
	"return": true, "super": true, "switch": true, "this": true, "throw": true, "true": true,
	"try": true, "typeof": true, "var": true, "void": true, "while": true, "with": true,
}

// checkEscapedIdent reports an identifier spelled with escapes that decodes
// to a reserved word (or to await/yield where those are reserved).
func (p *Parser) checkEscapedIdent(t lexer.Token) {
	if !t.Escaped {
		return
	}
	bad := reservedWords[t.Value] ||
		p.strict && strictReserved[t.Value] ||
		t.Value == "await" && (p.inAsync || p.inStaticBlock) ||
		t.Value == "yield" && (p.inGenerator || p.strict)
	if bad {
		p.errorAt(t, "keyword `"+t.Value+"` must not contain escape sequences", "write the word without \\u escapes")
	}
}

// checkEscapedKeyword reports a contextual keyword (static, get, async, ...)
// written with escapes and used as a keyword.
func (p *Parser) checkEscapedKeyword(t lexer.Token) {
	if t.Escaped {
		p.errorAt(t, "keyword `"+t.Value+"` must not contain escape sequences", "write the word without \\u escapes")
	}
}
