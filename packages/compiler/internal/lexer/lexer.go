// Package lexer tokenizes TypeScript/TSX source for the Krate parser.
//
// Positions: every Token carries its start byte Offset and End byte offset
// (exclusive) into the source, plus a 1-based start Line/Col and end
// EndLine/EndCol. Columns count UTF-16 code units (what LSP clients, editors
// and the MCP tools expect), so an astral-plane character such as an emoji
// advances the column by 2. `\r\n`, `\n`, `\r`, U+2028 and U+2029 all
// terminate a line.
package lexer

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
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
	POW
	POW_ASSIGN
	AND_ASSIGN
	OR_ASSIGN
	NULLISH_ASSIGN

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

	// Apostrophe is a literal quote character that is NOT a string delimiter
	// (a quote directly after a value, e.g. `5" screen`).
	Apostrophe

	TEMPLATE_START
	TEMPLATE_MID
	TEMPLATE_END

	// Compound assignments appended to keep earlier Kind values stable.
	BIT_AND_ASSIGN // &=
	BIT_OR_ASSIGN  // |=
	BIT_XOR_ASSIGN // ^=
	USHR_ASSIGN    // >>>=

	// JSXText is a run of literal text inside a JSX element's children.
	JSXText
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

// Token is one lexeme.
//
// Value is the exact source text src[Offset:End], except for identifiers
// written with `\u` escapes, whose Value is the decoded name. Template
// literals are split into chunks that include their delimiters: "`a${",
// "}b${", "}c`" (or "`plain`" without substitutions); the interpolated
// expressions are lexed as ordinary tokens in between.
type Token struct {
	Kind  Kind
	Value string
	// Offset and End are the byte offsets [Offset, End) of the lexeme.
	Offset int
	End    int
	// Line and Col are the 1-based start position. Col counts UTF-16 units.
	Line int
	Col  int
	// NewlineBefore reports that a line terminator (in whitespace or inside a
	// comment) separates this token from the previous non-whitespace token.
	// The parser uses it for automatic semicolon insertion.
	NewlineBefore bool
	// JSX marks an LT token that the lexer decided opens a JSX element or
	// fragment (as opposed to a comparison or a type-parameter list).
	JSX bool
	// Escaped marks an identifier written with `\u` escapes. Such a token is
	// never a keyword, and using it where a keyword is required is an error.
	Escaped bool
	// BadEscape marks a template chunk containing an escape sequence that is
	// only permitted in tagged templates (`\01`, `\u{zz}`, `\xg`).
	BadEscape bool
	// Err carries a human-readable message for Error tokens.
	Err string
}

type Lexer struct {
	src    string
	pos    int
	start  int
	tokens []Token

	lastKind Kind
	// lastSignificant is the most recent non-whitespace token kind. It tells an
	// expression-position `<` (which may open JSX) or `/` (which may open a
	// regex) from a binary operator.
	lastSignificant Kind
	// prevSignificant is the significant token before lastSignificant.
	prevSignificant Kind

	// nlPending records a line terminator seen since the last significant token.
	nlPending bool

	// tmplBraceStack tracks brace depth inside each open template
	// interpolation (`${ ... }`). One entry per open `${`.
	tmplBraceStack []int
	// jsxStack tracks the lexer's JSX context. Inside JSX text, `/`, quotes
	// and backticks are literal characters rather than division, comments,
	// string/regex/template delimiters. An empty stack means plain JavaScript.
	jsxStack []jsxFrame
	// parenStack records, for each open `(`, whether it opened the head of an
	// if/while/for/with statement. After the matching `)` a `/` starts a regex.
	parenStack       []bool
	lastParenControl bool

	// Position tracker. Token offsets are visited in increasing order, so the
	// line/column of each offset is computed incrementally.
	tOff    int
	tLine   int
	tCol    int
	afterCR bool
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
	return &Lexer{src: src, tLine: 1, tCol: 1}
}

// SplitLines splits src into lines using the same line terminators as the
// lexer (`\r\n`, `\n`, `\r`, U+2028, U+2029), so lines[tok.Line-1] is the
// source line of a token. Terminators are not included.
func SplitLines(src string) []string {
	var lines []string
	begin := 0
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\n':
			lines = append(lines, src[begin:i])
			i++
			begin = i
		case c == '\r':
			lines = append(lines, src[begin:i])
			i++
			if i < len(src) && src[i] == '\n' {
				i++
			}
			begin = i
		case c == 0xE2 && i+2 < len(src) && src[i+1] == 0x80 && (src[i+2] == 0xA8 || src[i+2] == 0xA9):
			lines = append(lines, src[begin:i])
			i += 3
			begin = i
		default:
			i++
		}
	}
	return append(lines, src[begin:])
}

// UTF16Len returns the number of UTF-16 code units needed to encode s.
// Invalid UTF-8 bytes count as one unit each.
func UTF16Len(s string) int {
	n := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
		i += size
	}
	return n
}

