package parser

import (
	"fmt"
	"html"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// Diagnostic is an alias for the shared compiler diagnostic type.
type Diagnostic = diag.Diagnostic

type Parser struct {
	tokens   []lexer.Token
	pos      int
	errs     []error
	Filename string
	srcLines []string

	// droppedTypes counts type-only constructs the parser intentionally
	// discards (interfaces, type aliases, enums, classes, annotations,
	// generics, `as`/angle-bracket casts). A non-zero count means the AST
	// cannot reproduce the original source - used by tooling that round-trips
	// the AST back to source (see internal/astprint and the MCP edit_ast tool).
	droppedTypes int

	// noIn disables `in` as a binary operator while parsing a `for` statement's
	// initializer, so `for (x in obj)` is not consumed as `x in obj` and the
	// for-in branch is reachable. It is set only around that init expression.
	noIn bool

	// arrowFrom is a lazily built index: arrowFrom[i] is the index of the first
	// ARROW token at or after i, or -1. It makes isArrowFunction's "is there an
	// arrow ahead" check O(1) instead of O(n) per call.
	arrowFrom []int
}

func New(tokens []lexer.Token) *Parser {
	return &Parser{tokens: tokens}
}

// DroppedTypes reports how many type-only constructs the parse discarded.
func (p *Parser) DroppedTypes() int { return p.droppedTypes }

func (p *Parser) SetSource(src string) {
	p.srcLines = strings.Split(src, "\n")
}

func (p *Parser) Errors() []error { return p.errs }

func isIdentifierToken(k lexer.Kind) bool {
	switch k {
	case lexer.Identifier, lexer.As, lexer.From, lexer.Of, lexer.Async,
		lexer.Type_, lexer.Interface_, lexer.Any_, lexer.Unknown_, lexer.Never_,
		lexer.Boolean_, lexer.String_, lexer.Number_, lexer.Symbol_,
		lexer.Public_, lexer.Private_, lexer.Protected_, lexer.Readonly_,
		lexer.Declare_, lexer.Namespace_, lexer.Enum_, lexer.Implements_,
		lexer.Class_, lexer.For, lexer.If_, lexer.Else_, lexer.While_, lexer.Do_,
		lexer.Switch, lexer.Case_, lexer.Default_, lexer.Break_, lexer.Continue_,
		lexer.Return, lexer.Throw_, lexer.Try_, lexer.Catch_, lexer.Finally_,
		lexer.Const_, lexer.Let_, lexer.Var_, lexer.Import, lexer.Export,
		lexer.In_, lexer.Extends_:
		return true
	}
	return false
}

func isStmtBoundary(kind lexer.Kind) bool {
	switch kind {
	case lexer.SEMI, lexer.RBRACE, lexer.EOF:
		return true
	case lexer.Export, lexer.Import, lexer.Const_, lexer.Let_, lexer.Var_:
		return true
	case lexer.Function, lexer.Return, lexer.If_, lexer.For, lexer.While_:
		return true
	case lexer.Do_, lexer.Switch, lexer.Try_, lexer.Throw_:
		return true
	case lexer.Break_, lexer.Continue_, lexer.Case_, lexer.Default_:
		return true
	case lexer.Class_, lexer.Interface_, lexer.Enum_, lexer.Type_:
		return true
	}
	return false
}

func (p *Parser) recoverToStmtBoundary() {
	for p.pos < len(p.tokens) {
		if isStmtBoundary(p.tokens[p.pos].Kind) {
			return
		}
		p.pos++
	}
}

func (p *Parser) err(msg string) {
	tok := p.peek()
	d := diag.New(p.Filename, tok.Line, tok.Col, msg, "", "")
	if len(p.srcLines) > 0 && tok.Line > 0 && tok.Line <= len(p.srcLines) {
		d.Source = p.srcLines[tok.Line-1]
	}
	p.errs = append(p.errs, d)
}

func (p *Parser) errWithMsg(msg, hint string) {
	tok := p.peek()
	d := diag.New(p.Filename, tok.Line, tok.Col, msg, hint, "")
	if len(p.srcLines) > 0 && tok.Line > 0 && tok.Line <= len(p.srcLines) {
		d.Source = p.srcLines[tok.Line-1]
	}
	p.errs = append(p.errs, d)
}

func (p *Parser) skipWS() {
	for p.pos < len(p.tokens) && p.tokens[p.pos].Kind == lexer.Whitespace {
		p.pos++
	}
}

func (p *Parser) peek() lexer.Token {
	p.skipWS()
	if p.pos >= len(p.tokens) {
		return lexer.Token{Kind: lexer.EOF}
	}
	return p.tokens[p.pos]
}

// peekN returns the kind of the Nth non-whitespace token ahead of the current
// position without consuming it (peekN(0) is the current token, mirroring
// peek().Kind). Used for cheap disambiguation lookahead.
func (p *Parser) peekN(kindCount int) lexer.Kind {
	i := p.pos
	seen := 0
	for i < len(p.tokens) {
		if p.tokens[i].Kind == lexer.Whitespace {
			i++
			continue
		}
		if seen == kindCount {
			return p.tokens[i].Kind
		}
		seen++
		i++
	}
	return lexer.EOF
}

func (p *Parser) next() lexer.Token {
	p.skipWS()
	tok := p.peek()
	p.pos++
	return tok
}

// newlineBefore reports whether a line break separates the given (already
// consumed) token from the upcoming token. Used for automatic semicolon
// insertion (ASI) in restricted productions (return/throw/break/continue and
// postfix ++/--).
func (p *Parser) newlineBefore(prev lexer.Token) bool {
	next := p.peek()
	return next.Kind != lexer.EOF && next.Line > prev.Line
}

// newlineAfterPrev reports whether a line break separates the token before pos
// from the token at pos. Used to decide whether a postfix operator attaches to
// the preceding operand.
func (p *Parser) newlineAfterPrev(pos int) bool {
	if pos <= 0 || pos >= len(p.tokens) {
		return false
	}
	j := pos - 1
	for j >= 0 && p.tokens[j].Kind == lexer.Whitespace {
		j--
	}
	if j < 0 {
		return false
	}
	return p.tokens[pos].Line > p.tokens[j].Line
}

func (p *Parser) match(kind lexer.Kind) bool {
	if p.peek().Kind == kind {
		p.next()
		return true
	}
	return false
}

func (p *Parser) expect(kind lexer.Kind) lexer.Token {
	if p.peek().Kind == kind {
		return p.next()
	}
	p.err(fmt.Sprintf("expected %s, got %s", kindLabel(kind), kindLabel(p.peek().Kind)))
	p.recoverToStmtBoundary()
	return lexer.Token{Kind: lexer.Error}
}

const (
	precComma = iota - 1
	precLowest
	precAssign
	precCond
	precOr
	precAnd
	precBitwiseOr
	precBitwiseXor
	precBitwiseAnd
	precEq
	precCompare
	precShift
	precAs
	precTerm
	precFactor
	precUnary
	precCall
	precMember
)

var precMap = map[lexer.Kind]int{
	lexer.ARROW:        precAssign,
	lexer.ASSIGN:       precAssign,
	lexer.ADD_ASSIGN:   precAssign,
	lexer.SUB_ASSIGN:   precAssign,
	lexer.MUL_ASSIGN:   precAssign,
	lexer.DIV_ASSIGN:   precAssign,
	lexer.MOD_ASSIGN:   precAssign,
	lexer.SHL_ASSIGN:   precAssign,
	lexer.SHR_ASSIGN:   precAssign,
	lexer.QUEST:        precCond,
	lexer.NULLISH:      precOr,
	lexer.OR:           precOr,
	lexer.AND:          precAnd,
	lexer.BIT_OR:       precBitwiseOr,
	lexer.BIT_XOR:      precBitwiseXor,
	lexer.BIT_AND:      precBitwiseAnd,
	lexer.EQ:           precEq,
	lexer.NEQ:          precEq,
	lexer.STRICT_EQ:    precEq,
	lexer.STRICT_NEQ:   precEq,
	lexer.LT:           precCompare,
	lexer.GT:           precCompare,
	lexer.LTE:          precCompare,
	lexer.GTE:          precCompare,
	lexer.In_:          precCompare,
	lexer.Instanceof_:  precCompare,
	lexer.SHL:          precShift,
	lexer.SHR:          precShift,
	lexer.USHIFT_RIGHT: precShift,
	lexer.PLUS:         precTerm,
	lexer.MINUS:        precTerm,
	lexer.STAR:         precFactor,
	lexer.DIV:          precFactor,
	lexer.MOD:          precFactor,
	lexer.LPAREN:       precCall,
	lexer.LBRACKET:     precCall,
	lexer.DOT:          precMember,
	lexer.QUESTION_DOT: precMember,
	lexer.INC:          precUnary,
	lexer.DEC:          precUnary,
	lexer.NOT:          precAs,
	lexer.As:           precAs,
}

func (p *Parser) ParseProgram() *ast.Program {
	prog := &ast.Program{}
	for p.peek().Kind != lexer.EOF {
		pos := p.pos
		stmt := p.parseStmt()
		if stmt != nil {
			prog.Body = append(prog.Body, stmt)
		}
		if p.pos == pos {
			p.next()
		}
	}
	return prog
}

func (p *Parser) parseStmt() ast.Stmt {
	// Bare `async function name() {...}` declaration at statement level (not
	// preceded by `export`). The `export async function` combos are handled in
	// parseExport; here we consume the `async` keyword and delegate to
	// parseFnDecl (which expects the current token to be `function`).
	if p.peek().Kind == lexer.Async {
		// The lexer emits Whitespace tokens, so scan past them to find the
		// token following `async` without advancing p.pos.
		nextPos := p.pos + 1
		for nextPos < len(p.tokens) && p.tokens[nextPos].Kind == lexer.Whitespace {
			nextPos++
		}
		if nextPos < len(p.tokens) && p.tokens[nextPos].Kind == lexer.Function {
			p.next() // consume `async`, now at `function`
			return p.parseFnDecl()
		}
	}
	switch p.peek().Kind {
	case lexer.Import:
		// Distinguish a static import statement (`import x from '...'`) from a
		// dynamic import expression used as a statement
		// (`import('./m.js').then(...)`) by checking for the paren form.
		if p.peekN(1) == lexer.LPAREN {
			return p.parseDynamicImportStmt()
		}
		return p.parseImport()
	case lexer.Export:
		return p.parseExport()
	case lexer.Const_, lexer.Let_, lexer.Var_:
		return p.parseVarStmt()
	case lexer.Function:
		return p.parseFnDecl()
	case lexer.Return:
		return p.parseReturn()
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
	case lexer.Class_:
		return p.parseClassDecl()
	case lexer.Interface_:
		return p.parseInterfaceDecl()
	case lexer.Enum_:
		return p.parseEnumDecl()
	case lexer.Type_:
		// peekN(1) skips Whitespace, so the lookahead sees the alias name even
		// when `type` is followed by a space (the common case).
		if isIdentifierToken(p.peekN(1)) {
			return p.parseTypeAliasDecl()
		}
		p.next()
		return nil
	case lexer.LBRACE:
		blk := p.parseBlock()
		return &ast.BlockStmt{Position: posFromStmts(blk), Body: blk}
	case lexer.SEMI:
		p.next()
		return nil
	case lexer.RBRACE:
		return nil
	case lexer.AT:
		p.next()
		_ = p.parseExpr(precLowest)
		return nil
	case lexer.Error:
		// The lexer emitted an Error token for an unlexable character. Surface
		// it instead of silently skipping it.
		p.errWithMsg("unexpected character or token while parsing", "Krate could not interpret this input")
		p.next()
		return nil
	default:
		expr := p.parseExpr(precLowest)
		if expr != nil {
			p.match(lexer.SEMI)
			return &ast.ExprStmt{Position: expr.Pos(), Expression: expr}
		}
		p.next()
		return nil
	}
}

func (p *Parser) parseClassDecl() ast.Stmt {
	p.droppedTypes++
	p.next()
	if isIdentifierToken(p.peek().Kind) {
		p.next()
	}
	if p.match(lexer.Extends_) {
		_ = p.parseExpr(precLowest)
	}
	if p.peek().Kind == lexer.LBRACE {
		p.next()
		depth := 1
		for depth > 0 && p.peek().Kind != lexer.EOF {
			tok := p.next()
			if tok.Kind == lexer.LBRACE {
				depth++
			} else if tok.Kind == lexer.RBRACE {
				depth--
			}
		}
	}
	return nil
}

func (p *Parser) parseInterfaceDecl() ast.Stmt {
	p.droppedTypes++
	p.next()
	if isIdentifierToken(p.peek().Kind) {
		p.next()
	}
	// Skip generic type parameters (`interface A<T, U extends V> ...`) so the
	// heritage clause and body are reached.
	if p.peek().Kind == lexer.LT {
		depth := 0
		for p.peek().Kind != lexer.EOF {
			switch p.peek().Kind {
			case lexer.LT:
				depth++
			case lexer.GT:
				depth--
				if depth == 0 {
					p.next()
					goto heritage
				}
			case lexer.SHR: // `>>` closes two levels
				depth -= 2
				p.next()
				if depth <= 0 {
					goto heritage
				}
				continue
			}
			p.next()
		}
	}
heritage:
	// `interface X extends A, B<C> ...` - skip the heritage clause up to the
	// body brace. Without this the `extends` clause is parsed as an expression
	// statement and the interface body's `?:` members error.
	if p.match(lexer.Extends_) {
		angle, paren, bracket := 0, 0, 0
		for p.peek().Kind != lexer.EOF {
			k := p.peek().Kind
			if k == lexer.LBRACE && angle == 0 && paren == 0 && bracket == 0 {
				break
			}
			switch k {
			case lexer.LT:
				angle++
			case lexer.GT:
				if angle > 0 {
					angle--
				}
			case lexer.LPAREN:
				paren++
			case lexer.RPAREN:
				if paren > 0 {
					paren--
				}
			case lexer.LBRACKET:
				bracket++
			case lexer.RBRACKET:
				if bracket > 0 {
					bracket--
				}
			}
			p.next()
		}
	}
	if p.peek().Kind == lexer.LBRACE {
		p.next()
		depth := 1
		for depth > 0 && p.peek().Kind != lexer.EOF {
			tok := p.next()
			if tok.Kind == lexer.LBRACE {
				depth++
			} else if tok.Kind == lexer.RBRACE {
				depth--
			}
		}
	}
	return nil
}

func (p *Parser) parseTypeAliasDecl() ast.Stmt {
	p.droppedTypes++
	p.next()
	if isIdentifierToken(p.peek().Kind) {
		p.next()
	}
	if p.match(lexer.ASSIGN) {
		_ = p.parseExpr(precLowest)
	}
	p.match(lexer.SEMI)
	return nil
}

func (p *Parser) parseEnumDecl() ast.Stmt {
	p.droppedTypes++
	p.next()
	if isIdentifierToken(p.peek().Kind) {
		p.next()
	}
	if p.peek().Kind == lexer.LBRACE {
		p.next()
		depth := 1
		for depth > 0 && p.peek().Kind != lexer.EOF {
			tok := p.next()
			if tok.Kind == lexer.LBRACE {
				depth++
			} else if tok.Kind == lexer.RBRACE {
				depth--
			}
		}
	}
	return nil
}

func (p *Parser) parseDynamicImportStmt() ast.Stmt {
	expr := p.parseExpr(precLowest)
	if expr != nil {
		p.match(lexer.SEMI)
		return &ast.ExprStmt{Position: expr.Pos(), Expression: expr}
	}
	p.next()
	return nil
}

func (p *Parser) parseImport() ast.Stmt {
	pos := tokPos(p.next())
	stmt := &ast.ImportStmt{Position: pos}

	// Skip `type` in `import type {...}` - type-only imports carry no runtime
	// binding, so they can be ignored by the hydration pipeline.
	if isIdentifierToken(p.peek().Kind) && p.peek().Value == "type" {
		p.next()
	}

	if isIdentifierToken(p.peek().Kind) {
		stmt.Default = p.next().Value
		if p.match(lexer.COMMA) {
			if p.peek().Kind == lexer.LBRACE {
				p.parseNamedImports(&stmt.Named)
			} else if p.match(lexer.STAR) {
				if p.match(lexer.As) && isIdentifierToken(p.peek().Kind) {
					stmt.Namespace = p.next().Value
				}
			}
		}
	} else if p.match(lexer.LBRACE) {
		p.parseNamedImports(&stmt.Named)
	} else if p.match(lexer.STAR) {
		if p.match(lexer.As) && isIdentifierToken(p.peek().Kind) {
			stmt.Namespace = p.next().Value
		}
	}

	if p.match(lexer.From) {
		if p.peek().Kind == lexer.String {
			stmt.Source = p.next().Value
		}
	} else if p.peek().Kind == lexer.String {
		stmt.Source = p.next().Value
	}

	p.match(lexer.SEMI)
	return stmt
}

func (p *Parser) parseNamedImports(named *[]ast.NamedImport) {
	for {
		if p.peek().Kind == lexer.RBRACE || p.peek().Kind == lexer.EOF {
			break
		}
		// Skip an inline `type` modifier (`import { type Foo }`). Type-only
		// specifiers carry no runtime binding, so the whole entry is dropped.
		if isIdentifierToken(p.peek().Kind) && p.peek().Value == "type" && isIdentifierToken(p.peekN(1)) {
			p.next() // consume `type`
			p.next() // consume the type name
			if p.match(lexer.As) && isIdentifierToken(p.peek().Kind) {
				p.next()
			}
			if !p.match(lexer.COMMA) {
				break
			}
			continue
		}
		if isIdentifierToken(p.peek().Kind) {
			remote := p.next().Value
			local := remote
			if p.match(lexer.As) {
				if isIdentifierToken(p.peek().Kind) {
					local = p.next().Value
				}
			}
			*named = append(*named, ast.NamedImport{Local: local, Remote: remote})
		}
		if !p.match(lexer.COMMA) {
			break
		}
	}
	p.expect(lexer.RBRACE)
}

// decodeStringToken turns a lexer String token (raw source text including the
// delimiter quotes) into its runtime value: the delimiters are stripped and
// escape sequences are decoded. JSX attribute strings intentionally do NOT use
// this - JSX does not process backslash escapes.
func decodeStringToken(raw string) string {
	if len(raw) >= 2 {
		raw = raw[1 : len(raw)-1]
	}
	return escape.UnescapeJSString(raw)
}

func (p *Parser) parseForStmt() ast.Stmt {
	pos := tokPos(p.next())
	p.expect(lexer.LPAREN)

	var init ast.Stmt
	if p.peek().Kind != lexer.SEMI {
		if p.peek().Kind == lexer.Let_ || p.peek().Kind == lexer.Var_ || p.peek().Kind == lexer.Const_ {
			init = p.parseVarStmt()
		} else {
			// Bare init expression: block `in` so `for (x in obj)` leaves the
			// `in` for the for-in branch below instead of parsing `x in obj`.
			prevNoIn := p.noIn
			p.noIn = true
			expr := p.parseExpr(precLowest)
			p.noIn = prevNoIn
			if expr != nil {
				init = &ast.ExprStmt{Position: expr.Pos(), Expression: expr}
			}
		}
	}

	if p.peek().Kind == lexer.In_ || p.peek().Kind == lexer.Of {
		isForOf := p.peek().Kind == lexer.Of
		keyword := ""
		if vs, ok := init.(*ast.VarStmt); ok {
			switch vs.Kind {
			case ast.VarConst:
				keyword = "const"
			case ast.VarLet:
				keyword = "let"
			default:
				keyword = "var"
			}
		}
		p.next()
		right := p.parseExpr(precLowest)
		p.expect(lexer.RPAREN)
		body := p.parseBlockOrStmt()
		return &ast.ForInStmt{Position: pos, Left: forInitToExpr(init), Right: right, Body: body, IsForOf: isForOf, Keyword: keyword}
	}

	p.match(lexer.SEMI)

	var test ast.Expr
	if p.peek().Kind != lexer.SEMI {
		test = p.parseExpr(precLowest)
	}
	p.match(lexer.SEMI)

	var update ast.Expr
	if p.peek().Kind != lexer.RPAREN {
		update = p.parseExpr(precLowest)
	}
	p.expect(lexer.RPAREN)

	body := p.parseBlockOrStmt()
	return &ast.ForStmt{Position: pos, Init: init, Test: test, Update: update, Body: body}
}

func forInitToExpr(init ast.Stmt) ast.Expr {
	if init == nil {
		return nil
	}
	if es, ok := init.(*ast.ExprStmt); ok {
		return es.Expression
	}
	if vs, ok := init.(*ast.VarStmt); ok {
		if len(vs.Decls) > 0 && vs.Decls[0].Name != "" {
			return &ast.Identifier{Name: vs.Decls[0].Name}
		}
	}
	return nil
}

func (p *Parser) parseWhileStmt() ast.Stmt {
	pos := tokPos(p.next())
	p.expect(lexer.LPAREN)
	test := p.parseExpr(precLowest)
	p.expect(lexer.RPAREN)
	body := p.parseBlockOrStmt()
	return &ast.WhileStmt{Position: pos, Test: test, Body: body}
}

func (p *Parser) parseDoWhileStmt() ast.Stmt {
	pos := tokPos(p.next())
	body := p.parseBlockOrStmt()
	p.expect(lexer.While_)
	p.expect(lexer.LPAREN)
	test := p.parseExpr(precLowest)
	p.expect(lexer.RPAREN)
	p.match(lexer.SEMI)
	return &ast.DoWhileStmt{Position: pos, Body: body, Test: test}
}

func (p *Parser) parseSwitchStmt() ast.Stmt {
	pos := tokPos(p.next())
	p.expect(lexer.LPAREN)
	discriminant := p.parseExpr(precLowest)
	p.expect(lexer.RPAREN)
	p.expect(lexer.LBRACE)

	var cases []*ast.CaseClause
	for p.peek().Kind != lexer.RBRACE && p.peek().Kind != lexer.EOF {
		casePos := tokPos(p.peek())
		if p.match(lexer.Case_) {
			test := p.parseExpr(precLowest)
			p.expect(lexer.COLON)
			body := p.parseSwitchCaseBody()
			cases = append(cases, &ast.CaseClause{Position: casePos, Test: test, Body: body})
		} else if p.match(lexer.Default_) {
			p.expect(lexer.COLON)
			body := p.parseSwitchCaseBody()
			cases = append(cases, &ast.CaseClause{Position: casePos, Body: body})
		} else {
			break
		}
	}
	p.expect(lexer.RBRACE)
	return &ast.SwitchStmt{Position: pos, Discriminant: discriminant, Cases: cases}
}

func (p *Parser) parseSwitchCaseBody() []ast.Stmt {
	var stmts []ast.Stmt
	for {
		switch p.peek().Kind {
		case lexer.RBRACE, lexer.Case_, lexer.Default_, lexer.EOF:
			return stmts
		}
		stmt := p.parseStmt()
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
	}
}

func (p *Parser) parseTryStmt() ast.Stmt {
	pos := tokPos(p.next())
	body := p.parseBlock()

	var catch *ast.CatchClause
	if p.match(lexer.Catch_) {
		catchPos := tokPos(p.tokens[p.pos-1])
		var param string
		if p.match(lexer.LPAREN) {
			if isIdentifierToken(p.peek().Kind) {
				param = p.next().Value
			}
			p.expect(lexer.RPAREN)
		}
		catchBody := p.parseBlock()
		catch = &ast.CatchClause{Position: catchPos, Param: param, Body: catchBody}
	}

	var finallyBody []ast.Stmt
	if p.match(lexer.Finally_) {
		finallyBody = p.parseBlock()
	}

	return &ast.TryStmt{Position: pos, Body: body, Catch: catch, Finally: finallyBody}
}

func (p *Parser) parseThrowStmt() ast.Stmt {
	kw := p.next()
	pos := tokPos(kw)
	// A line break after `throw` is a syntax error in JS; emitting a
	// diagnostic is clearer than silently parsing across the newline.
	if p.newlineBefore(kw) {
		p.errWithMsg("illegal newline after `throw`", "`throw` and its expression must be on the same line")
	}
	value := p.parseExpr(precLowest)
	p.match(lexer.SEMI)
	return &ast.ThrowStmt{Position: pos, Value: value}
}

func (p *Parser) parseBreakStmt() ast.Stmt {
	kw := p.next()
	pos := tokPos(kw)
	var label string
	// ASI: a label must be on the same line as `break`.
	if !p.newlineBefore(kw) && isIdentifierToken(p.peek().Kind) {
		label = p.next().Value
	}
	p.match(lexer.SEMI)
	return &ast.BreakStmt{Position: pos, Label: label}
}

func (p *Parser) parseContinueStmt() ast.Stmt {
	kw := p.next()
	pos := tokPos(kw)
	var label string
	if !p.newlineBefore(kw) && isIdentifierToken(p.peek().Kind) {
		label = p.next().Value
	}
	p.match(lexer.SEMI)
	return &ast.ContinueStmt{Position: pos, Label: label}
}

func (p *Parser) parseExport() ast.Stmt {
	pos := tokPos(p.next())
	exp := &ast.ExportStmt{Position: pos}

	if p.match(lexer.Default_) {
		exp.Default = true
	}

	switch p.peek().Kind {
	case lexer.Const_, lexer.Let_, lexer.Var_:
		exp.Declaration = p.parseVarStmt()
	case lexer.Function:
		exp.Declaration = p.parseFnDecl()
	case lexer.Async:
		p.next()
		if p.peek().Kind == lexer.Function {
			fn := p.parseFnDecl()
			if fnDecl, ok := fn.(*ast.FnDecl); ok {
				fnDecl.Async = true
			}
			exp.Declaration = fn
		}
	case lexer.Class_:
		exp.Declaration = p.parseClassDecl()
	case lexer.Interface_:
		exp.Declaration = p.parseInterfaceDecl()
	case lexer.Enum_:
		exp.Declaration = p.parseEnumDecl()
	case lexer.Type_:
		exp.Declaration = p.parseTypeAliasDecl()
	case lexer.STAR:
		// export * from 'source'   or   export * as Name from 'source'
		p.next()
		exp.StarReexport = true
		if p.match(lexer.As) && isIdentifierToken(p.peek().Kind) {
			// `export * as Name from 'x'` - a namespace re-export. Record the
			// namespace binding so downstream alias resolution can address
			// `Name.<Export>` (mirrors `import * as Name`).
			exp.Namespace = p.next().Value
		}
		if p.match(lexer.From) {
			if p.peek().Kind == lexer.String {
				exp.ReexportSource = p.next().Value
			}
		} else if p.peek().Kind == lexer.String {
			exp.ReexportSource = p.next().Value
		}
		p.match(lexer.SEMI)
	case lexer.LBRACE:
		if exp.Default {
			// `export default { ... }` is an object-literal expression, not a
			// named re-export. Fall through to expression parsing below.
			expr := p.parseExpr(precLowest)
			if expr != nil {
				exp.Declaration = &ast.ExprStmt{Expression: expr}
			}
			p.match(lexer.SEMI)
			break
		}
		// export { name } from 'source' - named re-export
		names := p.parseExportNames()
		if p.match(lexer.From) && p.peek().Kind == lexer.String {
			exp.ReexportSource = p.next().Value
			exp.Local = strings.Join(names, ",")
		}
		p.match(lexer.SEMI)
	default:
		if exp.Default {
			// `export default <expression>` - object/call/literal/JSX/identifier.
			// Previously non-identifier expression defaults were dropped
			// silently; represent them as an ExprStmt so config descriptors
			// (e.g. `export default defineContent({...})`) survive parsing.
			// A bare identifier is recorded as Local so the annotator can
			// resolve the default component name.
			if p.peek().Kind == lexer.EOF {
				break
			}
			expr := p.parseExpr(precLowest)
			if id, ok := expr.(*ast.Identifier); ok {
				exp.Local = id.Name
			} else if expr != nil {
				exp.Declaration = &ast.ExprStmt{Expression: expr}
			}
			p.match(lexer.SEMI)
		} else if isIdentifierToken(p.peek().Kind) {
			exp.Local = p.next().Value
			p.match(lexer.SEMI)
		} else {
			p.match(lexer.SEMI)
		}
	}

	return exp
}

// parseExportNames parses "{ name1, name2 }" inside an export statement.
func (p *Parser) parseExportNames() []string {
	p.next() // consume '{'
	var names []string
	for {
		if p.peek().Kind == lexer.RBRACE || p.peek().Kind == lexer.EOF {
			break
		}
		if isIdentifierToken(p.peek().Kind) {
			names = append(names, p.next().Value)
		} else {
			break
		}
		p.match(lexer.COMMA)
	}
	p.match(lexer.RBRACE)
	return names
}

func (p *Parser) parseVarStmt() ast.Stmt {
	tok := p.next()
	pos := tokPos(tok)
	kind := ast.VarConst
	switch tok.Kind {
	case lexer.Let_:
		kind = ast.VarLet
	case lexer.Var_:
		kind = ast.VarVar
	}

	stmt := &ast.VarStmt{Position: pos, Kind: kind}

	for {
		if p.peek().Kind == lexer.LBRACKET {
			decl := p.parseArrayDestructuring()
			if p.match(lexer.ASSIGN) {
				decl.Init = p.parseExpr(precLowest)
			}
			stmt.Decls = append(stmt.Decls, decl)
		} else if p.peek().Kind == lexer.LBRACE {
			decl := p.parseObjectDestructuring()
			if p.match(lexer.ASSIGN) {
				decl.Init = p.parseExpr(precLowest)
			}
			stmt.Decls = append(stmt.Decls, decl)
		} else if isIdentifierToken(p.peek().Kind) {
			name := p.next().Value
			p.skipTypeAnnotation(false)
			decl := &ast.VarDecl{Name: name}
			if p.match(lexer.ASSIGN) {
				decl.Init = p.parseExpr(precLowest)
			}
			stmt.Decls = append(stmt.Decls, decl)
		}
		if !p.match(lexer.COMMA) {
			break
		}
	}

	p.match(lexer.SEMI)
	return stmt
}

func (p *Parser) parseFnDecl() ast.Stmt {
	p.next()
	pos := tokPos(p.peek())
	name := ""

	if isIdentifierToken(p.peek().Kind) {
		name = p.next().Value
	}

	fn := &ast.FnDecl{
		Position: pos,
		Name:     name,
	}

	p.skipTypeParams()

	p.expect(lexer.LPAREN)
	fn.Params = p.parseParamList()
	p.expect(lexer.RPAREN)
	p.skipTypeAnnotation(true)

	if p.match(lexer.LBRACE) {
		fn.Body = p.parseStmtList(lexer.RBRACE)
		p.expect(lexer.RBRACE)
	}

	return fn
}

func (p *Parser) parseReturn() ast.Stmt {
	kw := p.next()
	pos := tokPos(kw)
	stmt := &ast.ReturnStmt{Position: pos}

	// ASI: a line break after `return` ends the statement, so the following
	// token starts a new statement (JS restricted production).
	if !p.newlineBefore(kw) &&
		p.peek().Kind != lexer.SEMI && p.peek().Kind != lexer.RBRACE && p.peek().Kind != lexer.EOF {
		stmt.Value = p.parseExpr(precLowest)
	}

	p.match(lexer.SEMI)
	return stmt
}

func (p *Parser) parseIfStmt() ast.Stmt {
	pos := tokPos(p.next())
	stmt := &ast.IfStmt{Position: pos}

	p.expect(lexer.LPAREN)
	stmt.Test = p.parseExpr(precLowest)
	p.expect(lexer.RPAREN)

	if p.match(lexer.LBRACE) {
		stmt.Consequent = p.parseStmtList(lexer.RBRACE)
		p.expect(lexer.RBRACE)
	} else {
		stmt.Consequent = []ast.Stmt{p.parseStmt()}
	}

	if p.match(lexer.Else_) {
		if p.match(lexer.LBRACE) {
			stmt.Alternate = p.parseStmtList(lexer.RBRACE)
			p.expect(lexer.RBRACE)
		} else {
			if p.peek().Kind == lexer.If_ {
				alt := p.parseIfStmt()
				stmt.Alternate = []ast.Stmt{alt}
			} else {
				stmt.Alternate = []ast.Stmt{p.parseStmt()}
			}
		}
	}

	return stmt
}

func (p *Parser) parseBlock() []ast.Stmt {
	p.expect(lexer.LBRACE)
	body := p.parseStmtList(lexer.RBRACE)
	p.expect(lexer.RBRACE)
	return body
}

// parseBlockOrStmt parses a control-flow body that may be either a
// brace-delimited block or a single unbraced statement (`for (...) x++;`,
// `while (...) if (y) z();`). The single statement is wrapped in a one-element
// list so callers keep the []Stmt shape.
func (p *Parser) parseBlockOrStmt() []ast.Stmt {
	if p.peek().Kind == lexer.LBRACE {
		return p.parseBlock()
	}
	stmt := p.parseStmt()
	if stmt == nil {
		return nil
	}
	return []ast.Stmt{stmt}
}

func (p *Parser) parseStmtList(end lexer.Kind) []ast.Stmt {
	var stmts []ast.Stmt
	for p.peek().Kind != end && p.peek().Kind != lexer.EOF {
		stmt := p.parseStmt()
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
	}
	return stmts
}

func (p *Parser) parseArrayDestructuring() *ast.VarDecl {
	decl := &ast.VarDecl{IsDestructuring: true}
	var sb strings.Builder
	var names []string
	rest := ""
	p.parseArrayPatternInto(&sb, &names, &rest)
	decl.Pattern = sb.String()
	decl.Names = names
	decl.RestName = rest
	p.skipDestructTypeAnnotation()
	return decl
}

func (p *Parser) parseObjectDestructuring() *ast.VarDecl {
	decl := &ast.VarDecl{IsDestructuring: true}
	var sb strings.Builder
	var names []string
	rest := ""
	p.parseObjectPatternInto(&sb, &names, &rest)
	decl.Pattern = sb.String()
	decl.Names = names
	decl.RestName = rest
	p.skipDestructTypeAnnotation()
	return decl
}

func (p *Parser) skipDestructTypeAnnotation() {
	if p.peek().Kind == lexer.COLON {
		p.skipTypeAnnotation(false)
	}
}

func (p *Parser) parseArrayPatternInto(sb *strings.Builder, names *[]string, rest *string) {
	p.expect(lexer.LBRACKET)
	sb.WriteString("[")
	for p.peek().Kind != lexer.RBRACKET && p.peek().Kind != lexer.EOF {
		if p.peek().Kind == lexer.SPREAD {
			p.next()
			sb.WriteString("...")
			// A trailing `...rest` records its binding in rest so consumers
			// (sigutil) can distinguish the rest element from normal names.
			if isIdentifierToken(p.peek().Kind) && rest != nil {
				*rest = p.peek().Value
			}
			if !p.parsePatternElement(sb, names, rest) {
				p.next()
			}
		} else if p.peek().Kind == lexer.COMMA {
			p.next()
			sb.WriteString(",")
			continue
		} else if !p.parsePatternElement(sb, names, rest) {
			p.next()
		}
		if p.match(lexer.ASSIGN) {
			_ = p.parseExpr(precLowest)
		}
		if !p.match(lexer.COMMA) {
			break
		}
		sb.WriteString(",")
	}
	p.expect(lexer.RBRACKET)
	sb.WriteString("]")
}

func (p *Parser) parseObjectPatternInto(sb *strings.Builder, names *[]string, rest *string) {
	p.expect(lexer.LBRACE)
	sb.WriteString("{")
	for p.peek().Kind != lexer.RBRACE && p.peek().Kind != lexer.EOF {
		if p.peek().Kind == lexer.SPREAD {
			p.next()
			sb.WriteString("...")
			// Record the trailing object rest binding name.
			if isIdentifierToken(p.peek().Kind) && rest != nil {
				*rest = p.peek().Value
			}
			if !p.parsePatternElement(sb, names, rest) {
				p.next()
			}
		} else if isIdentifierToken(p.peek().Kind) || p.peek().Kind == lexer.String || p.peek().Kind == lexer.Number {
			key := p.next()
			kv := key.Value
			if key.Kind == lexer.String {
				kv = decodeStringToken(kv)
			}
			sb.WriteString(kv)
			if p.match(lexer.COLON) {
				sb.WriteString(":")
				if !p.parsePatternElement(sb, names, rest) {
					p.next()
				}
			} else {
				*names = append(*names, kv)
			}
		} else {
			p.next()
		}
		if p.match(lexer.ASSIGN) {
			_ = p.parseExpr(precLowest)
		}
		if !p.match(lexer.COMMA) {
			break
		}
		sb.WriteString(",")
	}
	p.expect(lexer.RBRACE)
	sb.WriteString("}")
}

func (p *Parser) parsePatternElement(sb *strings.Builder, names *[]string, rest *string) bool {
	if isIdentifierToken(p.peek().Kind) {
		tok := p.next()
		*names = append(*names, tok.Value)
		sb.WriteString(tok.Value)
		return true
	}
	if p.peek().Kind == lexer.LBRACKET {
		p.parseArrayPatternInto(sb, names, rest)
		return true
	}
	if p.peek().Kind == lexer.LBRACE {
		p.parseObjectPatternInto(sb, names, rest)
		return true
	}
	return false
}

func (p *Parser) parseParamList() []*ast.Param {
	var params []*ast.Param

	for p.peek().Kind != lexer.RPAREN && p.peek().Kind != lexer.EOF {
		param := &ast.Param{}

		// A `this` parameter (`method(this: Counter)`) is compile-time only -
		// skip it and its annotation entirely.
		if isIdentifierToken(p.peek().Kind) && p.peek().Value == "this" {
			p.next()
			if p.peek().Kind == lexer.COLON {
				p.skipTypeAnnotation(false)
			}
			if !p.match(lexer.COMMA) {
				break
			}
			continue
		}

		if p.peek().Kind == lexer.LBRACE {
			var sb strings.Builder
			var names []string
			p.parseObjectPatternInto(&sb, &names, nil)
			param.Name = "{...}"
			param.Pattern = sb.String()
		} else if p.peek().Kind == lexer.LBRACKET {
			var sb strings.Builder
			var names []string
			p.parseArrayPatternInto(&sb, &names, nil)
			param.Name = "[...]"
			param.Pattern = sb.String()
		} else if isIdentifierToken(p.peek().Kind) {
			param.Name = p.next().Value
		} else if p.match(lexer.SPREAD) {
			param.IsRest = true
			if isIdentifierToken(p.peek().Kind) {
				param.Name = p.next().Value
			}
		} else {
			break
		}

		// Optional parameter marker (`name?: Type`) - skip the `?`.
		p.match(lexer.QUEST)

		if p.peek().Kind == lexer.COLON {
			p.skipTypeAnnotation(false)
		}

		if p.match(lexer.ASSIGN) {
			param.Default = p.parseExpr(precLowest)
		}

		params = append(params, param)

		if !p.match(lexer.COMMA) {
			break
		}
	}
	return params
}

func (p *Parser) parseExpr(prec int) ast.Expr {
	prefix := p.parsePrefix()
	if prefix == nil {
		return nil
	}

	for {
		tok := p.peek()
		// `satisfies T` is a TS type assertion with the precedence of `as`. It
		// lexes as an identifier (not a keyword), so handle it here. The type is
		// compile-time only and dropped, keeping the runtime expression.
		if tok.Kind == lexer.Identifier && tok.Value == "satisfies" && prec < precAs {
			p.droppedTypes++
			p.next()
			p.parseExpr(precAs)
			// Consume a union/intersection type and generic type arguments so
			// they don't leak into the runtime expression (mirrors `as`).
			for p.peek().Kind == lexer.BIT_OR || p.peek().Kind == lexer.BIT_AND {
				p.next()
				p.parseExpr(precAs)
			}
			if p.peek().Kind == lexer.LT {
				p.next()
				p.skipTypeArgs()
			}
			continue
		}
		nextPrec, ok := precMap[tok.Kind]
		if !ok || prec >= nextPrec {
			break
		}

		infix := p.parseInfix(prefix)
		if infix == nil {
			break
		}
		prefix = infix
	}

	return prefix
}

func (p *Parser) parsePrefix() ast.Expr {
	tok := p.peek()

	// `async` is a contextual keyword but isIdentifierToken() treats it as a
	// plain identifier (so `async` can be a property/var name). Handle it
	// explicitly BEFORE the identifier fast-path so async arrows and async
	// function expressions are recognized: `async () => ...`, `async function ...`.
	if tok.Kind == lexer.Async {
		p.next()
		if p.peek().Kind == lexer.Function {
			return p.parseFnExpr()
		}
		if p.peek().Kind == lexer.LPAREN {
			p.next()
			params := p.parseParamList()
			p.expect(lexer.RPAREN)
			// Async arrow return type annotation: `async (x: string): Promise<string> =>`
			// skipTypeAnnotation consumes the leading `:` itself.
			p.skipTypeAnnotation(false, true)
			p.expect(lexer.ARROW)
			fn := p.parseArrowBody(params)
			fn.Async = true
			return fn
		}
		if isIdentifierToken(p.peek().Kind) {
			name := p.next().Value
			if p.peek().Kind == lexer.ARROW {
				p.next()
				fn := p.parseArrowBody([]*ast.Param{{Name: name}})
				fn.Async = true
				return fn
			}
			return &ast.Identifier{Name: "async"}
		}
		return &ast.Identifier{Name: "async"}
	}

	if tok.Kind == lexer.Import {
		// `import.meta` (module metadata read) vs dynamic import `import('./m.js')`.
		if p.peekN(1) == lexer.DOT {
			p.next() // consume `import`
			p.next() // consume `.`
			p.next() // consume `meta`
			return &ast.ImportMetaExpr{Position: tokPos(tok)}
		}
		// Dynamic import expression: `import('./module.js')`.
		p.next()
		p.expect(lexer.LPAREN)
		arg := p.parseExpr(precLowest)
		p.expect(lexer.RPAREN)
		return &ast.DynamicImport{Position: tokPos(tok), Arg: arg}
	}

	if isIdentifierToken(tok.Kind) {
		p.next()
		return &ast.Identifier{Position: tokPos(tok), Name: tok.Value}
	}

	switch tok.Kind {
	case lexer.Number:
		p.next()
		return &ast.Literal{Position: tokPos(tok), Kind: ast.NumberLit, Value: tok.Value}

	case lexer.String:
		p.next()
		return &ast.Literal{Position: tokPos(tok), Kind: ast.StringLit, Value: decodeStringToken(tok.Value)}

	case lexer.True:
		p.next()
		return &ast.Literal{Position: tokPos(tok), Kind: ast.BoolLit, Value: "true"}

	case lexer.False:
		p.next()
		return &ast.Literal{Position: tokPos(tok), Kind: ast.BoolLit, Value: "false"}

	case lexer.Null_:
		p.next()
		return &ast.Literal{Position: tokPos(tok), Kind: ast.NullLit, Value: "null"}

	case lexer.Undefined:
		p.next()
		return &ast.Literal{Position: tokPos(tok), Kind: ast.NullLit, Value: "undefined"}

	case lexer.Regexp:
		p.next()
		return &ast.Literal{Position: tokPos(tok), Kind: ast.RegexpLit, Value: tok.Value}

	case lexer.LT:
		// `<T>expr` angle-bracket type assertion (e.g. `<string>foo`) - only
		// distinguished from JSX when the bracketed name is a primitive type
		// keyword, which never names a JSX element.
		if p.pos+1 < len(p.tokens) && isPrimitiveTypeKeyword(p.tokens[p.pos+1].Kind) {
			return p.parseTypeAssertion()
		}
		return p.parseJSXElement()

	case lexer.LPAREN:
		if p.isArrowFunction() {
			p.next()
			params := p.parseParamList()
			p.expect(lexer.RPAREN)
			// Arrow function return type annotation: `(x: string): number =>`
			// skipTypeAnnotation consumes the leading `:` itself, so do not
			// p.match(COLON) here.
			p.skipTypeAnnotation(false, true)
			p.expect(lexer.ARROW)
			return p.parseArrowBody(params)
		}

		p.next()
		exprs := []ast.Expr{p.parseExpr(precLowest)}
		for p.match(lexer.COMMA) {
			if p.peek().Kind == lexer.RPAREN {
				break
			}
			exprs = append(exprs, p.parseExpr(precLowest))
		}
		p.expect(lexer.RPAREN)
		if len(exprs) == 1 {
			return exprs[0]
		}
		result := exprs[0]
		for i := 1; i < len(exprs); i++ {
			result = &ast.BinaryExpr{Left: result, Op: ",", Right: exprs[i]}
		}
		return result

	case lexer.LBRACKET:
		p.next()
		var elems []ast.Expr
		for p.peek().Kind != lexer.RBRACKET && p.peek().Kind != lexer.EOF {
			if p.match(lexer.COMMA) {
				continue
			}
			if p.peek().Kind == lexer.SPREAD {
				p.next()
				elems = append(elems, &ast.UnaryExpr{Op: "...", Arg: p.parseExpr(precAssign)})
			} else {
				elem := p.parseExpr(precLowest)
				elems = append(elems, elem)
			}
			if !p.match(lexer.COMMA) {
				break
			}
		}
		p.expect(lexer.RBRACKET)
		return &ast.ArrayExpr{Elements: elems}

	case lexer.LBRACE:
		p.next()
		var props []*ast.ObjectProp
		for p.peek().Kind != lexer.RBRACE && p.peek().Kind != lexer.EOF {
			if p.match(lexer.SPREAD) {
				props = append(props, &ast.ObjectProp{Spread: true, Value: p.parseExpr(precLowest)})
			} else if p.peek().Kind == lexer.LBRACKET {
				p.next()
				keyExpr := p.parseExpr(precLowest)
				p.expect(lexer.RBRACKET)
				key := ""
				if id, ok := keyExpr.(*ast.Identifier); ok {
					key = id.Name
				} else if lit, ok := keyExpr.(*ast.Literal); ok {
					key = lit.Value
				}
				if p.match(lexer.COLON) {
					props = append(props, &ast.ObjectProp{Key: key, Value: p.parseExpr(precLowest)})
				} else if p.match(lexer.ASSIGN) {
					props = append(props, &ast.ObjectProp{Key: key, Value: p.parseExpr(precLowest)})
				}
			} else if isIdentifierToken(p.peek().Kind) {
				name := p.next().Value

				if p.peek().Kind == lexer.LPAREN {
					p.next()
					fn := &ast.ArrowFn{}
					fn.Params = p.parseParamList()
					p.expect(lexer.RPAREN)
					p.skipTypeAnnotation(true)
					if p.match(lexer.LBRACE) {
						fn.Body = p.parseStmtList(lexer.RBRACE)
						p.expect(lexer.RBRACE)
						fn.Expression = false
					} else {
						fn.Body = []ast.Stmt{&ast.ReturnStmt{Value: p.parseExpr(precLowest)}}
						fn.Expression = true
					}
					props = append(props, &ast.ObjectProp{Key: name, Value: fn, Method: true})
				} else if p.match(lexer.ASSIGN) {
					props = append(props, &ast.ObjectProp{Key: name, Value: p.parseExpr(precLowest)})
				} else if p.peek().Kind == lexer.COLON {
					p.next()
					props = append(props, &ast.ObjectProp{Key: name, Value: p.parseExpr(precLowest)})
				} else {
					props = append(props, &ast.ObjectProp{Key: name, Value: &ast.Identifier{Name: name}, Shorthand: true})
				}
			} else if p.peek().Kind == lexer.String {
				key := decodeStringToken(p.next().Value)
				if p.peek().Kind == lexer.COLON {
					p.next()
					props = append(props, &ast.ObjectProp{Key: key, Value: p.parseExpr(precLowest)})
				}
			} else if p.peek().Kind == lexer.Number {
				name := p.next().Value
				if p.peek().Kind == lexer.COLON {
					p.next()
					props = append(props, &ast.ObjectProp{Key: name, Value: p.parseExpr(precLowest)})
				}
			} else {
				p.next()
			}

			if !p.match(lexer.COMMA) {
				break
			}
		}
		p.expect(lexer.RBRACE)
		return &ast.ObjectExpr{Properties: props}

	case lexer.MINUS:
		p.next()
		return &ast.UnaryExpr{Op: "-", Arg: p.parseExpr(precUnary)}

	case lexer.NOT:
		p.next()
		return &ast.UnaryExpr{Op: "!", Arg: p.parseExpr(precUnary)}

	case lexer.BIT_NOT:
		p.next()
		return &ast.UnaryExpr{Op: "~", Arg: p.parseExpr(precUnary)}

	case lexer.Typeof_:
		p.next()
		return &ast.UnaryExpr{Op: "typeof", Arg: p.parseExpr(precUnary)}

	case lexer.Delete_:
		p.next()
		return &ast.UnaryExpr{Op: "delete", Arg: p.parseExpr(precUnary)}

	case lexer.Void_:
		p.next()
		return &ast.UnaryExpr{Op: "void", Arg: p.parseExpr(precUnary)}

	case lexer.Yield_:
		p.next()
		return &ast.UnaryExpr{Op: "yield", Arg: p.parseExpr(precUnary)}

	case lexer.Super_:
		p.next()
		return &ast.Identifier{Position: tokPos(tok), Name: "super"}

	case lexer.INC:
		p.next()
		return &ast.UnaryExpr{Op: "++", Arg: p.parseExpr(precUnary)}

	case lexer.DEC:
		p.next()
		return &ast.UnaryExpr{Op: "--", Arg: p.parseExpr(precUnary)}

	case lexer.Function:
		return p.parseFnExpr()

	case lexer.TEMPLATE_START:
		return p.parseTemplateExpr()

	case lexer.TEMPLATE_END:
		tok := p.next()
		val := tok.Value
		if len(val) >= 2 && val[0] == '`' && val[len(val)-1] == '`' {
			val = val[1 : len(val)-1]
		}
		return &ast.TemplateExpr{Raw: []string{val}}

	case lexer.ARROW:
		p.next()
		return p.parseArrowBody(nil)

	case lexer.New_:
		p.next()
		callee := p.parseExpr(precMember)
		var args []ast.Expr
		if p.match(lexer.LPAREN) {
			for p.peek().Kind != lexer.RPAREN && p.peek().Kind != lexer.EOF {
				args = append(args, p.parseExpr(precLowest))
				if !p.match(lexer.COMMA) {
					break
				}
			}
			p.expect(lexer.RPAREN)
		}
		return &ast.NewExpr{Callee: callee, Args: args}

	case lexer.This_:
		p.next()
		return &ast.ThisExpr{Position: tokPos(tok)}

	case lexer.Await_:
		p.next()
		return &ast.AwaitExpr{Arg: p.parseExpr(precUnary)}

	default:
		return nil
	}
}

func (p *Parser) isArrowFunction() bool {
	// Quick check: if no ARROW token exists ahead, this is not an arrow function.
	// arrowFrom[i] is the index of the first ARROW token at or after i (-1 when
	// none), precomputed in one pass so this check is O(1) rather than O(n) per
	// parenthesized expression - otherwise a paren-heavy file is O(n).
	if p.arrowFrom == nil {
		p.arrowFrom = make([]int, len(p.tokens)+1)
		next := -1
		for i := len(p.tokens) - 1; i >= 0; i-- {
			if p.tokens[i].Kind == lexer.ARROW {
				next = i
			}
			p.arrowFrom[i] = next
		}
		p.arrowFrom[len(p.tokens)] = -1
	}
	if p.pos >= len(p.tokens) || p.arrowFrom[p.pos] < 0 {
		return false
	}

	depth := 0
	for i := p.pos; i < len(p.tokens); i++ {
		tok := p.tokens[i]

		if tok.Kind == lexer.LPAREN {
			depth++
		} else if tok.Kind == lexer.RPAREN {
			depth--

			if depth == 0 {
				lookahead := i + 1

				for lookahead < len(p.tokens) && p.tokens[lookahead].Kind == lexer.Whitespace {
					lookahead++
				}

				// Skip an optional return-type annotation (`: Type =>`), e.g.
				// `(x: string): number => x.length`. After the closing paren,
				// a `:` means a return type follows; scan past type tokens to
				// the `=>` at the same nesting depth. The scan is bounded so it
				// cannot leak onto arrows from unrelated later code: it stops
				// at top-level statement/expression delimiters (;, ,, ), {, =,
				// a second top-level `:`) which never occur inside a return
				// type annotation before its `=>`.
				if lookahead < len(p.tokens) && p.tokens[lookahead].Kind == lexer.COLON {
					la := lookahead + 1
					tdepth := 0
					for la < len(p.tokens) {
						k := p.tokens[la].Kind
						switch k {
						case lexer.LPAREN, lexer.LT, lexer.LBRACKET, lexer.LBRACE:
							tdepth++
						case lexer.RPAREN, lexer.GT, lexer.RBRACKET, lexer.RBRACE:
							if tdepth > 0 {
								tdepth--
							} else {
								return false
							}
						case lexer.ARROW:
							if tdepth == 0 {
								return true
							}
						case lexer.SEMI, lexer.COMMA, lexer.ASSIGN, lexer.EOF, lexer.COLON:
							if tdepth == 0 {
								return false
							}
						}
						la++
					}
					return false
				}

				return lookahead < len(p.tokens) && p.tokens[lookahead].Kind == lexer.ARROW
			}
		}
	}
	return false
}

// parseCallArgs parses a call's argument list starting at the current `(`,
// producing a CallExpr over callee. optional marks an optional call (`fn?.(...)`).
func (p *Parser) parseCallArgs(callee ast.Expr, optional bool) ast.Expr {
	p.expect(lexer.LPAREN)
	var args []ast.Expr
	for p.peek().Kind != lexer.RPAREN && p.peek().Kind != lexer.EOF {
		args = append(args, p.parseExpr(precLowest))
		if !p.match(lexer.COMMA) {
			break
		}
	}
	p.expect(lexer.RPAREN)
	return &ast.CallExpr{Callee: callee, Args: args, Optional: optional}
}

func (p *Parser) parseInfix(left ast.Expr) ast.Expr {
	tok := p.peek()

	switch tok.Kind {
	case lexer.PLUS:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "+", Right: p.parseExpr(precTerm)}
	case lexer.MINUS:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "-", Right: p.parseExpr(precTerm)}
	case lexer.STAR:
		p.next()
		if p.peek().Kind == lexer.STAR {
			// `2 ** 10` lexes as two STAR tokens; without this guard it would be
			// mangled into `(2 * nil) * 10` and emitted as incorrect code.
			p.errWithMsg("exponentiation operator ** is not supported", "use Math.pow(a, b) or a * a instead")
			p.next()
			_ = p.parseExpr(precFactor)
			return left
		}
		return &ast.BinaryExpr{Left: left, Op: "*", Right: p.parseExpr(precFactor)}
	case lexer.DIV:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "/", Right: p.parseExpr(precFactor)}
	case lexer.MOD:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "%", Right: p.parseExpr(precFactor)}
	case lexer.EQ:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "==", Right: p.parseExpr(precEq)}
	case lexer.NEQ:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "!=", Right: p.parseExpr(precEq)}
	case lexer.STRICT_EQ:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "===", Right: p.parseExpr(precEq)}
	case lexer.STRICT_NEQ:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "!==", Right: p.parseExpr(precEq)}
	case lexer.BIT_AND:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "&", Right: p.parseExpr(precBitwiseAnd)}
	case lexer.BIT_OR:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "|", Right: p.parseExpr(precBitwiseOr)}
	case lexer.BIT_XOR:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "^", Right: p.parseExpr(precBitwiseXor)}
	case lexer.LT:
		if p.looksLikeTypeArgs() {
			p.next()
			p.skipTypeArgs()
			return left
		}
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "<", Right: p.parseExpr(precCompare)}
	case lexer.GT:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: ">", Right: p.parseExpr(precCompare)}
	case lexer.LTE:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "<=", Right: p.parseExpr(precCompare)}
	case lexer.GTE:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: ">=", Right: p.parseExpr(precCompare)}
	case lexer.In_:
		if p.noIn {
			// Inside a `for` initializer, `in` is not a binary operator - it
			// begins the for-in clause. Stop so the caller can handle it.
			return nil
		}
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "in", Right: p.parseExpr(precCompare)}
	case lexer.Instanceof_:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "instanceof", Right: p.parseExpr(precCompare)}
	case lexer.SHL:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: "<<", Right: p.parseExpr(precShift)}
	case lexer.SHR:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: ">>", Right: p.parseExpr(precShift)}
	case lexer.SHL_ASSIGN, lexer.SHR_ASSIGN:
		p.errWithMsg(fmt.Sprintf("compound assignment %s is not supported", kindLabel(tok.Kind)), "")
		p.next()
		_ = p.parseExpr(precAssign)
		return left
	case lexer.USHIFT_RIGHT:
		p.next()
		return &ast.BinaryExpr{Left: left, Op: ">>>", Right: p.parseExpr(precShift)}
	case lexer.AND:
		p.next()
		if p.peek().Kind == lexer.ASSIGN {
			// `x &&= 1` lexes as `x && = 1`; without this guard the RHS would
			// parse to nil and emit incorrect code.
			p.errWithMsg("compound assignment &&= is not supported", "")
			p.next()
			_ = p.parseExpr(precAssign)
			return left
		}
		return &ast.BinaryExpr{Left: left, Op: "&&", Right: p.parseExpr(precAnd)}
	case lexer.OR:
		p.next()
		if p.peek().Kind == lexer.ASSIGN {
			p.errWithMsg("compound assignment ||= is not supported", "")
			p.next()
			_ = p.parseExpr(precAssign)
			return left
		}
		return &ast.BinaryExpr{Left: left, Op: "||", Right: p.parseExpr(precOr)}
	case lexer.NULLISH:
		p.next()
		if p.peek().Kind == lexer.ASSIGN {
			p.errWithMsg("compound assignment ??= is not supported", "")
			p.next()
			_ = p.parseExpr(precAssign)
			return left
		}
		return &ast.BinaryExpr{Left: left, Op: "??", Right: p.parseExpr(precOr)}
	case lexer.ARROW:
		p.next()
		params := []*ast.Param{}
		if id, ok := left.(*ast.Identifier); ok {
			params = append(params, &ast.Param{Name: id.Name})
		}
		return p.parseArrowBody(params)
	case lexer.ASSIGN, lexer.ADD_ASSIGN, lexer.SUB_ASSIGN, lexer.MUL_ASSIGN, lexer.DIV_ASSIGN, lexer.MOD_ASSIGN:
		op := tok.Value
		p.next()
		// Assignment is right-associative: parse the RHS one level below
		// assignment so `a = b = c` nests as `a = (b = c)`, not `(a = b) = c`.
		// The precedence-climbing loop breaks on `prec >= nextPrec`, so a lower
		// bound lets a same-precedence `=` bind inside the RHS.
		return &ast.BinaryExpr{Left: left, Op: op, Right: p.parseExpr(precAssign - 1)}
	case lexer.QUEST:
		p.next()
		con := p.parseExpr(precAssign)
		p.expect(lexer.COLON)
		alt := p.parseExpr(precAssign)
		return &ast.ConditionalExpr{Test: left, Consequent: con, Alternate: alt}
	case lexer.DOT:
		p.next()
		if isIdentifierToken(p.peek().Kind) {
			name := p.next().Value
			return &ast.MemberExpr{Object: left, Property: &ast.Identifier{Name: name}, Computed: false}
		}
		return nil
	case lexer.QUESTION_DOT:
		p.next()
		// Optional call `fn?.(args)`.
		if p.peek().Kind == lexer.LPAREN {
			return p.parseCallArgs(left, true)
		}
		// Optional computed member `obj?.[expr]`.
		if p.peek().Kind == lexer.LBRACKET {
			p.next()
			prop := p.parseExpr(precLowest)
			p.expect(lexer.RBRACKET)
			return &ast.MemberExpr{Object: left, Property: prop, Computed: true, Optional: true}
		}
		if isIdentifierToken(p.peek().Kind) {
			name := p.next().Value
			return &ast.MemberExpr{Object: left, Property: &ast.Identifier{Name: name}, Computed: false, Optional: true}
		}
		// `?.` followed by anything else is not a valid chain. The token is
		// already consumed, so return the receiver rather than nil (which would
		// drop the whole expression) and surface a diagnostic.
		p.errWithMsg("unexpected token after `?.`", "optional chaining expects a property, `[...]`, or `(...)`")
		return left
	case lexer.INC:
		// ASI: `a\n++b` must not postfix-bind across the newline.
		if p.newlineAfterPrev(p.pos) {
			return nil
		}
		p.next()
		return &ast.UnaryExpr{Op: "++", Arg: left, Postfix: true}
	case lexer.DEC:
		if p.newlineAfterPrev(p.pos) {
			return nil
		}
		p.next()
		return &ast.UnaryExpr{Op: "--", Arg: left, Postfix: true}
	case lexer.NOT:
		p.next()
		return &ast.TypeAssertion{Expr: left}
	case lexer.LPAREN:
		return p.parseCallArgs(left, false)
	case lexer.LBRACKET:
		p.next()
		prop := p.parseExpr(precLowest)
		p.expect(lexer.RBRACKET)
		return &ast.MemberExpr{Object: left, Property: prop, Computed: true}
	case lexer.As:
		p.droppedTypes++
		p.next()
		typeRef := p.parseExpr(precAs)
		// Type unions/intersections in `as` type annotations (e.g. `as HTMLElement | null`)
		// must be consumed here, not left to the enclosing expression where the `|`
		// would be parsed as a bitwise-OR on the asserted value.
		for p.peek().Kind == lexer.BIT_OR || p.peek().Kind == lexer.BIT_AND {
			p.next()
			p.parseExpr(precAs)
		}
		if p.peek().Kind == lexer.LT {
			p.next()
			p.skipTypeArgs()
		}
		typeName := ""
		if id, ok := typeRef.(*ast.Identifier); ok {
			typeName = id.Name
		}
		return &ast.TypeAssertion{Expr: left, TypeRef: typeName}
	default:
		return nil
	}
}

