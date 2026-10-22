package parser

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// stmtCtx says where a statement appears, which decides what is allowed.
type stmtCtx int

const (
	// stmtTop is the module/script top level: imports and exports allowed.
	stmtTop stmtCtx = iota
	// stmtList is a block or function body: declarations allowed.
	stmtList
	// stmtSingle is a single-statement position (`if (x) <stmt>`): no
	// lexical declarations.
	stmtSingle
)

func (p *Parser) parseStmt(ctx stmtCtx) ast.Stmt {
	p.enter()
	s := p.parseStmtInner(ctx)
	p.leave()
	return s
}

func (p *Parser) parseStmtInner(ctx stmtCtx) ast.Stmt {
	t := p.tok()
	switch t.Kind {
	case lexer.LBRACE:
		start := p.next()
		p.pushScope(false)
		body := p.parseStmtList(lexer.RBRACE)
		p.popScope()
		p.expect(lexer.RBRACE)
		return &ast.BlockStmt{Position: tokPos(start), Body: body}
	case lexer.SEMI:
		p.next()
		if ctx == stmtSingle {
			return &ast.EmptyStmt{Position: tokPos(t)}
		}
		return nil
	case lexer.Var_:
		return p.parseVarStmt(true)
	case lexer.Const_:
		if p.peekN(1) == lexer.Enum_ {
			return p.parseEnumDecl(false)
		}
		p.checkLexicalPosition(ctx)
		return p.parseVarStmt(true)
	case lexer.Let_:
		if p.letStartsDeclaration() && !(ctx == stmtSingle && p.at(1).NewlineBefore && p.peekN(1) != lexer.LBRACKET) {
			p.checkLexicalPosition(ctx)
			return p.parseVarStmt(true)
		}
	case lexer.Function:
		if ctx == stmtSingle {
			p.errWithMsg("function declarations are not allowed in a single-statement position", "wrap the declaration in a block `{ ... }`")
		}
		return p.parseFnDecl(t)
	case lexer.Async:
		if p.peekN(1) == lexer.Function && !p.at(1).NewlineBefore {
			if ctx == stmtSingle {
				p.errWithMsg("async function declarations are not allowed in a single-statement position", "wrap the declaration in a block `{ ... }`")
			}
			p.next()
			return p.parseFnDecl(t)
		}
	case lexer.Class_:
		p.checkLexicalPosition(ctx)
		return p.parseClassDecl()
	case lexer.If_:
		return p.parseIfStmt()
	case lexer.For:
		return p.parseForStmt()
	case lexer.While_:
		return p.parseWhileStmt()
	case lexer.Do_:
		return p.parseDoWhileStmt()
	case lexer.Switch:
		return p.parseSwitchStmt()
	case lexer.Try_:
		return p.parseTryStmt()
	case lexer.Throw_:
		return p.parseThrowStmt()
	case lexer.Break_:
		return p.parseBreakStmt()
	case lexer.Continue_:
		return p.parseContinueStmt()
	case lexer.Return:
		return p.parseReturn()
	case lexer.With_:
		return p.parseWithStmt()
	case lexer.Debugger_:
		p.next()
		p.consumeSemicolon()
		return &ast.DebuggerStmt{Position: tokPos(t)}
	case lexer.Import:
		if k := p.peekN(1); k != lexer.LPAREN && k != lexer.DOT {
			if ctx != stmtTop {
				p.errWithMsg("import declarations are only allowed at the top level of a module", "move the import to the top of the file, or use `await import(...)`")
			}
			return p.parseImport()
		}
	case lexer.Export:
		if ctx != stmtTop {
			p.errWithMsg("export declarations are only allowed at the top level of a module", "move the export to the top level of the file")
		}
		return p.parseExport()
	case lexer.Interface_:
		if isIdentifierToken(p.peekN(1)) && !p.at(1).NewlineBefore {
			return p.skipInterfaceDecl()
		}
	case lexer.Type_:
		if isIdentifierToken(p.peekN(1)) && !p.at(1).NewlineBefore {
			if k := p.peekN(2); k == lexer.ASSIGN || k == lexer.LT {
				return p.skipTypeAliasDecl()
			}
		}
	case lexer.Enum_:
		return p.parseEnumDecl(false)
	case lexer.Declare_:
		if isWordKind(p.peekN(1)) && !p.at(1).NewlineBefore {
			return p.skipDeclare()
		}
	case lexer.Namespace_:
		if (isIdentifierToken(p.peekN(1)) || p.peekN(1) == lexer.String) && !p.at(1).NewlineBefore {
			return p.parseNamespace(false)
		}
	case lexer.AT:
		p.parseDecorators()
		return p.parseStmt(ctx)
	case lexer.RBRACE:
		p.unexpected()
		p.next()
		return nil
	case lexer.Error:
		p.next()
		return nil
	}

	// Contextual statement forms that start with an identifier.
	if t.Kind == lexer.Identifier || isIdentifierToken(t.Kind) {
		switch {
		case p.peekN(1) == lexer.COLON && p.identOK():
			return p.parseLabeledStmt(ctx)
		case t.Value == "abstract" && p.peekN(1) == lexer.Class_ && !p.at(1).NewlineBefore:
			p.next()
			return p.parseClassDecl()
		case t.Value == "module" && p.peekN(1) == lexer.String && !p.at(1).NewlineBefore:
			return p.parseNamespace(false)
		case t.Value == "module" && isIdentifierToken(p.peekN(1)) && p.peekN(2) == lexer.LBRACE && !p.at(1).NewlineBefore:
			return p.parseNamespace(false)
		case t.Value == "global" && p.peekN(1) == lexer.LBRACE && !p.at(1).NewlineBefore:
			return p.parseNamespace(false)
		case t.Value == "using" && p.usingStartsDeclaration(1):
			p.checkLexicalPosition(ctx)
			return p.parseUsingStmt(false)
		}
	}
	if t.Kind == lexer.Await_ && p.at(1).Value == "using" && !p.at(1).NewlineBefore && p.usingStartsDeclaration(2) {
		p.checkLexicalPosition(ctx)
		return p.parseUsingStmt(true)
	}

	expr := p.parseExpression()
	if expr == nil {
		p.unexpected()
		if p.kind() != lexer.EOF {
			p.next()
		}
		return nil
	}
	p.consumeSemicolon()
	return &ast.ExprStmt{Position: expr.Pos(), Expression: expr}
}

// checkLexicalPosition rejects let/const/class/using in a single-statement
// position such as `if (x) let y = 1;`.
func (p *Parser) checkLexicalPosition(ctx stmtCtx) {
	if ctx == stmtSingle {
		p.errWithMsg("lexical declaration cannot appear in a single-statement context", "wrap the declaration in a block `{ ... }`")
	}
}

// letStartsDeclaration reports whether `let` at the current position begins a
// lexical declaration (rather than being an identifier, as sloppy-mode code
// may use it).
func (p *Parser) letStartsDeclaration() bool {
	n := p.at(1)
	switch n.Kind {
	case lexer.LBRACKET, lexer.LBRACE:
		return true
	}
	if isIdentifierToken(n.Kind) && !strings.HasPrefix(n.Value, "#") {
		// `let` followed by `in`/`instanceof` on a new line is an expression.
		if n.NewlineBefore && (n.Kind == lexer.In_ || n.Kind == lexer.Instanceof_) {
			return false
		}
		return true
	}
	return false
}

