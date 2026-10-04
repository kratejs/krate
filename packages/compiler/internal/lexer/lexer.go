package lexer

import (
	"fmt"
	"unicode"
)

type Kind int

const (
	EOF Kind = iota
	Error
	Whitespace

	Identifier
	String
	Number

	Import
	Export
	Const_
	Let_
	Var_
	Function
	Return
	If_
	Else_
	True
	False
	Null_
	Undefined
	Async
	Default_
	From
	Of
	As
	In_

	LBRACE
	RBRACE
	LPAREN
	RPAREN
	LBRACKET
	RBRACKET
	COMMA
	SEMI
	DOT
	COLON
	ARROW
	QUEST

	ASSIGN
	EQ
	STRICT_EQ
	NEQ
	STRICT_NEQ
	PLUS
	MINUS
	STAR
	DIV
	MOD
	LT
	GT
	LTE
	GTE
	AND
	OR
	NOT
	INC
	DEC
	ADD_ASSIGN
	SUB_ASSIGN
	MUL_ASSIGN
	DIV_ASSIGN
	MOD_ASSIGN
	BIT_AND
	BIT_OR
	BIT_XOR
	BIT_NOT

	SHL
	SHR
	SHL_ASSIGN
	SHR_ASSIGN
	USHIFT_RIGHT

	Regexp

	AT

	For
	While_
	Do_
	Switch
	Case_
	Try_
	Catch_
	Finally_
	Throw_
	Break_
	Continue_

	Class_
	Super_
	Extends_
	Yield_
	Delete_
	Typeof_
	Instanceof_
	Void_
	Debugger_
	With_

	Interface_
	Type_
	Implements_
	Enum_
	Declare_
	Namespace_
	Public_
	Private_
	Protected_
	Readonly_
	Any_
	Unknown_
	Never_
	Boolean_
	String_
	Number_
	Symbol_

	New_
	This_
	Await_
	QUESTION_DOT
	NULLISH

	SLASH_GT
	LT_SLASH
	SPREAD

	// Apostrophe is a literal quote character that is NOT a string delimiter.
	// Used for apostrophes/quotes in JSX text (e.g. "doesn't") that would
	// otherwise be lexed as the start of an unterminated string.
	Apostrophe

	TEMPLATE_START
	TEMPLATE_MID
	TEMPLATE_END
)

var keywords = map[string]Kind{
	"import":     Import,
	"export":     Export,
	"const":      Const_,
	"let":        Let_,
	"var":        Var_,
	"function":   Function,
	"return":     Return,
	"if":         If_,
	"else":       Else_,
	"true":       True,
	"false":      False,
	"null":       Null_,
	"undefined":  Undefined,
	"async":      Async,
	"default":    Default_,
	"from":       From,
	"of":         Of,
	"as":         As,
	"in":         In_,
	"for":        For,
	"while":      While_,
	"do":         Do_,
	"switch":     Switch,
	"case":       Case_,
	"try":        Try_,
	"catch":      Catch_,
	"finally":    Finally_,
	"throw":      Throw_,
	"break":      Break_,
	"continue":   Continue_,
	"new":        New_,
	"this":       This_,
	"await":      Await_,
	"class":      Class_,
	"super":      Super_,
	"extends":    Extends_,
	"yield":      Yield_,
	"delete":     Delete_,
	"typeof":     Typeof_,
	"instanceof": Instanceof_,
	"void":       Void_,
	"debugger":   Debugger_,
	"with":       With_,
	"interface":  Interface_,
	"type":       Type_,
	"implements": Implements_,
	"enum":       Enum_,
	"declare":    Declare_,
	"namespace":  Namespace_,
	"public":     Public_,
	"private":    Private_,
	"protected":  Protected_,
	"readonly":   Readonly_,
	"any":        Any_,
	"unknown":    Unknown_,
	"never":      Never_,
	"boolean":    Boolean_,
	"string":     String_,
	"number":     Number_,
	"symbol":     Symbol_,
}

type Token struct {
	Kind  Kind
	Value string
	Line  int
	Col   int
	Err   string
}

