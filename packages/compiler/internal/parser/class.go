package parser

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

func (p *Parser) parseClassDecl() ast.Stmt {
	return p.parseClass(true)
}

// parseClass parses a class declaration or expression whose current token is
// `class`. A declaration may omit its name only after `export default`, which
// the caller handles; the parser accepts a missing name either way.
func (p *Parser) parseClass(isDecl bool) *ast.ClassDecl {
	start := p.expect(lexer.Class_)
	decl := &ast.ClassDecl{Position: tokPos(start)}
	savedStrict := p.strict
	p.strict = true // class bodies and heritage are strict mode code
	if p.identOK() && p.kind() != lexer.Implements_ {
		nt := p.next()
		p.checkBindingName(nt)
		p.checkEscapedIdent(nt)
		decl.Name = nt.Value
		if isDecl {
			p.declare(nt, nt.Value, declLexical)
		}
	} else if p.kind() == lexer.Yield_ || p.kind() == lexer.Await_ && p.inAsync {
		p.errWithMsg("`"+p.tok().Value+"` is not a valid class name", "")
		p.next()
	}
	p.skipTypeParams()
	if p.match(lexer.Extends_) {
		decl.SuperClass = p.parseLHS()
		if decl.SuperClass == nil {
			p.errWithMsg("expected a superclass after `extends`", "")
		}
		if p.kind() == lexer.LT && !p.tok().JSX {
			if c, ok := p.scanTypeArgs(p.cursor()); ok {
				p.applyCursor(c)
				p.droppedTypes++
			}
		}
	}
	if p.kind() == lexer.Implements_ {
		p.next()
		p.droppedTypes++
		for {
			p.skipType()
			if !p.match(lexer.COMMA) {
				break
			}
		}
	}
	if p.kind() != lexer.LBRACE {
		p.errWithMsg("expected `{` to start the class body, got "+describe(p.tok()), "")
		p.strict = savedStrict
		return decl
	}
	p.next()
	savedInClass := p.inClass
	p.inClass = true
	p.classes = append(p.classes, &privateScope{declared: map[string]string{}})
	hasCtor := false
	for p.kind() != lexer.RBRACE && p.kind() != lexer.EOF {
		before := p.pos
		if p.match(lexer.SEMI) {
			continue
		}
		m := p.parseClassMember(decl)
		if m != nil {
			if m.Kind == ast.ClassConstructor {
				if hasCtor {
					p.errAtPos(m.Position, "a class may only have one constructor", "merge the constructors")
				}
				hasCtor = true
			}
			decl.Members = append(decl.Members, m)
		}
		if p.pos == before {
			p.unexpected()
			p.next()
		}
	}
	p.expect(lexer.RBRACE)
	p.closePrivateScope()
	p.inClass = savedInClass
	p.strict = savedStrict
	return decl
}

// isMemberNameStart reports whether t can begin a class member name (used to
// tell a modifier keyword from a member named like one).
func isMemberNameStart(t lexer.Token) bool {
	switch t.Kind {
	case lexer.String, lexer.Number, lexer.LBRACKET, lexer.STAR:
		return true
	}
	return isWordKind(t.Kind)
}

func isTSMemberModifier(v string) bool {
	switch v {
	case "public", "private", "protected", "readonly", "abstract", "override", "declare":
		return true
	}
	return false
}

