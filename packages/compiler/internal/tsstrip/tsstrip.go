// Package tsstrip removes TypeScript syntax from page/component sources before
// Krate's own lexer and parser see them.
//
// Krate's parser only understands JavaScript + JSX and skips type syntax by
// counting tokens, which misparses enums, namespaces, parameter properties,
// decorators, nested generics (`Map<string, Set<string>>`), generic arrows and
// more. Strip runs esbuild's TypeScript transform with JSX preserved, so the
// output is plain modern JS + JSX with types removed and TS-only runtime
// constructs (enums, namespaces, parameter properties, decorators) lowered.
//
// esbuild reprints the code, so positions change. Parse maps every AST
// position and parser diagnostic back to the original source through the
// source map esbuild emits, and esbuild's own syntax errors are reported as
// diag.Diagnostics against the original file.
package tsstrip

import (
	"crypto/sha256"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// Options carries the project tsconfig compilerOptions that change how esbuild
// lowers TypeScript. The zero value matches TypeScript's defaults for an
// ESNext target (standard decorators, define semantics for class fields).
type Options struct {
	ExperimentalDecorators  bool
	UseDefineForClassFields *bool
}

func (o Options) tsconfigRaw() string {
	co := map[string]any{
		// Keep every value import, even one only referenced from JSX or by
		// Krate's own rewrites; only `import type` and inline `type`
		// specifiers are removed.
		"verbatimModuleSyntax": true,
	}
	if o.ExperimentalDecorators {
		co["experimentalDecorators"] = true
	}
	if o.UseDefineForClassFields != nil {
		co["useDefineForClassFields"] = *o.UseDefineForClassFields
	}
	b, _ := json.Marshal(map[string]any{"compilerOptions": co})
	return string(b)
}

// Applies reports whether path is a TypeScript source that Strip handles.
func Applies(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx", ".mts", ".cts":
		return true
	}
	return false
}

func loaderFor(path string) api.Loader {
	if strings.EqualFold(filepath.Ext(path), ".tsx") {
		return api.LoaderTSX
	}
	return api.LoaderTS
}

// utf8BOM is the byte-order mark esbuild drops before parsing.
const utf8BOM = "\xef\xbb\xbf"

// Result is the stripped code plus what is needed to map positions in it back
// to the original source. A Result is immutable and safe for concurrent use.
type Result struct {
	// Code is plain JS (+ JSX for .tsx inputs).
	Code string

	path string
	gen  *textIndex
	orig *textIndex // original source without a leading BOM
	full *textIndex // original source as given
	bom  bool
	sm   *sourceMap
}

// Error reports esbuild syntax errors as Krate diagnostics.
type Error struct {
	Diags []diag.Diagnostic
}

func (e *Error) Error() string { return diag.FormatDiagnostics(e.Unwrap()) }

// Unwrap exposes each diagnostic so build error reporting can pick them up.
func (e *Error) Unwrap() []error {
	out := make([]error, len(e.Diags))
	for i, d := range e.Diags {
		out[i] = d
	}
	return out
}

type cacheEntry struct {
	res *Result
	err *Error
}

// maxCacheEntries bounds the process-wide cache; the dev watcher keeps one
// process alive across many rebuilds and edits.
const maxCacheEntries = 2048

var (
	cacheMu sync.Mutex
	cache   = map[[32]byte]cacheEntry{}
)

func cacheKey(path, src string, opts Options) [32]byte {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write([]byte(opts.tsconfigRaw()))
	h.Write([]byte{0})
	h.Write([]byte(src))
	var k [32]byte
	copy(k[:], h.Sum(nil))
	return k
}

// Strip removes TypeScript syntax from src. Results are cached by content, so
// stripping the same file repeatedly (several pages importing one component,
// dev rebuilds) costs one esbuild transform.
func Strip(path, src string, opts Options) (*Result, error) {
	key := cacheKey(path, src, opts)
	cacheMu.Lock()
	e, ok := cache[key]
	cacheMu.Unlock()
	if !ok {
		e = strip(path, src, opts)
		cacheMu.Lock()
		if len(cache) >= maxCacheEntries {
			for k := range cache {
				delete(cache, k)
				if len(cache) < maxCacheEntries/2 {
					break
				}
			}
		}
		cache[key] = e
		cacheMu.Unlock()
	}
	if e.err != nil {
		return nil, e.err
	}
	return e.res, nil
}

