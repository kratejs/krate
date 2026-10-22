// Package parser is Krate's TSX/JSX parser. It parses ES2024 plus JSX into the
// ast package's tree and skips TypeScript-only syntax (annotations, generics,
// interfaces, type aliases, casts), counting each skipped construct in
// DroppedTypes. TypeScript constructs that carry runtime semantics and cannot
// be represented (enums, namespaces with values, parameter properties,
// decorators) are reported as diagnostics instead of being dropped.
package parser

import (
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// Diagnostic is an alias for the shared compiler diagnostic type.
type Diagnostic = diag.Diagnostic

type Parser struct {
	// tokens holds the significant tokens (whitespace removed). The last token
	// is always EOF.
	tokens   []lexer.Token
	pos      int
	errs     []error
	Filename string
	srcLines []string

	// droppedTypes counts type-only constructs the parser intentionally
	// discards (interfaces, type aliases, annotations, generics, `as` and
	// angle-bracket casts). A non-zero count means the AST cannot reproduce
	// the original source - used by tooling that round-trips the AST back to
	// source (see internal/astprint and the MCP edit_ast tool).
	droppedTypes int

	// pair[i] is the index of the bracket matching the (, [ or { at index i,
	// or -1. Computed once so lookahead over a bracketed region is O(1).
	pair []int

	// depth tracks the recursive-descent nesting depth so pathologically nested
	// input is rejected with a diagnostic instead of exhausting the stack.
	depth int

	// errAt de-duplicates diagnostics reported at the same token offset.
	errAt map[int]bool

	// Context flags.
	noIn        bool // `in` is not a binary operator (for-statement init)
	inFunction  bool
	inAsync     bool
	inGenerator bool
	inClass     bool
	strict      bool
	labels      []labelInfo
	breakable   int // enclosing loops + switches in the current function
	loops       int // enclosing loops in the current function
	// noReturnTypeArrow disables `(x): T => ...` detection in a conditional's
	// consequent, where `a ? (b) : c` is not an arrow.
	noReturnTypeArrow bool

	// More function-context flags (saved and restored with the ones above).
	superProp     bool // `super.x` allowed (methods, field initializers)
	superCall     bool // `super()` allowed (derived-class constructors)
	inFieldInit   bool // class field initializer: `arguments` is an error
	inStaticBlock bool // class static block: `await` is reserved
	inParams      bool // parameter list: yield/await expressions are errors
	yieldIsError  bool // arrow parameters inside a generator
	// derivedCtor is set by the class parser just before parsing the
	// constructor of a class with an `extends` clause.
	derivedCtor bool

	scope *scope
	// classes tracks private names per enclosing class body.
	classes []*privateScope
	// paramNames holds the binding-name tokens of the last parameter list.
	paramNames []lexer.Token

	// parenthesized records expressions written inside grouping parentheses.
	// It is consulted for early errors (`a ?? (b || c)` is valid,
	// `a ?? b || c` is not) and for assignment-target checks.
	parenthesized map[ast.Expr]bool
	// protoDup records object literals with a duplicate `__proto__` key; the
	// error is dropped if the literal turns out to be a destructuring pattern.
	protoDup map[*ast.ObjectExpr]lexer.Token
	// coverInit records object literals containing `{ a = 1 }` shorthand
	// initializers, which are only valid as destructuring patterns.
	coverInit map[*ast.ObjectExpr]lexer.Token
}

type labelInfo struct {
	name   string
	isLoop bool
}

// tmplNoSub is a parser-internal token kind for a template literal without
// substitutions ("`text`"), which the lexer reports as TEMPLATE_END.
const tmplNoSub lexer.Kind = 1000

// maxParseDepth bounds recursive-descent nesting. Real code stays far below it;
// input that exceeds it is a hard parse error.
const maxParseDepth = 4000

// maxErrors bounds the number of diagnostics collected for one file.
const maxErrors = 100

// parseDepthError is the sentinel panic used to unwind out of deep recursion;
// it is recovered in ParseProgram.
type parseDepthError struct{}

// enter increments the nesting depth and aborts parsing when it is exceeded.
func (p *Parser) enter() {
	p.depth++
	if p.depth > maxParseDepth {
		panic(parseDepthError{})
	}
}

func (p *Parser) leave() { p.depth-- }

// New creates a parser over a token stream produced by lexer.Tokenize.
// Whitespace tokens are dropped; layout information the parser needs is
// carried by Token.NewlineBefore.
func New(tokens []lexer.Token) *Parser {
	sig := make([]lexer.Token, 0, len(tokens)/2+1)
	for _, t := range tokens {
		if t.Kind == lexer.Whitespace {
			continue
		}
		if t.Kind == lexer.TEMPLATE_END && strings.HasPrefix(t.Value, "`") {
			t.Kind = tmplNoSub
		}
		sig = append(sig, t)
	}
	if len(sig) == 0 || sig[len(sig)-1].Kind != lexer.EOF {
		eof := lexer.Token{Kind: lexer.EOF}
		if len(sig) > 0 {
			last := sig[len(sig)-1]
			eof.Offset, eof.End = last.End, last.End
			eof.Line, eof.Col = last.EndLine, last.EndCol
		}
		sig = append(sig, eof)
	}
	p := &Parser{tokens: sig, errAt: map[int]bool{}}
	p.computeMatches()
	return p
}

// computeMatches pairs brackets in one pass. Unbalanced brackets stay -1.
func (p *Parser) computeMatches() {
	p.pair = make([]int, len(p.tokens))
	stack := make([]int, 0, 64)
	for i, t := range p.tokens {
		p.pair[i] = -1
		switch t.Kind {
		case lexer.LPAREN, lexer.LBRACKET, lexer.LBRACE:
			stack = append(stack, i)
		case lexer.RPAREN, lexer.RBRACKET, lexer.RBRACE:
			open := lexer.LPAREN
			if t.Kind == lexer.RBRACKET {
				open = lexer.LBRACKET
			} else if t.Kind == lexer.RBRACE {
				open = lexer.LBRACE
			}
			if n := len(stack); n > 0 && p.tokens[stack[n-1]].Kind == open {
				j := stack[n-1]
				stack = stack[:n-1]
				p.pair[j] = i
				p.pair[i] = j
			}
		}
	}
}

// DroppedTypes reports how many type-only constructs the parse discarded.
func (p *Parser) DroppedTypes() int { return p.droppedTypes }

// SetSource provides the original source so diagnostics can show the
// offending line.
func (p *Parser) SetSource(src string) {
	p.srcLines = lexer.SplitLines(src)
}

func (p *Parser) Errors() []error { return p.errs }

// ParseProgram parses the whole token stream.
func (p *Parser) ParseProgram() (prog *ast.Program) {
	prog = &ast.Program{Position: tokPos(p.tok())}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(parseDepthError); ok {
				p.errorAt(p.tok(), fmt.Sprintf("input nesting too deep (depth limit of %d exceeded)", maxParseDepth),
					"split deeply nested expressions, parentheses or JSX into smaller named pieces")
				return
			}
			panic(r)
		}
	}()
	p.reportLexErrors()
	p.strict = p.directivePrologueIsStrict()
	p.pushScope(true)
	defer p.popScope()
	for p.kind() != lexer.EOF {
		pos := p.pos
		stmt := p.parseStmt(stmtTop)
		if stmt != nil {
			prog.Body = append(prog.Body, stmt)
		}
		if p.pos == pos {
			p.unexpected()
			p.next()
		}
	}
	p.reportPendingObjectErrors()
	return prog
}