func (p *Parser) parseFnExpr() ast.Expr {
	p.next()
	fn := &ast.ArrowFn{Async: false}
	// A function expression may carry a name (`function Foo() {}`). It is only
	// visible inside the body, so it is consumed and ignored - matching how the
	// compiler treats unnamed function expressions. Compiled library output
	// (e.g. Radix UI's `__name(function Foo() {...}, "Foo")`) relies on this.
	if isIdentifierToken(p.peek().Kind) {
		p.next()
	}
	p.expect(lexer.LPAREN)
	fn.Params = p.parseParamList()
	p.expect(lexer.RPAREN)
	p.skipTypeAnnotation(true)
	if p.match(lexer.LBRACE) {
		fn.Body = p.parseStmtList(lexer.RBRACE)
		p.expect(lexer.RBRACE)
		fn.Expression = false
	} else {
		fn.Body = []ast.Stmt{&ast.ReturnStmt{Value: p.parseExpr(precLowest)}}
		fn.Expression = true
	}
	return fn
}

func (p *Parser) parseArrowBody(params []*ast.Param) *ast.ArrowFn {
	fn := &ast.ArrowFn{Params: params}
	if p.peek().Kind == lexer.LBRACE {
		if p.looksLikeObjectInArrow() {
			fn.Body = []ast.Stmt{&ast.ReturnStmt{Value: p.parseExpr(precLowest)}}
			fn.Expression = true
		} else {
			p.next()
			fn.Body = p.parseStmtList(lexer.RBRACE)
			p.expect(lexer.RBRACE)
			fn.Expression = false
		}
	} else {
		fn.Body = []ast.Stmt{&ast.ReturnStmt{Value: p.parseExpr(precLowest)}}
		fn.Expression = true
	}
	return fn
}

