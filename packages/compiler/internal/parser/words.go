package parser

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// wordKinds are token kinds spelled as words: identifiers plus every keyword.
// Any of them is a valid IdentifierName (after `.`, as an object key, class
// member name or import/export specifier name).
var wordKinds = map[lexer.Kind]bool{
	lexer.Identifier: true,
	lexer.Import:     true, lexer.Export: true, lexer.Const_: true, lexer.Let_: true, lexer.Var_: true,
	lexer.Function: true, lexer.Return: true, lexer.If_: true, lexer.Else_: true,
	lexer.True: true, lexer.False: true, lexer.Null_: true, lexer.Undefined: true,
	lexer.Async: true, lexer.Default_: true, lexer.From: true, lexer.Of: true, lexer.As: true, lexer.In_: true,
	lexer.For: true, lexer.While_: true, lexer.Do_: true, lexer.Switch: true, lexer.Case_: true,
	lexer.Try_: true, lexer.Catch_: true, lexer.Finally_: true, lexer.Throw_: true,
	lexer.Break_: true, lexer.Continue_: true,
	lexer.Class_: true, lexer.Super_: true, lexer.Extends_: true, lexer.Yield_: true,
	lexer.Delete_: true, lexer.Typeof_: true, lexer.Instanceof_: true, lexer.Void_: true,
	lexer.Debugger_: true, lexer.With_: true,
	lexer.Interface_: true, lexer.Type_: true, lexer.Implements_: true, lexer.Enum_: true,
	lexer.Declare_: true, lexer.Namespace_: true, lexer.Public_: true, lexer.Private_: true,
	lexer.Protected_: true, lexer.Readonly_: true, lexer.Any_: true, lexer.Unknown_: true,
	lexer.Never_: true, lexer.Boolean_: true, lexer.String_: true, lexer.Number_: true,
	lexer.Symbol_: true, lexer.New_: true, lexer.This_: true, lexer.Await_: true,
}

func isWordKind(k lexer.Kind) bool { return wordKinds[k] }

// isWordToken reports a word token that is not a private name.
func isWordToken(t lexer.Token) bool {
	return wordKinds[t.Kind] && !strings.HasPrefix(t.Value, "#")
}

// contextualIdentKinds are keyword kinds that are nevertheless valid
// identifiers (contextual keywords and TypeScript-only words).
var contextualIdentKinds = map[lexer.Kind]bool{
	lexer.As: true, lexer.From: true, lexer.Of: true, lexer.Async: true,
	lexer.Type_: true, lexer.Interface_: true, lexer.Any_: true, lexer.Unknown_: true,
	lexer.Never_: true, lexer.Boolean_: true, lexer.String_: true, lexer.Number_: true,
	lexer.Symbol_: true, lexer.Public_: true, lexer.Private_: true, lexer.Protected_: true,
	lexer.Readonly_: true, lexer.Declare_: true, lexer.Namespace_: true,
	lexer.Implements_: true, lexer.Let_: true, lexer.Yield_: true, lexer.Await_: true,
	lexer.Undefined: true,
}

// isIdentifierToken reports whether a token kind can be an identifier
// reference or binding name (ignoring strict-mode and generator/async
// context, which identOK checks).
func isIdentifierToken(k lexer.Kind) bool {
	return k == lexer.Identifier || contextualIdentKinds[k]
}

// strictReserved are identifiers reserved only in strict mode code.
var strictReserved = map[string]bool{
	"implements": true, "interface": true, "let": true, "package": true,
	"private": true, "protected": true, "public": true, "static": true, "yield": true,
}

// identOK reports whether the current token can be used as an identifier in
// the current context. Private names are never plain identifiers.
func (p *Parser) identOK() bool {
	t := p.tok()
	if !isIdentifierToken(t.Kind) || strings.HasPrefix(t.Value, "#") {
		return false
	}
	switch t.Kind {
	case lexer.Yield_:
		return !p.inGenerator && !p.strict
	case lexer.Await_:
		return !p.inAsync && !p.inStaticBlock
	}
	return true
}

// checkBindingName reports strict-mode violations for a binding identifier.
func (p *Parser) checkBindingName(t lexer.Token) {
	if t.Kind == lexer.Await_ && p.inStaticBlock {
		p.errorAt(t, "`await` is reserved in class static blocks", "")
	}
	if !p.strict {
		return
	}
	if t.Value == "eval" || t.Value == "arguments" {
		p.errorAt(t, "cannot bind `"+t.Value+"` in strict mode code", "choose a different name")
		return
	}
	if strictReserved[t.Value] {
		p.errorAt(t, "`"+t.Value+"` is a reserved word in strict mode code", "choose a different name")
	}
}

// isJSIdentName reports whether s is spelled like a plain identifier.
func isJSIdentName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || r == '$' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 0x7f {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}