// reportLexErrors surfaces the lexer's Error tokens as diagnostics.
func (p *Parser) reportLexErrors() {
	for _, t := range p.tokens {
		if t.Kind != lexer.Error {
			continue
		}
		msg := t.Err
		if msg == "" {
			msg = "unexpected character"
		}
		p.errorAt(t, msg, lexHint(msg))
	}
}

func lexHint(msg string) string {
	switch msg {
	case "unterminated string literal":
		return "close the string with its opening quote; use a template literal for multi-line text"
	case "unterminated template literal":
		return "close the template literal with a backtick"
	case "unterminated block comment":
		return "close the comment with */"
	case "unterminated regular expression literal":
		return "close the regular expression with /, or escape a / inside it as \\/"
	}
	return "remove or escape the character"
}

// Token helpers

func (p *Parser) tok() lexer.Token { return p.tokens[p.pos] }

func (p *Parser) kind() lexer.Kind { return p.tokens[p.pos].Kind }

// peek returns the current token.
func (p *Parser) peek() lexer.Token { return p.tokens[p.pos] }

// at returns the token n positions ahead (clamped to EOF).
func (p *Parser) at(n int) lexer.Token {
	i := p.pos + n
	if i >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[i]
}

func (p *Parser) peekN(n int) lexer.Kind { return p.at(n).Kind }

func (p *Parser) next() lexer.Token {
	t := p.tokens[p.pos]
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return t
}

func (p *Parser) match(kind lexer.Kind) bool {
	if p.kind() == kind {
		p.next()
		return true
	}
	return false
}

