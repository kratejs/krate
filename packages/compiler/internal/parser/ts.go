package parser

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// TypeScript type skipping.
//
// Types are scanned without building an AST. The scanner works on a cursor
// (token index plus the number of `>` characters already consumed from that
// token) so a compound token such as `>>`, `>>>`, `>=` or `>>=` can close
// nested type-argument lists: `Map<string, Set<string>>` ends in one `>>`.
// When a skip finishes inside such a token, the token is split in place.

type tcur struct {
	i int // token index
	k int // `>` characters already consumed from tokens[i]
}

func (p *Parser) cursor() tcur { return tcur{i: p.pos} }

// rest returns the unconsumed text of the token under c.
func (p *Parser) rest(c tcur) string {
	return p.tokens[c.i].Value[c.k:]
}

// vkind returns the kind of the (possibly partial) token under c.
func (p *Parser) vkind(c tcur) lexer.Kind {
	if c.k == 0 {
		return p.tokens[c.i].Kind
	}
	return gtKind(p.rest(c))
}

func gtKind(s string) lexer.Kind {
	switch s {
	case ">":
		return lexer.GT
	case ">>":
		return lexer.SHR
	case ">>>":
		return lexer.USHIFT_RIGHT
	case ">=":
		return lexer.GTE
	case ">>=":
		return lexer.SHR_ASSIGN
	case ">>>=":
		return lexer.USHR_ASSIGN
	case "=":
		return lexer.ASSIGN
	}
	return lexer.Error
}

func (p *Parser) adv(c tcur) tcur {
	if c.i < len(p.tokens)-1 {
		return tcur{i: c.i + 1}
	}
	return tcur{i: c.i}
}

func (p *Parser) vtok(c tcur) lexer.Token { return p.tokens[c.i] }

// closeAngle consumes one `>` at c.
func (p *Parser) closeAngle(c tcur) (tcur, bool) {
	r := p.rest(c)
	if len(r) == 0 || r[0] != '>' {
		return c, false
	}
	if len(r) == 1 {
		return p.adv(c), true
	}
	return tcur{i: c.i, k: c.k + 1}, true
}

// applyCursor moves the parser to c, splitting a partially consumed token.
func (p *Parser) applyCursor(c tcur) {
	if c.k > 0 {
		t := p.tokens[c.i]
		r := t.Value[c.k:]
		t.Kind = gtKind(r)
		t.Value = r
		t.Offset += c.k
		t.Col += c.k
		t.NewlineBefore = false
		p.tokens[c.i] = t
	}
	p.pos = c.i
}

// skipType skips a type at the current position, reporting an error if no
// type is found.
func (p *Parser) skipType() {
	c, ok := p.scanType(p.cursor())
	if !ok {
		p.errWithMsg("expected a type, got "+describe(p.tok()), "")
		return
	}
	p.applyCursor(c)
}

// skipTypeAnnotation skips `: Type` at the current position.
func (p *Parser) skipTypeAnnotation() {
	if p.kind() != lexer.COLON {
		return
	}
	p.next()
	p.droppedTypes++
	p.skipType()
}

func (p *Parser) skipOptionalTypeAnnotation() {
	if p.kind() == lexer.COLON {
		p.skipTypeAnnotation()
	}
}

// skipReturnType skips a function return type annotation.
func (p *Parser) skipReturnType() {
	if p.kind() == lexer.COLON {
		p.skipTypeAnnotation()
	}
}

// skipTypeParams skips `<T, U extends X = Y>` if present.
func (p *Parser) skipTypeParams() {
	if p.kind() != lexer.LT || p.tok().JSX {
		return
	}
	c, ok := p.scanTypeParams(p.cursor())
	if !ok {
		p.errWithMsg("malformed type parameter list", "")
		p.next()
		return
	}
	p.droppedTypes++
	p.applyCursor(c)
}

// skipTypeAliasDecl skips `type Name<T> = Type;`.
func (p *Parser) skipTypeAliasDecl() ast.Stmt {
	p.next() // type
	p.next() // name
	p.droppedTypes++
	if p.kind() == lexer.LT {
		if c, ok := p.scanTypeParams(p.cursor()); ok {
			p.applyCursor(c)
		}
	}
	if !p.match(lexer.ASSIGN) {
		p.errWithMsg("expected `=` in type alias", "")
		p.skipToStatementEnd()
		return nil
	}
	p.skipType()
	p.consumeSemicolon()
	return nil
}

// skipInterfaceDecl skips `interface Name<T> extends A, B { ... }`.
func (p *Parser) skipInterfaceDecl() ast.Stmt {
	p.next() // interface
	p.droppedTypes++
	if isWordKind(p.kind()) {
		p.next()
	}
	if p.kind() == lexer.LT {
		if c, ok := p.scanTypeParams(p.cursor()); ok {
			p.applyCursor(c)
		}
	}
	if p.match(lexer.Extends_) {
		for {
			p.skipType()
			if !p.match(lexer.COMMA) {
				break
			}
		}
	}
	if p.kind() != lexer.LBRACE {
		p.errWithMsg("expected `{` to start the interface body", "")
		return nil
	}
	p.skipBalanced()
	return nil
}