func strip(path, src string, opts Options) cacheEntry {
	body := src
	bom := strings.HasPrefix(src, utf8BOM)
	if bom {
		body = src[len(utf8BOM):]
	}
	r := api.Transform(body, api.TransformOptions{
		Loader: loaderFor(path),
		JSX:    api.JSXPreserve,
		Target: api.ESNext,
		// Krate's parser has no decorator syntax, so standard (TC39)
		// decorators are lowered like TypeScript's own emit; nothing else is.
		Supported:      map[string]bool{"decorators": false},
		Format:         api.FormatDefault,
		Charset:        api.CharsetUTF8,
		LegalComments:  api.LegalCommentsInline,
		Sourcemap:      api.SourceMapExternal,
		SourcesContent: api.SourcesContentExclude,
		Sourcefile:     path,
		TsconfigRaw:    opts.tsconfigRaw(),
		LogLevel:       api.LogLevelSilent,
	})
	full := newTextIndex(src)
	if len(r.Errors) > 0 {
		orig := newTextIndex(body)
		out := &Error{}
		for _, m := range r.Errors {
			out.Diags = append(out.Diags, messageDiag(path, m, orig, full, bom))
		}
		return cacheEntry{err: out}
	}
	var raw struct {
		Mappings string `json:"mappings"`
	}
	if err := json.Unmarshal(r.Map, &raw); err != nil {
		return cacheEntry{err: internalErr(path, err)}
	}
	sm, err := decodeMappings(raw.Mappings)
	if err != nil {
		return cacheEntry{err: internalErr(path, err)}
	}
	code := string(r.Code)
	return cacheEntry{res: &Result{
		Code: code,
		path: path,
		gen:  newTextIndex(code),
		orig: newTextIndex(body),
		full: full,
		bom:  bom,
		sm:   sm,
	}}
}

func internalErr(path string, err error) *Error {
	return &Error{Diags: []diag.Diagnostic{diag.New(path, 0, 0,
		"stripping TypeScript failed: "+err.Error(),
		"this is a Krate bug; please report it with the file that triggered it", "")}}
}

// messageDiag converts an esbuild error to a diagnostic at the original
// file:line:col (1-based rune column).
func messageDiag(path string, m api.Message, orig, full *textIndex, bom bool) diag.Diagnostic {
	hint := "Krate strips TypeScript with esbuild before compiling; fix the TypeScript/JSX syntax at this location"
	for _, n := range m.Notes {
		if n.Text != "" {
			hint = n.Text
			break
		}
	}
	d := diag.New(path, 0, 0, m.Text, hint, "")
	if m.Location == nil {
		return d
	}
	loc := m.Location
	// esbuild reports a 1-based line and a 0-based byte column.
	off := 0
	if ln := loc.Line - 1; ln >= 0 && ln < len(orig.es) {
		off = orig.es[ln] + loc.Column
		if off > len(orig.s) {
			off = len(orig.s)
		}
	} else {
		off = len(orig.s)
	}
	if bom {
		off += len(utf8BOM)
	}
	line, col := full.kratePos(off)
	d.Line, d.Col = line, col+1
	d.Source = full.line(line)
	return d
}

// line returns the text of 1-based '\n' line n without its terminator.
func (t *textIndex) line(n int) string {
	if n < 1 || n > len(t.nl) {
		return ""
	}
	end := len(t.s)
	if n < len(t.nl) {
		end = t.nl[n] - 1
	}
	return strings.TrimSuffix(t.s[t.nl[n-1]:end], "\r")
}

// Pos maps a position in Code to the original source. line and col follow
// Krate's lexer convention (1-based line split on '\n', column counted in
// runes); col may be the column of any character of a token, and the result
// is the column of the matching character in the original. A zero position is
// returned unchanged.
func (r *Result) Pos(line, col int) (int, int) {
	if line <= 0 {
		return line, col
	}
	p := r.gen.krateOffset(line, col)
	o := r.mapOffset(p)
	if r.bom {
		o += len(utf8BOM)
	}
	return r.full.kratePos(o)
}

// mapOffset maps a byte offset in Code to a byte offset in the BOM-less
// original. The character just before p identifies the token, so positions
// recorded at either the start or the end of a token map consistently.
func (r *Result) mapOffset(p int) int {
	if p <= 0 {
		return r.mapChar(0)
	}
	_, n := utf8.DecodeLastRuneInString(r.Code[:p])
	q := p - n
	oq := r.mapChar(q)
	if oq >= len(r.orig.s) {
		return len(r.orig.s)
	}
	if r.Code[q] == '\n' {
		// A position just past a newline stays at the start of a line.
		if i := strings.IndexByte(r.orig.s[oq:], '\n'); i >= 0 {
			return oq + i + 1
		}
		return len(r.orig.s)
	}
	_, n = utf8.DecodeRuneInString(r.orig.s[oq:])
	return oq + n
}