func (p *Parser) parseClassMember(decl *ast.ClassDecl) *ast.ClassMember {
	if p.kind() == lexer.AT {
		p.parseDecorators()
	}
	start := p.tok()
	m := &ast.ClassMember{Position: tokPos(start)}
	typeOnly := false
	accessor := false
	for {
		t := p.tok()
		n := p.at(1)
		switch {
		case t.Value == "static" && isWordKind(t.Kind) && n.Kind == lexer.LBRACE && !m.Static:
			// `static { ... }` initialization block.
			p.checkEscapedKeyword(t)
			p.next()
			saved := p.saveFnContext()
			p.inFunction, p.inAsync, p.inGenerator = true, false, false
			p.superProp, p.superCall, p.inFieldInit, p.inStaticBlock = true, false, true, true
			p.labels, p.breakable, p.loops = nil, 0, 0
			m.Kind = ast.ClassStaticBlock
			m.Static = true
			p.pushScope(true)
			m.Body = p.parseBlock()
			p.popScope()
			p.restoreFnContext(saved)
			return m
		case t.Value == "static" && isWordKind(t.Kind) && isMemberNameStart(n) && !m.Static:
			p.checkEscapedKeyword(t)
			p.next()
			m.Static = true
			continue
		case isWordKind(t.Kind) && isTSMemberModifier(t.Value) && isMemberNameStart(n) && !n.NewlineBefore:
			if t.Value == "abstract" || t.Value == "declare" {
				typeOnly = true
			}
			p.next()
			p.droppedTypes++
			continue
		case t.Value == "accessor" && isWordKind(t.Kind) && isMemberNameStart(n) && !n.NewlineBefore && n.Kind != lexer.STAR:
			p.next()
			accessor = true
			continue
		}
		break
	}
	isAsync, isGen, getter, setter := false, false, false, false
	if p.kind() == lexer.Async && (isMemberNameStart(p.at(1)) || p.peekN(1) == lexer.STAR) && !p.at(1).NewlineBefore {
		p.next()
		isAsync = true
	}
	if p.match(lexer.STAR) {
		isGen = true
	}
	if !isAsync && !isGen && (p.isContextual("get") || p.isContextual("set")) && isMemberNameStart(p.at(1)) && p.peekN(1) != lexer.STAR {
		getter = p.tok().Value == "get"
		setter = !getter
		p.next()
	}
	// TypeScript index signature `[key: string]: T;`.
	if p.kind() == lexer.LBRACKET && isWordKind(p.peekN(1)) && p.peekN(2) == lexer.COLON {
		p.skipBalanced()
		p.skipOptionalTypeAnnotation()
		p.consumeSemicolon()
		p.droppedTypes++
		return nil
	}
	keyTok := p.tok()
	key, ok := p.parsePropertyKey(true)
	if !ok {
		return nil
	}
	m.Name = key.name
	m.Computed, m.KeyExpr, m.KeyQuoted, m.KeyNumeric = key.computed, key.expr, key.quoted, key.numeric
	m.Private = key.private
	m.Async, m.Generator = isAsync, isGen
	isCtorName := !key.computed && !key.private && key.name == "constructor"
	if key.private && key.name == "#constructor" {
		p.errorAt(keyTok, "`#constructor` is not a valid private name", "")
	}
	// TypeScript optional (`x?`) and definite (`x!`) markers.
	if p.kind() == lexer.QUEST || p.kind() == lexer.NOT && !p.tok().NewlineBefore {
		p.next()
		p.droppedTypes++
	}
	if key.private {
		kind := "field"
		switch {
		case getter:
			kind = "get"
		case setter:
			kind = "set"
		}
		p.declarePrivate(keyTok, key.name, kind, m.Static)
	}
	if p.kind() == lexer.LPAREN || p.kind() == lexer.LT {
		fnTok := p.tok()
		p.derivedCtor = isCtorName && !m.Static && decl.SuperClass != nil
		params, body, hasBody := p.parseFunctionRest(isAsync, isGen, true)
		if !hasBody || typeOnly {
			// Overload signature or abstract method: type-only.
			p.droppedTypes++
			return nil
		}
		m.Params, m.Body = params, body
		switch {
		case getter:
			m.Kind = ast.ClassGetter
		case setter:
			m.Kind = ast.ClassSetter
		case isCtorName && !m.Static:
			m.Kind = ast.ClassConstructor
		default:
			m.Kind = ast.ClassMethod
		}
		p.checkAccessorParams(fnTok, getter, setter, params)
		if isCtorName && !m.Static && (getter || setter || isAsync || isGen) {
			p.errorAt(keyTok, "a class constructor cannot be a getter, setter, async or generator method", "")
		}
		if m.Static && !key.computed && key.name == "prototype" {
			p.errorAt(keyTok, "classes may not have a static member named `prototype`", "")
		}
		return m
	}
	if isAsync || isGen || getter || setter {
		p.errWithMsg("expected `(` after method name, got "+describe(p.tok()), "")
		return nil
	}
	// Field.
	if isCtorName || (!key.computed && key.name == "constructor" && key.quoted) {
		p.errorAt(keyTok, "classes may not have a field named `constructor`", "")
	}
	if m.Static && !key.computed && key.name == "prototype" {
		p.errorAt(keyTok, "classes may not have a static field named `prototype`", "")
	}
	m.Kind = ast.ClassField
	if accessor {
		m.Kind = ast.ClassAccessor
	}
	p.skipOptionalTypeAnnotation()
	if p.match(lexer.ASSIGN) {
		saved := p.saveFnContext()
		p.inFunction, p.inAsync, p.inGenerator = true, false, false
		p.superProp, p.superCall, p.inFieldInit = true, false, true
		m.Init = p.parseAssignAllowIn()
		p.restoreFnContext(saved)
		if m.Init == nil {
			p.errWithMsg("expected a field initializer after `=`", "")
		}
	}
	p.consumeSemicolon()
	if typeOnly {
		return nil
	}
	return m
}