type Lexer struct {
	src      []rune
	pos      int
	line     int
	col      int
	start    int
	tokens   []Token
	lastKind Kind
	// lastSignificant is the most recent non-whitespace token kind. It is used
	// to tell an expression-position `<` (which may open JSX) from a comparison
	// `<`, since whitespace before `<` would otherwise erase the context.
	lastSignificant Kind
	// tmplBraceStack tracks brace depth inside each open template
	// interpolation (`${ ... }`). One entry per open `${`. A value of 0
	// means we're at the top level of that interpolation, so the next
	// `}` closes it and we resume reading template-string text.
	tmplBraceStack []int
	// jsxStack tracks the lexer's JSX context. Inside JSX text, `/`, `/*`,
	// `//`, quotes and backticks are literal characters rather than division,
	// comments, string/regex/template delimiters — otherwise a JSX text child
	// like `<code>/api/*</code>` or `<code>/about</code>` would swallow the
	// rest of the line. An empty stack means plain JavaScript.
	jsxStack []jsxFrame
}

// jsxFrameKind identifies one lexical JSX context.
type jsxFrameKind int

const (
	// jsxTag is inside an opening/closing tag, between `<name` and its `>`/`/>`.
	jsxTag jsxFrameKind = iota
	// jsxChildren is an element's children region: literal text plus nested
	// elements and `{ ... }` expression containers.
	jsxChildren
	// jsxBrace is a `{ ... }` expression container inside JSX.
	jsxBrace
)

type jsxFrame struct {
	kind jsxFrameKind
	// closing is set for jsxTag frames opened by `</`.
	closing bool
	// brace counts nested `{` inside a jsxBrace frame.
	brace int
}

func New(src string) *Lexer {
	return &Lexer{
		src:  []rune(src),
		line: 1,
		col:  0,
	}
}