// lineCol returns the 1-based line and UTF-16 column of byte offset off. Calls
// must use non-decreasing offsets.
func (l *Lexer) lineCol(off int) (int, int) {
	if off > len(l.src) {
		off = len(l.src)
	}
	i := l.tOff
	for i < off {
		c := l.src[i]
		if c < utf8.RuneSelf {
			switch c {
			case '\n':
				if !l.afterCR {
					l.tLine++
				}
				l.tCol = 1
				l.afterCR = false
			case '\r':
				l.tLine++
				l.tCol = 1
				l.afterCR = true
			default:
				l.tCol++
				l.afterCR = false
			}
			i++
			continue
		}
		l.afterCR = false
		r, size := utf8.DecodeRuneInString(l.src[i:])
		switch {
		case r == '\u2028' || r == '\u2029':
			l.tLine++
			l.tCol = 1
		case r >= 0x10000:
			l.tCol += 2
		default:
			l.tCol++
		}
		i += size
	}
	l.tOff = i
	return l.tLine, l.tCol
}

func (l *Lexer) Tokenize() []Token {
	if l.tokens == nil {
		l.tokens = make([]Token, 0, len(l.src)/5+8)
	}
	// A hashbang line (`#!/usr/bin/env node`) is only valid at offset 0.
	if strings.HasPrefix(l.src, "#!") {
		for l.pos < len(l.src) && !isLineTerminatorAt(l.src, l.pos) {
			l.pos++
		}
	}
	for l.pos < len(l.src) {
		l.start = l.pos
		if l.inJSXChildren() {
			l.lexJSXChildren()
			continue
		}
		ch, size := l.peekRune()
		if isWhitespaceRune(ch) {
			l.readWhitespace()
			continue
		}
		l.pos += size

		switch ch {
		case '{':
			l.incTemplateBrace()
			// A `{` inside JSX opens an expression container; nested `{` inside
			// it (an object literal) only deepens that container.
			if l.inJSXTag() || l.inJSXBrace() {
				l.openJSXBrace()
			}
			l.emit(LBRACE)
		case '}':
			// If this closes an open template interpolation (`${ ... }`),
			// emit RBRACE for the closing brace, then resume reading
			// template-string text.
			if l.closeTemplateBrace() {
				l.readTemplate(true)
			} else {
				l.closeJSXBrace()
				l.emit(RBRACE)
			}
		case '(':
			l.parenStack = append(l.parenStack, l.parenOpensControlHead())
			l.emit(LPAREN)
		case ')':
			l.lastParenControl = false
			if n := len(l.parenStack); n > 0 {
				l.lastParenControl = l.parenStack[n-1]
				l.parenStack = l.parenStack[:n-1]
			}
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
			switch {
			case l.peek() == '?' && l.peekAt(1) == '=':
				l.pos += 2
				l.emit(NULLISH_ASSIGN)
			case l.peek() == '?':
				l.pos++
				l.emit(NULLISH)
			case l.peek() == '.' && !isDecimalDigit(l.peekAt(1)):
				// `a?.5:1` is a conditional with `.5`, not optional chaining.
				l.pos++
				l.emit(QUESTION_DOT)
			default:
				l.emit(QUEST)
			}
		case ':':
			l.emit(COLON)
		case '@':
			l.emit(AT)
		case '#':
			// A class private name (`#count`) is an Identifier whose Value
			// includes the `#`.
			if isIDStartAt(l.src, l.pos) {
				if _, ok := l.readIdentifierRest(); !ok {
					l.skipBadEscape()
					l.emitError("invalid escape sequence in private name")
				} else {
					l.emit(Identifier)
				}
			} else {
				l.emitError("unexpected character `#`")
			}
		case '\'', '"':
			if l.inJSXTag() {
				l.readJSXString(byte(ch))
			} else if isValueEnd(l.lastKind) {
				// A quote directly after a value (5" screen) is a literal
				// character, not the start of a string.
				l.emit(Apostrophe)
			} else {
				l.readString(byte(ch))
			}
		case '`':
			l.readTemplate(false)
		case '/':
			switch {
			case l.peek() == '/':
				l.readLineComment()
			case l.peek() == '*':
				l.readBlockComment()
			case l.peek() == '>' && l.inJSXTag():
				l.pos++
				l.closeJSXSelfClosing()
				l.emit(SLASH_GT)
			case l.regexContext():
				l.readRegex()
			case l.peek() == '=':
				l.pos++
				l.emit(DIV_ASSIGN)
			default:
				l.emit(DIV)
			}
		case '.':
			if l.peek() == '.' && l.peekAt(1) == '.' {
				l.pos += 2
				l.emit(SPREAD)
			} else if isDecimalDigit(l.peek()) {
				// Leading-dot number literal (`.5`, `.25e3`).
				l.pos--
				l.readNumber()
			} else {
				l.emit(DOT)
			}
		case '<':
			if strings.HasPrefix(l.src[l.pos:], "!--") && !l.inJSXTag() {
				// Annex B HTML-like comment: `<!--` starts a line comment.
				l.readLineComment()
				continue
			}
			if l.peek() == '=' {
				l.pos++
				l.emit(LTE)
			} else if l.peek() == '<' {
				l.pos++
				if l.peek() == '=' {
					l.pos++
					l.emit(SHL_ASSIGN)
				} else {
					l.emit(SHL)
				}
			} else if l.atJSXTagStart() {
				l.openJSXTag(false)
				l.emitJSXOpen()
			} else {
				l.emit(LT)
			}
		case '>':
			if l.inJSXTag() {
				// Inside a tag `>` always ends it; never a shift operator.
				l.closeJSXTag()
				l.emit(GT)
			} else if l.peek() == '=' {
				l.pos++
				l.emit(GTE)
			} else if l.peek() == '>' {
				l.pos++
				if l.peek() == '>' {
					l.pos++
					if l.peek() == '=' {
						l.pos++
						l.emit(USHR_ASSIGN)
					} else {
						l.emit(USHIFT_RIGHT)
					}
				} else if l.peek() == '=' {
					l.pos++
					l.emit(SHR_ASSIGN)
				} else {
					l.emit(SHR)
				}
			} else {
				l.emit(GT)
			}
		case '=':
			if l.peek() == '=' {
				l.pos++
				if l.peek() == '=' {
					l.pos++
					l.emit(STRICT_EQ)
				} else {
					l.emit(EQ)
				}
			} else if l.peek() == '>' {
				l.pos++
				l.emit(ARROW)
			} else {
				l.emit(ASSIGN)
			}
		case '!':
			if l.peek() == '=' {
				l.pos++
				if l.peek() == '=' {
					l.pos++
					l.emit(STRICT_NEQ)
				} else {
					l.emit(NEQ)
				}
			} else {
				l.emit(NOT)
			}
		case '+':
			if l.peek() == '+' {
				l.pos++
				l.emit(INC)
			} else if l.peek() == '=' {
				l.pos++
				l.emit(ADD_ASSIGN)
			} else {
				l.emit(PLUS)
			}
		case '-':
			if l.peek() == '-' {
				l.pos++
				l.emit(DEC)
			} else if l.peek() == '=' {
				l.pos++
				l.emit(SUB_ASSIGN)
			} else {
				l.emit(MINUS)
			}
		case '*':
			if l.peek() == '*' {
				l.pos++
				if l.peek() == '=' {
					l.pos++
					l.emit(POW_ASSIGN)
				} else {
					l.emit(POW)
				}
			} else if l.peek() == '=' {
				l.pos++
				l.emit(MUL_ASSIGN)
			} else {
				l.emit(STAR)
			}
		case '%':
			if l.peek() == '=' {
				l.pos++
				l.emit(MOD_ASSIGN)
			} else {
				l.emit(MOD)
			}
		case '&':
			if l.peek() == '&' {
				l.pos++
				if l.peek() == '=' {
					l.pos++
					l.emit(AND_ASSIGN)
				} else {
					l.emit(AND)
				}
			} else if l.peek() == '=' {
				l.pos++
				l.emit(BIT_AND_ASSIGN)
			} else {
				l.emit(BIT_AND)
			}
		case '|':
			if l.peek() == '|' {
				l.pos++
				if l.peek() == '=' {
					l.pos++
					l.emit(OR_ASSIGN)
				} else {
					l.emit(OR)
				}
			} else if l.peek() == '=' {
				l.pos++
				l.emit(BIT_OR_ASSIGN)
			} else {
				l.emit(BIT_OR)
			}
		case '^':
			if l.peek() == '=' {
				l.pos++
				l.emit(BIT_XOR_ASSIGN)
			} else {
				l.emit(BIT_XOR)
			}
		case '~':
			l.emit(BIT_NOT)
		default:
			switch {
			case isIDStart(ch) || ch == '\\':
				l.pos = l.start
				l.readIdentifier()
			case isDecimalDigit(ch):
				l.pos = l.start
				l.readNumber()
			case ch == utf8.RuneError && size == 1:
				l.emitError("invalid UTF-8 byte in source")
			default:
				l.emitError(fmt.Sprintf("unexpected character %q", ch))
			}
		}
	}

	l.start = l.pos
	line, col := l.lineCol(l.pos)
	l.tokens = append(l.tokens, Token{
		Kind: EOF, Offset: l.pos, End: l.pos,
		Line: line, Col: col,
		NewlineBefore: l.nlPending,
	})
	return l.tokens
}

