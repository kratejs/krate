package migrate

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
)

// codePiece is a fragment of next.config that is copied into krate.config.ts
// as code, because its value is computed (a helper call, an env-built list).
type codePiece struct {
	// Label names the piece in messages, such as "headers[1]".
	Label string
	// Text is the verbatim JavaScript of one array element: an object
	// expression or a spread such as `...buildCacheRules()`.
	Text string
	// Disabled is non-empty when the piece must not be emitted: the error that
	// made the generated config fail. tempOff marks a probe in progress.
	Disabled string
	tempOff  bool
	// Fallback is the rule's static part, written instead of the code when the
	// code fails to evaluate.
	Fallback *nextHeaderRule
}

func (p *codePiece) active() bool { return p != nil && p.Disabled == "" && !p.tempOff }

type stmtKind int

const (
	stmtSkip stmtKind = iota
	stmtImport
	stmtDecl
	stmtEffect
)

type preStmt struct {
	kind   stmtKind
	names  []string // names a declaration binds, or an import brings in
	text   string   // the statement, with the comment lines above it
	idents map[string]bool
	// blankBefore is true when the source had a blank line above the statement.
	blankBefore bool
}

// preambleSource holds next.config's top-level statements so the ones a code
// piece needs can be copied into krate.config.ts.
type preambleSource struct {
	stmts []preStmt
}

// droppedImportSources are imports that krate.config.ts must not carry: Next
// itself (types only) and @next/env (loadEnvConfig is mapped to envDir).
func droppedImportSource(src string) bool {
	return src == "next" || strings.HasPrefix(src, "next/") || src == "@next/env"
}

var (
	typeImportRe  = regexp.MustCompile(`^\s*import\s+type\b`)
	typeOnlyDecl  = regexp.MustCompile(`^\s*(?:export\s+)?(?:declare\s+|type\s+\w|interface\s+\w)`)
	loadEnvCallRe = regexp.MustCompile(`\bloadEnvConfig\b`)
)

// newPreambleSource splits prog's top-level statements. A statement runs from
// its start to the next statement's start; the comment lines just above a
// statement travel with it. configOffset is the offset of the config object:
// the statement holding it, and the default export, are not part of the
// preamble.
func newPreambleSource(src string, prog *ast.Program, configOffset int) *preambleSource {
	if prog == nil {
		return nil
	}
	body := prog.Body
	starts := make([]int, len(body))
	for i, s := range body {
		starts[i] = s.Pos().Offset
		if i > 0 && starts[i] < starts[i-1] {
			return nil
		}
	}
	ps := &preambleSource{}
	carry := ""
	prevRest := ""
	for i, s := range body {
		end := len(src)
		if i+1 < len(body) {
			end = starts[i+1]
		}
		if starts[i] < 0 || starts[i] > end || end > len(src) {
			return nil
		}
		raw := src[starts[i]:end]
		text, tail := splitTrailingComments(raw)
		st := preStmt{text: strings.TrimRight(carry+text, " \t\r\n")}
		st.blankBefore = leadingBlankLine(prevRest)
		carry = tail
		prevRest = raw[len(text):]
		holdsConfig := configOffset >= starts[i] && configOffset < end
		switch n := s.(type) {
		case *ast.ImportStmt:
			if typeImportRe.MatchString(text) || droppedImportSource(n.Source) {
				break
			}
			st.kind = stmtImport
			if n.Default != "" {
				st.names = append(st.names, n.Default)
			}
			if n.Namespace != "" {
				st.names = append(st.names, n.Namespace)
			}
			for _, ni := range n.Named {
				st.names = append(st.names, ni.Local)
			}
		case *ast.ExportStmt:
			if n.Default || holdsConfig || n.Declaration == nil {
				break
			}
			st.kind, st.names = declKind(n.Declaration)
		case *ast.VarStmt, *ast.FnDecl, *ast.ClassDecl:
			if holdsConfig {
				break
			}
			st.kind, st.names = declKind(s)
		default:
			if holdsConfig || loadEnvCallRe.MatchString(text) || typeOnlyDecl.MatchString(text) {
				break
			}
			st.kind = stmtEffect
		}
		if st.kind == stmtDecl && (typeOnlyDecl.MatchString(text) || loadEnvCallRe.MatchString(text)) {
			st.kind = stmtSkip
		}
		if st.kind != stmtSkip {
			st.idents = identifiers(st.text)
		}
		ps.stmts = append(ps.stmts, st)
	}
	return ps
}

