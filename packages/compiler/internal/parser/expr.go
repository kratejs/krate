package parser

import (
	"strings"
	"unicode/utf8"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// Precedence levels for binary operators (higher binds tighter).
const (
	precNullish = 1 + iota
	precOr
	precAnd
	precBitOr
	precBitXor
	precBitAnd
	precEquality
	precRelational
	precShift
	precAdditive
	precMultiplicative
	precExponent
)

// binaryOp returns the precedence and spelling of a binary operator token,
// or 0 when the token is not one.
func binaryOp(k lexer.Kind) (int, string) {
	switch k {
	case lexer.NULLISH:
		return precNullish, "??"
	case lexer.OR:
		return precOr, "||"
	case lexer.AND:
		return precAnd, "&&"
	case lexer.BIT_OR:
		return precBitOr, "|"
	case lexer.BIT_XOR:
		return precBitXor, "^"
	case lexer.BIT_AND:
		return precBitAnd, "&"
	case lexer.EQ:
		return precEquality, "=="
	case lexer.NEQ:
		return precEquality, "!="
	case lexer.STRICT_EQ:
		return precEquality, "==="
	case lexer.STRICT_NEQ:
		return precEquality, "!=="
	case lexer.LT:
		return precRelational, "<"
	case lexer.GT:
		return precRelational, ">"
	case lexer.LTE:
		return precRelational, "<="
	case lexer.GTE:
		return precRelational, ">="
	case lexer.Instanceof_:
		return precRelational, "instanceof"
	case lexer.In_:
		return precRelational, "in"
	case lexer.SHL:
		return precShift, "<<"
	case lexer.SHR:
		return precShift, ">>"
	case lexer.USHIFT_RIGHT:
		return precShift, ">>>"
	case lexer.PLUS:
		return precAdditive, "+"
	case lexer.MINUS:
		return precAdditive, "-"
	case lexer.STAR:
		return precMultiplicative, "*"
	case lexer.DIV:
		return precMultiplicative, "/"
	case lexer.MOD:
		return precMultiplicative, "%"
	case lexer.POW:
		return precExponent, "**"
	}
	return 0, ""
}

func isAssignOp(k lexer.Kind) bool {
	switch k {
	case lexer.ASSIGN, lexer.ADD_ASSIGN, lexer.SUB_ASSIGN, lexer.MUL_ASSIGN, lexer.DIV_ASSIGN,
		lexer.MOD_ASSIGN, lexer.SHL_ASSIGN, lexer.SHR_ASSIGN, lexer.USHR_ASSIGN, lexer.POW_ASSIGN,
		lexer.AND_ASSIGN, lexer.OR_ASSIGN, lexer.NULLISH_ASSIGN,
		lexer.BIT_AND_ASSIGN, lexer.BIT_OR_ASSIGN, lexer.BIT_XOR_ASSIGN:
		return true
	}
	return false
}

// parseExpression parses a comma-separated expression sequence.
func (p *Parser) parseExpression() ast.Expr {
	left := p.parseAssign()
	if left == nil {
		return nil
	}
	for p.kind() == lexer.COMMA {
		comma := p.next()
		right := p.parseAssign()
		if right == nil {
			p.errWithMsg("expected an expression after `,`", "remove the trailing comma")
			break
		}
		left = &ast.BinaryExpr{Position: left.Pos(), Left: left, Op: ",", Right: right}
		_ = comma
	}
	return left
}

// parseAssign parses an AssignmentExpression (including arrow functions and
// `yield`).
func (p *Parser) parseAssign() ast.Expr {
	p.enter()
	defer p.leave()
	if arrow := p.tryArrow(); arrow != nil {
		return arrow
	}
	if p.kind() == lexer.Yield_ && p.inGenerator {
		return p.parseYield()
	}
	startTok := p.tok()
	left := p.parseConditional()
	if left == nil {
		return nil
	}
	if isAssignOp(p.kind()) {
		opTok := p.next()
		if opTok.Kind == lexer.ASSIGN {
			left = p.toAssignTarget(left, true)
		} else if !p.isSimpleTarget(left) {
			p.errorAt(startTok, "invalid left-hand side in assignment", "only variables and properties can be assigned to")
		}
		right := p.parseAssign()
		if right == nil {
			p.errWithMsg("expected an expression after `"+opTok.Value+"`", "")
			return left
		}
		return &ast.BinaryExpr{Position: left.Pos(), Left: left, Op: opTok.Value, Right: right}
	}
	return left
}

func (p *Parser) parseYield() ast.Expr {
	yt := p.next()
	if p.inParams {
		p.errorAt(yt, "`yield` expressions are not allowed in parameters", "")
	}
	p.checkEscapedKeyword(yt)
	op := "yield"
	if p.kind() == lexer.STAR && !p.tok().NewlineBefore {
		p.next()
		op = "yield*"
		arg := p.mustAssign()
		return &ast.UnaryExpr{Position: tokPos(yt), Op: op, Arg: arg}
	}
	var arg ast.Expr
	if !p.tok().NewlineBefore && canStartExpression(p.tok()) {
		arg = p.parseAssign()
	}
	return &ast.UnaryExpr{Position: tokPos(yt), Op: op, Arg: arg}
}

// canStartExpression reports whether a token can begin an expression.
func canStartExpression(t lexer.Token) bool {
	switch t.Kind {
	case lexer.RPAREN, lexer.RBRACKET, lexer.RBRACE, lexer.COMMA, lexer.SEMI, lexer.COLON,
		lexer.EOF, lexer.QUEST, lexer.ARROW, lexer.DOT, lexer.QUESTION_DOT,
		lexer.TEMPLATE_MID, lexer.In_, lexer.Of, lexer.Instanceof_, lexer.GT, lexer.LTE, lexer.GTE,
		lexer.EQ, lexer.NEQ, lexer.STRICT_EQ, lexer.STRICT_NEQ, lexer.AND, lexer.OR, lexer.NULLISH,
		lexer.STAR, lexer.POW, lexer.DIV, lexer.MOD, lexer.BIT_AND, lexer.BIT_OR, lexer.BIT_XOR,
		lexer.SHL, lexer.SHR, lexer.USHIFT_RIGHT, lexer.SLASH_GT, lexer.LT_SLASH, lexer.As:
		return false
	}
	return !isAssignOp(t.Kind)
}

func (p *Parser) parseConditional() ast.Expr {
	test := p.parseBinary(precNullish)
	if test == nil || p.kind() != lexer.QUEST {
		return test
	}
	p.next()
	prevNoIn := p.noIn
	p.noIn = false
	prevNoRT := p.noReturnTypeArrow
	p.noReturnTypeArrow = true
	cons := p.parseAssign()
	p.noReturnTypeArrow = prevNoRT
	p.noIn = prevNoIn
	p.expect(lexer.COLON)
	alt := p.parseAssign()
	if cons == nil || alt == nil {
		if cons == nil {
			p.errWithMsg("expected an expression after `?`", "")
		} else {
			p.errWithMsg("expected an expression after `:`", "")
		}
	}
	return &ast.ConditionalExpr{Position: test.Pos(), Test: test, Consequent: cons, Alternate: alt}
}

// parseBinary parses binary operators with precedence >= minPrec.
func (p *Parser) parseBinary(minPrec int) ast.Expr {
	p.enter()
	defer p.leave()
	left := p.parseUnary()
	if left == nil {
		return nil
	}
	for {
		t := p.tok()
		// TypeScript `x as T`, `x as const` and `x satisfies T` (relational
		// precedence; never across a line break).
		if (t.Kind == lexer.As || (t.Kind == lexer.Identifier && t.Value == "satisfies")) &&
			!t.NewlineBefore && precRelational >= minPrec {
			p.next()
			p.droppedTypes++
			typeRef := ""
			if p.kind() == lexer.Const_ {
				p.next()
				typeRef = "const"
			} else {
				if isWordToken(p.tok()) {
					typeRef = p.tok().Value
				}
				p.skipType()
			}
			if t.Kind == lexer.As {
				left = &ast.TypeAssertion{Position: left.Pos(), Expr: left, TypeRef: typeRef}
			}
			continue
		}
		prec, op := binaryOp(t.Kind)
		if prec == 0 || prec < minPrec {
			return left
		}
		if t.Kind == lexer.In_ && p.noIn {
			return left
		}
		if t.Kind == lexer.LT && t.JSX {
			return left
		}
		p.next()
		var right ast.Expr
		if op == "**" {
			if p.isUnaryOperand(left) {
				p.errAtPos(left.Pos(), "unary operator before `**` must be parenthesized", "write `(-a) ** b` or `-(a ** b)`")
			}
			right = p.parseBinary(prec)
		} else {
			right = p.parseBinary(prec + 1)
		}
		if right == nil {
			p.errWithMsg("expected an expression after `"+op+"`", "")
			return left
		}
		p.checkNullishMix(op, left, right)
		left = &ast.BinaryExpr{Position: left.Pos(), Left: left, Op: op, Right: right}
	}
}

// isUnaryOperand reports an unparenthesized unary/await expression, which may
// not be the base of `**`.
func (p *Parser) isUnaryOperand(e ast.Expr) bool {
	if p.parenthesized[e] {
		return false
	}
	switch v := e.(type) {
	case *ast.UnaryExpr:
		return !v.Postfix && v.Op != "..."
	case *ast.AwaitExpr:
		return true
	}
	return false
}

// checkNullishMix reports `a ?? b || c` style mixing without parentheses.
func (p *Parser) checkNullishMix(op string, left, right ast.Expr) {
	isLogical := func(e ast.Expr, ops ...string) bool {
		b, ok := e.(*ast.BinaryExpr)
		if !ok || p.parenthesized[e] {
			return false
		}
		for _, o := range ops {
			if b.Op == o {
				return true
			}
		}
		return false
	}
	bad := false
	switch op {
	case "??":
		bad = isLogical(left, "||", "&&") || isLogical(right, "||", "&&")
	case "||", "&&":
		bad = isLogical(left, "??") || isLogical(right, "??")
	}
	if bad {
		p.errAtPos(left.Pos(), "`??` cannot be mixed with `||` or `&&` without parentheses", "add parentheses, e.g. `(a ?? b) || c`")
	}
}

func (p *Parser) parseUnary() ast.Expr {
	p.enter()
	defer p.leave()
	t := p.tok()
	switch t.Kind {
	case lexer.NOT, lexer.BIT_NOT, lexer.PLUS, lexer.MINUS, lexer.Typeof_, lexer.Void_, lexer.Delete_:
		p.next()
		arg := p.parseUnary()
		if arg == nil {
			p.errWithMsg("expected an expression after `"+t.Value+"`", "")
			return nil
		}
		if t.Kind == lexer.Delete_ {
			p.checkDelete(t, arg)
		}
		return &ast.UnaryExpr{Position: tokPos(t), Op: t.Value, Arg: arg}
	case lexer.INC, lexer.DEC:
		p.next()
		arg := p.parseUnary()
		if arg == nil {
			p.errWithMsg("expected an expression after `"+t.Value+"`", "")
			return nil
		}
		if !p.isSimpleTarget(arg) {
			p.errorAt(t, "invalid operand for `"+t.Value+"`", "only variables and properties can be incremented or decremented")
		}
		return &ast.UnaryExpr{Position: tokPos(t), Op: t.Value, Arg: arg}
	case lexer.Await_:
		if p.awaitIsOperator() {
			if p.inParams {
				p.errorAt(t, "`await` expressions are not allowed in parameters", "")
			}
			p.checkEscapedKeyword(t)
			p.next()
			arg := p.parseUnary()
			if arg == nil {
				p.errWithMsg("expected an expression after `await`", "")
				return nil
			}
			return &ast.AwaitExpr{Position: tokPos(t), Arg: arg}
		}
	case lexer.LT:
		if !t.JSX {
			return p.parseAngleAssertion()
		}
	}
	return p.parsePostfix()
}

// awaitIsOperator decides whether `await` is the await operator: always in
// async functions, and at module top level when an operand follows.
func (p *Parser) awaitIsOperator() bool {
	if p.inAsync {
		return true
	}
	if p.inFunction {
		return false
	}
	n := p.at(1)
	if n.Kind == lexer.LPAREN || n.Kind == lexer.LBRACKET || n.Kind == lexer.MINUS || n.Kind == lexer.PLUS || n.Kind == lexer.DIV {
		return !n.NewlineBefore
	}
	return canStartExpression(n)
}

func (p *Parser) checkDelete(t lexer.Token, arg ast.Expr) {
	if p.strict {
		if _, ok := unparen(arg).(*ast.Identifier); ok {
			p.errorAt(t, "deleting an unqualified identifier is not allowed in strict mode", "delete a property instead, e.g. `delete obj.key`")
		}
	}
	if m, ok := unparen(arg).(*ast.MemberExpr); ok && !m.Computed {
		if id, ok := m.Property.(*ast.Identifier); ok && strings.HasPrefix(id.Name, "#") {
			p.errorAt(t, "private fields cannot be deleted", "")
		}
	}
}

func unparen(e ast.Expr) ast.Expr {
	for {
		pe, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = pe.Expr
	}
}

// parseAngleAssertion parses a TypeScript `<T>expr` type assertion.
func (p *Parser) parseAngleAssertion() ast.Expr {
	start := p.tok()
	c, ok := p.scanTypeArgs(p.cursor())
	if !ok {
		p.errWithMsg("unexpected `<`", "a `<` here must start a JSX element or a TypeScript type assertion")
		p.next()
		return nil
	}
	p.applyCursor(c)
	p.droppedTypes++
	arg := p.parseUnary()
	if arg == nil {
		return nil
	}
	typeRef := ""
	if isWordToken(p.tokens[p.indexOf(start)+1]) {
		typeRef = p.tokens[p.indexOf(start)+1].Value
	}
	return &ast.TypeAssertion{Position: tokPos(start), Expr: arg, TypeRef: typeRef}
}

// indexOf returns the token index of t (by offset), searching backwards.
func (p *Parser) indexOf(t lexer.Token) int {
	for i := p.pos; i >= 0; i-- {
		if p.tokens[i].Offset == t.Offset && p.tokens[i].Kind == t.Kind {
			return i
		}
	}
	return 0
}

func (p *Parser) parsePostfix() ast.Expr {
	startTok := p.tok()
	expr := p.parseLHS()
	if expr == nil {
		return nil
	}
	if (p.kind() == lexer.INC || p.kind() == lexer.DEC) && !p.tok().NewlineBefore {
		op := p.next()
		if !p.isSimpleTarget(expr) {
			p.errorAt(startTok, "invalid operand for postfix `"+op.Value+"`", "only variables and properties can be incremented or decremented")
		}
		return &ast.UnaryExpr{Position: expr.Pos(), Op: op.Value, Arg: expr, Postfix: true}
	}
	return expr
}

// parseLHS parses a LeftHandSideExpression: new/call/member chains.
func (p *Parser) parseLHS() ast.Expr {
	t := p.tok()
	var expr ast.Expr
	switch t.Kind {
	case lexer.New_:
		expr = p.parseNew()
	case lexer.Super_:
		p.next()
		expr = &ast.Identifier{Position: tokPos(t), Name: "super"}
		switch p.kind() {
		case lexer.LPAREN:
			if !p.superCall {
				p.errorAt(t, "`super()` is only valid in the constructor of a derived class", "")
			}
		case lexer.DOT, lexer.LBRACKET:
			if !p.superProp {
				p.errorAt(t, "`super` property access is only valid in methods", "")
			}
		default:
			p.errorAt(t, "`super` must be followed by a call or property access", "write `super(...)` or `super.method()`")
		}
	case lexer.Import:
		expr = p.parseImportExpr()
	default:
		expr = p.parsePrimary()
	}
	if expr == nil {
		return nil
	}
	return p.parseCallTail(expr, true)
}

// parseImportExpr parses `import.meta` and dynamic `import(x[, options])`.
func (p *Parser) parseImportExpr() ast.Expr {
	t := p.next()
	if p.match(lexer.DOT) {
		if p.isContextual("meta") {
			p.next()
			return &ast.ImportMetaExpr{Position: tokPos(t)}
		}
		p.errWithMsg("expected `meta` after `import.`", "the only import meta-property is `import.meta`")
		return &ast.ImportMetaExpr{Position: tokPos(t)}
	}
	if !p.match(lexer.LPAREN) {
		p.errWithMsg("expected `(` after `import`", "dynamic imports are written `import('./module.js')`")
		return nil
	}
	di := &ast.DynamicImport{Position: tokPos(t)}
	if p.kind() == lexer.RPAREN {
		p.errWithMsg("import() requires a module specifier", "")
	} else {
		di.Arg = p.mustAssignAllowIn()
		if p.match(lexer.COMMA) && p.kind() != lexer.RPAREN {
			di.Options = p.mustAssignAllowIn()
			p.match(lexer.COMMA)
		}
	}
	p.expect(lexer.RPAREN)
	return di
}

func (p *Parser) parseAssignAllowIn() ast.Expr {
	prev := p.noIn
	p.noIn = false
	e := p.parseAssign()
	p.noIn = prev
	return e
}

func (p *Parser) parseExpressionAllowIn() ast.Expr {
	prev := p.noIn
	p.noIn = false
	e := p.parseExpression()
	p.noIn = prev
	return e
}

// parseNew parses `new.target` and `new Callee[(args)]`, binding `new` to
// the whole member chain (`new a.b.C(1)` constructs `a.b.C`).
func (p *Parser) parseNew() ast.Expr {
	nt := p.next()
	if p.kind() == lexer.DOT {
		p.next()
		if !p.isContextual("target") {
			p.errWithMsg("expected `target` after `new.`", "the only new meta-property is `new.target`")
		} else {
			p.next()
		}
		if !p.inFunction {
			p.errorAt(nt, "`new.target` is only valid inside functions", "")
		}
		return &ast.MetaProperty{Position: tokPos(nt), Meta: "new", Property: "target"}
	}
	var callee ast.Expr
	switch p.kind() {
	case lexer.New_:
		callee = p.parseNew()
	case lexer.Import:
		p.errWithMsg("`new import(...)` is not allowed", "call `import(...)` without `new`")
		callee = p.parseImportExpr()
	case lexer.Super_:
		st := p.next()
		callee = &ast.Identifier{Position: tokPos(st), Name: "super"}
	default:
		callee = p.parsePrimary()
	}
	if callee == nil {
		p.errWithMsg("expected a constructor after `new`", "")
		return nil
	}
	callee = p.parseCallTail(callee, false)
	ne := &ast.NewExpr{Position: tokPos(nt), Callee: callee}
	if p.kind() == lexer.LPAREN {
		ne.Args = p.parseArguments()
	}
	return ne
}

// parseArguments parses `( args )`.
func (p *Parser) parseArguments() []ast.Expr {
	p.expect(lexer.LPAREN)
	var args []ast.Expr
	prev := p.noIn
	p.noIn = false
	for p.kind() != lexer.RPAREN && p.kind() != lexer.EOF {
		if p.kind() == lexer.SPREAD {
			st := p.next()
			arg := p.parseAssign()
			if arg == nil {
				p.errWithMsg("expected an expression after `...`", "")
				break
			}
			args = append(args, &ast.UnaryExpr{Position: tokPos(st), Op: "...", Arg: arg})
		} else {
			arg := p.parseAssign()
			if arg == nil {
				p.errWithMsg("expected an argument, got "+describe(p.tok()), "")
				break
			}
			args = append(args, arg)
		}
		if !p.match(lexer.COMMA) {
			break
		}
	}
	p.noIn = prev
	p.expect(lexer.RPAREN)
	return args
}

// isOptionalChain reports whether e is an optional chain (`a?.b`, `a?.b.c()`).
func isOptionalChain(e ast.Expr) bool {
	for {
		switch v := e.(type) {
		case *ast.MemberExpr:
			if v.Optional {
				return true
			}
			e = v.Object
		case *ast.CallExpr:
			if v.Optional {
				return true
			}
			e = v.Callee
		case *ast.TypeAssertion:
			e = v.Expr
		default:
			return false
		}
	}
}

// parseCallTail applies member accesses, calls (when calls is true), tagged
// templates, TypeScript non-null assertions and type arguments to expr.
func (p *Parser) parseCallTail(expr ast.Expr, calls bool) ast.Expr {
	inChain := false
	if p.parenthesized[expr] && isOptionalChain(expr) {
		switch p.kind() {
		case lexer.DOT, lexer.LBRACKET, lexer.LPAREN, lexer.TEMPLATE_START, tmplNoSub:
			expr = &ast.ParenExpr{Position: expr.Pos(), Expr: expr}
		}
	}
	for {
		t := p.tok()
		switch t.Kind {
		case lexer.DOT:
			p.next()
			name := p.tok()
			if !isWordKind(name.Kind) {
				p.errWithMsg("expected a property name after `.`, got "+describe(name), "")
				return expr
			}
			p.next()
			if strings.HasPrefix(name.Value, "#") {
				p.refPrivate(name)
				if id, ok := expr.(*ast.Identifier); ok && id.Name == "super" {
					p.errorAt(name, "private names cannot be accessed through `super`", "")
				}
			}
			expr = &ast.MemberExpr{Position: expr.Pos(), Object: expr, Property: &ast.Identifier{Position: tokPos(name), Name: name.Value}}
		case lexer.QUESTION_DOT:
			if !calls {
				p.errWithMsg("optional chaining is not allowed in a `new` expression", "wrap the chain in parentheses")
				return expr
			}
			p.next()
			inChain = true
			switch p.kind() {
			case lexer.LPAREN:
				args := p.parseArguments()
				expr = &ast.CallExpr{Position: expr.Pos(), Callee: expr, Args: args, Optional: true}
			case lexer.LBRACKET:
				p.next()
				prop := p.mustExpressionAllowIn()
				p.expect(lexer.RBRACKET)
				expr = &ast.MemberExpr{Position: expr.Pos(), Object: expr, Property: prop, Computed: true, Optional: true}
			case lexer.TEMPLATE_START, tmplNoSub:
				p.errWithMsg("tagged templates are not allowed in an optional chain", "call the tag explicitly")
				p.parseTemplate()
			case lexer.LT:
				// `fn?.<T>()` TypeScript type arguments.
				if c, ok := p.scanTypeArgs(p.cursor()); ok {
					p.applyCursor(c)
					p.droppedTypes++
					if p.kind() == lexer.LPAREN {
						args := p.parseArguments()
						expr = &ast.CallExpr{Position: expr.Pos(), Callee: expr, Args: args, Optional: true}
					}
					continue
				}
				p.unexpected()
				return expr
			default:
				name := p.tok()
				if !isWordKind(name.Kind) {
					p.errWithMsg("unexpected "+describe(name)+" after `?.`", "optional chaining expects a property, `[...]`, or `(...)`")
					return expr
				}
				p.next()
				if strings.HasPrefix(name.Value, "#") {
					p.refPrivate(name)
				}
				expr = &ast.MemberExpr{Position: expr.Pos(), Object: expr, Property: &ast.Identifier{Position: tokPos(name), Name: name.Value}, Optional: true}
			}
		case lexer.LBRACKET:
			p.next()
			prop := p.parseExpressionAllowIn()
			if prop == nil {
				p.errWithMsg("expected an expression inside `[]`", "")
			}
			p.expect(lexer.RBRACKET)
			expr = &ast.MemberExpr{Position: expr.Pos(), Object: expr, Property: prop, Computed: true}
		case lexer.LPAREN:
			if !calls {
				return expr
			}
			args := p.parseArguments()
			expr = &ast.CallExpr{Position: expr.Pos(), Callee: expr, Args: args}
		case lexer.TEMPLATE_START, tmplNoSub:
			if inChain {
				p.errWithMsg("tagged templates are not allowed in an optional chain", "call the tag explicitly")
			}
			quasi := p.parseTemplateTagged(true)
			expr = &ast.TaggedTemplateExpr{Position: expr.Pos(), Tag: expr, Quasi: quasi}
		case lexer.NOT:
			// TypeScript non-null assertion `x!` (never across a line break).
			if t.NewlineBefore {
				return expr
			}
			p.next()
			p.droppedTypes++
			expr = &ast.TypeAssertion{Position: expr.Pos(), Expr: expr}
		case lexer.LT:
			if t.JSX || t.NewlineBefore && !calls {
				return expr
			}
			c, ok := p.scanTypeArgs(p.cursor())
			if !ok || !p.typeArgsEndOK(c) {
				return expr
			}
			p.applyCursor(c)
			p.droppedTypes++
		default:
			return expr
		}
	}
}

// typeArgsEndOK reports whether a scanned `<...>` should be read as type
// arguments: it must be followed by a call, a template, or a token that
// cannot continue an expression (TypeScript's instantiation-expression rule).
func (p *Parser) typeArgsEndOK(c tcur) bool {
	switch p.vkind(c) {
	case lexer.LPAREN, lexer.TEMPLATE_START, tmplNoSub:
		return true
	case lexer.RPAREN, lexer.RBRACKET, lexer.COMMA, lexer.SEMI, lexer.DOT, lexer.QUESTION_DOT,
		lexer.EOF, lexer.RBRACE, lexer.COLON, lexer.EQ, lexer.STRICT_EQ, lexer.NEQ, lexer.STRICT_NEQ,
		lexer.AND, lexer.OR, lexer.NULLISH, lexer.QUEST:
		return c.k == 0
	}
	return false
}

// parsePrimary parses a PrimaryExpression.
func (p *Parser) parsePrimary() ast.Expr {
	t := p.tok()
	switch t.Kind {
	case lexer.Number:
		p.next()
		if p.strict && isLegacyOctal(t.Value) {
			p.errorAt(t, "legacy octal literals are not allowed in strict mode", "use the 0o prefix, e.g. 0o17")
		}
		return &ast.Literal{Position: tokPos(t), Kind: ast.NumberLit, Value: t.Value}
	case lexer.String:
		p.next()
		if p.strict && hasLegacyOctalEscape(t.Value) {
			p.errorAt(t, "octal escape sequences are not allowed in strict mode", "use \\x or \\u escapes")
		}
		return &ast.Literal{Position: tokPos(t), Kind: ast.StringLit, Value: decodeStringToken(t.Value)}
	case lexer.True, lexer.False:
		p.next()
		return &ast.Literal{Position: tokPos(t), Kind: ast.BoolLit, Value: t.Value}
	case lexer.Null_:
		p.next()
		return &ast.Literal{Position: tokPos(t), Kind: ast.NullLit, Value: "null"}
	case lexer.Undefined:
		p.next()
		return &ast.Literal{Position: tokPos(t), Kind: ast.NullLit, Value: "undefined"}
	case lexer.Regexp:
		p.next()
		return &ast.Literal{Position: tokPos(t), Kind: ast.RegexpLit, Value: t.Value}
	case lexer.This_:
		p.next()
		return &ast.ThisExpr{Position: tokPos(t)}
	case lexer.LPAREN:
		return p.parseParenExpr()
	case lexer.LBRACKET:
		return p.parseArrayLiteral()
	case lexer.LBRACE:
		return p.parseObjectLiteral()
	case lexer.Function:
		return p.parseFunctionExpr(t, false)
	case lexer.Async:
		if p.peekN(1) == lexer.Function && !p.at(1).NewlineBefore {
			p.next()
			return p.parseFunctionExpr(t, true)
		}
	case lexer.Class_:
		cls := p.parseClass(false)
		return &ast.ClassExpr{Position: tokPos(t), Class: cls}
	case lexer.TEMPLATE_START, tmplNoSub:
		return p.parseTemplate()
	case lexer.LT:
		if t.JSX {
			return p.parseJSXElement()
		}
	case lexer.AT:
		p.parseDecorators()
		if p.kind() == lexer.Class_ {
			return p.parsePrimary()
		}
		return nil
	case lexer.Error:
		p.next()
		return &ast.Literal{Position: tokPos(t), Kind: ast.NullLit, Value: "undefined"}
	}
	if t.Kind == lexer.Identifier && strings.HasPrefix(t.Value, "#") {
		// `#x in obj` brand checks are the only expression use of a private name.
		p.next()
		if p.kind() != lexer.In_ {
			p.errorAt(t, "private name `"+t.Value+"` can only be used as `"+t.Value+" in obj` or after `this.`", "")
		} else {
			p.refPrivate(t)
		}
		return &ast.Identifier{Position: tokPos(t), Name: t.Value}
	}
	if isIdentifierToken(t.Kind) {
		if t.Kind == lexer.Yield_ && (p.inGenerator || p.strict || p.yieldIsError) {
			p.errorAt(t, "`yield` is not a valid identifier here", "")
		}
		if t.Kind == lexer.Await_ && p.inAsync {
			p.errorAt(t, "`await` is not a valid identifier in an async function", "")
		}
		if p.strict && strictReserved[t.Value] {
			p.errorAt(t, "`"+t.Value+"` is a reserved word in strict mode code", "")
		}
		if t.Kind == lexer.Await_ && p.inStaticBlock {
			p.errorAt(t, "`await` is reserved in class static blocks", "")
		}
		if t.Value == "arguments" && p.inFieldInit {
			p.errorAt(t, "`arguments` is not allowed in class field initializers or static blocks", "")
		}
		p.checkEscapedIdent(t)
		p.next()
		return &ast.Identifier{Position: tokPos(t), Name: t.Value}
	}
	return nil
}

func isLegacyOctal(v string) bool {
	if len(v) < 2 || v[0] != '0' {
		return false
	}
	c := v[1]
	return c >= '0' && c <= '9'
}

// hasLegacyOctalEscape reports `\1`-`\7` (or `\0` followed by a digit) in a
// raw string literal.
func hasLegacyOctalEscape(raw string) bool {
	for i := 0; i+1 < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		c := raw[i+1]
		if c >= '1' && c <= '9' {
			return true
		}
		if c == '0' && i+2 < len(raw) && raw[i+2] >= '0' && raw[i+2] <= '9' {
			return true
		}
		i++
	}
	return false
}