func (l *Lexer) peek() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	c := l.src[l.pos]
	if c < utf8.RuneSelf {
		return rune(c)
	}
	r, _ := utf8.DecodeRuneInString(l.src[l.pos:])
	return r
}

// peekAt returns the byte-sized character at pos+offset as a rune (0 past the
// end). It is only used for ASCII lookahead.
func (l *Lexer) peekAt(offset int) rune {
	if l.pos+offset >= len(l.src) {
		return 0
	}
	return rune(l.src[l.pos+offset])
}

func (l *Lexer) peekRune() (rune, int) {
	c := l.src[l.pos]
	if c < utf8.RuneSelf {
		return rune(c), 1
	}
	return utf8.DecodeRuneInString(l.src[l.pos:])
}

func (l *Lexer) emit(kind Kind) {
	l.emitValue(kind, l.src[l.start:l.pos])
}

func (l *Lexer) emitValue(kind Kind, val string) {
	l.lastKind = kind
	if kind == Whitespace {
		// Whitespace carries no information the parser needs beyond
		// NewlineBefore (recorded on the next token), so it is not emitted.
		return
	}
	line, col := l.lineCol(l.start)
	l.tokens = append(l.tokens, Token{
		Kind: kind, Value: val,
		Offset: l.start, End: l.pos,
		Line: line, Col: col,
		NewlineBefore: l.nlPending,
	})
	l.nlPending = false
	l.prevSignificant = l.lastSignificant
	l.lastSignificant = kind
}