func (p *Parser) looksLikeObjectInArrow() bool {
	pos := p.pos
	for pos < len(p.tokens) && p.tokens[pos].Kind == lexer.Whitespace {
		pos++
	}
	if pos >= len(p.tokens) || p.tokens[pos].Kind != lexer.LBRACE {
		return false
	}
	pos++
	for pos < len(p.tokens) && p.tokens[pos].Kind == lexer.Whitespace {
		pos++
	}
	if pos >= len(p.tokens) {
		return false
	}
	if p.tokens[pos].Kind == lexer.RBRACE {
		return false
	}
	if p.tokens[pos].Kind == lexer.SPREAD {
		return true
	}
	if isIdentifierToken(p.tokens[pos].Kind) {
		pos++
		for pos < len(p.tokens) && p.tokens[pos].Kind == lexer.Whitespace {
			pos++
		}
		if pos < len(p.tokens) && p.tokens[pos].Kind == lexer.COLON {
			return true
		}
	}
	if p.tokens[pos].Kind == lexer.String {
		pos++
		for pos < len(p.tokens) && p.tokens[pos].Kind == lexer.Whitespace {
			pos++
		}
		if pos < len(p.tokens) && p.tokens[pos].Kind == lexer.COLON {
			return true
		}
	}
	return false
}