func (p *Parser) parseParenExpr() ast.Expr {
	open := p.next()
	if p.kind() == lexer.RPAREN {
		p.errorAt(open, "empty parentheses are not an expression", "write `() => ...` for a function with no parameters")
		p.next()
		return nil
	}
	prevNoIn, prevNoRT := p.noIn, p.noReturnTypeArrow
	p.noIn, p.noReturnTypeArrow = false, false
	e := p.parseExpression()
	p.noIn, p.noReturnTypeArrow = prevNoIn, prevNoRT
	p.expect(lexer.RPAREN)
	if e == nil {
		return nil
	}
	if p.parenthesized == nil {
		p.parenthesized = map[ast.Expr]bool{}
	}
	p.parenthesized[e] = true
	return e
}

func (p *Parser) parseArrayLiteral() ast.Expr {
	open := p.next()
	arr := &ast.ArrayExpr{Position: tokPos(open)}
	prev := p.noIn
	p.noIn = false
	for p.kind() != lexer.RBRACKET && p.kind() != lexer.EOF {
		if p.kind() == lexer.COMMA {
			p.next()
			arr.Elements = append(arr.Elements, nil)
			continue
		}
		var el ast.Expr
		if p.kind() == lexer.SPREAD {
			st := p.next()
			arg := p.parseAssign()
			if arg == nil {
				p.errWithMsg("expected an expression after `...`", "")
				break
			}
			el = &ast.UnaryExpr{Position: tokPos(st), Op: "...", Arg: arg}
		} else {
			el = p.parseAssign()
			if el == nil {
				p.errWithMsg("expected an array element, got "+describe(p.tok()), "")
				break
			}
		}
		arr.Elements = append(arr.Elements, el)
		if !p.match(lexer.COMMA) {
			break
		}
	}
	p.noIn = prev
	p.expect(lexer.RBRACKET)
	return arr
}