func (l *Lexer) emitError(msg string) {
	l.emit(Error)
	l.tokens[len(l.tokens)-1].Err = msg
}

func (l *Lexer) emitJSXOpen() {
	l.emit(LT)
	l.tokens[len(l.tokens)-1].JSX = true
}

// parenOpensControlHead reports whether a `(` at this point opens the head of
// an if/while/for/with statement (including `for await (`).
func (l *Lexer) parenOpensControlHead() bool {
	switch l.lastSignificant {
	case If_, While_, For, With_:
		return true
	case Await_:
		return l.prevSignificant == For
	}
	return false
}

func (l *Lexer) readWhitespace() {
	for l.pos < len(l.src) {
		ch, size := l.peekRune()
		if !isWhitespaceRune(ch) {
			break
		}
		if isLineTerminator(ch) {
			l.nlPending = true
		}
		l.pos += size
	}
	l.emit(Whitespace)
}

// isWhitespaceRune reports JS WhiteSpace or LineTerminator characters.
func isWhitespaceRune(ch rune) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\v', '\f', '\u00a0', '\ufeff', '\u2028', '\u2029':
		return true
	}
	return ch > 0x7f && unicode.Is(unicode.Zs, ch)
}

func isLineTerminator(ch rune) bool {
	return ch == '\n' || ch == '\r' || ch == '\u2028' || ch == '\u2029'
}

func isLineTerminatorAt(s string, i int) bool {
	c := s[i]
	if c == '\n' || c == '\r' {
		return true
	}
	return c == 0xE2 && i+2 < len(s) && s[i+1] == 0x80 && (s[i+2] == 0xA8 || s[i+2] == 0xA9)
}

func isDecimalDigit(ch rune) bool { return ch >= '0' && ch <= '9' }

// isIDStart reports Unicode ID_Start plus `$` and `_`.
func isIDStart(ch rune) bool {
	if ch < utf8.RuneSelf {
		return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch == '$' || ch == '_'
	}
	return unicode.IsLetter(ch) || unicode.Is(unicode.Nl, ch) || unicode.Is(unicode.Other_ID_Start, ch)
}

// isIDContinue reports Unicode ID_Continue plus `$`, ZWNJ and ZWJ.
func isIDContinue(ch rune) bool {
	if ch < utf8.RuneSelf {
		return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '$' || ch == '_'
	}
	if ch == '\u200c' || ch == '\u200d' {
		return true
	}
	return isIDStart(ch) || unicode.In(ch, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc, unicode.Other_ID_Continue)
}