// usingStartsDeclaration reports whether `using` (at offset n-1) is followed
// by a binding identifier on the same line.
func (p *Parser) usingStartsDeclaration(n int) bool {
	t := p.at(n)
	if t.NewlineBefore || !isIdentifierToken(t.Kind) || strings.HasPrefix(t.Value, "#") {
		return false
	}
	// `using in x` / `using of x` are not declarations.
	return t.Kind != lexer.In_ && t.Kind != lexer.Of
}

func (p *Parser) parseStmtList(end lexer.Kind) []ast.Stmt {
	var stmts []ast.Stmt
	for p.kind() != end && p.kind() != lexer.EOF {
		pos := p.pos
		stmt := p.parseStmt(stmtList)
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
		if p.pos == pos {
			p.unexpected()
			p.next()
		}
	}
	return stmts
}

// parseBody parses a control-flow body (a block or a single statement) into
// a statement list. A block's statements are returned directly; a lone `;`
// yields an empty list.
func (p *Parser) parseBody() []ast.Stmt {
	if p.kind() == lexer.LBRACE {
		p.next()
		p.pushScope(false)
		body := p.parseStmtList(lexer.RBRACE)
		p.popScope()
		p.expect(lexer.RBRACE)
		return body
	}
	stmt := p.parseStmt(stmtSingle)
	if stmt == nil {
		return nil
	}
	if _, empty := stmt.(*ast.EmptyStmt); empty {
		return nil
	}
	return []ast.Stmt{stmt}
}

func (p *Parser) parseBlock() []ast.Stmt {
	p.expect(lexer.LBRACE)
	p.pushScope(false)
	body := p.parseStmtList(lexer.RBRACE)
	p.popScope()
	p.expect(lexer.RBRACE)
	return body
}

// parseCondition parses the parenthesized condition of if/while/do/switch.
func (p *Parser) parseCondition() ast.Expr {
	p.expect(lexer.LPAREN)
	e := p.parseExpressionAllowIn()
	if e == nil {
		p.errWithMsg("expected an expression, got "+describe(p.tok()), "")
	}
	p.expect(lexer.RPAREN)
	return e
}

// directivePrologueIsStrict scans the directive prologue at the current
// position (without consuming it) for "use strict".
func (p *Parser) directivePrologueIsStrict() bool {
	for i := p.pos; i < len(p.tokens); i++ {
		t := p.tokens[i]
		if t.Kind != lexer.String {
			return false
		}
		n := p.tokens[i+1]
		if !(n.Kind == lexer.SEMI || n.Kind == lexer.RBRACE || n.Kind == lexer.EOF || n.NewlineBefore) {
			return false
		}
		if t.Value == `"use strict"` || t.Value == `'use strict'` {
			return true
		}
		if n.Kind == lexer.SEMI {
			i++
		}
	}
	return false
}

func (p *Parser) parseLabeledStmt(ctx stmtCtx) ast.Stmt {
	labelTok := p.next()
	p.next() // `:`
	p.checkEscapedIdent(labelTok)
	if labelTok.Kind == lexer.Yield_ && (p.inGenerator || p.strict) || labelTok.Kind == lexer.Await_ && (p.inAsync || p.inStaticBlock) {
		p.errorAt(labelTok, "`"+labelTok.Value+"` is not a valid label here", "")
	}
	for _, l := range p.labels {
		if l.name == labelTok.Value {
			p.errorAt(labelTok, "label `"+labelTok.Value+"` has already been declared", "use a different label name")
		}
	}
	isLoop := false
	switch p.kind() {
	case lexer.For, lexer.While_, lexer.Do_:
		isLoop = true
	}
	p.labels = append(p.labels, labelInfo{name: labelTok.Value, isLoop: isLoop})
	var body ast.Stmt
	if p.kind() == lexer.Function {
		if p.strict || ctx == stmtSingle {
			p.errWithMsg("labeled function declarations are only allowed in sloppy-mode statement lists", "remove the label")
		}
		body = p.parseFnDecl(p.tok())
	} else {
		body = p.parseStmt(stmtSingle)
	}
	p.labels = p.labels[:len(p.labels)-1]
	if body == nil {
		body = &ast.EmptyStmt{Position: tokPos(labelTok)}
	}
	return &ast.LabeledStmt{Position: tokPos(labelTok), Label: labelTok.Value, Body: body}
}

func (p *Parser) parseIfStmt() ast.Stmt {
	start := p.next()
	stmt := &ast.IfStmt{Position: tokPos(start)}
	stmt.Test = p.parseCondition()
	stmt.Consequent = p.parseIfBody()
	if p.match(lexer.Else_) {
		stmt.Alternate = p.parseIfBody()
	}
	return stmt
}

// parseIfBody parses an if/else branch. Sloppy-mode code may use a bare
// function declaration there (Annex B).
func (p *Parser) parseIfBody() []ast.Stmt {
	if p.kind() == lexer.Function && !p.strict && p.peekN(1) != lexer.STAR {
		p.pushScope(false)
		defer p.popScope()
		if s := p.parseFnDecl(p.tok()); s != nil {
			return []ast.Stmt{s}
		}
		return nil
	}
	return p.parseBody()
}

func (p *Parser) parseLoopBody() []ast.Stmt {
	p.loops++
	p.breakable++
	body := p.parseBody()
	p.loops--
	p.breakable--
	return body
}

func (p *Parser) parseWhileStmt() ast.Stmt {
	start := p.next()
	test := p.parseCondition()
	body := p.parseLoopBody()
	return &ast.WhileStmt{Position: tokPos(start), Test: test, Body: body}
}

func (p *Parser) parseDoWhileStmt() ast.Stmt {
	start := p.next()
	body := p.parseLoopBody()
	p.expect(lexer.While_)
	test := p.parseCondition()
	// A `;` after `do ... while (x)` is optional even without a line break.
	p.match(lexer.SEMI)
	return &ast.DoWhileStmt{Position: tokPos(start), Body: body, Test: test}
}

func (p *Parser) parseWithStmt() ast.Stmt {
	start := p.next()
	if p.strict {
		p.errorAt(start, "`with` statements are not allowed in strict mode", "access the object's properties explicitly")
	}
	p.expect(lexer.LPAREN)
	obj := p.mustExpression()
	p.expect(lexer.RPAREN)
	body := p.parseBody()
	return &ast.WithStmt{Position: tokPos(start), Object: obj, Body: body}
}

func (p *Parser) parseSwitchStmt() ast.Stmt {
	start := p.next()
	disc := p.parseCondition()
	p.expect(lexer.LBRACE)
	p.breakable++
	p.pushScope(false)
	defer p.popScope()
	var cases []*ast.CaseClause
	sawDefault := false
	for p.kind() != lexer.RBRACE && p.kind() != lexer.EOF {
		caseTok := p.tok()
		var test ast.Expr
		switch {
		case p.match(lexer.Case_):
			test = p.parseExpression()
			if test == nil {
				p.errWithMsg("expected an expression after `case`", "")
			}
		case p.match(lexer.Default_):
			if sawDefault {
				p.errorAt(caseTok, "a switch statement may only have one `default` clause", "merge the default clauses")
			}
			sawDefault = true
		default:
			p.unexpected()
			p.breakable--
			p.skipTo(lexer.RBRACE)
			p.match(lexer.RBRACE)
			return &ast.SwitchStmt{Position: tokPos(start), Discriminant: disc, Cases: cases}
		}
		p.expect(lexer.COLON)
		var body []ast.Stmt
		for {
			k := p.kind()
			if k == lexer.RBRACE || k == lexer.Case_ || k == lexer.Default_ || k == lexer.EOF {
				break
			}
			pos := p.pos
			if s := p.parseStmt(stmtList); s != nil {
				body = append(body, s)
			}
			if p.pos == pos {
				p.unexpected()
				p.next()
			}
		}
		cases = append(cases, &ast.CaseClause{Position: tokPos(caseTok), Test: test, Body: body})
	}
	p.breakable--
	p.expect(lexer.RBRACE)
	return &ast.SwitchStmt{Position: tokPos(start), Discriminant: disc, Cases: cases}
}