func (p *Parser) looksLikeTypeArgs() bool {
	depth := 0
	for i := p.pos; i < len(p.tokens); i++ {
		tok := p.tokens[i]
		switch tok.Kind {
		case lexer.LT:
			depth++
		case lexer.GT:
			depth--
			if depth == 0 {
				lookahead := i + 1
				for lookahead < len(p.tokens) && p.tokens[lookahead].Kind == lexer.Whitespace {
					lookahead++
				}
				return lookahead < len(p.tokens) && p.tokens[lookahead].Kind == lexer.LPAREN
			}
		case lexer.LPAREN:
			if depth == 1 {
				return false
			}
		case lexer.Identifier, lexer.COMMA, lexer.DOT, lexer.Number, lexer.String, lexer.As, lexer.Whitespace,
			lexer.Null_, lexer.True, lexer.False, lexer.LBRACKET, lexer.RBRACKET:
			continue
		case lexer.LBRACE, lexer.RBRACE, lexer.SEMI:
			return false
		case lexer.EOF:
			return false
		default:
			if tok.Value == "|" || tok.Value == "&" || isIdentifierToken(tok.Kind) {
				continue
			}
			if depth == 1 {
				return false
			}
		}
	}
	return false
}

func (p *Parser) skipTypeArgs() {
	p.droppedTypes++
	depth := 1
	for depth > 0 && p.pos < len(p.tokens) {
		tok := p.next()
		switch tok.Kind {
		case lexer.LT:
			depth++
		case lexer.GT:
			depth--
		case lexer.EOF:
			return
		}
	}
}