// propertyKey is a parsed object/class member name.
type propertyKey struct {
	tok      lexer.Token
	name     string // identifier name, decoded string, or number spelling
	expr     ast.Expr
	computed bool
	quoted   bool
	numeric  bool
	private  bool
}

// isPropertyKeyStart reports whether t can begin a property name.
func isPropertyKeyStart(t lexer.Token) bool {
	switch t.Kind {
	case lexer.String, lexer.Number, lexer.LBRACKET:
		return true
	}
	return isWordKind(t.Kind)
}

func (p *Parser) parsePropertyKey(allowPrivate bool) (propertyKey, bool) {
	t := p.tok()
	k := propertyKey{tok: t}
	switch {
	case t.Kind == lexer.LBRACKET:
		p.next()
		k.computed = true
		k.expr = p.parseAssignAllowIn()
		if k.expr == nil {
			p.errWithMsg("expected an expression inside a computed key", "")
		}
		p.expect(lexer.RBRACKET)
		switch v := k.expr.(type) {
		case *ast.Identifier:
			k.name = v.Name
		case *ast.Literal:
			k.name = v.Value
		}
	case t.Kind == lexer.String:
		p.next()
		k.name = decodeStringToken(t.Value)
		k.quoted = true
	case t.Kind == lexer.Number:
		p.next()
		k.name = t.Value
		k.numeric = true
	case isWordKind(t.Kind):
		if strings.HasPrefix(t.Value, "#") {
			if !allowPrivate {
				p.errorAt(t, "private names are only allowed in class bodies", "")
			}
			k.private = true
		}
		p.next()
		k.name = t.Value
	default:
		p.errWithMsg("expected a property name, got "+describe(t), "")
		return k, false
	}
	return k, true
}