// skipTo advances to the next token of kind k at the current nesting level.
func (p *Parser) skipTo(k lexer.Kind) {
	for p.kind() != k && p.kind() != lexer.EOF {
		if m := p.pair[p.pos]; m > p.pos {
			p.pos = m
		}
		p.next()
	}
}

func (p *Parser) parseTryStmt() ast.Stmt {
	start := p.next()
	stmt := &ast.TryStmt{Position: tokPos(start)}
	if p.kind() != lexer.LBRACE {
		p.errWithMsg("expected `{` after `try`", "a try statement needs a block: `try { ... } catch (e) { ... }`")
	}
	stmt.Body = p.parseBlock()
	if p.kind() == lexer.Catch_ {
		catchTok := p.next()
		clause := &ast.CatchClause{Position: tokPos(catchTok)}
		p.pushScope(false)
		if p.match(lexer.LPAREN) {
			from := p.pos
			paramTok := p.tok()
			target, names, _ := p.parseBindingTarget()
			if id, ok := target.(*ast.Identifier); ok {
				clause.Param = id.Name
				p.declare(paramTok, id.Name, declCatchParam)
			} else if target != nil {
				clause.ParamTarget = target
				clause.Param = p.tokenText(from, p.pos)
				seen := map[string]bool{}
				for _, n := range names {
					if seen[n] {
						p.errorAt(paramTok, "duplicate binding `"+n+"` in catch parameter", "")
					}
					seen[n] = true
					p.declare(paramTok, n, declLexical)
				}
			} else {
				p.errWithMsg("expected a catch parameter, got "+describe(p.tok()), "omit the parentheses for an unused error: `catch { ... }`")
			}
			if p.kind() == lexer.COLON {
				p.skipTypeAnnotation()
			}
			p.expect(lexer.RPAREN)
		}
		p.expect(lexer.LBRACE)
		clause.Body = p.parseStmtList(lexer.RBRACE)
		p.expect(lexer.RBRACE)
		p.popScope()
		stmt.Catch = clause
	}
	hasFinally := false
	if p.match(lexer.Finally_) {
		hasFinally = true
		stmt.Finally = p.parseBlock()
	}
	if stmt.Catch == nil && !hasFinally {
		p.errWithMsg("missing `catch` or `finally` after `try` block", "add `catch (e) { ... }` or `finally { ... }`")
	}
	return stmt
}

func (p *Parser) parseThrowStmt() ast.Stmt {
	kw := p.next()
	if p.tok().NewlineBefore {
		p.errWithMsg("illegal newline after `throw`", "`throw` and its expression must be on the same line")
	}
	value := p.mustExpression()
	p.consumeSemicolon()
	return &ast.ThrowStmt{Position: tokPos(kw), Value: value}
}

func (p *Parser) parseBreakStmt() ast.Stmt {
	kw := p.next()
	stmt := &ast.BreakStmt{Position: tokPos(kw)}
	if !p.tok().NewlineBefore && isIdentifierToken(p.kind()) && p.kind() != lexer.Of {
		lt := p.next()
		stmt.Label = lt.Value
		if !p.hasLabel(lt.Value, false) {
			p.errorAt(lt, "undefined label `"+lt.Value+"`", "break to a label of an enclosing statement")
		}
	} else if p.breakable == 0 {
		p.errorAt(kw, "`break` outside of a loop or switch", "use `return` to leave a function, or label the enclosing block")
	}
	p.consumeSemicolon()
	return stmt
}

func (p *Parser) parseContinueStmt() ast.Stmt {
	kw := p.next()
	stmt := &ast.ContinueStmt{Position: tokPos(kw)}
	if !p.tok().NewlineBefore && isIdentifierToken(p.kind()) {
		lt := p.next()
		stmt.Label = lt.Value
		if !p.hasLabel(lt.Value, true) {
			p.errorAt(lt, "undefined loop label `"+lt.Value+"`", "`continue` must name the label of an enclosing loop")
		}
	} else if p.loops == 0 {
		p.errorAt(kw, "`continue` outside of a loop", "`continue` may only be used inside for/while/do loops")
	}
	p.consumeSemicolon()
	return stmt
}

func (p *Parser) hasLabel(name string, loopOnly bool) bool {
	for _, l := range p.labels {
		if l.name == name && (!loopOnly || l.isLoop) {
			return true
		}
	}
	return false
}

func (p *Parser) parseReturn() ast.Stmt {
	kw := p.next()
	if !p.inFunction {
		p.errorAt(kw, "`return` outside of a function", "remove the return, or move the code into a function")
	}
	stmt := &ast.ReturnStmt{Position: tokPos(kw)}
	t := p.tok()
	if !t.NewlineBefore && t.Kind != lexer.SEMI && t.Kind != lexer.RBRACE && t.Kind != lexer.EOF {
		stmt.Value = p.mustExpression()
	}
	p.consumeSemicolon()
	return stmt
}

// For statements