func (l *Lexer) Tokenize() []Token {
	for l.pos < len(l.src) {
		l.start = l.pos
		ch := l.next()

		if unicode.IsSpace(ch) || ch == '\ufeff' {
			l.emit(Whitespace)
			continue
		}

		switch ch {
		case '{':
			l.incTemplateBrace()
			// A `{` inside JSX opens an expression container; nested `{` inside
			// it (an object literal) only deepens that container.
			if l.inJSXChildren() || l.inJSXTag() || l.inJSXBrace() {
				l.openJSXBrace()
			}
			l.emit(LBRACE)
		case '}':
			// If this closes an open template interpolation (`${ ... }`),
			// emit RBRACE for the closing brace, then resume reading
			// template-string text.
			if l.closeTemplateBrace() {
				l.emit(RBRACE)
				l.readTemplate()
			} else {
				l.closeJSXBrace()
				l.emit(RBRACE)
			}
		case '(':
			l.emit(LPAREN)
		case ')':
			l.emit(RPAREN)
		case '[':
			l.emit(LBRACKET)
		case ']':
			l.emit(RBRACKET)
		case ',':
			l.emit(COMMA)
		case ';':
			l.emit(SEMI)
		case '?':
			if l.peek() == '.' {
				l.next()
				l.emit(QUESTION_DOT)
			} else if l.peek() == '?' {
				l.next()
				l.emit(NULLISH)
			} else {
				l.emit(QUEST)
			}
		case ':':
			l.emit(COLON)
		case '@':
			l.emit(AT)
		case '\'':
			// Quotes are literal characters in JSX text (e.g. "doesn't"), and a
			// quote directly after a value elsewhere (5" screen) is too.
			if l.inJSXChildren() || isValueEnd(l.lastKind) {
				l.emit(Apostrophe)
			} else {
				l.readString('\'')
			}
		case '"':
			if l.inJSXChildren() || isValueEnd(l.lastKind) {
				l.emit(Apostrophe)
			} else {
				l.readString('"')
			}
		case '`':
			// A backtick is literal text in JSX; otherwise it opens (or closes)
			// a template literal, and readTemplate consumes the closing backtick.
			if l.inJSXChildren() {
				l.emit(Apostrophe)
			} else {
				l.readTemplate()
			}
		case '/':
			if l.inJSXChildren() {
				// JSX text: `/`, `//` and `/*` are literal, never comments,
				// division or regex starts.
				l.emit(DIV)
			} else if l.peek() == '=' {
				l.next()
				l.emit(DIV_ASSIGN)
			} else if l.peek() == '>' {
				l.next()
				if l.inJSXTag() {
					l.closeJSXSelfClosing()
				}
				l.emit(SLASH_GT)
			} else if l.peek() == '/' {
				l.readLineComment()
			} else if l.peek() == '*' {
				l.readBlockComment()
			} else if l.regexContext() {
				l.readRegex()
			} else {
				l.emit(DIV)
			}
		case '.':
			if l.peek() == '.' && l.peekAt(1) == '.' {
				l.next()
				l.next()
				l.emit(SPREAD)
			} else if unicode.IsDigit(l.peek()) {
				// Leading-dot number literal (`.5`, `.25e3`). Consume the
				// fraction so it lexes as a single Number, not DOT + Number.
				l.readNumber()
			} else {
				l.emit(DOT)
			}
		case '<':
			if l.peek() == '/' && l.inJSXChildren() {
				// Closing tag `</name>` or fragment `</>`; only meaningful
				// inside an element's children region.
				l.next()
				l.openJSXTag(true)
				l.emit(LT_SLASH)
			} else if l.peek() == '=' {
				l.next()
				l.emit(LTE)
			} else if l.peek() == '<' {
				l.next()
				if l.peek() == '=' {
					l.next()
					l.emit(SHL_ASSIGN)
				} else {
					l.emit(SHL)
				}
			} else if l.atJSXTagStart() {
				l.openJSXTag(false)
				l.emit(LT)
			} else {
				l.emit(LT)
			}
		case '>':
			if l.peek() == '=' {
				l.next()
				l.emit(GTE)
			} else if l.peek() == '>' {
				l.next()
				if l.peek() == '>' {
					l.next()
					l.emit(USHIFT_RIGHT)
				} else if l.peek() == '=' {
					l.next()
					l.emit(SHR_ASSIGN)
				} else {
					l.emit(SHR)
				}
			} else {
				if l.inJSXTag() {
					l.closeJSXTag()
				}
				l.emit(GT)
			}
		case '=':
			if l.peek() == '=' {
				l.next()
				if l.peek() == '=' {
					l.next()
					l.emit(STRICT_EQ)
				} else {
					l.emit(EQ)
				}
			} else if l.peek() == '>' {
				l.next()
				l.emit(ARROW)
			} else {
				l.emit(ASSIGN)
			}
		case '!':
			if l.peek() == '=' {
				l.next()
				if l.peek() == '=' {
					l.next()
					l.emit(STRICT_NEQ)
				} else {
					l.emit(NEQ)
				}
			} else {
				l.emit(NOT)
			}
		case '+':
			if l.peek() == '+' {
				l.next()
				l.emit(INC)
			} else if l.peek() == '=' {
				l.next()
				l.emit(ADD_ASSIGN)
			} else {
				l.emit(PLUS)
			}
		case '-':
			if l.peek() == '-' {
				l.next()
				l.emit(DEC)
			} else if l.peek() == '=' {
				l.next()
				l.emit(SUB_ASSIGN)
			} else {
				l.emit(MINUS)
			}
		case '*':
			if l.peek() == '=' {
				l.next()
				l.emit(MUL_ASSIGN)
			} else {
				l.emit(STAR)
			}
		case '%':
			if l.peek() == '=' {
				l.next()
				l.emit(MOD_ASSIGN)
			} else {
				l.emit(MOD)
			}
		case '&':
			if l.peek() == '&' {
				l.next()
				l.emit(AND)
			} else {
				l.emit(BIT_AND)
			}
		case '|':
			if l.peek() == '|' {
				l.next()
				l.emit(OR)
			} else {
				l.emit(BIT_OR)
			}
		case '^':
			l.emit(BIT_XOR)
		case '~':
			l.emit(BIT_NOT)
		default:
			if unicode.IsLetter(ch) || ch == '_' || ch == '$' {
				l.readIdentifier()
			} else if unicode.IsDigit(ch) {
				l.readNumber()
			} else {
				l.emit(Error)
			}
		}
	}

	l.tokens = append(l.tokens, Token{Kind: EOF, Line: l.line, Col: l.col})
	return l.tokens
}

func (l *Lexer) next() rune {
	ch := l.src[l.pos]
	l.pos++
	l.col++
	if ch == '\n' {
		l.line++
		l.col = 0
	}
	return ch
}

func (l *Lexer) peek() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *Lexer) peekAt(offset int) rune {
	if l.pos+offset >= len(l.src) {
		return 0
	}
	return l.src[l.pos+offset]
}