func (p *Parser) parseObjectLiteral() ast.Expr {
	open := p.next()
	obj := &ast.ObjectExpr{Position: tokPos(open)}
	prev := p.noIn
	p.noIn = false
	protoCount := 0
	for p.kind() != lexer.RBRACE && p.kind() != lexer.EOF {
		if p.kind() == lexer.SPREAD {
			p.next()
			v := p.parseAssign()
			if v == nil {
				p.errWithMsg("expected an expression after `...`", "")
				break
			}
			obj.Properties = append(obj.Properties, &ast.ObjectProp{Spread: true, Value: v})
		} else {
			prop, isProto := p.parseObjectMember(obj)
			if prop == nil {
				break
			}
			if isProto {
				protoCount++
				if protoCount == 2 {
					if p.protoDup == nil {
						p.protoDup = map[*ast.ObjectExpr]lexer.Token{}
					}
					p.protoDup[obj] = p.tok()
				}
			}
			obj.Properties = append(obj.Properties, prop)
		}
		if !p.match(lexer.COMMA) {
			break
		}
	}
	p.noIn = prev
	p.expect(lexer.RBRACE)
	return obj
}

// parseObjectMember parses one non-spread object literal member. isProto
// reports a `__proto__: value` data property.
func (p *Parser) parseObjectMember(obj *ast.ObjectExpr) (*ast.ObjectProp, bool) {
	start := p.tok()
	isAsync, isGen, getter, setter := false, false, false, false
	// Modifiers only count when a property name follows them.
	if start.Kind == lexer.Async && isPropertyKeyStart(p.at(1)) && !p.at(1).NewlineBefore || start.Kind == lexer.Async && p.peekN(1) == lexer.STAR {
		p.next()
		isAsync = true
	}
	if p.kind() == lexer.STAR {
		p.next()
		isGen = true
	}
	if !isAsync && !isGen && (p.isContextual("get") || p.isContextual("set")) && isPropertyKeyStart(p.at(1)) {
		getter = p.tok().Value == "get"
		setter = !getter
		p.next()
	}
	key, ok := p.parsePropertyKey(false)
	if !ok {
		return nil, false
	}
	prop := &ast.ObjectProp{Key: key.name, Computed: key.computed, KeyExpr: key.expr, KeyQuoted: key.quoted, KeyNumeric: key.numeric}
	if p.kind() == lexer.LPAREN || p.kind() == lexer.LT {
		fnStart := p.tok()
		params, body, _ := p.parseFunctionRest(isAsync, isGen, true)
		p.checkAccessorParams(fnStart, getter, setter, params)
		prop.Value = &ast.ArrowFn{Position: tokPos(start), Params: params, Body: body, Async: isAsync, Generator: isGen, Function: true}
		prop.Method = true
		prop.Getter, prop.Setter = getter, setter
		return prop, false
	}
	if isAsync || isGen || getter || setter {
		p.errWithMsg("expected `(` after method name", "")
		return nil, false
	}
	if p.match(lexer.COLON) {
		prop.Value = p.parseAssign()
		if prop.Value == nil {
			p.errWithMsg("expected a property value, got "+describe(p.tok()), "")
			return nil, false
		}
		isProto := !key.computed && key.name == "__proto__"
		return prop, isProto
	}
	// Shorthand `{ a }` or cover-initialized `{ a = 1 }` (patterns only).
	if key.computed || key.quoted || key.numeric || !isIdentifierToken(key.tok.Kind) {
		p.errWithMsg("expected `:` after property name, got "+describe(p.tok()), "")
		return nil, false
	}
	p.checkIdentReference(key.tok)
	p.checkEscapedIdent(key.tok)
	if reservedWords[key.name] && !key.tok.Escaped {
		p.errorAt(key.tok, "`"+key.name+"` is a reserved word and cannot be a shorthand property", "write `"+key.name+": value`")
	}
	id := &ast.Identifier{Position: tokPos(key.tok), Name: key.name}
	prop.Shorthand = true
	prop.Value = id
	if p.kind() == lexer.ASSIGN {
		eq := p.next()
		def := p.mustAssign()
		prop.Value = &ast.BinaryExpr{Position: id.Position, Left: id, Op: "=", Right: def}
		if p.coverInit == nil {
			p.coverInit = map[*ast.ObjectExpr]lexer.Token{}
		}
		if _, seen := p.coverInit[obj]; !seen {
			p.coverInit[obj] = eq
		}
	}
	return prop, false
}