func (p *Parser) parseForStmt() ast.Stmt {
	start := p.next()
	isAwait := false
	if p.kind() == lexer.Await_ {
		if !p.inAsync && p.inFunction {
			p.errWithMsg("`for await` is only valid in async functions", "mark the enclosing function `async`")
		}
		p.next()
		isAwait = true
	}
	p.expect(lexer.LPAREN)
	p.pushScope(false)
	defer p.popScope()

	var init ast.Stmt
	var initExpr ast.Expr
	keyword := ""
	initStart := p.pos
	prevNoIn := p.noIn
	p.noIn = true
	switch {
	case p.kind() == lexer.SEMI:
	case p.kind() == lexer.Var_ || p.kind() == lexer.Const_ ||
		(p.kind() == lexer.Let_ && p.letStartsForDeclaration()):
		keyword = p.tok().Value
		init = p.parseVarStmt(false)
	case p.isContextual("using") && p.usingStartsDeclaration(1) && p.at(1).Kind != lexer.Of:
		keyword = "using"
		init = p.parseUsingStmtNoSemi(false)
	case p.kind() == lexer.Await_ && p.at(1).Value == "using" && p.usingStartsDeclaration(2):
		keyword = "await using"
		init = p.parseUsingStmtNoSemi(true)
	default:
		initExpr = p.parseExpression()
		if initExpr != nil {
			init = &ast.ExprStmt{Position: initExpr.Pos(), Expression: initExpr}
		}
	}
	p.noIn = prevNoIn

	if p.kind() == lexer.In_ || p.kind() == lexer.Of {
		isOf := p.kind() == lexer.Of
		if isAwait && !isOf {
			p.errWithMsg("`for await` requires `of`", "write `for await (const x of iterable)`")
		}
		var left ast.Expr
		if vs, ok := init.(*ast.VarStmt); ok {
			if len(vs.Decls) != 1 {
				p.errAtPos(vs.Position, "a for-"+map[bool]string{true: "of", false: "in"}[isOf]+" loop declares exactly one binding", "")
			}
			if len(vs.Decls) > 0 {
				d := vs.Decls[0]
				if d.Init != nil && (isOf || p.strict || vs.Kind != ast.VarVar || d.IsDestructuring) {
					p.errAtPos(vs.Position, "for-in/of loop variable may not have an initializer", "remove the `= ...` initializer")
				}
				if d.IsDestructuring {
					left = d.Target
				} else {
					left = &ast.Identifier{Position: vs.Position, Name: d.Name}
				}
			}
		} else if initExpr != nil {
			if initStart < len(p.tokens) && p.tokens[initStart].Kind == lexer.Let_ && isOf {
				p.errorAt(p.tokens[initStart], "the left side of a for-of loop may not start with `let`", "declare the variable: `for (let x of ...)`")
			}
			if isOf && p.tokens[initStart].Kind == lexer.Async && p.tokens[initStart+1].Kind == lexer.Of && !isAwait {
				p.errorAt(p.tokens[initStart], "the left side of a for-of loop may not be `async`", "rename the variable")
			}
			left = p.toAssignTarget(initExpr, true)
		} else {
			p.unexpected()
		}
		p.next()
		var right ast.Expr
		if isOf {
			right = p.mustAssign()
		} else {
			right = p.mustExpression()
		}
		p.expect(lexer.RPAREN)
		body := p.parseLoopBody()
		return &ast.ForInStmt{Position: tokPos(start), Await: isAwait, Left: left, Right: right, Body: body, IsForOf: isOf, Keyword: keyword}
	}
	if isAwait {
		p.errWithMsg("`for await` requires `of`", "write `for await (const x of iterable)`")
	}
	if vs, ok := init.(*ast.VarStmt); ok {
		for _, d := range vs.Decls {
			if d.Init == nil && (vs.Kind == ast.VarConst || d.IsDestructuring) {
				p.errAtPos(vs.Position, "missing initializer in declaration", "add `= value`")
			}
		}
	}
	p.expect(lexer.SEMI)
	var test, update ast.Expr
	if p.kind() != lexer.SEMI {
		test = p.mustExpression()
	}
	p.expect(lexer.SEMI)
	if p.kind() != lexer.RPAREN {
		update = p.mustExpression()
	}
	p.expect(lexer.RPAREN)
	body := p.parseLoopBody()
	return &ast.ForStmt{Position: tokPos(start), Init: init, Test: test, Update: update, Body: body}
}

// letStartsForDeclaration is letStartsDeclaration for a for-statement head,
// where `for (let in x)` and `for (let.x of y)` use `let` as an identifier.
func (p *Parser) letStartsForDeclaration() bool {
	n := p.at(1)
	if n.Kind == lexer.In_ || n.Kind == lexer.Of && p.at(2).Kind != lexer.ASSIGN {
		return n.Kind == lexer.Of && false
	}
	return p.letStartsDeclaration()
}

// Variable declarations

// parseVarStmt parses `var|let|const declarators`. withSemi consumes the
// trailing semicolon (false inside a for head).
func (p *Parser) parseVarStmt(withSemi bool) ast.Stmt {
	kwTok := p.next()
	kind := ast.VarConst
	switch kwTok.Kind {
	case lexer.Let_:
		kind = ast.VarLet
	case lexer.Var_:
		kind = ast.VarVar
	}
	stmt := &ast.VarStmt{Position: tokPos(kwTok), Kind: kind}
	p.parseDeclarators(stmt, withSemi)
	if withSemi {
		p.consumeSemicolon()
	}
	return stmt
}

func (p *Parser) parseUsingStmt(await bool) ast.Stmt {
	s := p.parseUsingStmtNoSemi(await)
	p.consumeSemicolon()
	return s
}

func (p *Parser) parseUsingStmtNoSemi(await bool) ast.Stmt {
	start := p.tok()
	kind := ast.VarUsing
	if await {
		kind = ast.VarAwaitUsing
		p.next()
	}
	p.next() // `using`
	stmt := &ast.VarStmt{Position: tokPos(start), Kind: kind}
	for {
		if !p.identOK() {
			p.errWithMsg("expected a binding name after `using`", "`using` declarations bind plain identifiers, not patterns")
			break
		}
		name := p.next()
		p.checkBindingName(name)
		p.declare(name, name.Value, declLexical)
		p.skipOptionalTypeAnnotation()
		decl := &ast.VarDecl{Name: name.Value}
		if p.match(lexer.ASSIGN) {
			decl.Init = p.mustAssign()
		} else if p.kind() != lexer.Of {
			p.errWithMsg("missing initializer in `using` declaration", "add `= value`")
		}
		stmt.Decls = append(stmt.Decls, decl)
		if !p.match(lexer.COMMA) {
			break
		}
	}
	return stmt
}

func (p *Parser) parseDeclarators(stmt *ast.VarStmt, requireInit bool) {
	lexical := stmt.Kind == ast.VarLet || stmt.Kind == ast.VarConst
	for {
		declTok := p.tok()
		decl := &ast.VarDecl{}
		dk := declVar
		if lexical {
			dk = declLexical
		}
		if p.kind() == lexer.LBRACKET || p.kind() == lexer.LBRACE {
			from := p.pos
			target, names, rest := p.parseBindingTarget()
			decl.IsDestructuring = true
			decl.Target = target
			decl.Pattern = p.tokenText(from, p.pos)
			decl.Names = names
			decl.RestName = rest
			for _, n := range names {
				if lexical && n == "let" {
					p.errorAt(declTok, "`let` cannot be used as a lexically bound name", "choose a different name")
				}
			}
			p.declareNames(declTok, names, dk)
		} else if p.identOK() {
			name := p.next()
			if lexical && name.Value == "let" {
				p.errorAt(name, "`let` cannot be used as a lexically bound name", "choose a different name")
			}
			p.checkBindingName(name)
			p.checkEscapedIdent(name)
			decl.Name = name.Value
			p.declare(name, name.Value, dk)
		} else {
			p.errWithMsg("expected a variable name, got "+describe(p.tok()), "names must be identifiers (not reserved words) or destructuring patterns")
			return
		}
		// TS definite assignment assertion `let x!: number`.
		if p.kind() == lexer.NOT && p.peekN(1) == lexer.COLON {
			p.next()
		}
		p.skipOptionalTypeAnnotation()
		if p.match(lexer.ASSIGN) {
			decl.Init = p.mustAssign()
		} else if requireInit && (stmt.Kind == ast.VarConst || decl.IsDestructuring) {
			if !p.inAmbientContext() {
				p.errorAt(declTok, "missing initializer in "+map[bool]string{true: "destructuring", false: "const"}[decl.IsDestructuring]+" declaration", "add `= value`")
			}
		}
		stmt.Decls = append(stmt.Decls, decl)
		if !p.match(lexer.COMMA) {
			break
		}
	}
}

// inAmbientContext is reserved for `declare` contexts; ambient declarations
// are skipped before they reach parseDeclarators, so it is always false.
func (p *Parser) inAmbientContext() bool { return false }

// Functions