func (l *Lexer) emit(kind Kind) {
	val := string(l.src[l.start:l.pos])
	l.tokens = append(l.tokens, Token{
		Kind:  kind,
		Value: val,
		Line:  l.line,
		Col:   l.col,
	})
	l.lastKind = kind
	if kind != Whitespace {
		l.lastSignificant = kind
	}
}

// ─── JSX context helpers ─────────────────────────────────────────────────────

func (l *Lexer) jsxTop() (jsxFrame, bool) {
	n := len(l.jsxStack)
	if n == 0 {
		return jsxFrame{}, false
	}
	return l.jsxStack[n-1], true
}

func (l *Lexer) inJSXChildren() bool {
	top, ok := l.jsxTop()
	return ok && top.kind == jsxChildren
}

func (l *Lexer) inJSXTag() bool {
	top, ok := l.jsxTop()
	return ok && top.kind == jsxTag
}

func (l *Lexer) inJSXBrace() bool {
	top, ok := l.jsxTop()
	return ok && top.kind == jsxBrace
}

// atJSXTagStart reports whether a `<` at the current point opens a JSX element
// or fragment. Inside an element's children region every `<` starts a tag
// (a literal `<` in JSX text is invalid and must be escaped). Otherwise `<`
// only starts JSX in an expression position — a value on the left (identifier,
// literal, `)`, `]`, `}`, template end, …) makes it a comparison instead.
//
// Angle-bracket type assertions (`<string>foo`) are also ruled out: the parser
// treats `<` followed by a primitive type keyword as a cast, never JSX, so the
// lexer must not open a tag frame for them (doing so would leak JSX-text state
// into the rest of the file).
func (l *Lexer) atJSXTagStart() bool {
	if !isTagNameStart(l.peek()) && l.peek() != '>' {
		return false
	}
	if l.inJSXChildren() {
		// Inside children every `<` starts a tag; `<string>` is a JSX element
		// there, not a cast (the parser's JSX child path has no assertion rule).
		return true
	}
	// Outside children, `<` only starts JSX in an expression position — and
	// never for a primitive type name, which the parser reads as a cast.
	if isPrimitiveTypeName(l.peekWord()) {
		return false
	}
	// A generic type-parameter list (`<T,>`, `<T extends X>`) is not a JSX tag.
	// Treating it as one would desync the JSX stack for the rest of the file.
	if l.looksLikeTypeParams() {
		return false
	}
	return !isValueEnd(l.lastSignificant)
}

// looksLikeTypeParams reports whether the `<` at l.pos-1 opens a generic
// type-parameter list rather than a JSX element. The signal is a `,` or
// `extends` after the first identifier and before the matching `>` — neither
// can appear in a JSX opening tag. In `.tsx` TypeScript only accepts these
// unambiguous forms (bare `<T>(...)` is JSX), so the heuristic is safe.
func (l *Lexer) looksLikeTypeParams() bool {
	i := l.pos
	// First type-parameter name (or an empty `<>`, which is a fragment).
	if i >= len(l.src) || !isTagNameStart(l.src[i]) {
		return false
	}
	for i < len(l.src) && (unicode.IsLetter(l.src[i]) || unicode.IsDigit(l.src[i]) || l.src[i] == '_' || l.src[i] == '$') {
		i++
	}
	for i < len(l.src) && (l.src[i] == ' ' || l.src[i] == '\t' || l.src[i] == '\n' || l.src[i] == '\r') {
		i++
	}
	if i >= len(l.src) {
		return false
	}
	if l.src[i] == ',' {
		return true
	}
	// `extends` keyword after the parameter name.
	const ext = "extends"
	if i+len(ext) <= len(l.src) && string(l.src[i:i+len(ext)]) == ext {
		after := i + len(ext)
		if after >= len(l.src) || !unicode.IsLetter(l.src[after]) && !unicode.IsDigit(l.src[after]) && l.src[after] != '_' && l.src[after] != '$' {
			return true
		}
	}
	return false
}

// isTagNameStart reports whether ch can begin a JSX tag name.
func isTagNameStart(ch rune) bool {
	return unicode.IsLetter(ch) || ch == '_' || ch == '$'
}