func isIDStartAt(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	if s[i] == '\\' {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return isIDStart(r)
}

// JSX context helpers

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

// lexJSXChildren lexes one token inside a JSX children region: a `{`
// expression container, a `<`/`</` tag start, or a run of literal text.
func (l *Lexer) lexJSXChildren() {
	switch l.src[l.pos] {
	case '{':
		l.pos++
		l.incTemplateBrace()
		l.openJSXBrace()
		l.emit(LBRACE)
		return
	case '<':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '/' {
			l.pos += 2
			l.openJSXTag(true)
			l.emit(LT_SLASH)
			return
		}
		l.pos++
		if l.atJSXTagStart() {
			l.openJSXTag(false)
			l.emitJSXOpen()
			return
		}
		// A stray `<` that starts no tag is literal text.
		l.pos--
	}
	// Literal text runs to the next `{` or tag start. JSX text is not
	// JavaScript: quotes, slashes and backslashes carry no special meaning.
	i := l.pos
	for i < len(l.src) {
		c := l.src[i]
		if c == '{' {
			break
		}
		if c == '<' && i > l.pos && i+1 < len(l.src) {
			n := l.src[i+1]
			if n == '/' || n == '>' || isIDStartAt(l.src, i+1) {
				break
			}
		}
		if c == '<' && i == l.pos {
			// Stray `<` handled above: consume it as text.
			i++
			continue
		}
		if c == '\n' || c == '\r' {
			l.nlPending = true
		}
		i++
	}
	l.pos = i
	// Text never carries NewlineBefore semantics for ASI.
	l.nlPending = false
	l.emit(JSXText)
}

// atJSXTagStart reports whether a `<` (already consumed) opens a JSX element
// or fragment. Inside an element's children region every `<` followed by a
// name or `>` starts a tag. Otherwise `<` only starts JSX in an expression
// position - a value on the left (identifier, literal, `)`, `]`, `}`, template
// end, ...) makes it a comparison instead. Angle-bracket type assertions on a
// primitive (`<string>foo`) and generic type-parameter lists (`<T,>`,
// `<T extends X>`, `<T>(x: T) => T`) are ruled out so the lexer does not open
// a tag frame and leak JSX-text state into the rest of the file.
func (l *Lexer) atJSXTagStart() bool {
	if l.inJSXChildren() {
		return isIDStartAt(l.src, l.pos) || l.peek() == '>'
	}
	if isValueEnd(l.lastSignificant) {
		// `let x\n<div />`: a tag on a new line after a complete value is JSX
		// when it is unmistakably a tag (`<name>`, `<name/>`, `<name attr=`).
		return l.nlPending && l.looksLikeTagOnNewLine()
	}
	// Whitespace and comments may separate `<` from the tag name
	// (`< div>`, `< >`).
	if j := skipSpacesAndComments(l.src, l.pos); j > l.pos && j < len(l.src) && (l.src[j] == '>' || isIDStartAt(l.src, j)) {
		save := l.pos
		l.pos = j
		ok := l.src[j] == '>' || (!isPrimitiveTypeName(l.peekWord()) && !l.looksLikeTypeParams())
		l.pos = save
		return ok
	}
	if !isIDStartAt(l.src, l.pos) && l.peek() != '>' {
		return false
	}
	if isPrimitiveTypeName(l.peekWord()) {
		return false
	}
	return !l.looksLikeTypeParams()
}

// looksLikeTagOnNewLine reports `<name>`, `<name/>` or `<name attr=` at
// l.pos (just after the `<`).
func (l *Lexer) looksLikeTagOnNewLine() bool {
	if !isIDStartAt(l.src, l.pos) {
		return false
	}
	i := l.pos + len(l.peekWord())
	j := skipSpaces(l.src, i)
	if j >= len(l.src) {
		return false
	}
	switch l.src[j] {
	case '>':
		return true
	case '/':
		return j+1 < len(l.src) && l.src[j+1] == '>'
	}
	if j > i && isIDStartAt(l.src, j) {
		k := j
		for k < len(l.src) {
			r, size := utf8.DecodeRuneInString(l.src[k:])
			if !isIDContinue(r) && r != '-' {
				break
			}
			k += size
		}
		k = skipSpaces(l.src, k)
		return k < len(l.src) && (l.src[k] == '=' || l.src[k] == '/' || l.src[k] == '>')
	}
	return false
}

// looksLikeTypeParams reports whether the `<` at l.pos-1 opens a generic
// type-parameter list rather than a JSX element. Signals:
//   - a `,`, `=` or `extends` after the first name (never valid in a JSX tag);
//   - `<Name>(...)` followed by `=>` (a generic arrow or function type) or
//     by `:` (a call signature `<T>(a: T): T`), when Name is capitalized.
//     Lowercase names are left as JSX so text like `<p>(Optional): name</p>`
//     keeps working.
func (l *Lexer) looksLikeTypeParams() bool {
	src := l.src
	i := l.pos
	if i >= len(src) || src[i] == '>' {
		return false
	}
	// `<const T>` const type parameters (TS 5.0).
	if strings.HasPrefix(src[i:], "const ") {
		return true
	}
	nameStart := i
	for i < len(src) {
		r, size := utf8.DecodeRuneInString(src[i:])
		if !isIDContinue(r) {
			break
		}
		i += size
	}
	name := src[nameStart:i]
	i = skipSpaces(src, i)
	if i >= len(src) {
		return false
	}
	switch src[i] {
	case ',':
		return true
	case '=':
		return i+1 >= len(src) || src[i+1] != '='
	case '>':
		j := skipSpaces(src, i+1)
		if j >= len(src) || src[j] != '(' {
			return false
		}
		end := matchParenBounded(src, j, 4096)
		if end < 0 {
			return false
		}
		k := skipSpaces(src, end+1)
		// Lowercase names are HTML elements, so `<p>(a) => b</p>` stays JSX.
		capital := name != "" && name[0] >= 'A' && name[0] <= 'Z'
		return capital && (strings.HasPrefix(src[k:], "=>") || k < len(src) && src[k] == ':')
	}
	const ext = "extends"
	if strings.HasPrefix(src[i:], ext) {
		after := i + len(ext)
		if after >= len(src) {
			return true
		}
		r, _ := utf8.DecodeRuneInString(src[after:])
		return !isIDContinue(r)
	}
	return false
}

// skipSpacesAndComments skips whitespace and complete comments.
func skipSpacesAndComments(s string, i int) int {
	for {
		i = skipSpaces(s, i)
		switch {
		case strings.HasPrefix(s[i:], "//"):
			for i < len(s) && s[i] != '\n' && s[i] != '\r' {
				i++
			}
		case strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return i
			}
			i += end + 4
		default:
			return i
		}
	}
}

func skipSpaces(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

// matchParenBounded returns the index of the `)` matching the `(` at s[open],
// or -1 if it is not found within limit bytes.
func matchParenBounded(s string, open, limit int) int {
	depth := 0
	end := open + limit
	if end > len(s) {
		end = len(s)
	}
	for i := open; i < end; i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// isPrimitiveTypeName reports whether name is a TypeScript primitive type
// keyword - the names that turn `<name>expr` into a type assertion rather than
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
		r, size := utf8.DecodeRuneInString(l.src[i:])
		if !isIDContinue(r) {
			break
		}
		i += size
	}
	return l.src[l.pos:i]
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

// readString reads a JS string literal whose opening quote is consumed. A raw
// line terminator (other than an escaped line continuation, or U+2028/U+2029)
// ends it with an error.
func (l *Lexer) readString(quote byte) {
	bad := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch c {
		case quote:
			l.pos++
			if bad {
				l.emitError("invalid escape sequence in string literal")
				return
			}
			l.emit(String)
			return
		case '\\':
			if l.pos+1 < len(l.src) && (l.src[l.pos+1] == 'u' || l.src[l.pos+1] == 'x') && !validTemplateEscape(l.src[l.pos:]) {
				bad = true
			}
			l.pos++
			if l.pos < len(l.src) {
				if l.src[l.pos] == '\r' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '\n' {
					l.pos += 2
				} else {
					_, size := utf8.DecodeRuneInString(l.src[l.pos:])
					l.pos += size
				}
			}
			continue
		case '\n', '\r':
			l.emitError("unterminated string literal")
			return
		}
		l.pos++
	}
	l.emitError("unterminated string literal")
}