// checkIdentReference validates a shorthand property used as a reference.
func (p *Parser) checkIdentReference(t lexer.Token) {
	switch {
	case t.Kind == lexer.Yield_ && (p.inGenerator || p.strict):
		p.errorAt(t, "`yield` is not a valid identifier here", "")
	case t.Kind == lexer.Await_ && p.inAsync:
		p.errorAt(t, "`await` is not a valid identifier in an async function", "")
	case p.strict && strictReserved[t.Value]:
		p.errorAt(t, "`"+t.Value+"` is a reserved word in strict mode code", "")
	}
}

func (p *Parser) checkAccessorParams(at lexer.Token, getter, setter bool, params []*ast.Param) {
	if getter && len(params) != 0 {
		p.errorAt(at, "a getter must not have parameters", "remove the parameters")
	}
	if setter && (len(params) != 1 || params[0].IsRest) {
		p.errorAt(at, "a setter must have exactly one parameter", "")
	}
}

// reportPendingObjectErrors reports object-literal errors that only apply
// when the literal was not reinterpreted as a destructuring pattern.
func (p *Parser) reportPendingObjectErrors() {
	for _, t := range p.protoDup {
		p.errorAt(t, "duplicate `__proto__` property in object literal", "an object literal may set `__proto__` only once")
	}
	for _, t := range p.coverInit {
		p.errorAt(t, "shorthand property initializers (`{ a = 1 }`) are only valid in destructuring patterns", "use `{ a: 1 }` in an object literal")
	}
}