// parseFnDecl parses a function declaration whose first token (`function`, or
// a consumed `async`) is start. It returns nil for a TypeScript overload
// signature (no body), which is type-only.
func (p *Parser) parseFnDecl(start lexer.Token) ast.Stmt {
	isAsync := start.Kind == lexer.Async
	p.expect(lexer.Function)
	isGen := p.match(lexer.STAR)
	fn := &ast.FnDecl{Position: tokPos(start), Async: isAsync, Generator: isGen}
	var nameTok lexer.Token
	if p.identOK() || (p.kind() == lexer.Yield_ && !p.strict && !p.inGenerator) || (p.kind() == lexer.Await_ && !p.inAsync && !p.inStaticBlock) {
		nameTok = p.next()
		p.checkBindingName(nameTok)
		p.checkEscapedIdent(nameTok)
		fn.Name = nameTok.Value
	} else if p.kind() != lexer.LPAREN && p.kind() != lexer.LT {
		p.errWithMsg("expected a function name, got "+describe(p.tok()), "")
	}
	params, body, hasBody := p.parseFunctionRest(isAsync, isGen, false)
	if !hasBody {
		// `function f(a: string): void;` - a TypeScript overload signature.
		p.droppedTypes++
		return nil
	}
	if fn.Name != "" {
		kind := declFunction
		if isAsync || isGen {
			if p.scope != nil && !p.scope.fn {
				kind = declLexical
			}
		}
		p.declare(nameTok, fn.Name, kind)
	}
	fn.Params = params
	fn.Body = body
	return fn
}

// parseFunctionRest parses `<T>(params): Ret { body }` for functions and
// methods. hasBody is false for a body-less TypeScript signature.
func (p *Parser) parseFunctionRest(isAsync, isGen, isMethod bool) ([]*ast.Param, []ast.Stmt, bool) {
	p.skipTypeParams()
	saved := p.saveFnContext()
	derived := p.derivedCtor
	p.derivedCtor = false
	p.inAsync, p.inGenerator, p.inFunction = isAsync, isGen, true
	p.superProp, p.superCall = isMethod, isMethod && derived
	p.inFieldInit, p.inStaticBlock, p.yieldIsError = false, false, false
	p.pushScope(true)
	defer p.popScope()
	if !p.match(lexer.LPAREN) {
		p.errWithMsg("expected `(` to start the parameter list, got "+describe(p.tok()), "")
		p.restoreFnContext(saved)
		return nil, nil, true
	}
	p.inParams = true
	params := p.parseParams()
	p.inParams = false
	names := p.paramNames
	p.expect(lexer.RPAREN)
	p.skipReturnType()
	if p.kind() != lexer.LBRACE {
		if p.kind() == lexer.SEMI || p.tok().NewlineBefore || p.kind() == lexer.EOF || p.kind() == lexer.RBRACE {
			p.match(lexer.SEMI)
			p.restoreFnContext(saved)
			return params, nil, false
		}
		p.errWithMsg("expected `{` to start the function body, got "+describe(p.tok()), "")
		p.restoreFnContext(saved)
		return params, nil, true
	}
	p.declareParams(names)
	body := p.parseFunctionBodyChecked(params, names, isMethod)
	p.restoreFnContext(saved)
	return params, body, true
}

func (p *Parser) declareParams(names []lexer.Token) {
	for _, t := range names {
		p.declare(t, t.Value, declParam)
	}
}

// parseFunctionBodyChecked parses a body and then validates the parameter
// names, whose rules depend on whether the body turned out to be strict.
func (p *Parser) parseFunctionBodyChecked(params []*ast.Param, names []lexer.Token, unique bool) []ast.Stmt {
	body := p.parseFunctionBody(params)
	// parseFunctionBody leaves p.strict set for a "use strict" body.
	p.checkParamNames(names, params, unique)
	return body
}

type fnContext struct {
	inFunction, inAsync, inGenerator, strict         bool
	superProp, superCall, inFieldInit, inStaticBlock bool
	inParams, yieldIsError                           bool
	labels                                           []labelInfo
	breakable, loops                                 int
}

func (p *Parser) saveFnContext() fnContext {
	return fnContext{
		inFunction: p.inFunction, inAsync: p.inAsync, inGenerator: p.inGenerator, strict: p.strict,
		superProp: p.superProp, superCall: p.superCall, inFieldInit: p.inFieldInit, inStaticBlock: p.inStaticBlock,
		inParams: p.inParams, yieldIsError: p.yieldIsError,
		labels: p.labels, breakable: p.breakable, loops: p.loops,
	}
}

func (p *Parser) restoreFnContext(c fnContext) {
	p.inFunction, p.inAsync, p.inGenerator, p.strict = c.inFunction, c.inAsync, c.inGenerator, c.strict
	p.superProp, p.superCall, p.inFieldInit, p.inStaticBlock = c.superProp, c.superCall, c.inFieldInit, c.inStaticBlock
	p.inParams, p.yieldIsError = c.inParams, c.yieldIsError
	p.labels, p.breakable, p.loops = c.labels, c.breakable, c.loops
}

// checkParamNames reports duplicate parameter names (an error in strict
// code, arrows, methods and functions with non-simple parameters) and
// strict-mode-only invalid names.
func (p *Parser) checkParamNames(names []lexer.Token, params []*ast.Param, alwaysUnique bool) {
	seen := map[string]bool{}
	unique := alwaysUnique || p.strict || !simpleParams(params)
	for _, t := range names {
		if seen[t.Value] && unique {
			p.errorAt(t, "duplicate parameter name `"+t.Value+"`", "rename one of the parameters")
		}
		seen[t.Value] = true
		if p.strict {
			p.checkBindingName(t)
		}
	}
}

// parseFunctionBody parses `{ ... }` in a function context already set up by
// the caller (inAsync/inGenerator/inFunction).
func (p *Parser) parseFunctionBody(params []*ast.Param) []ast.Stmt {
	p.expect(lexer.LBRACE)
	p.labels, p.breakable, p.loops = nil, 0, 0
	if p.directivePrologueIsStrict() {
		if !simpleParams(params) {
			p.errWithMsg("\"use strict\" is not allowed in a function with non-simple parameters", "move the directive to the enclosing scope")
		}
		p.strict = true
	}
	body := p.parseStmtList(lexer.RBRACE)
	p.expect(lexer.RBRACE)
	return body
}

func simpleParams(params []*ast.Param) bool {
	for _, prm := range params {
		if prm.IsRest || prm.Default != nil || prm.Target != nil {
			return false
		}
	}
	return true
}