// readJSXString reads a JSX attribute string. JSX strings do not process
// backslash escapes and may span lines: they end at the matching quote only.
func (l *Lexer) readJSXString(quote byte) {
	end := strings.IndexByte(l.src[l.pos:], quote)
	if end < 0 {
		l.pos = len(l.src)
		l.emitError("unterminated JSX attribute string")
		return
	}
	l.pos += end + 1
	l.emit(String)
}

// readTemplate reads one chunk of a template literal. resume is false when the
// opening backtick was just consumed and true when the `}` closing a `${ ... }`
// interpolation was just consumed. The chunk ends at `${` (TEMPLATE_START, or
// TEMPLATE_MID when resuming) or at the closing backtick (TEMPLATE_END). Every
// chunk's Value is its exact lexeme including delimiters: "`a${", "}b${",
// "}c`" or "`plain`".
func (l *Lexer) readTemplate(resume bool) {
	l.start = l.pos - 1 // the backtick or the closing brace
	badEscape := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' && l.pos+1 < len(l.src) {
			if !validTemplateEscape(l.src[l.pos:]) {
				badEscape = true
			}
			l.pos += 2
			continue
		}
		if c == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{' {
			l.pos += 2
			defer l.markBadEscape(badEscape)
			if resume {
				l.emit(TEMPLATE_MID)
			} else {
				l.emit(TEMPLATE_START)
			}
			l.tmplBraceStack = append(l.tmplBraceStack, 0)
			return
		}
		if c == '`' {
			l.pos++
			l.emit(TEMPLATE_END)
			l.markBadEscape(badEscape)
			return
		}
		l.pos++
	}
	l.emitError("unterminated template literal")
}

func (l *Lexer) markBadEscape(bad bool) {
	if bad {
		for i := len(l.tokens) - 1; i >= 0; i-- {
			k := l.tokens[i].Kind
			if k == TEMPLATE_START || k == TEMPLATE_MID || k == TEMPLATE_END {
				l.tokens[i].BadEscape = true
				return
			}
		}
	}
}

// validTemplateEscape reports whether the escape at the start of s (which
// begins with a backslash) is valid in an untagged template literal.
func validTemplateEscape(s string) bool {
	if len(s) < 2 {
		return true
	}
	switch c := s[1]; {
	case c == '0':
		return len(s) < 3 || s[2] < '0' || s[2] > '9'
	case c >= '1' && c <= '9':
		return false
	case c == 'x':
		_, ok := parseHex(safeSlice(s, 2, 4))
		return ok && len(s) >= 4
	case c == 'u':
		_, n := decodeUnicodeEscape(s)
		return n > 0
	}
	return true
}

func safeSlice(s string, from, to int) string {
	if to > len(s) {
		to = len(s)
	}
	if from > to {
		return ""
	}
	return s[from:to]
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
// returns true so the caller resumes template-string reading.
func (l *Lexer) closeTemplateBrace() bool {
	n := len(l.tmplBraceStack)
	if n == 0 {
		return false
	}
	if l.tmplBraceStack[n-1] > 0 {
		l.tmplBraceStack[n-1]--
		return false
	}
	l.tmplBraceStack = l.tmplBraceStack[:n-1]
	return true
}

func (l *Lexer) readLineComment() {
	for l.pos < len(l.src) && !isLineTerminatorAt(l.src, l.pos) {
		l.pos++
	}
}

// readBlockComment skips a block comment whose `/` is consumed and whose `*` is
// at l.pos. `/*/` is not a complete comment: the closing `*/` must follow the
// opening `/*`.
func (l *Lexer) readBlockComment() {
	body := l.pos + 1
	end := strings.Index(l.src[body:], "*/")
	if end < 0 {
		l.pos = len(l.src)
		l.emitError("unterminated block comment")
		return
	}
	text := l.src[body : body+end]
	if strings.ContainsAny(text, "\n\r\u2028\u2029") {
		l.nlPending = true
	}
	l.pos = body + end + 2
}

// readIdentifier reads an identifier or keyword starting at l.pos. Unicode
// ID_Start/ID_Continue characters and `\uXXXX`/`\u{X}` escapes are accepted;
// an identifier containing escapes is never a keyword and its Value is the
// decoded name. Inside a JSX tag, `-` is also an identifier character.
func (l *Lexer) readIdentifier() {
	escaped, ok := l.readIdentifierRest()
	if !ok {
		l.skipBadEscape()
		l.emitError("invalid escape sequence in identifier")
		return
	}
	if escaped != "" {
		l.emitValue(Identifier, escaped)
		l.tokens[len(l.tokens)-1].Escaped = true
		return
	}
	val := l.src[l.start:l.pos]
	kind := Identifier
	if k, ok := keywords[val]; ok {
		kind = k
	}
	l.emit(kind)
}

// skipBadEscape consumes a malformed `\` escape so lexing always advances.
func (l *Lexer) skipBadEscape() {
	if l.pos < len(l.src) && l.src[l.pos] == '\\' {
		l.pos++
		if l.pos < len(l.src) {
			_, size := utf8.DecodeRuneInString(l.src[l.pos:])
			l.pos += size
		}
	}
	if l.pos == l.start {
		l.pos++
	}
}

// readIdentifierRest consumes identifier characters from l.pos. It returns the
// decoded name when escapes were used (otherwise ""), and false on a malformed
// escape.
func (l *Lexer) readIdentifierRest() (string, bool) {
	jsxName := l.inJSXTag()
	var decoded *strings.Builder
	segStart := l.pos
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' && jsxName {
			return "", false
		}
		if c == '\\' {
			r, n := decodeUnicodeEscape(l.src[l.pos:])
			if n == 0 || !(isIDContinue(r) || r == '\u200c' || r == '\u200d') {
				return "", false
			}
			if decoded == nil {
				decoded = &strings.Builder{}
			}
			decoded.WriteString(l.src[segStart:l.pos])
			decoded.WriteRune(r)
			l.pos += n
			segStart = l.pos
			continue
		}
		if c == '-' && jsxName {
			l.pos++
			continue
		}
		r, size := rune(c), 1
		if c >= utf8.RuneSelf {
			r, size = utf8.DecodeRuneInString(l.src[l.pos:])
		}
		if !isIDContinue(r) {
			break
		}
		l.pos += size
	}
	if decoded == nil {
		return "", true
	}
	decoded.WriteString(l.src[segStart:l.pos])
	return decoded.String(), true
}