func (p *Parser) parseFunctionExpr(start lexer.Token, isAsync bool) ast.Expr {
	p.expect(lexer.Function)
	isGen := p.match(lexer.STAR)
	fn := &ast.ArrowFn{Position: tokPos(start), Async: isAsync, Generator: isGen, Function: true}
	if p.kind() != lexer.LPAREN && p.kind() != lexer.LT {
		nt := p.tok()
		switch {
		case nt.Kind == lexer.Yield_ && (isGen || p.strict),
			nt.Kind == lexer.Await_ && isAsync:
			p.errorAt(nt, "`"+nt.Value+"` is not a valid function name here", "")
			p.next()
		case isIdentifierToken(nt.Kind) && !strings.HasPrefix(nt.Value, "#"):
			p.checkBindingName(nt)
			fn.Name = nt.Value
			p.next()
		default:
			p.errWithMsg("expected a function name or `(`, got "+describe(nt), "")
		}
	}
	params, body, _ := p.parseFunctionRest(isAsync, isGen, false)
	fn.Params = params
	fn.Body = body
	return fn
}

// Templates

// parseTemplate parses a template literal starting at TEMPLATE_START or a
// substitution-free TEMPLATE_END token.
func (p *Parser) parseTemplate() *ast.TemplateExpr { return p.parseTemplateTagged(false) }

