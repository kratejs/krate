package hostts

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

const appPkg = "@krate/app"

// inlineFn is an inline host function that was moved out of a UI module.
type inlineFn struct {
	Name string
	ID   string
	Kind Kind
	// Line is the 1-based line of the declaration in the UI module.
	Line int
	// Boundary-check input: offset of the declaration in the UI module.
	Off int
	// genLine is the 1-based line of the function in the generated module.
	genLine int
	lines   int
}

// splitResult is the outcome of splitting one UI module.
type splitResult struct {
	// UISrc is the UI module without its host functions.
	UISrc string
	// HostSrc is the generated host module; "" when nothing was split.
	HostSrc string
	Fns     []*inlineFn
	// Loader names the esbuild loader for HostSrc.
	Loader string
}

var preambleRe = regexp.MustCompile(`^(@host\s+)?(export\s+(default\s+)?)?`)

func moduleID(rel string) string {
	return strings.TrimSuffix(rel, filepath.Ext(rel))
}

func loaderFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsx":
		return "tsx"
	case ".jsx":
		return "jsx"
	case ".js", ".mjs":
		return "js"
	}
	return "ts"
}

// importBindings lists the local names an import declaration binds.
func importBindings(imp *ast.ImportStmt) []string {
	var out []string
	if imp.Default != "" {
		out = append(out, imp.Default)
	}
	if imp.Namespace != "" {
		out = append(out, imp.Namespace)
	}
	for _, n := range imp.Named {
		out = append(out, n.Local)
	}
	return out
}

// regenImport writes an import declaration keeping only the bindings in keep.
func regenImport(imp *ast.ImportStmt, keep func(string) bool) string {
	var parts []string
	if imp.Default != "" && keep(imp.Default) {
		parts = append(parts, imp.Default)
	}
	if imp.Namespace != "" && keep(imp.Namespace) {
		parts = append(parts, "* as "+imp.Namespace)
	}
	var named []string
	for _, n := range imp.Named {
		if !keep(n.Local) {
			continue
		}
		if n.Local == n.Remote {
			named = append(named, n.Local)
		} else {
			named = append(named, n.Remote+" as "+n.Local)
		}
	}
	if len(named) > 0 {
		parts = append(parts, "{ "+strings.Join(named, ", ")+" }")
	}
	if len(parts) == 0 {
		return ""
	}
	s := "import " + strings.Join(parts, ", ") + " from " + imp.Source
	if len(imp.Attributes) > 0 {
		var attrs []string
		for _, a := range imp.Attributes {
			attrs = append(attrs, a.Key+": \""+a.Value+"\"")
		}
		s += " with { " + strings.Join(attrs, ", ") + " }"
	}
	return s + ";"
}

// importRange returns the byte range of the import declaration at off,
// including its semicolon.
func importRange(src string, off int) (int, int) {
	i := off
	for i < len(src) && src[i] != '"' && src[i] != '\'' {
		i++
	}
	if i >= len(src) {
		return off, off
	}
	i = native.SkipQuoted(src, i) + 1
	j := i
	for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
		j++
	}
	if strings.HasPrefix(src[j:], "with") || strings.HasPrefix(src[j:], "assert") {
		if k := strings.IndexByte(src[j:], '{'); k >= 0 {
			if e := native.MatchBrace(src, j+k); e > 0 {
				i = e
				j = e
				for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
					j++
				}
			}
		}
	}
	if j < len(src) && src[j] == ';' {
		i = j + 1
	}
	return off, i
}

