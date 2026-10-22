package parser

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// parseBindingTarget parses a BindingIdentifier or a destructuring binding
// pattern. Patterns use the cover-grammar shape shared with destructuring
// assignment: ObjectExpr/ArrayExpr whose defaults are BinaryExpr{Op: "="},
// rest elements UnaryExpr{Op: "..."} and array holes nil. It returns the
// bound names in source order and the name bound by a rest element (if
// any).
func (p *Parser) parseBindingTarget() (ast.Expr, []string, string) {
	var names []string
	rest := ""
	e := p.bindingTarget(&names, &rest)
	return e, names, rest
}

func (p *Parser) bindingTarget(names *[]string, rest *string) ast.Expr {
	p.enter()
	defer p.leave()
	t := p.tok()
	switch t.Kind {
	case lexer.LBRACKET:
		return p.arrayBindingPattern(names, rest)
	case lexer.LBRACE:
		return p.objectBindingPattern(names, rest)
	}
	if p.identOK() {
		p.next()
		p.checkBindingName(t)
		p.checkEscapedIdent(t)
		*names = append(*names, t.Value)
		return &ast.Identifier{Position: tokPos(t), Name: t.Value}
	}
	return nil
}

// bindingElement parses a target with an optional `= default`.
func (p *Parser) bindingElement(names *[]string, rest *string) ast.Expr {
	target := p.bindingTarget(names, rest)
	if target == nil {
		p.errWithMsg("expected a binding name or pattern, got "+describe(p.tok()), "")
		return nil
	}
	if p.kind() == lexer.ASSIGN {
		p.next()
		def := p.parseAssignAllowIn()
		if def == nil {
			p.errWithMsg("expected a default value after `=`", "")
			return target
		}
		return &ast.BinaryExpr{Position: target.Pos(), Left: target, Op: "=", Right: def}
	}
	return target
}

func (p *Parser) arrayBindingPattern(names *[]string, rest *string) ast.Expr {
	open := p.next()
	arr := &ast.ArrayExpr{Position: tokPos(open)}
	for p.kind() != lexer.RBRACKET && p.kind() != lexer.EOF {
		if p.kind() == lexer.COMMA {
			p.next()
			arr.Elements = append(arr.Elements, nil)
			continue
		}
		if p.kind() == lexer.SPREAD {
			st := p.next()
			nameTok := p.tok()
			target := p.bindingTarget(names, rest)
			if target == nil {
				p.errWithMsg("expected a binding after `...`", "")
				break
			}
			if _, ok := target.(*ast.Identifier); ok {
				*rest = nameTok.Value
			}
			arr.Elements = append(arr.Elements, &ast.UnaryExpr{Position: tokPos(st), Op: "...", Arg: target})
			if p.kind() == lexer.ASSIGN {
				p.errWithMsg("a rest element cannot have a default value", "")
			}
			if p.kind() == lexer.COMMA {
				p.errWithMsg("a rest element must be last in a destructuring pattern", "remove the trailing elements or comma")
			}
			break
		}
		el := p.bindingElement(names, rest)
		if el == nil {
			break
		}
		arr.Elements = append(arr.Elements, el)
		if !p.match(lexer.COMMA) {
			break
		}
	}
	p.expect(lexer.RBRACKET)
	return arr
}

func (p *Parser) objectBindingPattern(names *[]string, rest *string) ast.Expr {
	open := p.next()
	obj := &ast.ObjectExpr{Position: tokPos(open)}
	for p.kind() != lexer.RBRACE && p.kind() != lexer.EOF {
		if p.kind() == lexer.SPREAD {
			p.next()
			nameTok := p.tok()
			if !p.identOK() {
				p.errWithMsg("expected a binding name after `...` in an object pattern", "")
				break
			}
			p.next()
			p.checkBindingName(nameTok)
			*names = append(*names, nameTok.Value)
			*rest = nameTok.Value
			obj.Properties = append(obj.Properties, &ast.ObjectProp{Spread: true, Value: &ast.Identifier{Position: tokPos(nameTok), Name: nameTok.Value}})
			if p.kind() == lexer.COMMA {
				p.errWithMsg("a rest element must be last in a destructuring pattern", "remove the trailing elements or comma")
			}
			break
		}
		key, ok := p.parsePropertyKey(false)
		if !ok {
			break
		}
		prop := &ast.ObjectProp{Key: key.name, Computed: key.computed, KeyExpr: key.expr, KeyQuoted: key.quoted, KeyNumeric: key.numeric}
		if p.match(lexer.COLON) {
			prop.Value = p.bindingElement(names, rest)
			if prop.Value == nil {
				break
			}
		} else {
			if key.computed || key.quoted || key.numeric || !isIdentifierToken(key.tok.Kind) {
				p.errWithMsg("expected `:` after property name in a destructuring pattern", "")
				break
			}
			if key.tok.Kind == lexer.Yield_ && (p.inGenerator || p.strict) || key.tok.Kind == lexer.Await_ && p.inAsync {
				p.errorAt(key.tok, "`"+key.name+"` is not a valid binding name here", "")
			}
			p.checkBindingName(key.tok)
			p.checkEscapedIdent(key.tok)
			if reservedWords[key.name] {
				p.errorAt(key.tok, "`"+key.name+"` is a reserved word and cannot be bound", "")
			}
			*names = append(*names, key.name)
			id := &ast.Identifier{Position: tokPos(key.tok), Name: key.name}
			prop.Shorthand = true
			prop.Value = id
			if p.kind() == lexer.ASSIGN {
				p.next()
				def := p.mustAssignAllowIn()
				prop.Value = &ast.BinaryExpr{Position: id.Position, Left: id, Op: "=", Right: def}
			}
		}
		obj.Properties = append(obj.Properties, prop)
		if !p.match(lexer.COMMA) {
			break
		}
	}
	p.expect(lexer.RBRACE)
	return obj
}