// parseParams parses a parameter list up to (not including) the `)`.
func (p *Parser) parseParams() []*ast.Param {
	var params []*ast.Param
	p.paramNames = nil
	var nameToks []lexer.Token
	defer func() { p.paramNames = nameToks }()
	for p.kind() != lexer.RPAREN && p.kind() != lexer.EOF {
		start := p.tok()
		if p.kind() == lexer.AT {
			p.parseDecorators()
		}
		// TypeScript parameter properties carry runtime semantics (they assign
		// `this.x = x`), so they cannot be skipped like other modifiers.
		for p.isParamModifier() {
			mod := p.next()
			p.errorAt(mod, "TypeScript parameter property `"+mod.Value+"` is not supported",
				"declare the field in the class body and assign it in the constructor: `this.x = x`")
		}
		// A `this` parameter (`method(this: Counter)`) is compile-time only.
		if p.kind() == lexer.This_ {
			p.next()
			p.skipOptionalTypeAnnotation()
			p.droppedTypes++
			if !p.match(lexer.COMMA) {
				break
			}
			continue
		}
		param := &ast.Param{Position: tokPos(start)}
		if p.match(lexer.SPREAD) {
			param.IsRest = true
		}
		from := p.pos
		target, names, _ := p.parseBindingTarget()
		for i := from; i < p.pos && len(names) > 0; i++ {
			if t := p.tokens[i]; isIdentifierToken(t.Kind) && t.Value == names[0] {
				nameToks = append(nameToks, t)
				names = names[1:]
			}
		}
		switch t := target.(type) {
		case nil:
			p.errWithMsg("expected a parameter name, got "+describe(p.tok()), "")
			return params
		case *ast.Identifier:
			param.Name = t.Name
		case *ast.ObjectExpr:
			param.Name = "{...}"
			param.Pattern = p.tokenText(from, p.pos)
			param.Target = target
		default:
			param.Name = "[...]"
			param.Pattern = p.tokenText(from, p.pos)
			param.Target = target
		}
		if p.kind() == lexer.QUEST {
			p.next()
			p.droppedTypes++
		}
		p.skipOptionalTypeAnnotation()
		if p.match(lexer.ASSIGN) {
			if param.IsRest {
				p.errWithMsg("a rest parameter cannot have a default value", "remove the `= ...` default")
			}
			param.Default = p.mustAssign()
		}
		params = append(params, param)
		if param.IsRest && p.kind() == lexer.COMMA {
			p.errWithMsg("a rest parameter must be the last parameter", "move `...rest` to the end of the list")
		}
		if !p.match(lexer.COMMA) {
			break
		}
	}
	return params
}

func (p *Parser) isParamModifier() bool {
	t := p.tok()
	switch t.Kind {
	case lexer.Public_, lexer.Private_, lexer.Protected_, lexer.Readonly_:
	case lexer.Identifier:
		if t.Value != "override" {
			return false
		}
	default:
		return false
	}
	// A modifier is followed by the parameter name (not `,` `)` `:` `=` `?`).
	n := p.at(1)
	return isIdentifierToken(n.Kind) || n.Kind == lexer.LBRACE || n.Kind == lexer.LBRACKET ||
		n.Kind == lexer.Public_ || n.Kind == lexer.Private_ || n.Kind == lexer.Protected_ || n.Kind == lexer.Readonly_
}

// Imports and exports

func (p *Parser) parseImport() ast.Stmt {
	start := p.next()
	stmt := &ast.ImportStmt{Position: tokPos(start)}

	// `import type X from`, `import type { A } from`: type-only, dropped.
	if p.kind() == lexer.Type_ {
		n := p.at(1)
		if n.Kind == lexer.LBRACE || n.Kind == lexer.STAR || (isIdentifierToken(n.Kind) && n.Kind != lexer.From) ||
			(n.Kind == lexer.From && p.peekN(2) == lexer.From) {
			p.skipToStatementEnd()
			p.droppedTypes++
			return nil
		}
	}
	if p.kind() == lexer.String {
		stmt.Source = p.next().Value
		p.parseImportAttributes(&stmt.Attributes)
		p.consumeSemicolon()
		return stmt
	}
	if p.identOK() {
		name := p.next()
		if p.kind() == lexer.ASSIGN {
			p.errorAt(start, "TypeScript `import x = ...` is not supported", "use `import x from '...'` or `const x = ...`")
			p.skipToStatementEnd()
			return nil
		}
		stmt.Default = name.Value
		p.checkBindingName(name)
		if !p.match(lexer.COMMA) {
			goto from
		}
	}
	switch p.kind() {
	case lexer.LBRACE:
		p.next()
		p.parseNamedImports(&stmt.Named)
	case lexer.STAR:
		p.next()
		if !p.isContextual("as") {
			p.errWithMsg("expected `as` after `import *`", "write `import * as name from '...'`")
			break
		}
		p.next()
		if !p.identOK() {
			p.errWithMsg("expected a namespace name after `as`", "")
			break
		}
		nt := p.next()
		p.checkBindingName(nt)
		stmt.Namespace = nt.Value
	default:
		p.errWithMsg("unexpected "+describe(p.tok())+" in import declaration", "write `import name from '...'`, `import { a } from '...'` or `import * as ns from '...'`")
		p.skipToStatementEnd()
		return stmt
	}
from:
	if !p.isContextual("from") {
		p.errWithMsg("expected `from` in import declaration, got "+describe(p.tok()), "")
		p.skipToStatementEnd()
		return stmt
	}
	p.next()
	if p.kind() != lexer.String {
		p.errWithMsg("expected a module specifier string after `from`", "")
		return stmt
	}
	stmt.Source = p.next().Value
	p.parseImportAttributes(&stmt.Attributes)
	p.consumeSemicolon()
	return stmt
}

// parseNamedImports parses `a, b as c, "str" as d, type T }` after the `{`.
func (p *Parser) parseNamedImports(named *[]ast.NamedImport) {
	for p.kind() != lexer.RBRACE && p.kind() != lexer.EOF {
		// Inline `type` specifier (`import { type Foo }`): type-only, dropped.
		if p.kind() == lexer.Type_ && (isWordKind(p.peekN(1)) || p.peekN(1) == lexer.String) &&
			!(p.peekN(1) == lexer.As && (p.peekN(2) == lexer.COMMA || p.peekN(2) == lexer.RBRACE)) {
			p.next()
			p.next()
			if p.isContextual("as") {
				p.next()
				p.next()
			}
			p.droppedTypes++
			if !p.match(lexer.COMMA) {
				break
			}
			continue
		}
		t := p.tok()
		var remote string
		isString := false
		switch {
		case t.Kind == lexer.String:
			remote = decodeStringToken(t.Value)
			isString = true
		case isWordToken(t):
			remote = t.Value
		default:
			p.errWithMsg("expected an import name, got "+describe(t), "")
			p.skipTo(lexer.RBRACE)
			p.match(lexer.RBRACE)
			return
		}
		p.next()
		local := remote
		if p.isContextual("as") {
			p.next()
			if !p.identOK() {
				p.errWithMsg("expected a local binding name after `as`, got "+describe(p.tok()), "")
			} else {
				lt := p.next()
				p.checkBindingName(lt)
				local = lt.Value
			}
		} else if isString || !isIdentifierToken(t.Kind) {
			p.errorAt(t, "`"+remote+"` cannot be imported without a local name", "write `"+t.Value+" as localName`")
		} else {
			p.checkBindingName(t)
		}
		*named = append(*named, ast.NamedImport{Local: local, Remote: remote})
		if !p.match(lexer.COMMA) {
			break
		}
	}
	p.expect(lexer.RBRACE)
}