// decodeUnicodeEscape decodes `\uXXXX` or `\u{X...}` at the start of s. It
// returns the rune and the number of bytes consumed (0 when malformed).
func decodeUnicodeEscape(s string) (rune, int) {
	if len(s) < 3 || s[0] != '\\' || s[1] != 'u' {
		return 0, 0
	}
	if s[2] == '{' {
		end := strings.IndexByte(s, '}')
		if end < 4 {
			return 0, 0
		}
		v, ok := parseHex(s[3:end])
		if !ok || v > unicode.MaxRune {
			return 0, 0
		}
		return rune(v), end + 1
	}
	if len(s) < 6 {
		return 0, 0
	}
	v, ok := parseHex(s[2:6])
	if !ok {
		return 0, 0
	}
	return rune(v), 6
}

func parseHex(s string) (int, bool) {
	if s == "" || len(s) > 8 {
		return 0, false
	}
	v := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return 0, false
		}
		v = v*16 + d
	}
	return v, true
}

// readNumber reads a numeric literal starting at l.pos: decimal (with optional
// fraction and exponent), `0x`/`0o`/`0b` integers, numeric separators and the
// BigInt `n` suffix. `0xE+1` lexes as `0xE`, `+`, `1`.
func (l *Lexer) readNumber() {
	src := l.src
	i := l.pos
	isDigit := func(c byte, base int) bool {
		switch base {
		case 16:
			return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
		case 8:
			return c >= '0' && c <= '7'
		case 2:
			return c == '0' || c == '1'
		}
		return c >= '0' && c <= '9'
	}
	// A numeric separator must sit between two digits.
	digits := func(base int) {
		start := i
		for i < len(src) && (isDigit(src[i], base) || src[i] == '_' && i > start && i+1 < len(src) && isDigit(src[i+1], base)) {
			i++
		}
	}
	if src[i] == '0' && i+1 < len(src) && strings.IndexByte("xXoObB", src[i+1]) >= 0 {
		base := 16
		switch src[i+1] {
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		i += 2
		digitStart := i
		digits(base)
		if i == digitStart {
			l.pos = i
			l.emitError("missing digits after numeric prefix")
			return
		}
		if i < len(src) && src[i] == 'n' {
			i++
		}
	} else if src[i] == '0' && i+1 < len(src) && (src[i+1] >= '0' && src[i+1] <= '9' || src[i+1] == '_') {
		// Legacy octal (`017`) or decimal with a leading zero (`089`): no
		// separators, fractions on octals or BigInt suffix are allowed.
		i++
		for i < len(src) && src[i] >= '0' && src[i] <= '9' {
			i++
		}
		if i < len(src) && (src[i] == '_' || src[i] == 'n') {
			for i < len(src) && (src[i] == '_' || src[i] == 'n' || src[i] >= '0' && src[i] <= '9') {
				i++
			}
			l.pos = i
			l.emitError("numeric separators and BigInt suffixes are not allowed after a leading zero")
			return
		}
		octal := !strings.ContainsAny(src[l.pos:i], "89")
		if !octal {
			if i < len(src) && src[i] == '.' {
				i++
				digits(10)
			}
			if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
				j := i + 1
				if j < len(src) && (src[j] == '+' || src[j] == '-') {
					j++
				}
				if j < len(src) && isDigit(src[j], 10) {
					i = j
					digits(10)
				}
			}
		}
	} else {
		digits(10)
		isInt := true
		if i < len(src) && src[i] == '.' {
			isInt = false
			i++
			digits(10)
		}
		if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
			j := i + 1
			if j < len(src) && (src[j] == '+' || src[j] == '-') {
				j++
			}
			if j < len(src) && isDigit(src[j], 10) {
				isInt = false
				i = j
				digits(10)
			}
		}
		if isInt && i < len(src) && src[i] == 'n' {
			i++
		}
	}
	l.pos = i
	if i < len(src) && isIDStartAt(src, i) && src[i] != '\\' {
		// `3in`, `1px`: an identifier may not directly follow a number.
		l.readIdentifierRest()
		l.emitError("identifier starts immediately after numeric literal")
		return
	}
	l.emit(Number)
}