// splitInline moves the inline host functions of one UI module into a
// generated host module. rel is the module's path relative to the project root.
func splitInline(rel, path, src string, funcs []native.Func) (*splitResult, []diag.Diagnostic) {
	var ds []diag.Diagnostic
	modID := moduleID(rel)
	fail := func(f native.Func, msg, hint string) {
		line, col := f.Line, 1
		ds = append(ds, diag.New(rel, line, col, msg, hint, lineText(src, line)).WithCode(diag.CodeHostSplit))
	}

	sorted := append([]native.Func(nil), funcs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	var fns []native.Func
	names := map[string]bool{}
	lastEnd := -1
	for _, f := range sorted {
		if f.Start < lastEnd {
			continue // nested in an earlier host function: it moves with it
		}
		if f.Name == "" || f.Name == "default" {
			fail(f, "an anonymous default-exported host function cannot be split out of the UI module",
				"give the function a name, or move it to a `.host.ts` module")
			continue
		}
		if f.End <= f.Start || f.End > len(src) {
			fail(f, "cannot find the extent of host function "+f.Name,
				"give the function a block body ({ ... }); an expression-bodied arrow cannot be split")
			continue
		}
		if names[f.Name] {
			fail(f, "two inline host functions named "+f.Name+" in one module share the host id "+modID+"#"+f.Name,
				"rename one of them")
			continue
		}
		names[f.Name] = true
		lastEnd = f.End
		fns = append(fns, f)
	}
	if len(ds) > 0 || len(fns) == 0 {
		return nil, ds
	}

	prog, errs := tsstrip.Parse(path, src, tsstrip.Options{})
	if prog == nil || len(errs) > 0 {
		return nil, ds
	}

	type piece struct {
		f        native.Func
		text     string // normalized for the host module
		export   bool
		isDefExp bool
		kind     Kind
		isConst  bool
	}
	var pieces []piece
	for _, f := range fns {
		raw := src[f.Start:f.End]
		pre := preambleRe.FindString(raw)
		body := raw[len(pre):]
		p := piece{f: f, text: strings.TrimRight(body, " \t\r\n"), export: strings.Contains(pre, "export"), isDefExp: strings.Contains(pre, "default")}
		head := mask(body)
		switch {
		case regexp.MustCompile(`^(async\s+)?function\s*\*`).MatchString(head):
			p.kind = KindStream
		case regexp.MustCompile(`^(async\s+)?function\b`).MatchString(head):
			p.kind = KindFunc
		case regexp.MustCompile(`^(const|let|var)\b`).MatchString(head):
			p.isConst = true
			p.kind = KindFunc
			if k := strings.Index(head, "=>"); k >= 0 {
				if rest := strings.TrimLeft(head[k+2:], " \t\r\n"); !strings.HasPrefix(rest, "{") {
					fail(f, "host function "+f.Name+" has an expression body and cannot be split out",
						"give the function a block body: `=> { return ...; }`")
					continue
				}
			} else if regexp.MustCompile(`=\s*(async\s+)?function\s*\*`).MatchString(head) {
				p.kind = KindStream
			}
		default:
			fail(f, "cannot split host function "+f.Name+" out of the UI module", "declare it with `function`, or `const name = async () => { ... }`")
			continue
		}
		pieces = append(pieces, p)
	}
	if len(ds) > 0 {
		return nil, ds
	}

	// Words used by the host functions decide which imports move.
	hostWords := map[string]bool{}
	for _, p := range pieces {
		for w := range words(mask(p.text)) {
			hostWords[w] = true
		}
	}

	// UI source: replace each function with a stub, keeping line numbers.
	var sb strings.Builder
	pos := 0
	usesCall, usesStream := false, false
	for _, p := range pieces {
		sb.WriteString(src[pos:p.f.Start])
		id := modID + "#" + p.f.Name
		fnName := "__krateCall"
		if p.kind == KindStream {
			fnName = "__krateStream"
			usesStream = true
		} else {
			usesCall = true
		}
		stub := "const " + p.f.Name + " = (...a) => " + fnName + "(" + quote(id) + ", a);"
		switch {
		case p.isDefExp:
			stub += " export default " + p.f.Name + ";"
		case p.export:
			stub = "export " + stub
		}
		sb.WriteString(stub)
		sb.WriteString(strings.Repeat("\n", strings.Count(src[p.f.Start:p.f.End], "\n")))
		pos = p.f.End
	}
	sb.WriteString(src[pos:])
	ui := sb.String()

	// Drop the imports only the host functions used. The ranges are in the
	// original source, so offsets are mapped through the replaced pieces.
	ui = dropUnusedImports(prog, ui, pieces[0].f.Start, hostWords)

	var imports []string
	if usesCall {
		imports = append(imports, "call as __krateCall")
	}
	if usesStream {
		imports = append(imports, "stream as __krateStream")
	}
	if !strings.HasSuffix(ui, "\n") {
		ui += "\n"
	}
	ui += "import { " + strings.Join(imports, ", ") + " } from " + quote(appPkg) + ";\n"

	// Host module: the imports the functions use, then the functions.
	var hb strings.Builder
	hb.WriteString("// generated from " + rel + "\n")
	for _, s := range prog.Body {
		imp, ok := s.(*ast.ImportStmt)
		if !ok {
			continue
		}
		bound := false
		for _, n := range importBindings(imp) {
			if hostWords[n] {
				bound = true
			}
		}
		if !bound {
			continue
		}
		if line := regenImport(imp, func(n string) bool { return hostWords[n] }); line != "" {
			hb.WriteString(line + "\n")
		}
	}
	res := &splitResult{UISrc: ui, Loader: loaderFor(path)}
	line := 1 + strings.Count(hb.String(), "\n")
	for _, p := range pieces {
		body := p.text
		hb.WriteString("export " + body + "\n")
		n := 1 + strings.Count(body, "\n")
		res.Fns = append(res.Fns, &inlineFn{
			Name: p.f.Name, ID: modID + "#" + p.f.Name, Kind: p.kind,
			Line: p.f.Line, Off: p.f.Start, genLine: line, lines: n,
		})
		line += n
	}
	res.HostSrc = hb.String()
	return res, nil
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }

// dropUnusedImports removes from ui the bindings of imports that only the
// moved functions used. Offsets before firstMoved are the same in the module
// before and after the split; imports sit at the top of a module, so only
// those are rewritten and an import after a moved function is left alone.
func dropUnusedImports(prog *ast.Program, ui string, firstMoved int, hostWords map[string]bool) string {
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	// Words still used outside the imports being considered.
	var ranges [][2]int
	for _, s := range prog.Body {
		if imp, ok := s.(*ast.ImportStmt); ok && imp.Position.Offset < firstMoved {
			a, b := importRange(ui, imp.Position.Offset)
			ranges = append(ranges, [2]int{a, b})
		}
	}
	var rest strings.Builder
	prev := 0
	for _, r := range ranges {
		rest.WriteString(ui[prev:r[0]])
		prev = r[1]
	}
	rest.WriteString(ui[prev:])
	used := words(rest.String())

	for _, s := range prog.Body {
		imp, ok := s.(*ast.ImportStmt)
		if !ok || imp.Position.Offset >= firstMoved {
			continue
		}
		cand := false
		for _, n := range importBindings(imp) {
			if hostWords[n] && !used[n] {
				cand = true
			}
		}
		if !cand {
			continue
		}
		a, b := importRange(ui, imp.Position.Offset)
		repl := regenImport(imp, func(n string) bool { return used[n] || !hostWords[n] })
		if len(importBindings(imp)) == 0 {
			continue
		}
		edits = append(edits, edit{a, b, repl + strings.Repeat("\n", strings.Count(ui[a:b], "\n"))})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		ui = ui[:e.start] + e.text + ui[e.end:]
	}
	return ui
}