// mapChar maps the byte offset of one character in Code to the original.
func (r *Result) mapChar(q int) int {
	gl, gc := r.gen.esPos(q)
	segLn, k, ok := r.sm.lookup(gl, gc)
	if !ok {
		return 0
	}
	seg := r.sm.lines[segLn][k]
	segGen := r.gen.esOffset(segLn, seg.genCol)
	segOrig := r.orig.esOffset(seg.origLn, seg.origCol)
	// esbuild places a statement's mapping before its indentation.
	if segOrig < len(r.orig.s) && !isSpace(r.orig.s[segOrig]) {
		for segGen < q && isSpace(r.Code[segGen]) {
			segGen++
		}
	}
	d := q - segGen
	if d <= 0 {
		return segOrig
	}
	// Text esbuild printed verbatim (identifiers, JSX text, template chunks)
	// maps by byte distance, even across lines.
	if segOrig+d <= len(r.orig.s) && r.Code[segGen:q] == r.orig.s[segOrig:segOrig+d] {
		return segOrig + d
	}
	// Text esbuild inserted at the start of a segment (`/* @__PURE__ */ new`)
	// still ends like the original, so align from the following segment.
	if nseg, nLn, ok := r.sm.next(segLn, k); ok {
		nGen := r.gen.esOffset(nLn, nseg.genCol)
		nOrig := r.orig.esOffset(nseg.origLn, nseg.origCol)
		if tail := nGen - q; tail > 0 && nOrig-tail >= segOrig && r.Code[q:nGen] == r.orig.s[nOrig-tail:nOrig] {
			return nOrig - tail
		}
	}
	// A mapping can point past text esbuild kept but re-associated (an
	// `export` keyword mapped to its declaration): find the segment's text
	// on the original line, nearest to the mapped position.
	if segLn == gl {
		_, sz := utf8.DecodeRuneInString(r.Code[q:])
		w := r.Code[segGen : q+sz]
		oln, _ := r.orig.esPos(segOrig)
		ls := r.orig.es[oln]
		le := len(r.orig.s)
		if oln+1 < len(r.orig.es) {
			le = r.orig.es[oln+1]
		}
		best := -1
		for from := ls; from < le; {
			i := strings.Index(r.orig.s[from:le], w)
			if i < 0 {
				break
			}
			at := from + i
			if best < 0 || abs(at-segOrig) < abs(best-segOrig) {
				best = at
			}
			from = at + 1
		}
		if best >= 0 {
			return best + d
		}
	}
	if segLn == gl {
		_, sgc := r.gen.esPos(segGen)
		oln, ocol := r.orig.esPos(segOrig)
		return r.orig.esOffset(oln, ocol+(gc-sgc))
	}
	return segOrig
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// RemapProgram rewrites every ast.Pos reachable from prog from Code
// coordinates to original-source coordinates.
func (r *Result) RemapProgram(prog *ast.Program) {
	if prog == nil {
		return
	}
	remapPositions(prog, func(p ast.Pos) ast.Pos {
		l, c := r.Pos(p.Line, p.Col)
		return ast.Pos{Line: l, Col: c}
	})
}

// RemapErrors maps parser diagnostics (raised against Code) back to the
// original source, replacing the quoted source line.
func (r *Result) RemapErrors(errs []error) []error {
	if len(errs) == 0 {
		return errs
	}
	out := make([]error, len(errs))
	for i, e := range errs {
		switch d := e.(type) {
		case diag.Diagnostic:
			out[i] = r.remapDiag(d)
		case *diag.Diagnostic:
			nd := r.remapDiag(*d)
			out[i] = &nd
		default:
			out[i] = e
		}
	}
	return out
}

func (r *Result) remapDiag(d diag.Diagnostic) diag.Diagnostic {
	if d.Line <= 0 {
		return d
	}
	d.Line, d.Col = r.Pos(d.Line, d.Col)
	d.Source = r.full.line(d.Line)
	return d
}

// Parse lexes and parses a page/component/module source for compilation. For
// TypeScript paths the source is stripped first and all AST positions and
// parser diagnostics are mapped back to the original; other paths are parsed
// as-is. A TypeScript syntax error returns a nil program and its diagnostics.
func Parse(path, src string, opts Options) (*ast.Program, []error) {
	if !Applies(path) {
		p := parser.New(lexer.New(src).Tokenize())
		p.Filename = path
		p.SetSource(src)
		prog := p.ParseProgram()
		return prog, p.Errors()
	}
	res, err := Strip(path, src, opts)
	if err != nil {
		if se, ok := err.(*Error); ok {
			return nil, se.Unwrap()
		}
		return nil, []error{err}
	}
	p := parser.New(lexer.New(res.Code).Tokenize())
	p.Filename = path
	p.SetSource(res.Code)
	prog := p.ParseProgram()
	res.RemapProgram(prog)
	return prog, res.RemapErrors(p.Errors())
}