func (p *Parser) parseTemplateExpr() ast.Expr {
	tmpl := &ast.TemplateExpr{}
	for p.pos < len(p.tokens) {
		tok := p.peek()
		if tok.Kind == lexer.TEMPLATE_END {
			tmpl.Raw = append(tmpl.Raw, tok.Value)
			p.next()
			break
		}
		if tok.Kind == lexer.EOF {
			break
		}
		if tok.Kind == lexer.LBRACE {
			p.next()
			expr := p.parseExpr(precLowest)
			tmpl.Parts = append(tmpl.Parts, expr)
			if p.peek().Kind == lexer.RBRACE {
				p.next()
			}
		} else {
			tok := p.next()
			tmpl.Raw = append(tmpl.Raw, tok.Value)
		}
	}
	return tmpl
}

func (p *Parser) parseJSXElement() ast.Expr {
	if p.peek().Kind == lexer.LT_SLASH {
		return nil
	}

	p.expect(lexer.LT)

	if p.peek().Kind == lexer.GT {
		return p.parseJSXFragment()
	}

	if !isIdentifierToken(p.peek().Kind) {
		p.err("expected tag name in JSX")
		return nil
	}

	pos := tokPos(p.peek())
	name := p.next().Value
	for p.peek().Kind == lexer.DOT && p.pos+1 < len(p.tokens) && isIdentifierToken(p.tokens[p.pos+1].Kind) {
		p.next()
		name += "." + p.next().Value
	}
	opening := &ast.JSXOpening{Name: name}

	for {
		tok := p.peek()
		if tok.Kind == lexer.GT {
			p.next()
			break
		}
		if tok.Kind == lexer.SLASH_GT {
			p.next()
			opening.SelfClosing = true
			return &ast.JSXElement{Position: pos, Opening: opening}
		}
		if tok.Kind == lexer.EOF {
			p.errWithMsg("unterminated JSX opening tag", "the tag <"+name+"> is missing its closing `>`")
			return &ast.JSXElement{Position: pos, Opening: opening}
		}
		if tok.Kind == lexer.Whitespace {
			p.next()
			continue
		}
		attr := p.parseJSXAttr()
		if attr != nil {
			opening.Attributes = append(opening.Attributes, attr)
		} else {
			break
		}
	}

	el := &ast.JSXElement{Position: pos, Opening: opening}

	for {
		// rawPeek so whitespace before inline text isn't consumed: the text
		// between elements is handled by parseJSXChild -> parseJSXText.
		tok := p.rawPeek()
		if tok.Kind == lexer.LT_SLASH {
			p.next()
			closingName := ""
			if isIdentifierToken(p.peek().Kind) {
				closingName = p.next().Value
				for p.peek().Kind == lexer.DOT && p.pos+1 < len(p.tokens) && isIdentifierToken(p.tokens[p.pos+1].Kind) {
					p.next()
					closingName += "." + p.next().Value
				}
				el.Closing = &ast.JSXClosing{Name: closingName}
			}
			p.expect(lexer.GT)
			break
		}
		if tok.Kind == lexer.EOF {
			p.errWithMsg("unterminated JSX element", "the element <"+name+"> is missing its closing </"+name+">")
			break
		}
		child := p.parseJSXChild()
		if child != nil {
			el.Children = append(el.Children, child)
		} else {
			p.next()
		}
	}

	return el
}

