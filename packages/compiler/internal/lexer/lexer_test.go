package lexer

import (
	"testing"
)

func tokens(src string) []Token {
	l := New(src)
	all := l.Tokenize()
	var out []Token
	for _, t := range all {
		if t.Kind != Whitespace {
			out = append(out, t)
		}
	}
	return out
}

func kind(t *testing.T, tok Token, expected Kind) {
	t.Helper()
	if tok.Kind != expected {
		t.Errorf("expected kind %v, got %v (value=%q, line=%d, col=%d)", expected, tok.Kind, tok.Value, tok.Line, tok.Col)
	}
}

func value(t *testing.T, tok Token, expected string) {
	t.Helper()
	if tok.Value != expected {
		t.Errorf("expected value %q, got %q", expected, tok.Value)
	}
}

func TestBasicTokens(t *testing.T) {
	toks := tokens("42")
	if len(toks) < 2 {
		t.Fatal("expected at least 2 tokens (number + EOF)")
	}
	kind(t, toks[0], Number)
	value(t, toks[0], "42")
	kind(t, toks[len(toks)-1], EOF)
}

func TestIdentifier(t *testing.T) {
	toks := tokens("hello")
	kind(t, toks[0], Identifier)
	value(t, toks[0], "hello")
}

func TestMultipleIdentifiers(t *testing.T) {
	toks := tokens("hello world")
	kind(t, toks[0], Identifier)
	value(t, toks[0], "hello")
	kind(t, toks[1], Identifier)
	value(t, toks[1], "world")
}

func TestKeywords(t *testing.T) {
	tests := []struct {
		input string
		kind  Kind
	}{
		{"import", Import},
		{"export", Export},
		{"const", Const_},
		{"let", Let_},
		{"var", Var_},
		{"function", Function},
		{"return", Return},
		{"if", If_},
		{"else", Else_},
		{"true", True},
		{"false", False},
		{"null", Null_},
		{"undefined", Undefined},
		{"async", Async},
		{"for", For},
		{"while", While_},
		{"do", Do_},
		{"switch", Switch},
		{"case", Case_},
		{"try", Try_},
		{"catch", Catch_},
		{"throw", Throw_},
		{"break", Break_},
		{"continue", Continue_},
		{"new", New_},
		{"this", This_},
		{"await", Await_},
		{"from", From},
		{"as", As},
		{"default", Default_},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			toks := tokens(tt.input)
			kind(t, toks[0], tt.kind)
			value(t, toks[0], tt.input)
		})
	}
}

func TestOperators(t *testing.T) {
	tests := []struct {
		input string
		kind  Kind
	}{
		{"+", PLUS},
		{"-", MINUS},
		{"*", STAR},
		{"%", MOD},
		{"=", ASSIGN},
		{"==", EQ},
		{"===", STRICT_EQ},
		{"!=", NEQ},
		{"!==", STRICT_NEQ},
		{"<", LT},
		{">", GT},
		{"<=", LTE},
		{">=", GTE},
		{"&&", AND},
		{"||", OR},
		{"!", NOT},
		{"++", INC},
		{"--", DEC},
		{"+=", ADD_ASSIGN},
		{"-=", SUB_ASSIGN},
		{"*=", MUL_ASSIGN},
		{"/=", DIV_ASSIGN},
		{"%=", MOD_ASSIGN},
		{"?.", QUESTION_DOT},
		{"??", NULLISH},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// Use "a" before each operator so lastKind is Identifier (not in regex context)
			src := "a" + tt.input
			toks := tokens(src)
			// toks[0] = Identifier "a", toks[1] = operator, toks[2] = EOF
			kind(t, toks[1], tt.kind)
			value(t, toks[1], tt.input)
		})
	}
}

func TestDivisionOperator(t *testing.T) {
	// After identifier, / should be DIV, not regex start
	toks := tokens("a/b")
	// a / b
	kind(t, toks[0], Identifier)
	value(t, toks[0], "a")
	kind(t, toks[1], DIV)
	value(t, toks[1], "/")
	kind(t, toks[2], Identifier)
	value(t, toks[2], "b")
}