// leadingBlankLine reports whether s, the text after a statement, has a blank
// line before its first non-space character.
func leadingBlankLine(s string) bool {
	nl := 0
	for _, r := range s {
		switch r {
		case '\n':
			nl++
		case ' ', '\t', '\r':
		default:
			return nl >= 2
		}
	}
	return nl >= 2
}

func declKind(s ast.Stmt) (stmtKind, []string) {
	switch d := s.(type) {
	case *ast.VarStmt:
		var names []string
		for _, decl := range d.Decls {
			if decl.Name != "" {
				names = append(names, decl.Name)
			}
			names = append(names, decl.Names...)
			if decl.RestName != "" {
				names = append(names, decl.RestName)
			}
		}
		return stmtDecl, names
	case *ast.FnDecl:
		return stmtDecl, []string{d.Name}
	case *ast.ClassDecl:
		return stmtDecl, []string{d.Name}
	}
	return stmtSkip, nil
}

// splitTrailingComments separates the comment-only lines that end raw (they
// belong to the next statement) from the statement text.
func splitTrailingComments(raw string) (text, tail string) {
	lines := strings.Split(raw, "\n")
	cut := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "//") || (strings.HasPrefix(l, "/*") && strings.HasSuffix(l, "*/")) {
			cut = i
			continue
		}
		break
	}
	return strings.Join(lines[:cut], "\n"), joinTail(lines[cut:])
}

func joinTail(lines []string) string {
	t := strings.TrimSpace(strings.Join(lines, "\n"))
	if t == "" {
		return ""
	}
	return t + "\n"
}

// declaredIn reports whether a top-level statement other than the config
// declares name.
func (ps *preambleSource) declares(name string) bool {
	if ps == nil {
		return false
	}
	for _, st := range ps.stmts {
		if st.kind != stmtDecl && st.kind != stmtImport {
			continue
		}
		for _, n := range st.names {
			if n == name {
				return true
			}
		}
	}
	return false
}

// mutatedElsewhere reports whether a top-level statement that is not the
// declaration of name and not an import mentions it.
func (ps *preambleSource) mutatedElsewhere(name string) bool {
	if ps == nil {
		return false
	}
	for _, st := range ps.stmts {
		if st.kind == stmtSkip || st.kind == stmtImport || !st.idents[name] {
			continue
		}
		own := false
		if st.kind == stmtDecl {
			for _, n := range st.names {
				own = own || n == name
			}
		}
		if !own {
			return true
		}
	}
	return false
}

// build returns the imports and statements that the code texts need, in
// source order: first the imports, then the declarations and the statements
// that mutate them.
func (ps *preambleSource) build(texts []string) string {
	if ps == nil || len(texts) == 0 {
		return ""
	}
	needed := map[string]bool{}
	for _, t := range texts {
		for id := range identifiers(t) {
			needed[id] = true
		}
	}
	include := make([]bool, len(ps.stmts))
	declared := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for i, st := range ps.stmts {
			if include[i] {
				continue
			}
			take := false
			switch st.kind {
			case stmtDecl:
				for _, n := range st.names {
					take = take || needed[n]
				}
			case stmtEffect:
				for id := range st.idents {
					take = take || declared[id]
				}
			}
			if !take {
				continue
			}
			include[i], changed = true, true
			if st.kind == stmtDecl {
				for _, n := range st.names {
					declared[n] = true
				}
			}
			for id := range st.idents {
				needed[id] = true
			}
		}
	}
	var imports []string
	var rest strings.Builder
	for i, st := range ps.stmts {
		switch {
		case st.kind == stmtImport:
			for _, n := range st.names {
				if needed[n] {
					imports = append(imports, st.text)
					break
				}
			}
		case include[i]:
			if rest.Len() > 0 {
				rest.WriteString("\n")
				if st.blankBefore {
					rest.WriteString("\n")
				}
			}
			rest.WriteString(st.text)
		}
	}
	var parts []string
	if len(imports) > 0 {
		parts = append(parts, strings.Join(imports, "\n"))
	}
	if rest.Len() > 0 {
		parts = append(parts, rest.String())
	}
	return strings.Join(parts, "\n\n")
}