// parseImportAttributes parses an optional `with { type: "json" }` clause (or
// the legacy `assert { ... }` spelling).
func (p *Parser) parseImportAttributes(out *[]ast.ImportAttribute) {
	if !(p.kind() == lexer.With_ || (p.isContextual("assert") && !p.tok().NewlineBefore)) {
		return
	}
	p.next()
	if !p.match(lexer.LBRACE) {
		p.errWithMsg("expected `{` after `with` in import attributes", "write `with { type: \"json\" }`")
		return
	}
	seen := map[string]bool{}
	for p.kind() != lexer.RBRACE && p.kind() != lexer.EOF {
		kt := p.tok()
		key := ""
		switch {
		case kt.Kind == lexer.String:
			key = decodeStringToken(kt.Value)
		case isWordToken(kt):
			key = kt.Value
		default:
			p.errWithMsg("expected an import attribute name", "")
			p.skipTo(lexer.RBRACE)
		}
		if key == "" && kt.Kind != lexer.String {
			break
		}
		p.next()
		if seen[key] {
			p.errorAt(kt, "duplicate import attribute `"+key+"`", "")
		}
		seen[key] = true
		p.expect(lexer.COLON)
		if p.kind() != lexer.String {
			p.errWithMsg("import attribute values must be strings", "")
			break
		}
		*out = append(*out, ast.ImportAttribute{Key: key, Value: decodeStringToken(p.next().Value)})
		if !p.match(lexer.COMMA) {
			break
		}
	}
	p.expect(lexer.RBRACE)
}

// skipToStatementEnd skips tokens to the end of the current statement.
func (p *Parser) skipToStatementEnd() {
	for {
		k := p.kind()
		if k == lexer.EOF || k == lexer.RBRACE {
			return
		}
		if k == lexer.SEMI {
			p.next()
			return
		}
		if m := p.pair[p.pos]; m > p.pos {
			p.pos = m
		}
		p.next()
		if p.tok().NewlineBefore && !continuesStatement(p.kind()) {
			return
		}
	}
}

// continuesStatement reports token kinds that continue a statement across a
// line break (used when skipping TypeScript-only declarations).
func continuesStatement(k lexer.Kind) bool {
	switch k {
	case lexer.DOT, lexer.COMMA, lexer.BIT_OR, lexer.BIT_AND, lexer.ASSIGN, lexer.ARROW,
		lexer.COLON, lexer.QUEST, lexer.LBRACE, lexer.GT, lexer.LT:
		return true
	}
	return false
}

func (p *Parser) parseExport() ast.Stmt {
	start := p.next()
	exp := &ast.ExportStmt{Position: tokPos(start)}

	if p.match(lexer.Default_) {
		exp.Default = true
		t := p.tok()
		switch {
		case t.Kind == lexer.Function:
			exp.Declaration = p.parseFnDecl(t)
		case t.Kind == lexer.Async && p.peekN(1) == lexer.Function && !p.at(1).NewlineBefore:
			p.next()
			exp.Declaration = p.parseFnDecl(t)
		case t.Kind == lexer.Class_:
			exp.Declaration = p.parseClassDecl()
		case t.Value == "abstract" && p.peekN(1) == lexer.Class_:
			p.next()
			exp.Declaration = p.parseClassDecl()
		case t.Kind == lexer.Interface_ && isIdentifierToken(p.peekN(1)):
			p.skipInterfaceDecl()
			return nil
		case t.Kind == lexer.AT:
			p.parseDecorators()
			exp.Declaration = p.parseClassDecl()
		default:
			expr := p.mustAssign()
			if expr == nil {
				p.unexpected()
				return exp
			}
			if id, ok := expr.(*ast.Identifier); ok {
				exp.Local = id.Name
			} else {
				exp.Declaration = &ast.ExprStmt{Position: expr.Pos(), Expression: expr}
			}
			p.consumeSemicolon()
		}
		if exp.Declaration == nil && exp.Local == "" {
			return nil // an overload signature
		}
		return exp
	}

	t := p.tok()
	switch {
	case t.Kind == lexer.Var_ || t.Kind == lexer.Const_ && p.peekN(1) != lexer.Enum_ || t.Kind == lexer.Let_:
		exp.Declaration = p.parseVarStmt(true)
	case t.Kind == lexer.Const_ || t.Kind == lexer.Enum_:
		return p.parseEnumDecl(false)
	case t.Kind == lexer.Function:
		exp.Declaration = p.parseFnDecl(t)
	case t.Kind == lexer.Async && p.peekN(1) == lexer.Function:
		p.next()
		exp.Declaration = p.parseFnDecl(t)
	case t.Kind == lexer.Class_:
		exp.Declaration = p.parseClassDecl()
	case t.Value == "abstract" && p.peekN(1) == lexer.Class_:
		p.next()
		exp.Declaration = p.parseClassDecl()
	case t.Kind == lexer.AT:
		p.parseDecorators()
		exp.Declaration = p.parseClassDecl()
	case t.Kind == lexer.Interface_:
		p.skipInterfaceDecl()
		return nil
	case t.Kind == lexer.Type_ && (p.peekN(1) == lexer.LBRACE || p.peekN(1) == lexer.STAR):
		// `export type { A }` / `export type * from '...'`: type-only.
		p.skipToStatementEnd()
		p.droppedTypes++
		return nil
	case t.Kind == lexer.Type_:
		p.skipTypeAliasDecl()
		return nil
	case t.Kind == lexer.Declare_:
		p.skipDeclare()
		return nil
	case t.Kind == lexer.Namespace_ || t.Value == "module" && !p.at(1).NewlineBefore:
		return p.parseNamespace(true)
	case t.Kind == lexer.Import:
		p.errorAt(t, "TypeScript `export import x = ...` is not supported", "use `import` and `export` separately")
		p.skipToStatementEnd()
		return nil
	case t.Kind == lexer.ASSIGN:
		p.errorAt(t, "TypeScript `export =` is not supported", "use `export default` instead")
		p.skipToStatementEnd()
		return nil
	case t.Kind == lexer.As && p.at(1).Kind == lexer.Namespace_:
		// `export as namespace X;` (UMD global declaration) is type-only.
		p.skipToStatementEnd()
		p.droppedTypes++
		return nil
	case t.Kind == lexer.STAR:
		p.next()
		exp.StarReexport = true
		if p.isContextual("as") {
			p.next()
			nt := p.tok()
			switch {
			case nt.Kind == lexer.String:
				exp.Namespace = decodeStringToken(nt.Value)
				p.next()
			case isWordToken(nt):
				exp.Namespace = nt.Value
				p.next()
			default:
				p.errWithMsg("expected a name after `export * as`", "")
			}
		}
		if !p.isContextual("from") {
			p.errWithMsg("expected `from` after `export *`", "write `export * from '...'`")
			return exp
		}
		p.next()
		if p.kind() != lexer.String {
			p.errWithMsg("expected a module specifier string after `from`", "")
			return exp
		}
		exp.ReexportSource = p.next().Value
		p.parseImportAttributes(&exp.Attributes)
		p.consumeSemicolon()
		return exp
	case t.Kind == lexer.LBRACE:
		specs, localTokens := p.parseExportSpecifiers()
		if p.isContextual("from") {
			p.next()
			if p.kind() != lexer.String {
				p.errWithMsg("expected a module specifier string after `from`", "")
				return exp
			}
			exp.ReexportSource = p.next().Value
			names := make([]string, 0, len(specs))
			for _, s := range specs {
				names = append(names, s.Remote)
			}
			exp.Local = strings.Join(names, ",")
			exp.ReexportNamed = specs
			p.parseImportAttributes(&exp.Attributes)
		} else {
			for _, lt := range localTokens {
				if lt.Kind == lexer.String || !isIdentifierToken(lt.Kind) {
					p.errorAt(lt, "`"+lt.Value+"` is not a local binding that can be exported", "export a declared name, or re-export it with `from '...'`")
				}
			}
			exp.Named = specs
		}
		p.consumeSemicolon()
		return exp
	default:
		p.errWithMsg("unexpected "+describe(t)+" after `export`", "export a declaration (`export const x = 1`) or a list (`export { x }`)")
		p.skipToStatementEnd()
		return exp
	}
	if exp.Declaration == nil {
		return nil // type-only declaration
	}
	return exp
}

