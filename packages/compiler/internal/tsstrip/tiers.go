package tsstrip

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Tier decorators (`@server`, `@static`, `@runtime`, `@client`) classify a
// component where it is declared. `@host` marks native host code instead of a
// tier (`@build` does too, for a host function evaluated during the native build): it is blanked and recorded on the declaration like a tier, but it never
// sets the module tier and may sit on an async function. They are compile-time syntax: JS has no
// decorators on functions or consts, and `static` is a reserved word, so they
// are blanked out before esbuild or the parser sees them. Blanking keeps every
// offset, so positions need no mapping.
//
// The text alone cannot tell a decorator from the same line in a template
// literal, JSX text or a class body. Each candidate is rewritten to an identifier
// of the same length and the file is parsed: only a real decorator comes back as
// a top-level expression statement.

var (
	tierRe              = regexp.MustCompile(`(?m)^(?:\x{FEFF})?[ \t]*(@(server|static|runtime|client|host|build))(?:[ \t]|\r?$)`)
	classDeclRe         = regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\b`)
	exportDefaultExprRe = regexp.MustCompile(`^export\s+default\b`)
	declRe              = regexp.MustCompile(`^(export\s+)?(default\s+)?(async\s+)?(?:function\s*\*?\s*([A-Za-z_$][\w$]*)?|class\b\s*([A-Za-z_$][\w$]*)?|(?:const|let|var)\s+([A-Za-z_$][\w$]*))`)
)

// tierMark is one confirmed tier decorator.
type tierMark struct {
	tier      string
	start     int // offset of `@`
	end       int // offset past the name
	line, col int
	name      string // declared name; "" for an anonymous default export
	isDefault bool
}

// tierScan is the result of scanning a source for tier decorators.
type tierScan struct {
	src        string // the source with decorators blanked
	marks      []tierMark
	moduleTier string
	errs       []error
}

func (s *tierScan) apply(prog *ast.Program) {
	prog.Tier = s.moduleTier
	for _, m := range s.marks {
		setDeclTier(prog.Body, m)
	}
}

func setDeclTier(body []ast.Stmt, m tierMark) {
	for _, stmt := range body {
		exportDefault := false
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			if exp.Declaration == nil {
				continue
			}
			exportDefault = exp.Default
			stmt = exp.Declaration
		}
		switch s := stmt.(type) {
		case *ast.FnDecl:
			if declMatches(s.Name, exportDefault, m) {
				s.Tier = m.tier
			}
		case *ast.ClassDecl:
			if declMatches(s.Name, exportDefault, m) {
				s.Tier = m.tier
			}
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d != nil && d.Name == m.name {
					d.Tier = m.tier
				}
			}
		}
	}
}

func declMatches(name string, exportDefault bool, m tierMark) bool {
	if m.name == "" {
		return m.isDefault && exportDefault
	}
	return name == m.name
}

// scanTiers finds the tier decorators in src. It returns nil when there are
// none, or when the file does not parse, which the real parse then reports.
func scanTiers(path, src string, opts Options) *tierScan {
	cands := tierRe.FindAllStringSubmatchIndex(src, -1)
	if cands == nil {
		return nil
	}
	marked := []byte(src)
	for _, c := range cands {
		at, end := c[2], c[3]
		marked[at] = '$'
		if end < len(marked) && (marked[end] == ' ' || marked[end] == '\t') {
			marked[end] = ';'
		}
	}
	prog, errs := parseSource(path, string(marked), opts)
	if prog == nil || len(errs) > 0 {
		return nil
	}
	top := map[string]bool{}
	bound := map[string]bool{}
	for _, stmt := range prog.Body {
		collectBound(stmt, bound)
		es, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		if id, ok := es.Expression.(*ast.Identifier); ok {
			top[fmt.Sprintf("%s:%d", id.Name, id.Position.Line)] = true
		}
	}

	idx := newTextIndex(src)
	s := &tierScan{}
	for _, c := range cands {
		at, end := c[2], c[3]
		m := tierMark{tier: src[c[4]:c[5]], start: at, end: end}
		m.line = 1 + strings.Count(src[:at], "\n")
		m.col = at - (strings.LastIndexByte(src[:at], '\n') + 1) + 1
		if !top[fmt.Sprintf("$%s:%d", m.tier, m.line)] {
			continue
		}
		fail := func(msg, hint string) {
			d := diag.New(path, m.line, m.col, msg, hint, idx.line(m.line)).WithCode(diag.CodeTierDecorator)
			s.errs = append(s.errs, d)
		}
		rest := skipDecoratorTrivia(src, end)
		// A file that binds the same name (`import { client } from "./x"`) can
		// mean a real class decorator. Only a class can take one, so anything
		// else carrying the tier name is a tier decorator.
		if bound[m.tier] && classDeclRe.MatchString(src[rest:]) {
			continue
		}
		decl := declRe.FindStringSubmatch(src[rest:])
		if decl == nil {
			if exportDefaultExprRe.MatchString(src[rest:]) {
				if fn, ok := inlineDefaultComponent(prog, 1+strings.Count(src[:rest], "\n")); ok {
					// `export default () => ...` / `export default memo(...)` /
					// `export default withX(Inner)`: the file's default component.
					// The bundler names it, so the mark is the file tier only.
					if fn != nil && fn.Async && m.tier != "runtime" && m.tier != "host" && m.tier != "build" {
						fail("@"+m.tier+" cannot be used on an async component",
							"an async component always renders per request; use @runtime or make it synchronous")
						continue
					}
					m.isDefault = true
					s.marks = append(s.marks, m)
					continue
				}
				fail("@"+m.tier+" cannot be placed on `export default` of this expression",
					"put the decorator on a function, or name the component and export it by name: `@"+m.tier+"` above `const View = ...;` then `export default View;`")
			} else {
				fail("@"+m.tier+" must be placed on a function, class or const declaration",
					"put it on the line above the component, e.g. `@"+m.tier+"` then `export default function Stats() {}`")
			}
			continue
		}
		m.isDefault = decl[2] != ""
		m.name = decl[4] + decl[5] + decl[6]
		if m.name == "extends" {
			m.name = ""
		}
		if decl[3] != "" && m.tier != "runtime" && m.tier != "host" && m.tier != "build" {
			fail("@"+m.tier+" cannot be used on an async component",
				"an async component always renders per request; use @runtime or make it synchronous")
			continue
		}
		for _, prev := range s.marks {
			if prev.name == m.name && prev.isDefault == m.isDefault {
				fail("conflicting tier decorators @"+prev.tier+" and @"+m.tier+" on one declaration", "keep one of them")
			}
		}
		s.marks = append(s.marks, m)
	}
	if len(s.marks) == 0 && len(s.errs) == 0 {
		return nil
	}

	defaultName, hasDefault, components := moduleExports(prog)
	var sole *tierMark
	for i := range s.marks {
		m := &s.marks[i]
		if m.tier == "host" || m.tier == "build" {
			continue
		}
		isDefault := m.isDefault || (hasDefault && m.name != "" && m.name == defaultName)
		isSole := !hasDefault && components == 1 && isExportedName(prog, m.name)
		if isDefault || isSole {
			s.moduleTier = m.tier
			sole = m
		}
	}
	for _, m := range s.marks {
		if m.tier == "runtime" && (sole == nil || sole.start != m.start) {
			d := diag.New(path, m.line, m.col, "@runtime must be on the default export or the only exported component",
				"a runtime component is bundled per file; move "+m.name+" into its own module", idx.line(m.line)).WithCode(diag.CodeTierDecorator)
			s.errs = append(s.errs, d)
		}
	}

	blank := []byte(src)
	for _, m := range s.marks {
		for i := m.start; i < m.end; i++ {
			blank[i] = ' '
		}
	}
	s.src = string(blank)
	return s
}

// inlineDefaultComponent finds the `export default <component expression>`
// statement starting on line and returns its function. A higher-order
// component application (`export default withX(Inner)`) has no function of its
// own: it returns ok with a nil function.
func inlineDefaultComponent(prog *ast.Program, line int) (*ast.ArrowFn, bool) {
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok || !exp.Default || exp.Position.Line != line {
			continue
		}
		es, ok := exp.Declaration.(*ast.ExprStmt)
		if !ok {
			return nil, false
		}
		e := es.Expression
		for {
			call, isCall := e.(*ast.CallExpr)
			if !isCall {
				break
			}
			id, isID := call.Callee.(*ast.Identifier)
			if !isID || len(call.Args) == 0 {
				return nil, false
			}
			if id.Name != "memo" && id.Name != "forwardRef" {
				return nil, true
			}
			e = call.Args[0]
			if _, named := e.(*ast.Identifier); named {
				return nil, true
			}
		}
		if fn, isFn := e.(*ast.ArrowFn); isFn && !fn.Generator {
			return fn, true
		}
		return nil, false
	}
	return nil, false
}

// skipDecoratorTrivia moves past whitespace, comments and other decorators.
func skipDecoratorTrivia(src string, i int) int {
	for i < len(src) {
		switch {
		case strings.ContainsRune(" \t\r\n", rune(src[i])):
			i++
		case strings.HasPrefix(src[i:], "//"):
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				return len(src)
			}
			i += j
		case strings.HasPrefix(src[i:], "/*"):
			j := strings.Index(src[i:], "*/")
			if j < 0 {
				return len(src)
			}
			i += j + 2
		case src[i] == '@':
			i++
			for i < len(src) && (isIdentByte(src[i]) || src[i] == '.') {
				i++
			}
			if i < len(src) && src[i] == '(' {
				i = skipParens(src, i)
			}
		default:
			return i
		}
	}
	return i
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func skipParens(src string, i int) int {
	depth := 0
	for ; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return i
}

func collectBound(stmt ast.Stmt, bound map[string]bool) {
	if exp, ok := stmt.(*ast.ExportStmt); ok && exp.Declaration != nil {
		stmt = exp.Declaration
	}
	switch s := stmt.(type) {
	case *ast.ImportStmt:
		bound[s.Default], bound[s.Namespace] = true, true
		for _, n := range s.Named {
			bound[n.Local] = true
		}
	case *ast.FnDecl:
		bound[s.Name] = true
	case *ast.ClassDecl:
		bound[s.Name] = true
	case *ast.VarStmt:
		for _, d := range s.Decls {
			if d != nil {
				bound[d.Name] = true
			}
		}
	}
}

// moduleExports returns the default export's local name, whether there is a
// default export, and how many exports are named like components.
func moduleExports(prog *ast.Program) (defaultName string, hasDefault bool, components int) {
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		if exp.Default {
			hasDefault = true
			defaultName = exp.Local
		}
		for _, n := range exportedNames(exp) {
			if exp.Default {
				defaultName = n
			} else if n != "" && n[0] >= 'A' && n[0] <= 'Z' {
				components++
			}
		}
	}
	return defaultName, hasDefault, components
}

func exportedNames(exp *ast.ExportStmt) []string {
	var names []string
	switch d := exp.Declaration.(type) {
	case *ast.FnDecl:
		names = append(names, d.Name)
	case *ast.ClassDecl:
		names = append(names, d.Name)
	case *ast.VarStmt:
		for _, v := range d.Decls {
			if v != nil {
				names = append(names, v.Name)
			}
		}
	}
	for _, n := range exp.Named {
		names = append(names, n.Local)
	}
	return names
}

func isExportedName(prog *ast.Program, name string) bool {
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			for _, n := range exportedNames(exp) {
				if n == name {
					return true
				}
			}
		}
	}
	return false
}

// BlankTierDecorators returns src with its tier decorators replaced by spaces,
// for tools that hand source to esbuild or the parser directly.
func BlankTierDecorators(path, src string) string {
	if s := scanTiers(path, src, Options{}); s != nil && s.src != "" {
		return s.src
	}
	return src
}

// ModuleTier returns the tier a decorator gives the whole file ("" for none):
// that of the default export, or of the only exported component.
func ModuleTier(path, src string) string {
	if s := scanTiers(path, src, Options{}); s != nil {
		return s.moduleTier
	}
	return ""
}

// HandleTierDecorators makes a bundle accept project source with tier
// decorators, which esbuild rejects. The plugin goes last, so a plugin that
// claims a module has to blank them itself.
func HandleTierDecorators(opts *api.BuildOptions) {
	if opts.Stdin != nil {
		name := opts.Stdin.Sourcefile
		if name == "" {
			name = "stdin.tsx"
		}
		opts.Stdin.Contents = BlankTierDecorators(name, opts.Stdin.Contents)
	}
	opts.Plugins = append(opts.Plugins[:len(opts.Plugins):len(opts.Plugins)], tierDecoratorPlugin)
}

var tierDecoratorPlugin = api.Plugin{
	Name: "krate-tier-decorators",
	Setup: func(pb api.PluginBuild) {
		pb.OnLoad(api.OnLoadOptions{Filter: `\.[cm]?[jt]sx?$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
			if strings.Contains(args.Path, "node_modules") {
				return api.OnLoadResult{}, nil
			}
			data, err := os.ReadFile(args.Path)
			if err != nil {
				return api.OnLoadResult{}, nil
			}
			src := string(data)
			blanked := BlankTierDecorators(args.Path, src)
			if blanked == src {
				return api.OnLoadResult{}, nil
			}
			return api.OnLoadResult{Contents: &blanked, Loader: api.LoaderDefault}, nil
		})
	},
}