// names returns every name the preamble build(texts) binds, for conflict
// checks when merging into an existing config.
func (ps *preambleSource) boundNames(texts []string) []string {
	if ps == nil {
		return nil
	}
	built := ps.build(texts)
	if built == "" {
		return nil
	}
	seen := map[string]bool{}
	for _, st := range ps.stmts {
		if st.kind != stmtDecl && st.kind != stmtImport {
			continue
		}
		if strings.Contains(built, st.text) {
			for _, n := range st.names {
				seen[n] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// codeMarkerRe finds the placeholders that stand for code pieces in encoded
// JSON. A marker is alone on its line, as an array element.
var codeMarkerRe = regexp.MustCompile(`(?m)^([ \t]*)"__krate_code_(\d+)__",?[ \t]*$`)

func codeMarker(i int) string { return "__krate_code_" + strconv.Itoa(i) + "__" }

// expandCodeMarkers replaces each marker line with the piece's code, or with
// a comment when the piece is disabled. pieces is indexed by marker number.
func expandCodeMarkers(body string, pieces []*codePiece) string {
	return codeMarkerRe.ReplaceAllStringFunc(body, func(m string) string {
		sub := codeMarkerRe.FindStringSubmatch(m)
		idx, _ := strconv.Atoi(sub[2])
		indent := sub[1]
		if idx >= len(pieces) {
			return m
		}
		p := pieces[idx]
		comma := ""
		if strings.HasSuffix(strings.TrimSpace(m), ",") {
			comma = ","
		}
		if !p.active() {
			if p.Disabled == "" {
				return ""
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s// krate migrate: %s was not carried over; it failed to evaluate here:\n", indent, p.Label)
			for _, l := range strings.Split(strings.TrimSpace(p.Disabled), "\n") {
				fmt.Fprintf(&b, "%s//   %s\n", indent, strings.TrimRight(l, " \t\r"))
			}
			for _, l := range strings.Split(dedent(p.Text), "\n") {
				fmt.Fprintf(&b, "%s// %s\n", indent, strings.TrimRight(l, " \t\r"))
			}
			return strings.TrimRight(b.String(), "\n")
		}
		return indent + indentLines(dedent(p.Text), indent) + comma
	})
}

// activeTexts lists the code of the pieces that will be emitted.
func activeTexts(pieces []*codePiece) []string {
	var out []string
	for _, p := range pieces {
		if p.active() {
			out = append(out, p.Text)
		}
	}
	return out
}

// codePieces lists the code pieces in marker order: header rules, then
// images.remotePatterns entries.
func (n nextConfigData) codePieces() []*codePiece {
	var out []*codePiece
	for _, r := range n.Headers {
		if r.Code != nil {
			out = append(out, r.Code)
		}
	}
	return append(out, n.ImageCode...)
}

// headersValue is the headers array to encode: static rules as they are, and
// a marker line for each code piece.
func (n nextConfigData) headersValue() []any {
	var out []any
	idx := 0
	for _, r := range n.Headers {
		if r.Code != nil {
			if r.Code.Disabled != "" && r.Code.Fallback != nil {
				out = append(out, *r.Code.Fallback)
			} else {
				out = append(out, codeMarker(idx))
			}
			idx++
			continue
		}
		out = append(out, r)
	}
	return out
}

// imagesValue is the images object to encode, or nil when there is none.
// Code pieces join the static remotePatterns as marker lines.
func (n nextConfigData) imagesValue() map[string]any {
	if len(n.Images) == 0 && len(n.ImageCode) == 0 {
		return nil
	}
	out := map[string]any{}
	for k, v := range n.Images {
		out[k] = v
	}
	if len(n.ImageCode) == 0 {
		return out
	}
	base := 0
	for _, r := range n.Headers {
		if r.Code != nil {
			base++
		}
	}
	var list []any
	if static, ok := n.Images["remotePatterns"].([]map[string]string); ok {
		for _, p := range static {
			list = append(list, p)
		}
	}
	for i := range n.ImageCode {
		list = append(list, codeMarker(base+i))
	}
	out["remotePatterns"] = list
	return out
}

// configVerifyEnv is the process.env krate.config.ts sees when the migrator
// checks it: the shell plus the project's .env files (from envDir when set),
// as a production build would load them.
func configVerifyEnv(root, envDir string) map[string]string {
	dir := root
	if envDir != "" {
		dir = filepath.Join(root, filepath.FromSlash(envDir))
	}
	files, err := environ.Load(dir, "production")
	if err != nil {
		files = nil
	}
	return environ.Merge(files)
}

// coreImportRe matches the defineConfig import. @krate/core is not installed in
// the project being migrated, so the check evaluates with an identity stand-in.
var coreImportRe = regexp.MustCompile(`import \{ defineConfig \} from ['"]@krate/core['"];?`)

// verifyCarried evaluates the generated config with the real config
// evaluator. When that fails, each code piece is tried alone; the ones that
// fail are disabled (written as comments) and reported as manual items. It
// returns the config the final text evaluates to, or nil when the final text
// could not be evaluated. render builds the config text from the current
// state of the pieces.
func verifyCarried(root string, pieces []*codePiece, env map[string]string, render func() ([]byte, error)) ([]ManualItem, *config.Config) {
	eval := func() (*config.Config, error) {
		content, err := render()
		if err != nil {
			return nil, err
		}
		if len(content) == 0 {
			return nil, nil
		}
		return config.EvaluateSource(root, coreImportRe.ReplaceAllString(string(content), "const defineConfig = (c: any) => c;"), env)
	}
	cfg, err := eval()
	if err == nil {
		return nil, cfg
	}
	failed := map[*codePiece]string{}
	for _, p := range pieces {
		for _, q := range pieces {
			q.tempOff = q != p
		}
		if _, perr := eval(); perr != nil {
			failed[p] = errorSummary(perr)
		}
	}
	for _, q := range pieces {
		q.tempOff = false
	}
	for p, msg := range failed {
		p.Disabled = msg
	}
	cfg, err = eval()
	if err != nil {
		msg := errorSummary(err)
		for _, p := range pieces {
			if p.Disabled == "" {
				p.Disabled = msg
			}
		}
		cfg, err = eval()
		if err != nil {
			cfg = nil
		}
	}
	var items []ManualItem
	for _, p := range pieces {
		if p.Disabled == "" {
			continue
		}
		item := ManualItem{
			Source: "krate.config.ts",
			Reason: fmt.Sprintf("next.config %s could not be carried into krate.config.ts and is commented out there: %s", p.Label, p.Disabled),
			Hint:   "krate.config.ts is evaluated by esbuild and an embedded QuickJS, so it cannot import Next.js packages (next/server, next/headers) or Node-only modules the evaluator does not shim. Move the value into a plain module and import it, or write the rule by hand",
		}
		switch {
		case p.Fallback != nil:
			item.Reason += " (its static headers were translated instead)"
		case strings.HasPrefix(p.Label, "headers"):
			item.Hint += ". Until it runs, `krate serve` does not send these headers"
		}
		items = append(items, item)
	}
	return items, cfg
}

// errorSummary is the first lines of an evaluation error, without the
// "evaluating config:" wrapper.
func errorSummary(err error) string {
	msg := strings.TrimPrefix(err.Error(), "evaluating config: ")
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(msg), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "at ") || l == "bundling entry:" {
			continue
		}
		lines = append(lines, l)
	}
	if len(lines) > 3 {
		lines = lines[:3]
	}
	return strings.Join(lines, "\n")
}

var securityOnlyHeaders = []string{
	"content-security-policy", "content-security-policy-report-only", "x-frame-options", "x-content-type-options",
	"permissions-policy", "referrer-policy", "cross-origin-opener-policy", "cross-origin-embedder-policy", "cross-origin-resource-policy",
}

// missingHSTS reports whether the header rules set security headers but no
// Strict-Transport-Security.
func missingHSTS(rules []map[string]string) bool {
	security := false
	for _, headers := range rules {
		for k := range headers {
			lk := strings.ToLower(k)
			if lk == "strict-transport-security" {
				return false
			}
			for _, name := range securityOnlyHeaders {
				security = security || lk == name
			}
		}
	}
	return security
}

const hstsInfo = `Krate also sends HSTS for https baseUrl; set security.hsts: "off" to match Next exactly`