// scanType scans one type starting at c (without consuming it).
func (p *Parser) scanType(c tcur) (tcur, bool) {
	p.enter()
	defer p.leave()
	c, ok := p.scanUnion(c)
	if !ok {
		return c, false
	}
	// Conditional type `A extends B ? C : D`.
	if p.vkind(c) == lexer.Extends_ && !p.vtok(c).NewlineBefore {
		c2, ok := p.scanUnion(p.adv(c))
		if !ok || p.vkind(c2) != lexer.QUEST {
			return c, true
		}
		c3, ok := p.scanType(p.adv(c2))
		if !ok || p.vkind(c3) != lexer.COLON {
			return c, false
		}
		return p.scanType(p.adv(c3))
	}
	return c, true
}

func (p *Parser) scanUnion(c tcur) (tcur, bool) {
	if k := p.vkind(c); k == lexer.BIT_OR || k == lexer.BIT_AND {
		c = p.adv(c)
	}
	c, ok := p.scanPostfixType(c)
	if !ok {
		return c, false
	}
	for {
		k := p.vkind(c)
		if k != lexer.BIT_OR && k != lexer.BIT_AND {
			return c, true
		}
		c, ok = p.scanPostfixType(p.adv(c))
		if !ok {
			return c, false
		}
	}
}

func (p *Parser) scanPostfixType(c tcur) (tcur, bool) {
	c, ok := p.scanPrimaryType(c)
	if !ok {
		return c, false
	}
	for p.vkind(c) == lexer.LBRACKET && c.k == 0 && !p.vtok(c).NewlineBefore {
		m := p.pair[c.i]
		if m < 0 {
			return c, false
		}
		c = tcur{i: m + 1}
	}
	return c, true
}

// scanEntityName scans `A.B.C` and optional type arguments.
func (p *Parser) scanTypeReference(c tcur) (tcur, bool) {
	c = p.adv(c)
	for p.vkind(c) == lexer.DOT && isWordKind(p.vkind(p.adv(c))) {
		c = p.adv(p.adv(c))
	}
	if p.vkind(c) == lexer.LT && c.k == 0 && !p.vtok(c).NewlineBefore {
		return p.scanTypeArgs(c)
	}
	return c, true
}

func (p *Parser) scanPrimaryType(c tcur) (tcur, bool) {
	if c.k != 0 {
		return c, false
	}
	t := p.vtok(c)
	switch t.Kind {
	case lexer.LPAREN:
		m := p.pair[c.i]
		if m < 0 {
			return c, false
		}
		after := tcur{i: m + 1}
		if p.vkind(after) == lexer.ARROW {
			return p.scanType(p.adv(after))
		}
		return after, true
	case lexer.LT:
		// Generic function type `<T>(x: T) => T`.
		c2, ok := p.scanTypeParams(c)
		if !ok || p.vkind(c2) != lexer.LPAREN {
			return c, false
		}
		return p.scanPrimaryType(c2)
	case lexer.New_:
		c = p.adv(c)
		if p.vkind(c) == lexer.LT {
			var ok bool
			if c, ok = p.scanTypeParams(c); !ok {
				return c, false
			}
		}
		if p.vkind(c) != lexer.LPAREN {
			return c, false
		}
		return p.scanPrimaryType(c)
	case lexer.LBRACE, lexer.LBRACKET:
		m := p.pair[c.i]
		if m < 0 {
			return c, false
		}
		return tcur{i: m + 1}, true
	case tmplNoSub:
		return p.adv(c), true
	case lexer.TEMPLATE_START:
		depth := 0
		for i := c.i; i < len(p.tokens); i++ {
			switch p.tokens[i].Kind {
			case lexer.TEMPLATE_START:
				depth++
			case lexer.TEMPLATE_END:
				depth--
				if depth == 0 {
					return tcur{i: i + 1}, true
				}
			case lexer.EOF:
				return c, false
			}
		}
		return c, false
	case lexer.Typeof_:
		c = p.adv(c)
		if p.vkind(c) == lexer.Import {
			return p.scanPrimaryType(c)
		}
		if !isWordKind(p.vkind(c)) {
			return c, false
		}
		return p.scanTypeReference(c)
	case lexer.Import:
		c = p.adv(c)
		if p.vkind(c) != lexer.LPAREN {
			return c, false
		}
		c = tcur{i: p.pair[c.i] + 1}
		if c.i == 0 {
			return c, false
		}
		for p.vkind(c) == lexer.DOT && isWordKind(p.vkind(p.adv(c))) {
			c = p.adv(p.adv(c))
		}
		if p.vkind(c) == lexer.LT {
			return p.scanTypeArgs(c)
		}
		return c, true
	case lexer.MINUS:
		c = p.adv(c)
		if p.vkind(c) != lexer.Number {
			return c, false
		}
		return p.adv(c), true
	case lexer.String, lexer.Number, lexer.True, lexer.False, lexer.Null_:
		return p.adv(c), true
	case lexer.Readonly_:
		return p.scanPostfixType(p.adv(c))
	case lexer.This_, lexer.Void_, lexer.Undefined:
		c = p.adv(c)
		return p.scanPredicate(c)
	}
	if !isWordKind(t.Kind) {
		return c, false
	}
	switch t.Value {
	case "keyof", "unique":
		if n := p.vtok(p.adv(c)); isWordKind(n.Kind) || n.Kind == lexer.LPAREN || n.Kind == lexer.LBRACKET || n.Kind == lexer.LBRACE {
			return p.scanPostfixType(p.adv(c))
		}
	case "infer":
		n := p.adv(c)
		if isWordKind(p.vkind(n)) {
			c = p.adv(n)
			if p.vkind(c) == lexer.Extends_ {
				// `infer U extends X` constraint (only when not a conditional).
				if c2, ok := p.scanUnion(p.adv(c)); ok && p.vkind(c2) != lexer.QUEST {
					return c2, true
				}
			}
			return c, true
		}
	case "asserts":
		n := p.adv(c)
		if isWordKind(p.vkind(n)) && !p.vtok(n).NewlineBefore {
			c = p.adv(n)
			return p.scanPredicate(c)
		}
	case "abstract":
		if p.vkind(p.adv(c)) == lexer.New_ {
			return p.scanPrimaryType(p.adv(c))
		}
	}
	c, ok := p.scanTypeReference(c)
	if !ok {
		return c, false
	}
	return p.scanPredicate(c)
}