// isPrimitiveTypeName reports whether name is a TypeScript primitive type
// keyword — the names that turn `<name>expr` into a type assertion rather than
// a JSX element. Mirrors the parser's isPrimitiveTypeKeyword.
func isPrimitiveTypeName(name string) bool {
	switch name {
	case "string", "number", "boolean", "any", "unknown", "never", "symbol", "void":
		return true
	}
	return false
}

// peekWord returns the identifier-like word starting at the current position,
// without consuming it.
func (l *Lexer) peekWord() string {
	i := l.pos
	for i < len(l.src) {
		ch := l.src[i]
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '$' {
			i++
			continue
		}
		break
	}
	return string(l.src[l.pos:i])
}

// openJSXTag pushes a tag context for an opening (`closing=false`) or closing
// (`closing=true`) tag.
func (l *Lexer) openJSXTag(closing bool) {
	l.jsxStack = append(l.jsxStack, jsxFrame{kind: jsxTag, closing: closing})
}

// closeJSXTag handles the `>` that terminates a JSX tag. An opening tag turns
// into the element's children region; a closing tag is popped together with the
// children region it closes.
func (l *Lexer) closeJSXTag() {
	n := len(l.jsxStack)
	if n == 0 {
		return
	}
	top := l.jsxStack[n-1]
	if top.kind != jsxTag {
		return
	}
	if top.closing {
		l.jsxStack = l.jsxStack[:n-1]
		if m := len(l.jsxStack); m > 0 && l.jsxStack[m-1].kind == jsxChildren {
			l.jsxStack = l.jsxStack[:m-1]
		}
		return
	}
	l.jsxStack[n-1].kind = jsxChildren
}

// closeJSXSelfClosing pops a tag context after a `/>`.
func (l *Lexer) closeJSXSelfClosing() {
	if l.inJSXTag() {
		l.jsxStack = l.jsxStack[:len(l.jsxStack)-1]
	}
}

// openJSXBrace pushes a `{ ... }` expression-container context. A `{` inside an
// existing brace context (an object literal) only deepens that frame.
func (l *Lexer) openJSXBrace() {
	if l.inJSXBrace() {
		l.jsxStack[len(l.jsxStack)-1].brace++
		return
	}
	l.jsxStack = append(l.jsxStack, jsxFrame{kind: jsxBrace})
}

// closeJSXBrace handles a `}` while inside a JSX expression container. It
// returns true when the frame was popped (the container ended).
func (l *Lexer) closeJSXBrace() bool {
	if !l.inJSXBrace() {
		return false
	}
	n := len(l.jsxStack)
	if l.jsxStack[n-1].brace > 0 {
		l.jsxStack[n-1].brace--
		return false
	}
	l.jsxStack = l.jsxStack[:n-1]
	return true
}

func (l *Lexer) readString(quote rune) {
	for l.pos < len(l.src) {
		ch := l.next()
		if ch == '\\' && l.pos < len(l.src) {
			l.next()
			continue
		}
		if ch == quote {
			break
		}
	}
	l.emit(String)
}

// readTemplate reads the body of a template literal.
//
// It is called in three situations:
//  1. From the main loop when a backtick is encountered (opening a template).
//     At entry, l.start points at the character AFTER the opening backtick.
//  2. From the main loop when a `}` closes a `${ ... }` interpolation — in
//     which case we resume reading template-string text. l.start again points
//     just past the `}`.
//  3. (Not directly, but logically) the caller guarantees l.start is set so
//     the emitted token spans only the template body text.
//
// The emitted token kind depends on how the run terminates:
//   - `${` → TEMPLATE_START (the text before `${` becomes its value; the `${`
//     is consumed but NOT part of the value). The `${` opens an interpolation,
//     so we push a brace-depth entry for it.
//   - closing backtick → TEMPLATE_END (the text up to but not including the
//     backtick becomes its value; the backtick is consumed).
func (l *Lexer) readTemplate() {
	l.start = l.pos

	// If we are resuming from a '}' that closed an interpolation,
	// the next token is a MID or END.
	isMid := l.lastKind == RBRACE

	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch == '\\' && l.pos+1 < len(l.src) {
			l.next()
			l.next()
			continue
		}
		if ch == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{' {
			if isMid {
				l.emit(TEMPLATE_MID)
			} else {
				l.emit(TEMPLATE_START)
			}
			l.next() // consume $
			l.next() // consume {
			l.tmplBraceStack = append(l.tmplBraceStack, 0)
			// Emit LBRACE token for the `{` that opens the interpolation.
			l.start = l.pos - 1
			l.emit(LBRACE)
			return
		}
		if ch == '`' {
			l.emit(TEMPLATE_END)
			l.next()
			return
		}
		l.next()
	}
	l.emit(TEMPLATE_END)
}