func (t Token) String() string {
	return fmt.Sprintf("%d:%d %s(%q)", t.Line, t.Col, kindName(t.Kind), t.Value)
}

var kindNames = map[Kind]string{
	EOF: "EOF", Error: "Error", Whitespace: "Whitespace",
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
	POW:          "**", POW_ASSIGN: "**=",
	AND_ASSIGN: "&&=", OR_ASSIGN: "||=", NULLISH_ASSIGN: "??=",
	BIT_AND_ASSIGN: "&=", BIT_OR_ASSIGN: "|=", BIT_XOR_ASSIGN: "^=",
	USHR_ASSIGN: ">>>=",
	JSXText:     "JSXText",
}

func kindName(k Kind) string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return fmt.Sprintf("Token(%d)", int(k))
}

// KindName returns a short human-readable label for a token kind (the
// operator or keyword spelling where one exists).
func KindName(k Kind) string { return kindName(k) }

// isValueEnd reports whether the given previous-token kind ended a value or
// expression, so a following `<` is a comparison, `/` is division and a quote
// is a literal character rather than a string start.
func isValueEnd(k Kind) bool {
	switch k {
	case Identifier, Number, String, Regexp,
		RPAREN, RBRACKET, RBRACE,
		True, False, Null_, Undefined, This_, Super_,
		TEMPLATE_END, INC, DEC, Apostrophe:
		return true
	}
	// Contextual keywords that are usually identifiers in value position.
	switch k {
	case As, From, Of, Async, Type_, Any_, Unknown_, Never_, Boolean_, String_,
		Number_, Symbol_, Interface_, Namespace_, Declare_, Readonly_,
		Public_, Private_, Protected_, Implements_:
		return true
	}
	return false
}

func IsKeyword(s string) bool {
	_, ok := keywords[s]
	return ok
}

// regexContext reports whether a `/` at this point begins a regular
// expression literal rather than a division operator.
func (l *Lexer) regexContext() bool {
	switch l.lastSignificant {
	case RPAREN:
		// `if (x) /re/.test(y)`: the `)` closed a control-statement head.
		return l.lastParenControl
	case 0, Error, LBRACE, LPAREN, LBRACKET, COMMA, SEMI, COLON, ARROW, QUEST,
		ASSIGN, ADD_ASSIGN, SUB_ASSIGN, MUL_ASSIGN, DIV_ASSIGN, MOD_ASSIGN,
		SHL_ASSIGN, SHR_ASSIGN, USHR_ASSIGN, POW_ASSIGN,
		AND_ASSIGN, OR_ASSIGN, NULLISH_ASSIGN, BIT_AND_ASSIGN, BIT_OR_ASSIGN, BIT_XOR_ASSIGN,
		PLUS, MINUS, STAR, POW, DIV, MOD, NOT, BIT_NOT, AND, OR, NULLISH, GT, LT,
		EQ, STRICT_EQ, NEQ, STRICT_NEQ, LTE, GTE, BIT_AND, BIT_OR, BIT_XOR,
		SHL, SHR, USHIFT_RIGHT, SPREAD, TEMPLATE_START, TEMPLATE_MID,
		Return, Throw_, Case_, New_, Await_, In_, Else_, Do_, Default_, Export,
		Typeof_, Void_, Delete_, Instanceof_, Yield_, Extends_:
		return true
	}
	return false
}

// readRegex reads a regular expression literal whose opening `/` is consumed.
// A `/` inside a character class (`[/]`) does not end the literal, escapes are
// honored, and the flags `dgimsuyv` are accepted.
func (l *Lexer) readRegex() {
	inClass := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if isLineTerminatorAt(l.src, l.pos) {
			l.emitError("unterminated regular expression literal")
			return
		}
		switch {
		case c == '\\':
			l.pos++
			if l.pos < len(l.src) && !isLineTerminatorAt(l.src, l.pos) {
				_, size := utf8.DecodeRuneInString(l.src[l.pos:])
				l.pos += size
			}
			continue
		case inClass:
			if c == ']' {
				inClass = false
			}
		case c == '[':
			inClass = true
		case c == '/':
			l.pos++
			flagStart := l.pos
			for l.pos < len(l.src) {
				r, size := utf8.DecodeRuneInString(l.src[l.pos:])
				if !isIDContinue(r) {
					break
				}
				l.pos += size
			}
			flags := l.src[flagStart:l.pos]
			for i := 0; i < len(flags); i++ {
				if strings.IndexByte("dgimsuyv", flags[i]) < 0 || strings.IndexByte(flags[i+1:], flags[i]) >= 0 {
					l.emitError(fmt.Sprintf("invalid regular expression flags %q", flags))
					return
				}
			}
			l.emit(Regexp)
			return
		}
		l.pos++
	}
	l.emitError("unterminated regular expression literal")
}
