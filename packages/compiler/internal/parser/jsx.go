package parser

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

func (p *Parser) parseJSXElement() ast.Expr {
	p.enter()
	defer p.leave()
	lt := p.next() // `<` (JSX)
	if p.kind() == lexer.GT {
		p.next()
		frag := &ast.JSXFragment{Position: tokPos(lt)}
		frag.Children = p.parseJSXChildren("")
		if p.kind() == lexer.LT_SLASH {
			p.next()
			if p.kind() != lexer.GT {
				p.errWithMsg("expected `</>` to close the fragment, got "+describe(p.tok()), "a fragment `<>` must be closed by `</>`")
				p.parseJSXName()
			}
			p.expect(lexer.GT)
		}
		return frag
	}
	name, ok := p.parseJSXName()
	if !ok {
		p.errWithMsg("expected a JSX tag name, got "+describe(p.tok()), "")
		return nil
	}
	if p.kind() == lexer.LT && !p.tok().JSX {
		// TypeScript type arguments on a component: `<Select<string> ...>`.
		if c, ok := p.scanTypeArgs(p.cursor()); ok {
			p.applyCursor(c)
			p.droppedTypes++
		}
	}
	opening := &ast.JSXOpening{Name: name}
	el := &ast.JSXElement{Position: tokPos(lt), Opening: opening}
	for {
		t := p.tok()
		switch t.Kind {
		case lexer.GT:
			p.next()
			el.Children = p.parseJSXChildren(name)
			if p.kind() == lexer.LT_SLASH {
				p.next()
				closeTok := p.tok()
				closing, ok := "", true
				if p.kind() != lexer.GT {
					closing, ok = p.parseJSXName()
				}
				if !ok || closing != name {
					p.errorAt(closeTok, "expected corresponding JSX closing tag for <"+name+">", "close the element with </"+name+">")
				}
				el.Closing = &ast.JSXClosing{Name: closing}
				p.expect(lexer.GT)
			}
			return el
		case lexer.SLASH_GT:
			p.next()
			opening.SelfClosing = true
			return el
		case lexer.EOF:
			p.errWithMsg("unterminated JSX opening tag", "the tag <"+name+"> is missing its closing `>`")
			return el
		}
		attr := p.parseJSXAttr()
		if attr == nil {
			p.errWithMsg("unexpected "+describe(p.tok())+" in JSX tag <"+name+">", "attributes are written name=\"value\", name={expr} or {...spread}")
			// Skip to the end of the tag to recover.
			for p.kind() != lexer.GT && p.kind() != lexer.SLASH_GT && p.kind() != lexer.EOF {
				p.next()
			}
			continue
		}
		opening.Attributes = append(opening.Attributes, attr)
	}
}

// parseJSXName parses a tag name: `div`, `my-element`, `svg:rect` or
// `Foo.Bar.Baz`.
func (p *Parser) parseJSXName() (string, bool) {
	t := p.tok()
	if !isWordToken(t) {
		return "", false
	}
	p.next()
	name := t.Value
	if p.kind() == lexer.COLON && isWordToken(p.at(1)) {
		p.next()
		name += ":" + p.next().Value
		return name, true
	}
	for p.kind() == lexer.DOT && isWordToken(p.at(1)) {
		p.next()
		name += "." + p.next().Value
	}
	return name, true
}

func (p *Parser) parseJSXAttr() *ast.JSXAttr {
	t := p.tok()
	pos := tokPos(t)
	if t.Kind == lexer.LBRACE {
		p.next()
		if !p.match(lexer.SPREAD) {
			p.errWithMsg("expected `...` in a JSX attribute spread", "spread props are written {...props}")
			p.skipTo(lexer.RBRACE)
			p.match(lexer.RBRACE)
			return &ast.JSXAttr{Position: pos, Spread: true, Value: &ast.Literal{Kind: ast.NullLit, Value: "undefined"}}
		}
		expr := p.parseAssignAllowIn()
		if expr == nil {
			p.errWithMsg("expected an expression after `...` in a JSX spread", "")
			expr = &ast.Literal{Kind: ast.NullLit, Value: "undefined"}
		}
		p.expect(lexer.RBRACE)
		return &ast.JSXAttr{Position: pos, Spread: true, Value: expr}
	}
	if !isWordToken(t) {
		return nil
	}
	p.next()
	name := t.Value
	if p.kind() == lexer.COLON && isWordToken(p.at(1)) {
		p.next()
		name += ":" + p.next().Value
	}
	if !p.match(lexer.ASSIGN) {
		return &ast.JSXAttr{Position: pos, Name: name, Value: &ast.Literal{Position: pos, Kind: ast.BoolLit, Value: "true"}}
	}
	vt := p.tok()
	switch {
	case vt.Kind == lexer.String:
		p.next()
		// JSX attribute strings process HTML entities but not backslash escapes.
		raw := vt.Value
		if len(raw) >= 2 {
			raw = raw[1 : len(raw)-1]
		}
		return &ast.JSXAttr{Position: pos, Name: name, Value: &ast.Literal{Position: tokPos(vt), Kind: ast.StringLit, Value: decodeJSXEntities(raw)}}
	case vt.Kind == lexer.LBRACE:
		p.next()
		if p.kind() == lexer.RBRACE {
			p.errWithMsg("JSX attributes must be assigned a non-empty expression", "remove the `={}` or put a value inside the braces")
			p.next()
			return &ast.JSXAttr{Position: pos, Name: name, Value: &ast.Literal{Kind: ast.BoolLit, Value: "true"}}
		}
		expr := p.parseAssignAllowIn()
		p.expect(lexer.RBRACE)
		return &ast.JSXAttr{Position: pos, Name: name, Value: expr}
	case vt.Kind == lexer.LT && vt.JSX:
		return &ast.JSXAttr{Position: pos, Name: name, Value: p.parseJSXElement()}
	}
	p.errWithMsg("expected a JSX attribute value after `=`, got "+describe(vt), "use name=\"text\" or name={expression}")
	return &ast.JSXAttr{Position: pos, Name: name, Value: &ast.Literal{Kind: ast.BoolLit, Value: "true"}}
}