func (p *Parser) parseJSXFragment() ast.Expr {
	pos := tokPos(p.peek())
	p.expect(lexer.GT)
	frag := &ast.JSXFragment{Position: pos}

	for {
		tok := p.rawPeek()
		if tok.Kind == lexer.LT_SLASH {
			p.next()
			p.expect(lexer.GT)
			break
		}
		if tok.Kind == lexer.EOF {
			p.errWithMsg("unterminated JSX fragment", "the fragment `<>` is missing its closing `</>`")
			break
		}
		child := p.parseJSXChild()
		if child == nil {
			break
		}
		frag.Children = append(frag.Children, child)
	}

	return frag
}

func (p *Parser) parseJSXChild() ast.JSXChild {
	// Use rawPeek (not peek, which skips whitespace) so a leading space before
	// inline text survives: `<code>a</code> and <code>b</code>` must keep the
	// space between the two elements. Whitespace-only/newline text is removed
	// by normalizeJSXText.
	tok := p.rawPeek()

	switch tok.Kind {
	case lexer.LBRACE:
		p.next()
		ec := &ast.JSXExprContainer{}
		if p.peek().Kind != lexer.RBRACE {
			ec.Expression = p.parseExpr(precLowest)
		}
		p.expect(lexer.RBRACE)
		return ec

	case lexer.LT:
		// `<` starts an element/fragment only when a tag name or `>` follows.
		// A stray `<` (e.g. `a < b` written without `{'{'}`) is preserved as
		// literal text rather than mis-parsed as a tag.
		if !p.jsxTagFollows() {
			p.next()
			return &ast.JSXText{Value: "<"}
		}
		elem := p.parseJSXElement()
		if el, ok := elem.(*ast.JSXElement); ok {
			return &ast.JSXElementChild{Element: el}
		}
		if frag, ok := elem.(*ast.JSXFragment); ok {
			return &ast.JSXFragmentChild{Fragment: frag}
		}
		return nil

	case lexer.LT_SLASH, lexer.EOF:
		return nil

	default:
		// Includes GT, which is literal text inside children (D2: preserve `>`
		// as text, not a terminator).
		return p.parseJSXText()
	}
}