// parseTemplateTagged parses a template; untagged templates may not contain
// escapes that only tagged templates allow (`\01`, `\xg`, `\u{zz}`).
func (p *Parser) parseTemplateTagged(tagged bool) *ast.TemplateExpr {
	t := p.tok()
	if !tagged {
		for i := p.pos; i < len(p.tokens); i++ {
			tk := p.tokens[i]
			if tk.BadEscape && (i == p.pos || tk.Kind == lexer.TEMPLATE_MID || tk.Kind == lexer.TEMPLATE_END) {
				p.errorAt(tk, "invalid escape sequence in template literal", "only tagged templates may contain malformed escapes; escape the backslash as \\\\")
			}
			if i > p.pos && (tk.Kind == lexer.TEMPLATE_END || tk.Kind == tmplNoSub) || i == p.pos && tk.Kind == tmplNoSub {
				break
			}
			if i > p.pos && tk.Kind == lexer.TEMPLATE_START {
				break
			}
		}
	}
	p.next()
	tmpl := &ast.TemplateExpr{Position: tokPos(t)}
	if t.Kind == tmplNoSub {
		tmpl.Raw = []string{templateChunk(t.Value)}
		return tmpl
	}
	tmpl.Raw = append(tmpl.Raw, templateChunk(t.Value))
	for {
		e := p.parseExpressionAllowIn()
		if e == nil {
			p.errWithMsg("expected an expression inside `${}`", "")
			e = &ast.Literal{Kind: ast.StringLit}
		}
		tmpl.Parts = append(tmpl.Parts, e)
		switch p.kind() {
		case lexer.TEMPLATE_MID:
			tmpl.Raw = append(tmpl.Raw, templateChunk(p.next().Value))
			continue
		case lexer.TEMPLATE_END:
			tmpl.Raw = append(tmpl.Raw, templateChunk(p.next().Value))
			return tmpl
		}
		p.errWithMsg("expected `}` to close the template substitution, got "+describe(p.tok()), "")
		tmpl.Raw = append(tmpl.Raw, "")
		return tmpl
	}
}

// templateChunk strips a template chunk's delimiters ("`", "${", "}").
func templateChunk(v string) string {
	if strings.HasPrefix(v, "`") || strings.HasPrefix(v, "}") {
		v = v[1:]
	}
	if strings.HasSuffix(v, "${") {
		v = v[:len(v)-2]
	} else if strings.HasSuffix(v, "`") {
		v = v[:len(v)-1]
	}
	return v
}

// Arrow functions

// tryArrow parses an arrow function if one starts at the current position.
func (p *Parser) tryArrow() ast.Expr {
	t := p.tok()
	switch {
	case isIdentifierToken(t.Kind) && !strings.HasPrefix(t.Value, "#") && p.peekN(1) == lexer.ARROW:
		// Also covers `async => x`, where `async` is the parameter name.
		return p.parseArrow(false)
	case t.Kind == lexer.Async && !p.at(1).NewlineBefore:
		n := p.at(1)
		switch {
		case isIdentifierToken(n.Kind) && p.peekN(2) == lexer.ARROW:
			return p.parseArrow(true)
		case n.Kind == lexer.LPAREN && p.arrowAfterParen(p.pos+1):
			return p.parseArrow(true)
		case n.Kind == lexer.LT && !n.JSX:
			if c, ok := p.scanTypeParams(tcur{i: p.pos + 1}); ok && c.k == 0 && p.tokens[c.i].Kind == lexer.LPAREN && p.arrowAfterParen(c.i) {
				return p.parseArrow(true)
			}
		}
	case t.Kind == lexer.LPAREN && p.arrowAfterParen(p.pos):
		return p.parseArrow(false)
	case t.Kind == lexer.LT && !t.JSX:
		if c, ok := p.scanTypeParams(p.cursor()); ok && c.k == 0 && p.tokens[c.i].Kind == lexer.LPAREN && p.arrowAfterParen(c.i) {
			return p.parseArrow(false)
		}
	}
	return nil
}

// arrowAfterParen reports whether the `(` at index i closes into `=>`
// (optionally after a TypeScript return type). The matching `)` comes from
// the precomputed bracket table, so this is O(1) apart from the return type.
func (p *Parser) arrowAfterParen(i int) bool {
	j := p.pair[i]
	if j < 0 {
		return false
	}
	n := p.tokens[j+1]
	if n.Kind == lexer.ARROW {
		return true
	}
	if n.Kind == lexer.COLON && !p.noReturnTypeArrow {
		c, ok := p.scanType(tcur{i: j + 2})
		return ok && c.k == 0 && p.tokens[c.i].Kind == lexer.ARROW
	}
	return false
}

func (p *Parser) parseArrow(isAsync bool) ast.Expr {
	start := p.tok()
	if isAsync {
		p.next()
	}
	saved := p.saveFnContext()
	p.yieldIsError = saved.inGenerator
	p.inAsync = isAsync
	p.inGenerator = false
	p.pushScope(true)
	defer p.popScope()
	var params []*ast.Param
	var names []lexer.Token
	if p.kind() == lexer.LT || p.kind() == lexer.LPAREN {
		p.skipTypeParams()
		p.expect(lexer.LPAREN)
		p.inParams = true
		params = p.parseParams()
		p.inParams = false
		names = p.paramNames
		p.expect(lexer.RPAREN)
		p.skipReturnType()
	} else {
		nt := p.next()
		if nt.Kind == lexer.Await_ && (isAsync || saved.inAsync || saved.inStaticBlock) {
			p.errorAt(nt, "`await` is not a valid parameter name here", "")
		}
		if nt.Kind == lexer.Yield_ && (saved.inGenerator || p.strict) {
			p.errorAt(nt, "`yield` is not a valid parameter name here", "")
		}
		p.checkEscapedIdent(nt)
		params = []*ast.Param{{Position: tokPos(nt), Name: nt.Value}}
		names = []lexer.Token{nt}
	}
	p.yieldIsError = false
	p.declareParams(names)
	if p.kind() != lexer.ARROW {
		p.errWithMsg("expected `=>`, got "+describe(p.tok()), "")
		p.restoreFnContext(saved)
		return &ast.ArrowFn{Position: tokPos(start), Params: params, Async: isAsync}
	}
	if p.tok().NewlineBefore {
		p.errWithMsg("line break before `=>` is not allowed", "put `=>` on the same line as the parameters")
	}
	p.next()
	fn := &ast.ArrowFn{Position: tokPos(start), Params: params, Async: isAsync}
	if p.kind() == lexer.LBRACE {
		p.inFunction = true
		fn.Body = p.parseFunctionBodyChecked(params, names, true)
	} else {
		p.checkParamNames(names, params, true)
		prevNoRT := p.noReturnTypeArrow
		p.noReturnTypeArrow = false
		body := p.parseAssign()
		p.noReturnTypeArrow = prevNoRT
		if body == nil {
			p.errWithMsg("expected an arrow function body, got "+describe(p.tok()), "")
		}
		fn.Body = []ast.Stmt{&ast.ReturnStmt{Position: posOf(body, start), Value: body}}
		fn.Expression = true
	}
	p.restoreFnContext(saved)
	return fn
}