// expect consumes a token of the given kind or reports an error (without
// consuming anything).
func (p *Parser) expect(kind lexer.Kind) lexer.Token {
	if p.kind() == kind {
		return p.next()
	}
	p.errWithMsg(fmt.Sprintf("expected %s, got %s", quoteLabel(kind), describe(p.tok())), expectHint(kind))
	return lexer.Token{Kind: lexer.Error}
}

func quoteLabel(k lexer.Kind) string {
	switch k {
	case lexer.EOF, lexer.Identifier, lexer.String, lexer.Number:
		return kindLabel(k)
	}
	return "`" + kindLabel(k) + "`"
}

func expectHint(kind lexer.Kind) string {
	switch kind {
	case lexer.RPAREN:
		return "check for a missing `)` or an extra token inside the parentheses"
	case lexer.RBRACE:
		return "check for a missing `}` or an unterminated block"
	case lexer.RBRACKET:
		return "check for a missing `]`"
	case lexer.COLON:
		return "a conditional expression needs both branches: `test ? a : b`"
	}
	return ""
}

// isContextual reports whether the current token is the word w (contextual
// keywords such as `of`, `get`, `static` lex as identifiers or TS keywords).
func (p *Parser) isContextual(w string) bool {
	t := p.tok()
	return t.Value == w && isWordKind(t.Kind) && !t.Escaped
}

// consumeSemicolon implements automatic semicolon insertion: a `;`, a `}`,
// EOF or a preceding line break ends a statement.
func (p *Parser) consumeSemicolon() {
	if p.match(lexer.SEMI) {
		return
	}
	t := p.tok()
	if t.Kind == lexer.RBRACE || t.Kind == lexer.EOF || t.NewlineBefore {
		return
	}
	p.errWithMsg(fmt.Sprintf("expected `;` but found %s", describe(t)),
		"separate statements with `;` or a line break")
}

// Diagnostics

func (p *Parser) errorAt(tok lexer.Token, msg, hint string) {
	if len(p.errs) >= maxErrors {
		return
	}
	if p.errAt[tok.Offset] {
		return
	}
	p.errAt[tok.Offset] = true
	line, col := tok.Line, tok.Col
	if line == 0 {
		line, col = 1, 1
	}
	d := diag.New(p.Filename, line, col, msg, hint, "")
	if line > 0 && line <= len(p.srcLines) {
		d.Source = p.srcLines[line-1]
	}
	p.errs = append(p.errs, d)
}

func (p *Parser) err(msg string) { p.errorAt(p.tok(), msg, "") }

func (p *Parser) errWithMsg(msg, hint string) { p.errorAt(p.tok(), msg, hint) }

func (p *Parser) errAtPos(pos ast.Pos, msg, hint string) {
	p.errorAt(lexer.Token{Offset: pos.Offset, Line: pos.Line, Col: pos.Col}, msg, hint)
}

func (p *Parser) unexpected() {
	t := p.tok()
	if t.Kind == lexer.Error {
		return // already reported by reportLexErrors
	}
	p.errWithMsg("unexpected "+describe(t), "")
}

// describe renders a token for an error message.
func describe(t lexer.Token) string {
	switch t.Kind {
	case lexer.EOF:
		return "end of file"
	case lexer.Identifier:
		return "identifier `" + t.Value + "`"
	case lexer.String:
		return "string " + t.Value
	case lexer.Number:
		return "number " + t.Value
	case lexer.JSXText:
		return "JSX text"
	}
	if t.Value != "" {
		return "`" + t.Value + "`"
	}
	return kindLabel(t.Kind)
}

func tokPos(tok lexer.Token) ast.Pos {
	return ast.Pos{Line: tok.Line, Col: tok.Col, Offset: tok.Offset}
}

func posFromStmts(stmts []ast.Stmt) ast.Pos {
	if len(stmts) > 0 && stmts[0] != nil {
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
	case lexer.Regexp:
		return "regex"
	case lexer.TEMPLATE_START:
		return "`${"
	case tmplNoSub:
		return "template"
	}
	return lexer.KindName(k)
}

func FormatDiagnostics(errs []error) string {
	return diag.FormatDiagnostics(errs)
}

// tokenText reconstructs compact source text for tokens [from, to)
// (comments are not preserved): tokens are joined without spaces except where
// two word-like tokens would otherwise merge.
func (p *Parser) tokenText(from, to int) string {
	var b []byte
	for i := from; i < to && i < len(p.tokens); i++ {
		t := p.tokens[i]
		if len(b) > 0 && t.Value != "" && isWordByte(b[len(b)-1]) && isWordByte(t.Value[0]) {
			b = append(b, ' ')
		}
		b = append(b, t.Value...)
	}
	return string(b)
}

func isWordByte(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}