// parseJSXChildren parses children up to the closing `</`.
func (p *Parser) parseJSXChildren(parent string) []ast.JSXChild {
	var children []ast.JSXChild
	for {
		t := p.tok()
		switch t.Kind {
		case lexer.LT_SLASH:
			return children
		case lexer.EOF:
			if parent == "" {
				p.errWithMsg("unterminated JSX fragment", "the fragment `<>` is missing its closing `</>`")
			} else {
				p.errWithMsg("unterminated JSX element", "the element <"+parent+"> is missing its closing </"+parent+">")
			}
			return children
		case lexer.JSXText:
			p.next()
			// A literal `>` in JSX text is kept as text (a deliberate leniency
			// for prose); a stray `}` is always a mistake.
			if strings.Contains(t.Value, "}") {
				p.errorAt(t, "unexpected `}` in JSX text", "write it as {'}'} or &rbrace;")
			}
			if text := normalizeJSXText(decodeJSXEntities(t.Value)); text != "" {
				children = append(children, &ast.JSXText{Value: text})
			}
		case lexer.LBRACE:
			p.next()
			if p.kind() == lexer.RBRACE {
				// `{}` or `{/* comment */}`: an empty container renders nothing.
				p.next()
				continue
			}
			if p.kind() == lexer.SPREAD {
				st := p.next()
				e := p.parseAssignAllowIn()
				p.expect(lexer.RBRACE)
				children = append(children, &ast.JSXExprContainer{Expression: &ast.UnaryExpr{Position: tokPos(st), Op: "...", Arg: e}})
				continue
			}
			e := p.parseAssignAllowIn()
			if e == nil {
				p.errWithMsg("expected an expression inside `{}`", "")
				p.skipTo(lexer.RBRACE)
			}
			if p.kind() == lexer.COMMA {
				p.errWithMsg("JSX expression containers hold a single expression", "wrap a sequence in parentheses or split it into separate {...} containers")
				p.skipTo(lexer.RBRACE)
			}
			p.expect(lexer.RBRACE)
			children = append(children, &ast.JSXExprContainer{Expression: e})
		case lexer.LT:
			if !t.JSX {
				p.next()
				children = append(children, &ast.JSXText{Value: "<"})
				continue
			}
			switch e := p.parseJSXElement().(type) {
			case *ast.JSXElement:
				children = append(children, &ast.JSXElementChild{Element: e})
			case *ast.JSXFragment:
				children = append(children, &ast.JSXFragmentChild{Fragment: e})
			}
		default:
			// Anything else inside children is a lexer desync; keep the text.
			p.next()
			children = append(children, &ast.JSXText{Value: t.Value})
		}
	}
}

// normalizeJSXText applies the canonical JSX whitespace transform (matching
// Babel's cleanJSXElementLiteralChild): whitespace runs that span newlines are
// trimmed at line boundaries and lines are joined with a single space;
// whitespace on a single line is preserved as-is.
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
	return b.String()
}

// splitJSXLines splits a string on \n, \r and \r\n.
func splitJSXLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			lines = append(lines, s[start:i])
			start = i + 1
		case '\r':
			lines = append(lines, s[start:i])
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	return append(lines, s[start:])
}

// decodeJSXEntities decodes HTML character references with React/Babel
// semantics: only the HTML 4 named entities plus `&#123;` / `&#x1F;`
// numeric forms, and the terminating `;` is required. Anything else is left
// as literal text.
func decodeJSXEntities(s string) string {
	if strings.IndexByte(s, '&') < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			b.WriteByte(s[i])
			continue
		}
		end := strings.IndexByte(s[i+1:], ';')
		if end < 1 || end > 10 {
			b.WriteByte('&')
			continue
		}
		name := s[i+1 : i+1+end]
		if r, ok := decodeEntityName(name); ok {
			b.WriteString(r)
			i += end + 1
			continue
		}
		b.WriteByte('&')
	}
	return b.String()
}

func decodeEntityName(name string) (string, bool) {
	if name[0] != '#' {
		v, ok := jsxEntities()[name]
		return v, ok
	}
	num := name[1:]
	var v int
	ok := false
	if len(num) > 1 && (num[0] == 'x' || num[0] == 'X') {
		v, ok = hexValue(num[1:])
	} else if num != "" {
		ok = true
		for i := 0; i < len(num); i++ {
			if num[i] < '0' || num[i] > '9' {
				ok = false
				break
			}
			v = v*10 + int(num[i]-'0')
			if v > 0x10FFFF {
				ok = false
				break
			}
		}
	}
	if !ok || v > 0x10FFFF || v >= 0xD800 && v <= 0xDFFF {
		return "", false
	}
	return string(rune(v)), true
}