// incTemplateBrace records a `{` seen inside an open template interpolation,
// so the matching `}` doesn't prematurely close the interpolation.
func (l *Lexer) incTemplateBrace() {
	n := len(l.tmplBraceStack)
	if n > 0 {
		l.tmplBraceStack[n-1]++
	}
}

// closeTemplateBrace is called when a `}` is encountered. If it closes the
// innermost template interpolation (brace depth 0), it pops that scope and
// returns true so the caller resumes template-string reading. Otherwise it
// decrements the depth and returns false (the `}` is a normal RBRACE).
//
// The `}` has already been consumed by the main loop's next() before this is
// called, so l.pos is just past the `}`.
func (l *Lexer) closeTemplateBrace() bool {
	n := len(l.tmplBraceStack)
	if n == 0 {
		return false
	}
	if l.tmplBraceStack[n-1] > 0 {
		l.tmplBraceStack[n-1]--
		return false
	}
	// depth == 0: this `}` ends the interpolation.
	l.tmplBraceStack = l.tmplBraceStack[:n-1]
	return true
}

func (l *Lexer) readLineComment() {
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
		l.col++
	}
}

func (l *Lexer) readBlockComment() {
	for l.pos+1 < len(l.src) {
		ch := l.next()
		if ch == '*' && l.peek() == '/' {
			l.next()
			break
		}
	}
}

func (l *Lexer) readIdentifier() {
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '$' {
			l.pos++
			l.col++
		} else {
			break
		}
	}
	val := string(l.src[l.start:l.pos])
	var kind Kind
	if k, ok := keywords[val]; ok {
		kind = k
	} else {
		kind = Identifier
	}
	l.tokens = append(l.tokens, Token{Kind: kind, Value: val, Line: l.line, Col: l.col})
	l.lastKind = kind
	l.lastSignificant = kind
}

func (l *Lexer) readNumber() {
	isFloat := false
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if unicode.IsDigit(ch) || ch == '_' {
			l.pos++
			l.col++
		} else if ch == '.' && !isFloat {
			if l.pos+1 < len(l.src) && (l.src[l.pos+1] == '.' || unicode.IsLetter(l.src[l.pos+1])) {
				break
			}
			isFloat = true
			l.pos++
			l.col++
		} else if ch == 'e' || ch == 'E' {
			l.pos++
			l.col++
			if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
				l.pos++
				l.col++
			}
		} else if ch == 'x' || ch == 'X' || ch == 'b' || ch == 'B' || ch == 'o' || ch == 'O' || ch == 'n' {
			l.pos++
			l.col++
		} else if (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			l.pos++
			l.col++
		} else {
			break
		}
	}
	l.emit(Number)
}

func (t Token) String() string {
	return fmt.Sprintf("%d:%d %s(%q)", t.Line, t.Col, kindName(t.Kind), t.Value)
}