func posOf(e ast.Expr, fallback lexer.Token) ast.Pos {
	if e != nil {
		return e.Pos()
	}
	return tokPos(fallback)
}

// Assignment targets

// isSimpleTarget reports whether e can be the target of `+=` or `++`.
func (p *Parser) isSimpleTarget(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Identifier:
		if strings.HasPrefix(v.Name, "#") || v.Name == "super" {
			return false
		}
		if p.strict && (v.Name == "eval" || v.Name == "arguments") {
			return false
		}
		return true
	case *ast.MemberExpr:
		return !isOptionalChain(v)
	case *ast.TypeAssertion:
		return p.isSimpleTarget(v.Expr)
	case *ast.ParenExpr:
		return p.isSimpleTarget(v.Expr)
	}
	return false
}

// toAssignTarget validates e as the target of `=` (or of a for-in/of loop)
// and reinterprets object/array literals as destructuring patterns.
func (p *Parser) toAssignTarget(e ast.Expr, allowPattern bool) ast.Expr {
	if e == nil {
		return nil
	}
	switch v := e.(type) {
	case *ast.ObjectExpr:
		if allowPattern && !p.parenthesized[e] {
			p.objectToPattern(v)
			return e
		}
	case *ast.ArrayExpr:
		if allowPattern && !p.parenthesized[e] {
			p.arrayToPattern(v)
			return e
		}
	default:
		if p.isSimpleTarget(e) {
			return e
		}
	}
	p.errAtPos(e.Pos(), "invalid assignment target", "only variables, properties and destructuring patterns can be assigned to")
	return e
}

func (p *Parser) objectToPattern(o *ast.ObjectExpr) {
	delete(p.protoDup, o)
	delete(p.coverInit, o)
	for i, prop := range o.Properties {
		switch {
		case prop.Spread:
			if i != len(o.Properties)-1 {
				p.errAtPos(prop.Value.Pos(), "a rest element must be last in a destructuring pattern", "")
			}
			if !p.isSimpleTarget(prop.Value) {
				p.errAtPos(prop.Value.Pos(), "invalid rest element target", "")
			}
		case prop.Method:
			p.errAtPos(prop.Value.Pos(), "methods are not valid in a destructuring pattern", "")
		default:
			p.patternElement(prop.Value)
		}
	}
}

func (p *Parser) arrayToPattern(a *ast.ArrayExpr) {
	for i, el := range a.Elements {
		if el == nil {
			continue
		}
		if u, ok := el.(*ast.UnaryExpr); ok && u.Op == "..." {
			if i != len(a.Elements)-1 {
				p.errAtPos(el.Pos(), "a rest element must be last in a destructuring pattern", "")
			}
			if b, ok := u.Arg.(*ast.BinaryExpr); ok && b.Op == "=" && !p.parenthesized[u.Arg] {
				p.errAtPos(el.Pos(), "a rest element cannot have a default value", "")
				continue
			}
			p.toAssignTarget(u.Arg, true)
			continue
		}
		p.patternElement(el)
	}
}

// patternElement validates one element of a destructuring assignment pattern
// (a target, optionally with a `= default`).
func (p *Parser) patternElement(e ast.Expr) {
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == "=" && !p.parenthesized[e] {
		p.toAssignTarget(b.Left, true)
		return
	}
	p.toAssignTarget(e, true)
}

// unescapeJS decodes the escape sequences of a string literal body.
func unescapeJS(s string) string {
	if strings.IndexByte(s, '\\') < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		c = s[i]
		switch c {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		case '\n':
		case 'x':
			if i+2 < len(s) {
				if v, ok := hexValue(s[i+1 : i+3]); ok {
					b.WriteRune(rune(v))
					i += 2
					continue
				}
			}
			b.WriteByte('x')
		case 'u':
			r, n := decodeUEscape(s[i-1:])
			if n == 0 {
				b.WriteByte('u')
				continue
			}
			i += n - 2
			// Combine a UTF-16 surrogate pair written as two escapes.
			if r >= 0xD800 && r <= 0xDBFF && i+1 < len(s) && s[i+1] == '\\' {
				if lo, m := decodeUEscape(s[i+1:]); m > 0 && lo >= 0xDC00 && lo <= 0xDFFF {
					r = (r-0xD800)<<10 + (lo - 0xDC00) + 0x10000
					i += m
				}
			}
			b.WriteRune(r)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			// Legacy octal escape (sloppy mode), up to three digits <= 0377.
			v := int(c - '0')
			j := i + 1
			for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' && v*8+int(s[j]-'0') <= 0377 {
				v = v*8 + int(s[j]-'0')
				j++
			}
			b.WriteRune(rune(v))
			i = j - 1
		default:
			// `\a` is `a`; line separators after a backslash are elided.
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == 0x2028 || r == 0x2029 {
				i += size - 1
				continue
			}
			b.WriteString(s[i : i+size])
			i += size - 1
		}
	}
	return b.String()
}

// decodeUEscape decodes `\uXXXX` or `\u{...}` at the start of s.
func decodeUEscape(s string) (rune, int) {
	if len(s) < 3 || s[0] != '\\' || s[1] != 'u' {
		return 0, 0
	}
	if s[2] == '{' {
		end := strings.IndexByte(s, '}')
		if end < 4 {
			return 0, 0
		}
		v, ok := hexValue(s[3:end])
		if !ok || v > 0x10FFFF {
			return 0, 0
		}
		return rune(v), end + 1
	}
	if len(s) < 6 {
		return 0, 0
	}
	v, ok := hexValue(s[2:6])
	if !ok {
		return 0, 0
	}
	return rune(v), 6
}

func hexValue(s string) (int, bool) {
	if s == "" || len(s) > 8 {
		return 0, false
	}
	v := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			v = v*16 + int(c-'0')
		case c >= 'a' && c <= 'f':
			v = v*16 + int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v = v*16 + int(c-'A') + 10
		default:
			return 0, false
		}
	}
	return v, true
}

// mustAssign parses an AssignmentExpression and reports a missing one.
func (p *Parser) mustAssign() ast.Expr {
	e := p.parseAssign()
	if e == nil {
		p.errWithMsg("expected an expression, got "+describe(p.tok()), "")
	}
	return e
}

func (p *Parser) mustAssignAllowIn() ast.Expr {
	prev := p.noIn
	p.noIn = false
	e := p.mustAssign()
	p.noIn = prev
	return e
}

// mustExpression parses an Expression and reports a missing one.
func (p *Parser) mustExpression() ast.Expr {
	e := p.parseExpression()
	if e == nil {
		p.errWithMsg("expected an expression, got "+describe(p.tok()), "")
	}
	return e
}

func (p *Parser) mustExpressionAllowIn() ast.Expr {
	prev := p.noIn
	p.noIn = false
	e := p.mustExpression()
	p.noIn = prev
	return e
}
