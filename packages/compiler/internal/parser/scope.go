package parser

import (
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// Declaration scopes for the redeclaration early errors (`let a; let a;`,
// `let a; var a;`, `function f(a) { let a; }`). Only names are tracked.

type declKind int

const (
	declVar declKind = iota
	declLexical
	declParam
	declCatchParam
	// declFunction is a function declaration: var-like at function/program
	// top level, lexical inside blocks (where sloppy mode allows duplicates).
	declFunction
	// declVarPass marks a `var` name hoisted through a block scope, so a
	// later lexical declaration of the same name in that block conflicts.
	declVarPass
)

type scope struct {
	parent *scope
	fn     bool // function, program or static-block scope (var target)
	names  map[string]declKind
}

func (p *Parser) pushScope(fn bool) {
	p.scope = &scope{parent: p.scope, fn: fn}
}

func (p *Parser) popScope() {
	if p.scope != nil {
		p.scope = p.scope.parent
	}
}

func (s *scope) lookup(name string) (declKind, bool) {
	if s.names == nil {
		return 0, false
	}
	k, ok := s.names[name]
	return k, ok
}

func (s *scope) set(name string, k declKind) {
	if s.names == nil {
		s.names = map[string]declKind{}
	}
	s.names[name] = k
}

func (p *Parser) redeclared(t lexer.Token, name string) {
	p.errorAt(t, "identifier `"+name+"` has already been declared", "rename one of the declarations")
}

// declare records a binding in the current scope and reports conflicts.
func (p *Parser) declare(t lexer.Token, name string, kind declKind) {
	s := p.scope
	if s == nil || name == "" {
		return
	}
	switch kind {
	case declVar:
		for cur := s; cur != nil; cur = cur.parent {
			if k, ok := cur.lookup(name); ok {
				switch {
				case k == declLexical:
					p.redeclared(t, name)
					return
				case k == declFunction && !cur.fn:
					p.redeclared(t, name)
					return
				}
			}
			if cur.fn {
				if _, ok := cur.lookup(name); !ok {
					cur.set(name, declVar)
				}
				return
			}
			if _, ok := cur.lookup(name); !ok {
				cur.set(name, declVarPass)
			}
		}
	case declFunction:
		if k, ok := s.lookup(name); ok {
			switch {
			case s.fn && (k == declVar || k == declFunction || k == declParam):
				// Var-like at function/program level: duplicates allowed.
				return
			case !s.fn && k == declFunction && !p.strict:
				// Annex B: sloppy duplicate block-level functions.
				return
			}
			p.redeclared(t, name)
			return
		}
		s.set(name, declFunction)
	case declLexical:
		if _, ok := s.lookup(name); ok {
			p.redeclared(t, name)
			return
		}
		s.set(name, declLexical)
	case declParam, declCatchParam:
		s.set(name, kind)
	}
}

// declareNames declares every name bound by a declaration.
func (p *Parser) declareNames(t lexer.Token, names []string, kind declKind) {
	for _, n := range names {
		p.declare(t, n, kind)
	}
}