func TestPunctuation(t *testing.T) {
	tests := []struct {
		input string
		kind  Kind
	}{
		{"{", LBRACE},
		{"}", RBRACE},
		{"(", LPAREN},
		{")", RPAREN},
		{"[", LBRACKET},
		{"]", RBRACKET},
		{",", COMMA},
		{";", SEMI},
		{".", DOT},
		{":", COLON},
		{"=>", ARROW},
		{"?", QUEST},
		{"...", SPREAD},
		{"@", AT},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			toks := tokens(tt.input)
			kind(t, toks[0], tt.kind)
			value(t, toks[0], tt.input)
		})
	}
}

func TestStringLiteral(t *testing.T) {
	toks := tokens(`"hello"`)
	kind(t, toks[0], String)
}

func TestSingleQuoteString(t *testing.T) {
	toks := tokens(`'world'`)
	kind(t, toks[0], String)
}

func TestTemplateLiteral(t *testing.T) {
	toks := tokens("`hello ${name} world`")
	// TEMPLATE_START (text before ${), then LBRACE, then Identifier "name", then RBRACE, then the closing backtick
	kind(t, toks[0], TEMPLATE_START)
	kind(t, toks[1], LBRACE)
	kind(t, toks[2], Identifier)
	value(t, toks[2], "name")
	kind(t, toks[3], RBRACE)
}

func TestRegexLiteral(t *testing.T) {
	// After operator like assignment, / starts a regex
	toks := tokens("re=/test/i")
	kind(t, toks[0], Identifier)
	value(t, toks[0], "re")
	kind(t, toks[1], ASSIGN)
	kind(t, toks[2], Regexp)
	value(t, toks[2], "/test/i")
}

func TestLineComment(t *testing.T) {
	toks := tokens("// comment\n42")
	kind(t, toks[0], Number)
	value(t, toks[0], "42")
}

func TestBlockComment(t *testing.T) {
	toks := tokens("/* comment */42")
	kind(t, toks[0], Number)
	value(t, toks[0], "42")
}

func TestJSXSelfClosing(t *testing.T) {
	toks := tokens("<div/>")
	kind(t, toks[0], LT)
	kind(t, toks[1], Identifier)
	value(t, toks[1], "div")
	kind(t, toks[2], SLASH_GT)
}

func TestJSXWithChildren(t *testing.T) {
	toks := tokens("<div>hello</div>")
	kind(t, toks[0], LT)
	kind(t, toks[1], Identifier)
	value(t, toks[1], "div")
	kind(t, toks[2], GT)
	kind(t, toks[3], Identifier)
	value(t, toks[3], "hello")
	kind(t, toks[4], LT_SLASH)
	kind(t, toks[5], Identifier)
	value(t, toks[5], "div")
	kind(t, toks[6], GT)
}

func TestArrowFunction(t *testing.T) {
	toks := tokens("()=>42")
	kind(t, toks[0], LPAREN)
	kind(t, toks[1], RPAREN)
	kind(t, toks[2], ARROW)
	kind(t, toks[3], Number)
	value(t, toks[3], "42")
}

func TestArrowFunctionObjectReturn(t *testing.T) {
	toks := tokens("()=>({key:42})")
	kind(t, toks[0], LPAREN)
	kind(t, toks[1], RPAREN)
	kind(t, toks[2], ARROW)
	kind(t, toks[3], LPAREN)
	kind(t, toks[4], LBRACE)
	kind(t, toks[5], Identifier)
	value(t, toks[5], "key")
	kind(t, toks[6], COLON)
	kind(t, toks[7], Number)
	value(t, toks[7], "42")
	kind(t, toks[8], RBRACE)
	kind(t, toks[9], RPAREN)
}

func TestOptionalChaining(t *testing.T) {
	toks := tokens("a?.b")
	kind(t, toks[0], Identifier)
	value(t, toks[0], "a")
	kind(t, toks[1], QUESTION_DOT)
	kind(t, toks[2], Identifier)
	value(t, toks[2], "b")
}

func TestNullishCoalescing(t *testing.T) {
	toks := tokens("a??b")
	kind(t, toks[0], Identifier)
	value(t, toks[0], "a")
	kind(t, toks[1], NULLISH)
	kind(t, toks[2], Identifier)
	value(t, toks[2], "b")
}

func TestNumberMultiple(t *testing.T) {
	toks := tokens("1 2 3")
	kind(t, toks[0], Number)
	value(t, toks[0], "1")
	kind(t, toks[1], Number)
	value(t, toks[1], "2")
	kind(t, toks[2], Number)
	value(t, toks[2], "3")
}