// parseExportSpecifiers parses `{ a, b as c, x as default, "s" as t }`. It
// also returns each specifier's local-name token for validation.
func (p *Parser) parseExportSpecifiers() ([]ast.NamedExport, []lexer.Token) {
	p.next() // `{`
	var specs []ast.NamedExport
	var locals []lexer.Token
	name := func() (string, lexer.Token, bool) {
		t := p.tok()
		switch {
		case t.Kind == lexer.String:
			p.next()
			return decodeStringToken(t.Value), t, true
		case isWordToken(t):
			p.next()
			return t.Value, t, true
		}
		return "", t, false
	}
	for p.kind() != lexer.RBRACE && p.kind() != lexer.EOF {
		// Inline `type` specifier: type-only.
		if p.kind() == lexer.Type_ && (isWordKind(p.peekN(1)) || p.peekN(1) == lexer.String) && p.peekN(1) != lexer.As {
			p.next()
			p.next()
			if p.isContextual("as") {
				p.next()
				p.next()
			}
			p.droppedTypes++
			if !p.match(lexer.COMMA) {
				break
			}
			continue
		}
		local, lt, ok := name()
		if !ok {
			p.errWithMsg("expected an export name, got "+describe(p.tok()), "")
			p.skipTo(lexer.RBRACE)
			break
		}
		remote := local
		if p.isContextual("as") {
			p.next()
			r, _, ok := name()
			if !ok {
				p.errWithMsg("expected an exported name after `as`", "")
			} else {
				remote = r
			}
		}
		specs = append(specs, ast.NamedExport{Local: local, Remote: remote})
		locals = append(locals, lt)
		if !p.match(lexer.COMMA) {
			break
		}
	}
	p.expect(lexer.RBRACE)
	return specs, locals
}

// decodeStringToken turns a lexer String token (raw source text including the
// delimiter quotes) into its runtime value: the delimiters are stripped and
// escape sequences are decoded. JSX attribute strings do NOT use this - JSX
// does not process backslash escapes.
func decodeStringToken(raw string) string {
	if len(raw) >= 2 {
		raw = raw[1 : len(raw)-1]
	}
	return unescapeJS(raw)
}

// TypeScript declarations with runtime meaning

// parseEnumDecl reports an enum (which has runtime values the parser cannot
// represent) and skips it. ambient enums (`declare enum`) are type-only.
func (p *Parser) parseEnumDecl(ambient bool) ast.Stmt {
	start := p.tok()
	if p.kind() == lexer.Const_ {
		p.next()
	}
	p.expect(lexer.Enum_)
	if ambient {
		p.droppedTypes++
	} else {
		p.errorAt(start, "TypeScript `enum` declarations are not supported by the Krate parser",
			"replace it with a const object, e.g. `const Color = { Red: 0, Green: 1 } as const`")
	}
	if isWordKind(p.kind()) {
		p.next()
	}
	if p.kind() == lexer.LBRACE {
		p.skipBalanced()
	}
	return nil
}

// parseNamespace skips a `namespace`/`module`/`global` block. A body holding
// only types is dropped; one with runtime members is an error, since dropping
// it would silently lose code.
func (p *Parser) parseNamespace(exported bool) ast.Stmt {
	start := p.next() // namespace / module / global
	for isWordKind(p.kind()) || p.kind() == lexer.String || p.kind() == lexer.DOT {
		p.next()
	}
	if p.kind() != lexer.LBRACE {
		p.consumeSemicolon()
		p.droppedTypes++
		return nil
	}
	p.next()
	errsBefore := len(p.errs)
	var body []ast.Stmt
	for p.kind() != lexer.RBRACE && p.kind() != lexer.EOF {
		pos := p.pos
		if st := p.parseStmt(stmtTop); st != nil {
			body = append(body, st)
		}
		if p.pos == pos {
			p.unexpected()
			p.next()
		}
	}
	p.expect(lexer.RBRACE)
	if len(body) > 0 && len(p.errs) == errsBefore {
		p.errorAt(start, "TypeScript namespaces with runtime members are not supported",
			"move the members to a module and import them, or use a plain object")
	}
	p.droppedTypes++
	_ = exported
	return nil
}

// skipDeclare skips an ambient `declare ...` declaration (type-only).
func (p *Parser) skipDeclare() ast.Stmt {
	p.next() // declare
	p.droppedTypes++
	switch {
	case p.kind() == lexer.Const_ && p.peekN(1) == lexer.Enum_, p.kind() == lexer.Enum_:
		return p.parseEnumDecl(true)
	case p.kind() == lexer.Namespace_ || p.isContextual("module") || p.isContextual("global"):
		p.next()
		for isWordKind(p.kind()) || p.kind() == lexer.String || p.kind() == lexer.DOT {
			p.next()
		}
		if p.kind() == lexer.LBRACE {
			p.skipBalanced()
		} else {
			p.consumeSemicolon()
		}
		return nil
	case p.kind() == lexer.Interface_:
		return p.skipInterfaceDecl()
	case p.kind() == lexer.Type_:
		return p.skipTypeAliasDecl()
	case p.kind() == lexer.Class_ || p.isContextual("abstract"):
		for p.kind() != lexer.LBRACE && p.kind() != lexer.EOF {
			p.next()
		}
		p.skipBalanced()
		return nil
	case p.kind() == lexer.Function || p.kind() == lexer.Async:
		for p.kind() != lexer.LPAREN && p.kind() != lexer.EOF {
			p.next()
		}
		p.skipBalanced()
		p.skipReturnType()
		p.consumeSemicolon()
		return nil
	}
	// declare const/let/var x: T, y: U;
	p.next()
	for {
		if isWordKind(p.kind()) {
			p.next()
		} else if p.kind() == lexer.LBRACE || p.kind() == lexer.LBRACKET {
			p.skipBalanced()
		}
		p.skipOptionalTypeAnnotation()
		if p.match(lexer.ASSIGN) {
			p.parseAssign()
		}
		if !p.match(lexer.COMMA) {
			break
		}
	}
	p.consumeSemicolon()
	return nil
}

// skipBalanced skips a bracketed region starting at the current token.
func (p *Parser) skipBalanced() {
	if m := p.pair[p.pos]; m > p.pos {
		p.pos = m
		p.next()
		return
	}
	// Unbalanced: skip to EOF.
	p.errWithMsg("unbalanced "+describe(p.tok()), "check for a missing closing bracket")
	p.pos = len(p.tokens) - 1
}

// parseDecorators reports decorators (which run code the parser cannot
// represent) and skips them.
func (p *Parser) parseDecorators() {
	for p.kind() == lexer.AT {
		at := p.next()
		p.errorAt(at, "decorators are not supported by the Krate parser",
			"remove the decorator and apply it explicitly, e.g. `const C = decorate(class { ... })`")
		if p.kind() == lexer.LPAREN {
			p.skipBalanced()
		} else {
			if isWordKind(p.kind()) {
				p.next()
			}
			for p.kind() == lexer.DOT && isWordKind(p.peekN(1)) {
				p.next()
				p.next()
			}
			if p.kind() == lexer.LPAREN {
				p.skipBalanced()
			}
		}
	}
}