var kindNames = map[Kind]string{
	EOF: "EOF", Error: "Error",
	Identifier: "Identifier", String: "String", Number: "Number",
	Import: "import", Export: "export", Const_: "const", Let_: "let", Var_: "var",
	Function: "function", Return: "return", If_: "if", Else_: "else",
	True: "true", False: "false", Null_: "null", Undefined: "undefined",
	Async: "async", Default_: "default", From: "from", Of: "of", As: "as", In_: "in",
	For: "for", While_: "while", Do_: "do", Switch: "switch", Case_: "case",
	Try_: "try", Catch_: "catch", Finally_: "finally",
	Throw_: "throw", Break_: "break", Continue_: "continue",
	New_: "new", This_: "this", Await_: "await",
	AT: "@", QUESTION_DOT: "?.", NULLISH: "??",
	LBRACE: "{", RBRACE: "}", LPAREN: "(", RPAREN: ")", LBRACKET: "[", RBRACKET: "]",
	COMMA: ",", SEMI: ";", DOT: ".", COLON: ":", ARROW: "=>", QUEST: "?",
	ASSIGN: "=", EQ: "==", STRICT_EQ: "===", NEQ: "!=", STRICT_NEQ: "!==",
	PLUS: "+", MINUS: "-", STAR: "*", DIV: "/", MOD: "%",
	LT: "<", GT: ">", LTE: "<=", GTE: ">=",
	AND: "&&", OR: "||", NOT: "!",
	INC: "++", DEC: "--",
	ADD_ASSIGN: "+=", SUB_ASSIGN: "-=", MUL_ASSIGN: "*=", DIV_ASSIGN: "/=", MOD_ASSIGN: "%=",
	SHL: "<<", SHR: ">>", SHL_ASSIGN: "<<=", SHR_ASSIGN: ">>=", USHIFT_RIGHT: ">>>",
	SLASH_GT: "/>", LT_SLASH: "</",
	Apostrophe: "'",
	Class_:     "class", Super_: "super", Extends_: "extends",
	Yield_: "yield", Delete_: "delete", Typeof_: "typeof",
	Instanceof_: "instanceof", Void_: "void", Debugger_: "debugger", With_: "with",
	Interface_: "interface", Type_: "type", Implements_: "implements",
	Enum_: "enum", Declare_: "declare", Namespace_: "namespace",
	Public_: "public", Private_: "private", Protected_: "protected", Readonly_: "readonly",
	Any_: "any", Unknown_: "unknown", Never_: "never", Boolean_: "boolean",
	String_: "string", Number_: "number", Symbol_: "symbol",
	SPREAD:         "...",
	TEMPLATE_START: "`", TEMPLATE_END: "`",
	TEMPLATE_MID: "`",
	Regexp:       "Regexp",
}

func kindName(k Kind) string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return fmt.Sprintf("Token(%d)", int(k))
}

// isValueEnd reports whether the given previous-token kind is a position where
// a string literal cannot legally begin — i.e. the previous token ended a
// value/expression. In JSX text this matters for apostrophes in contractions
// ("doesn't") and quotes directly after values ("5\" screen"): they must be
// lexed as literal characters, not as the start of an (unterminated) string
// that would swallow the rest of the file.
func isValueEnd(k Kind) bool {
	switch k {
	case Identifier, Number, String, Regexp,
		RPAREN, RBRACKET, RBRACE,
		True, False, Null_, Undefined, This_,
		TEMPLATE_END, INC, DEC, Apostrophe:
		return true
	}
	return false
}

func IsKeyword(s string) bool {
	_, ok := keywords[s]
	return ok
}

func (l *Lexer) regexContext() bool {
	// LT is excluded to avoid confusing </tagname> in JSX with a regex; JSX text
	// is handled by the JSX state machine before this is reached. Whitespace is
	// skipped via lastSignificant so `return /re/` still sees a regex context.
	switch l.lastSignificant {
	case 0, Error, LBRACE, LPAREN, LBRACKET, COMMA, SEMI, COLON, ARROW, QUEST,
		ASSIGN, ADD_ASSIGN, SUB_ASSIGN, MUL_ASSIGN, DIV_ASSIGN, MOD_ASSIGN,
		PLUS, MINUS, STAR, DIV, MOD, NOT, INC, DEC, AND, OR, GT,
		Return, Throw_, Case_, New_, Await_, Of, In_,
		Typeof_, Void_, Delete_, Instanceof_, Yield_:
		return true
	default:
		return false
	}
}

func (l *Lexer) readRegex() {
	for l.pos < len(l.src) {
		ch := l.next()
		if ch == '\\' && l.pos < len(l.src) {
			l.next()
			continue
		}
		if ch == '/' {
			// Consume regex flags (g, i, m, s, u, y, d)
			for l.pos < len(l.src) {
				f := l.src[l.pos]
				if f == 'g' || f == 'i' || f == 'm' || f == 's' || f == 'u' || f == 'y' || f == 'd' {
					l.pos++
					l.col++
				} else {
					break
				}
			}
			l.emit(Regexp)
			return
		}
		if ch == '\n' {
			// Unterminated regex — emit as Error to avoid hanging
			l.emit(Error)
			return
		}
	}
	l.emit(Error)
}