// jsxTagFollows reports whether the `<` at the current position begins a JSX
// element or fragment. A tag name must be adjacent to `<` (JSX allows no space
// after it), so `< b` in prose is literal text while `<b>` is a tag.
func (p *Parser) jsxTagFollows() bool {
	i := p.pos + 1
	if i >= len(p.tokens) {
		return false
	}
	k := p.tokens[i].Kind
	return k == lexer.GT || isIdentifierToken(k)
}

func (p *Parser) parseJSXText() ast.JSXChild {
	var b strings.Builder
	for {
		tok := p.rawPeek()
		switch tok.Kind {
		case lexer.LBRACE, lexer.LT, lexer.LT_SLASH, lexer.EOF:
			if b.Len() > 0 {
				return &ast.JSXText{Value: normalizeJSXText(b.String())}
			}
			return nil
		default:
			p.pos++
			b.WriteString(tok.Value)
		}
	}
}

// normalizeJSXText applies the canonical JSX whitespace transform (matching
// Babel's transform-jsx-text): whitespace runs that span newlines are trimmed
// at line boundaries and internal runs collapsed; whitespace on a single line
// between elements is preserved as-is.
func normalizeJSXText(text string) string {
	lines := splitJSXLines(text)
	if len(lines) == 0 {
		return ""
	}
	lastNonEmptyLine := 0
	for i, line := range lines {
		if strings.IndexFunc(line, func(r rune) bool { return r != ' ' && r != '\t' }) >= 0 {
			lastNonEmptyLine = i
		}
	}
	var b strings.Builder
	for i, line := range lines {
		trimmed := strings.ReplaceAll(line, "\t", " ")
		if i != 0 {
			trimmed = strings.TrimLeft(trimmed, " ")
		}
		if i != len(lines)-1 {
			trimmed = strings.TrimRight(trimmed, " ")
		}
		if trimmed != "" {
			if i != lastNonEmptyLine {
				trimmed += " "
			}
			b.WriteString(trimmed)
		}
	}
	// Decode HTML character references in JSX text (React/Babel do this), so
	// `&rsaquo;` renders as `` in both SSR and the client `h()` path.
	return html.UnescapeString(b.String())
}