// scanPredicate skips an `is T` type predicate following a parameter name.
func (p *Parser) scanPredicate(c tcur) (tcur, bool) {
	t := p.vtok(c)
	if c.k == 0 && t.Value == "is" && isWordKind(t.Kind) && !t.NewlineBefore {
		return p.scanType(p.adv(c))
	}
	return c, true
}

type scanResult struct {
	end tcur
	ok  bool
}

// scanTypeArgs scans `<A, B>` starting at the `<`. Results are memoized per
// start token so speculative scans (`a < b < c ...`) stay linear.
func (p *Parser) scanTypeArgs(c tcur) (tcur, bool) {
	if p.vkind(c) != lexer.LT || c.k != 0 {
		return c, false
	}
	if r, ok := p.typeArgsMemo[c.i]; ok {
		return r.end, r.ok
	}
	end, ok := p.scanTypeArgsAt(c)
	if p.typeArgsMemo == nil {
		p.typeArgsMemo = map[int]scanResult{}
	}
	p.typeArgsMemo[c.i] = scanResult{end, ok}
	return end, ok
}

func (p *Parser) scanTypeArgsAt(c tcur) (tcur, bool) {
	c = p.adv(c)
	for {
		var ok bool
		c, ok = p.scanType(c)
		if !ok {
			return c, false
		}
		if p.vkind(c) == lexer.COMMA {
			c = p.adv(c)
			if r := p.rest(c); len(r) > 0 && r[0] == '>' {
				return p.closeAngle(c)
			}
			continue
		}
		return p.closeAngle(c)
	}
}

// scanTypeParams scans `<const T extends X = Y, ...>` starting at the `<`.
func (p *Parser) scanTypeParams(c tcur) (tcur, bool) {
	if p.vkind(c) != lexer.LT || c.k != 0 {
		return c, false
	}
	c = p.adv(c)
	for {
		// Modifiers: const, in, out.
		for {
			t := p.vtok(c)
			if (t.Kind == lexer.Const_ || t.Kind == lexer.In_ || t.Value == "out") && isWordKind(p.vkind(p.adv(c))) {
				c = p.adv(c)
				continue
			}
			break
		}
		if !isWordKind(p.vkind(c)) || c.k != 0 {
			return c, false
		}
		c = p.adv(c)
		var ok bool
		if p.vkind(c) == lexer.Extends_ {
			if c, ok = p.scanType(p.adv(c)); !ok {
				return c, false
			}
		}
		if p.vkind(c) == lexer.ASSIGN {
			if c, ok = p.scanType(p.adv(c)); !ok {
				return c, false
			}
		}
		if p.vkind(c) == lexer.COMMA {
			c = p.adv(c)
			if r := p.rest(c); len(r) > 0 && r[0] == '>' {
				return p.closeAngle(c)
			}
			continue
		}
		return p.closeAngle(c)
	}
}
