package bundler

import (
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Tier conflicts.
//
// A component takes its tier from the first source that names one, but two
// sources that name different tiers for the same component are a mistake, not
// a priority question: the build used to pick one silently. The rule:
//
//   - File-wide sources are the leading `// @tier` comment, the `.server` /
//     `.runtime` / `.static` file name, the serverComponents and
//     runtimeComponents lists, and the serverDirs and runtimeDirs lists. They
//     must all agree.
//   - A tier written on a component (decorator or a `"@tier"` string at the
//     top of its body) must agree with the others on that component.
//   - A component-level tier on the file's default export, or on the only
//     exported component, classifies the whole file, so it must also agree
//     with the file-wide sources.
//   - A component-level tier on any other component overrides the file-wide
//     tier for that component alone (`@client` in a `// @server` file).

// tierSource is one place a tier was written.
type tierSource struct {
	what string
	tier ComponentClass
	line int // 1-based line in the module, 0 when unknown
}

// fileTierSources lists the file-wide sources that name a tier.
func fileTierSources(source, path string, serverComponents, runtimeComponents, serverDirs, runtimeDirs []string) []tierSource {
	var out []tierSource
	for _, d := range []struct {
		name string
		tier ComponentClass
	}{
		{DirectiveStatic, ComponentClassStatic},
		{DirectiveServer, ComponentClassServer},
		{DirectiveRuntime, ComponentClassRuntime},
		{DirectiveClient, ComponentClassClient},
	} {
		if HasDirective(source, d.name) {
			out = append(out, tierSource{what: "the `// " + d.name + "` comment", tier: d.tier, line: directiveLine(source, d.name)})
		}
	}
	switch {
	case IsServerComponentFile(path):
		out = append(out, tierSource{what: "the `.server` file name", tier: ComponentClassServer})
	case IsRuntimeComponentFile(path):
		out = append(out, tierSource{what: "the `.runtime` file name", tier: ComponentClassRuntime})
	case IsStaticComponentFile(path):
		out = append(out, tierSource{what: "the `.static` file name", tier: ComponentClassStatic})
	}
	base := extractComponentName(path)
	inList := func(list []string) bool {
		for _, n := range list {
			if n == base || n == path {
				return true
			}
		}
		return false
	}
	if inList(serverComponents) {
		out = append(out, tierSource{what: "`serverComponents` in the config", tier: ComponentClassServer})
	}
	if inList(runtimeComponents) {
		out = append(out, tierSource{what: "`runtimeComponents` in the config", tier: ComponentClassRuntime})
	}
	if isPathInDirs(path, serverDirs) {
		out = append(out, tierSource{what: "`serverDirs` in the config", tier: ComponentClassServer})
	}
	if isPathInDirs(path, runtimeDirs) {
		out = append(out, tierSource{what: "`runtimeDirs` in the config", tier: ComponentClassRuntime})
	}
	return out
}

// directiveLine returns the line of the leading comment that holds directive.
func directiveLine(source, directive string) int {
	for i, line := range strings.SplitN(source, "\n", 10) {
		if strings.Contains(strings.ToLower(line), directive) && (strings.Contains(line, "//") || strings.Contains(line, "/*")) {
			return i + 1
		}
	}
	return 0
}

// tierDecl is a component declaration with the tiers written on it.
type tierDecl struct {
	name      string
	pos       ast.Pos
	decorator string // tier name from a decorator
	body      string // tier name from a "@tier" string at the top of the body
	isDefault bool
	exported  bool
}

// checkTierConflicts reports the first conflicting pair of tiers in a module.
func checkTierConflicts(prog *ast.Program, path, source string, serverComponents, runtimeComponents, serverDirs, runtimeDirs []string) error {
	fileSrc := fileTierSources(source, path, serverComponents, runtimeComponents, serverDirs, runtimeDirs)
	lineOfSrc := func(n int) string {
		lines := strings.Split(source, "\n")
		if n >= 1 && n <= len(lines) {
			return strings.TrimRight(lines[n-1], "\r")
		}
		return ""
	}
	conflict := func(line int, msg string) error {
		hint := "a component has one tier: remove one of them. A decorator on a component that is not the file's default export may differ from the file's tier (for example `@client` in a `// @server` file); on the default export or the only exported component it must match"
		col := 1
		if line < 1 {
			line = 1
		}
		return diag.New(path, line, col, msg, hint, lineOfSrc(line)).WithCode(diag.CodeTierDecorator)
	}

	// File-wide sources must agree with each other.
	for i := 1; i < len(fileSrc); i++ {
		if fileSrc[i].tier != fileSrc[0].tier {
			line := fileSrc[i].line
			if line == 0 {
				line = fileSrc[0].line
			}
			return conflict(line, "conflicting tiers: "+fileSrc[0].what+" makes this file "+fileSrc[0].tier.String()+", but "+fileSrc[i].what+" makes it "+fileSrc[i].tier.String())
		}
	}

	decls, hasDefault := tierDecls(prog)
	for _, d := range decls {
		if d.decorator != "" && d.body != "" && d.decorator != d.body {
			return conflict(d.pos.Line, "conflicting tiers on "+d.name+": the @"+d.decorator+" decorator and the \""+"@"+d.body+"\" directive in its body")
		}
		tier := d.decorator
		what := "the @" + d.decorator + " decorator on " + d.name
		if tier == "" {
			tier = d.body
			what = "the \"@" + d.body + "\" directive in the body of " + d.name
		}
		if tier == "" || len(fileSrc) == 0 {
			continue
		}
		fileWide := d.isDefault || (d.decorator != "" && d.exported && !hasDefault && prog.Tier == d.decorator)
		if !fileWide {
			continue
		}
		cls, ok := classFromTier(tier)
		if !ok {
			continue
		}
		for _, fs := range fileSrc {
			if fs.tier != cls {
				return conflict(d.pos.Line, "conflicting tiers: "+what+" makes it "+cls.String()+", but "+fs.what+" makes the file "+fs.tier.String())
			}
		}
	}
	return nil
}

// tierDecls lists the top-level component declarations that carry a tier.
func tierDecls(prog *ast.Program) (out []tierDecl, hasDefault bool) {
	defaultName := ""
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok || !exp.Default {
			continue
		}
		switch d := exp.Declaration.(type) {
		case *ast.FnDecl:
			defaultName = d.Name
		case *ast.ClassDecl:
			defaultName = d.Name
		}
		if defaultName == "" {
			defaultName = exp.Local
		}
		hasDefault = true
	}
	add := func(name string, pos ast.Pos, deco string, body []ast.Stmt, exportedDefault, exported bool) {
		bodyTier := bodyDirectiveTier(body)
		if deco == "" && bodyTier == "" {
			return
		}
		out = append(out, tierDecl{name: name, pos: pos, decorator: deco, body: bodyTier, isDefault: exportedDefault || (defaultName != "" && name == defaultName), exported: exported})
	}
	for _, stmt := range prog.Body {
		exportDefault, exported := false, false
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			if exp.Declaration == nil {
				continue
			}
			exportDefault, exported = exp.Default, true
			stmt = exp.Declaration
		}
		switch s := stmt.(type) {
		case *ast.FnDecl:
			add(s.Name, s.Position, s.Tier, s.Body, exportDefault, exported)
		case *ast.ClassDecl:
			add(s.Name, s.Position, s.Tier, nil, exportDefault, exported)
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d == nil || d.Name == "" {
					continue
				}
				var body []ast.Stmt
				if fn, ok := d.Init.(*ast.ArrowFn); ok {
					body = fn.Body
				}
				add(d.Name, s.Position, d.Tier, body, false, exported)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].pos.Line < out[j].pos.Line })
	return out, hasDefault
}

// bodyDirectiveTier reads a leading `"@server"` style string statement.
func bodyDirectiveTier(body []ast.Stmt) string {
	if len(body) == 0 {
		return ""
	}
	es, ok := body[0].(*ast.ExprStmt)
	if !ok {
		return ""
	}
	lit, ok := es.Expression.(*ast.Literal)
	if !ok || lit.Kind != ast.StringLit {
		return ""
	}
	val := strings.TrimSpace(lit.Value)
	for _, t := range []string{"static", "server", "runtime", "client"} {
		if strings.Contains(val, "@"+t) {
			return t
		}
	}
	return ""
}