// splitJSXLines splits a string on \n and \r (handling \r\n) into its lines.
func splitJSXLines(s string) []string {
	var lines []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			lines = append(lines, cur.String())
			cur.Reset()
		case '\r':
			lines = append(lines, cur.String())
			cur.Reset()
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		default:
			cur.WriteByte(s[i])
		}
	}
	lines = append(lines, cur.String())
	return lines
}

func (p *Parser) rawPeek() lexer.Token {
	if p.pos >= len(p.tokens) {
		return lexer.Token{Kind: lexer.EOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) parseJSXAttr() *ast.JSXAttr {
	tok := p.peek()
	pos := tokPos(tok)

	if tok.Kind == lexer.LBRACE {
		p.next()
		if p.match(lexer.SPREAD) {
			expr := p.parseExpr(precLowest)
			p.expect(lexer.RBRACE)
			return &ast.JSXAttr{Spread: true, Value: expr}
		}
		expr := p.parseExpr(precLowest)
		p.expect(lexer.RBRACE)
		return &ast.JSXAttr{Name: "", Value: expr, Position: pos}
	}

	if isIdentifierToken(tok.Kind) {
		p.next()
		name := tok.Value
		for p.peek().Kind == lexer.MINUS && p.pos+1 < len(p.tokens) && isIdentifierToken(p.tokens[p.pos+1].Kind) {
			p.next()
			name += "-" + p.next().Value
		}
		if p.match(lexer.ASSIGN) {
			if p.peek().Kind == lexer.String {
				val := p.next()
				// JSX attribute strings do not process backslash escapes (unlike
				// JS string literals), so only the delimiter quotes are stripped.
				unquoted := val.Value
				if len(unquoted) >= 2 {
					unquoted = unquoted[1 : len(unquoted)-1]
				}
				return &ast.JSXAttr{Name: name, Value: &ast.Literal{Kind: ast.StringLit, Value: unquoted}, Position: pos}
			}
			if p.peek().Kind == lexer.LBRACE {
				p.next()
				expr := p.parseExpr(precLowest)
				p.expect(lexer.RBRACE)
				return &ast.JSXAttr{Name: name, Value: expr, Position: pos}
			}
		}
		return &ast.JSXAttr{Name: name, Value: &ast.Literal{Kind: ast.BoolLit, Value: "true"}, Position: pos}
	}

	return nil
}

// skipTypeParams skips a generic type-parameter list (`<T, U extends X>`)
// if one is present at the current position. These are pure compile-time
// syntax and carry no runtime meaning for hydration generation.
func (p *Parser) skipTypeParams() {
	if p.peek().Kind != lexer.LT {
		return
	}
	p.droppedTypes++
	depth := 0
	for {
		tok := p.peek()
		if tok.Kind == lexer.EOF {
			return
		}
		switch tok.Kind {
		case lexer.LT:
			depth++
		case lexer.GT:
			depth--
			if depth <= 0 {
				p.next()
				return
			}
		case lexer.SHL:
			p.next()
			continue
		case lexer.SEMI:
			return
		}
		p.next()
	}
}

func (p *Parser) skipTypeAnnotation(beforeBrace bool, stopAtArrow ...bool) {
	if p.peek().Kind != lexer.COLON {
		return
	}
	p.droppedTypes++
	p.next()
	stopArrow := len(stopAtArrow) > 0 && stopAtArrow[0]
	depth := 0
	for {
		tok := p.peek()
		if tok.Kind == lexer.EOF {
			break
		}
		if stopArrow && tok.Kind == lexer.ARROW && depth == 0 {
			return
		}
		switch tok.Kind {
		case lexer.LBRACE, lexer.LPAREN, lexer.LT, lexer.LBRACKET:
			if depth == 0 && beforeBrace && tok.Kind == lexer.LBRACE {
				return
			}
			depth++
		case lexer.RBRACE, lexer.RPAREN, lexer.GT, lexer.RBRACKET:
			depth--
			if depth < 0 {
				return
			}
		case lexer.COMMA, lexer.ASSIGN, lexer.SEMI:
			if depth == 0 {
				return
			}
		}
		p.next()
	}
}

// isPrimitiveTypeKeyword reports whether a token names a primitive type that
// can open an angle-bracket type assertion (`<T>`). These never name JSX
// elements, so a leading `<` of this form is unambiguous.
func isPrimitiveTypeKeyword(k lexer.Kind) bool {
	switch k {
	case lexer.String_, lexer.Number_, lexer.Boolean_, lexer.Any_, lexer.Unknown_, lexer.Never_, lexer.Symbol_, lexer.Void_:
		return true
	}
	return false
}

// parseTypeAssertion parses an angle-bracket type assertion `<T>expr`, where
// the leading `<` is already consumed. The cast itself is compile-time only;
// the embedded runtime expression is preserved.
func (p *Parser) parseTypeAssertion() ast.Expr {
	p.droppedTypes++
	pos := tokPos(p.peek())
	p.next() // consume `<`
	// Consume the type reference up to the closing `>`.
	for p.peek().Kind != lexer.GT && p.peek().Kind != lexer.EOF && p.peek().Kind != lexer.SEMI {
		p.next()
	}
	p.expect(lexer.GT)
	expr := p.parseExpr(precUnary)
	if expr == nil {
		return nil
	}
	return &ast.TypeAssertion{Position: pos, Expr: expr}
}

func tokPos(tok lexer.Token) ast.Pos {
	return ast.Pos{Line: tok.Line, Col: tok.Col}
}

func posFromStmts(stmts []ast.Stmt) ast.Pos {
	if len(stmts) > 0 {
		return stmts[0].Pos()
	}
	return ast.Pos{}
}

func kindLabel(k lexer.Kind) string {
	switch k {
	case lexer.EOF:
		return "EOF"
	case lexer.Identifier:
		return "identifier"
	case lexer.String:
		return "string"
	case lexer.Number:
		return "number"
	case lexer.Const_:
		return "const"
	case lexer.Let_:
		return "let"
	case lexer.Var_:
		return "var"
	case lexer.Function:
		return "function"
	case lexer.Return:
		return "return"
	case lexer.If_:
		return "if"
	case lexer.Else_:
		return "else"
	case lexer.Import:
		return "import"
	case lexer.Export:
		return "export"
	case lexer.From:
		return "from"
	case lexer.True:
		return "true"
	case lexer.False:
		return "false"
	case lexer.Null_:
		return "null"
	case lexer.Async:
		return "async"
	case lexer.Default_:
		return "default"
	case lexer.LBRACE:
		return "{"
	case lexer.RBRACE:
		return "}"
	case lexer.LPAREN:
		return "("
	case lexer.RPAREN:
		return ")"
	case lexer.LBRACKET:
		return "["
	case lexer.RBRACKET:
		return "]"
	case lexer.COMMA:
		return ","
	case lexer.SEMI:
		return ";"
	case lexer.DOT:
		return "."
	case lexer.COLON:
		return ":"
	case lexer.ARROW:
		return "=>"
	case lexer.QUEST:
		return "?"
	case lexer.ASSIGN:
		return "="
	case lexer.EQ:
		return "=="
	case lexer.STRICT_EQ:
		return "==="
	case lexer.NEQ:
		return "!="
	case lexer.STRICT_NEQ:
		return "!=="
	case lexer.PLUS:
		return "+"
	case lexer.MINUS:
		return "-"
	case lexer.STAR:
		return "*"
	case lexer.DIV:
		return "/"
	case lexer.MOD:
		return "%"
	case lexer.LT:
		return "<"
	case lexer.GT:
		return ">"
	case lexer.LTE:
		return "<="
	case lexer.GTE:
		return ">="
	case lexer.AND:
		return "&&"
	case lexer.OR:
		return "||"
	case lexer.NOT:
		return "!"
	case lexer.INC:
		return "++"
	case lexer.DEC:
		return "--"
	case lexer.ADD_ASSIGN:
		return "+="
	case lexer.SUB_ASSIGN:
		return "-="
	case lexer.MUL_ASSIGN:
		return "*="
	case lexer.DIV_ASSIGN:
		return "/="
	case lexer.MOD_ASSIGN:
		return "%="
	case lexer.SLASH_GT:
		return "/>"
	case lexer.LT_SLASH:
		return "</"
	case lexer.SPREAD:
		return "..."
	case lexer.TEMPLATE_START:
		return "`${"
	case lexer.TEMPLATE_END:
		return "`"
	case lexer.AT:
		return "@"
	case lexer.For:
		return "for"
	case lexer.While_:
		return "while"
	case lexer.Do_:
		return "do"
	case lexer.Switch:
		return "switch"
	case lexer.Case_:
		return "case"
	case lexer.Try_:
		return "try"
	case lexer.Catch_:
		return "catch"
	case lexer.Finally_:
		return "finally"
	case lexer.Throw_:
		return "throw"
	case lexer.Break_:
		return "break"
	case lexer.Continue_:
		return "continue"
	case lexer.New_:
		return "new"
	case lexer.This_:
		return "this"
	case lexer.Await_:
		return "await"
	case lexer.In_:
		return "in"
	case lexer.SHL:
		return "<<"
	case lexer.SHR:
		return ">>"
	case lexer.SHL_ASSIGN:
		return "<<="
	case lexer.SHR_ASSIGN:
		return ">>="
	case lexer.USHIFT_RIGHT:
		return ">>>"
	case lexer.QUESTION_DOT:
		return "?."
	case lexer.NULLISH:
		return "??"
	case lexer.As:
		return "as"
	case lexer.Regexp:
		return "regex"
	case lexer.BIT_AND:
		return "&"
	case lexer.BIT_OR:
		return "|"
	case lexer.BIT_XOR:
		return "^"
	case lexer.BIT_NOT:
		return "~"
	case lexer.Class_:
		return "class"
	case lexer.Super_:
		return "super"
	case lexer.Extends_:
		return "extends"
	case lexer.Yield_:
		return "yield"
	case lexer.Delete_:
		return "delete"
	case lexer.Typeof_:
		return "typeof"
	case lexer.Instanceof_:
		return "instanceof"
	case lexer.Void_:
		return "void"
	case lexer.Debugger_:
		return "debugger"
	case lexer.With_:
		return "with"
	case lexer.Interface_:
		return "interface"
	case lexer.Type_:
		return "type"
	case lexer.Implements_:
		return "implements"
	case lexer.Enum_:
		return "enum"
	default:
		return fmt.Sprintf("token(%d)", int(k))
	}
}

func FormatDiagnostics(errs []error) string {
	return diag.FormatDiagnostics(errs)
}
