package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
)

const publicFontDir = "migrated-fonts"

var (
	geistImport    = regexp.MustCompile(`import\s*(?:type\s+)?\{([^}]*)\}\s*from\s*["']geist/font/(sans|mono)["']\s*;?[ \t]*\r?\n?`)
	cssFamilyOK    = regexp.MustCompile(`^[A-Za-z0-9 _.\-]{1,80}$`)
	cssVarNameOK   = regexp.MustCompile(`^--[A-Za-z0-9_-]{1,80}$`)
	cssWeightOK    = regexp.MustCompile(`^(?:normal|bold|\d{1,4}(?: \d{1,4})?)$`)
	cssStyleOK     = regexp.MustCompile(`^(?:normal|italic|oblique(?: \d{1,3}deg)?)$`)
	cssDisplayOK   = regexp.MustCompile(`^(?:auto|block|swap|fallback|optional)$`)
	cssDeclPropOK  = regexp.MustCompile(`^[a-z][a-z-]{0,60}$`)
	cssDeclValueOK = regexp.MustCompile(`^[^;{}\\<>]{1,200}$`)
)

type fontFace struct {
	file   string // path relative to the project root
	weight string
	style  string
}

type localFontSpec struct {
	faces       []fontFace
	weight      string
	style       string
	variable    string
	display     string
	fallback    []string
	declaration [][2]string
	family      string // preferred family name; derived from the first file when empty
}

// localFontOut is the generated equivalent of one next/font/local call.
type localFontOut struct {
	class, variable, family, fallbackCSS, weight, style string
}

type localFonts struct {
	root    string
	rep     *Report
	byKey   map[string]*localFontOut
	ids     map[string]bool
	files   map[string]string // public name -> content hash
	css     strings.Builder
	manual  map[string]bool
	pending []PlannedFile
}

func newLocalFonts(root string, rep *Report) *localFonts {
	return &localFonts{root: root, rep: rep, byKey: map[string]*localFontOut{}, ids: map[string]bool{}, files: map[string]string{}, manual: map[string]bool{}}
}

func (l *localFonts) used() bool { return l.css.Len() > 0 }

func (l *localFonts) flag(source, reason, hint string) {
	k := source + "\x00" + reason
	if l.manual[k] {
		return
	}
	l.manual[k] = true
	l.rep.Manual = append(l.rep.Manual, ManualItem{Source: source, Reason: reason, Hint: hint})
}

// rewrite replaces next/font/local calls and geist font imports in one file
// with generated className/variable/style objects. It returns the new source
// and whether a stylesheet import is needed.
func (l *localFonts) rewrite(f *PlannedFile, src string) (string, bool) {
	if f.Source == "" || (!strings.Contains(src, "next/font/local") && !strings.Contains(src, "geist/font/")) {
		return src, false
	}
	changed := false
	if m := fontLocalImport.FindStringSubmatchIndex(src); m != nil {
		binding := src[m[2]:m[3]]
		if out, ok := l.rewriteLocalCalls(f, src, binding, m); ok {
			src, changed = out, true
		}
	}
	for {
		m := geistImport.FindStringSubmatchIndex(src)
		if m == nil {
			break
		}
		repl, ok := l.geistImportReplacement(f, src[m[2]:m[3]], src[m[4]:m[5]])
		if !ok {
			// Leave this import alone; mask it so the loop moves on.
			src = src[:m[0]] + strings.Replace(src[m[0]:m[1]], "geist/font/", "geist/font\x00/", 1) + src[m[1]:]
			continue
		}
		src = src[:m[0]] + repl + src[m[1]:]
		changed = true
	}
	src = strings.ReplaceAll(src, "geist/font\x00/", "geist/font/")
	return src, changed
}

// rewriteLocalCalls rewrites every binding(...) call. It removes the import
// only when no other use of the binding remains.
func (l *localFonts) rewriteLocalCalls(f *PlannedFile, src, binding string, imp []int) (string, bool) {
	callRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(binding) + `\s*\(`)
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	failed := false
	for _, loc := range callRe.FindAllStringIndex(src, -1) {
		if loc[0] >= imp[0] && loc[0] < imp[1] {
			continue
		}
		open := loc[1] - 1
		closeIdx := matchParen(src, open)
		if closeIdx < 0 {
			failed = true
			continue
		}
		spec, err := parseLocalFontCall(src[open+1 : closeIdx])
		if err != nil {
			l.flag(f.Source, "next/font/local call could not be migrated: "+err.Error(), "write the @font-face rules for it by hand and replace the call with { className, variable, style }")
			failed = true
			continue
		}
		out, err := l.generate(f.Source, spec)
		if err != nil {
			l.flag(f.Source, "next/font/local call could not be migrated: "+err.Error(), "check the font file paths and options")
			failed = true
			continue
		}
		edits = append(edits, edit{loc[0], closeIdx + 1, out.literal()})
	}
	if len(edits) == 0 {
		return src, false
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		src = src[:e.start] + e.text + src[e.end:]
	}
	if !failed {
		// The import statement is removed when the binding has no other use.
		imp := fontLocalImport.FindStringIndex(src)
		rest := src[:imp[0]] + src[imp[1]:]
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(binding) + `\b`).MatchString(rest) {
			src = rest
		}
	}
	f.Rewrites = append(f.Rewrites, fmt.Sprintf("migrated %d next/font/local call(s) to @font-face CSS and self-hosted font files", len(edits)))
	return src, true
}

// geistImportReplacement turns import { GeistSans } from "geist/font/sans"
// into const declarations backed by the font files shipped in the geist package.
func (l *localFonts) geistImportReplacement(f *PlannedFile, specs, kind string) (string, bool) {
	want, dir, file, variable := "GeistSans", "geist-sans", "Geist-Variable.woff2", "--font-geist-sans"
	family := "Geist"
	var fallback []string
	if kind == "mono" {
		want, dir, file, variable = "GeistMono", "geist-mono", "GeistMono-Variable.woff2", "--font-geist-mono"
		family = "Geist Mono"
		fallback = []string{"ui-monospace", "SFMono-Regular", "Roboto Mono", "Menlo", "Monaco", "Liberation Mono", "DejaVu Sans Mono", "Courier New", "monospace"}
	}
	var decls, keep []string
	for _, s := range strings.Split(specs, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		name, alias, hasAlias := strings.Cut(s, " as ")
		name = strings.TrimSpace(name)
		if name != want {
			keep = append(keep, s)
			continue
		}
		local := name
		if hasAlias {
			local = strings.TrimSpace(alias)
		}
		abs := findGeistFont(l.root, path.Join("dist", "fonts", dir, file))
		if abs == "" {
			l.flag(f.Source, "geist font files were not found in node_modules", "install the geist package, then migrate again")
			return "", false
		}
		spec := localFontSpec{faces: []fontFace{{file: abs}}, weight: "100 900", variable: variable, fallback: fallback, family: family}
		out, err := l.generate("", spec)
		if err != nil {
			l.flag(f.Source, "geist font could not be migrated: "+err.Error(), "install the geist package, then migrate again")
			return "", false
		}
		decls = append(decls, "const "+local+" = "+out.literal()+";\n")
	}
	if len(decls) == 0 {
		return "", false
	}
	var b strings.Builder
	if len(keep) > 0 {
		fmt.Fprintf(&b, "import { %s } from \"geist/font/%s\";\n", strings.Join(keep, ", "), kind)
	}
	for _, d := range decls {
		b.WriteString(d)
	}
	f.Rewrites = append(f.Rewrites, "mapped geist/font/"+kind+" to self-hosted @font-face CSS")
	return b.String(), true
}

func (o *localFontOut) literal() string {
	style := "fontFamily: " + escape.JSStringDQ(o.family+", "+o.fallbackCSS)
	if o.weight != "" {
		style += ", fontWeight: " + escape.JSStringDQ(o.weight)
	}
	if o.style != "" {
		style += ", fontStyle: " + escape.JSStringDQ(o.style)
	}
	vr := escape.JSStringDQ(o.class + "-variable")
	return "{ className: " + escape.JSStringDQ(o.class) + ", variable: " + vr + ", style: { " + style + " } }"
}

// generate validates spec, plans the public font files and appends the CSS.
// source is the file the call lives in (font paths are relative to it); an
// empty source means the paths are already relative to the project root.
func (l *localFonts) generate(source string, spec localFontSpec) (*localFontOut, error) {
	if len(spec.faces) == 0 {
		return nil, fmt.Errorf("no font files in src")
	}
	display := spec.display
	if display == "" {
		display = "swap"
	}
	if !cssDisplayOK.MatchString(display) {
		return nil, fmt.Errorf("unsupported display %q", display)
	}
	for _, v := range []struct {
		re  *regexp.Regexp
		val string
		w   string
	}{{cssWeightOK, spec.weight, "weight"}, {cssStyleOK, spec.style, "style"}} {
		if v.val != "" && !v.re.MatchString(v.val) {
			return nil, fmt.Errorf("unsupported %s %q", v.w, v.val)
		}
	}
	if spec.variable != "" && !cssVarNameOK.MatchString(spec.variable) {
		return nil, fmt.Errorf("unsupported variable name %q", spec.variable)
	}
	for _, d := range spec.declaration {
		if !cssDeclPropOK.MatchString(d[0]) || !cssDeclValueOK.MatchString(d[1]) {
			return nil, fmt.Errorf("unsupported declaration %q", d[0])
		}
	}
	fallback := []string{"ui-sans-serif", "system-ui", "sans-serif"}
	if len(spec.fallback) > 0 {
		fallback = spec.fallback
	}
	var fb []string
	for _, name := range fallback {
		if !cssFamilyOK.MatchString(name) {
			return nil, fmt.Errorf("unsupported fallback font %q", name)
		}
		if strings.ContainsAny(name, " ") {
			name = `"` + name + `"`
		}
		fb = append(fb, name)
	}

	type planned struct {
		url, format, weight, style string
	}
	var faces []planned
	for _, face := range spec.faces {
		abs := face.file
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(l.root, filepath.FromSlash(path.Dir(source)), filepath.FromSlash(face.file))
			if !pluginapi.WithinRoot(l.root, abs) {
				return nil, fmt.Errorf("font file %s is outside the project", face.file)
			}
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("font file %s: %v", face.file, err)
		}
		faceWeight, faceStyle := face.weight, face.style
		if faceWeight == "" {
			faceWeight = spec.weight
		}
		if faceStyle == "" {
			faceStyle = spec.style
		}
		for _, v := range []struct {
			re  *regexp.Regexp
			val string
		}{{cssWeightOK, faceWeight}, {cssStyleOK, faceStyle}} {
			if v.val != "" && !v.re.MatchString(v.val) {
				return nil, fmt.Errorf("unsupported weight or style %q", v.val)
			}
		}
		ext := strings.ToLower(path.Ext(abs))
		format, ok := map[string]string{".woff2": "woff2", ".woff": "woff", ".ttf": "truetype", ".otf": "opentype"}[ext]
		if !ok {
			return nil, fmt.Errorf("unsupported font format %s", ext)
		}
		faces = append(faces, planned{url: l.publicFont(filepath.Base(abs), data), format: format, weight: faceWeight, style: faceStyle})
	}

	family := spec.family
	if family == "" {
		base := path.Base(spec.faces[0].file)
		family = strings.TrimSuffix(base, path.Ext(base))
		family = regexp.MustCompile(`[^A-Za-z0-9]+`).ReplaceAllString(family, " ")
		family = strings.TrimSpace(family)
		if family == "" || !cssFamilyOK.MatchString(family) {
			family = "LocalFont"
		}
	}
	key := fmt.Sprintf("%q|%v|%q|%q|%q|%q|%v|%v", family, faces, spec.variable, display, spec.weight, spec.style, fb, spec.declaration)
	if out, ok := l.byKey[key]; ok {
		return out, nil
	}
	id := fontSlug(strings.TrimPrefix(spec.variable, "--font-"))
	if spec.variable == "" || id == "" {
		id = fontSlug(family)
	}
	base := id
	for n := 2; l.ids[id]; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	l.ids[id] = true
	out := &localFontOut{class: "krate-local-font-" + id, family: `"` + family + `"`, fallbackCSS: strings.Join(fb, ", ")}
	if len(faces) == 1 {
		if !strings.Contains(faces[0].weight, " ") {
			out.weight = faces[0].weight
		}
		out.style = faces[0].style
	}
	for _, fc := range faces {
		fmt.Fprintf(&l.css, "@font-face {\n  font-family: \"%s\";\n  src: url(\"%s\") format(\"%s\");\n", family, fc.url, fc.format)
		if fc.weight != "" {
			fmt.Fprintf(&l.css, "  font-weight: %s;\n", fc.weight)
		}
		if fc.style != "" {
			fmt.Fprintf(&l.css, "  font-style: %s;\n", fc.style)
		}
		fmt.Fprintf(&l.css, "  font-display: %s;\n", display)
		for _, d := range spec.declaration {
			fmt.Fprintf(&l.css, "  %s: %s;\n", d[0], d[1])
		}
		l.css.WriteString("}\n")
	}
	fmt.Fprintf(&l.css, ".%s { font-family: %s, %s; }\n", out.class, out.family, out.fallbackCSS)
	if spec.variable != "" {
		fmt.Fprintf(&l.css, ".%s-variable { %s: %s, %s; }\n", out.class, spec.variable, out.family, out.fallbackCSS)
	}
	l.byKey[key] = out
	return out, nil
}

// findGeistFont looks for a file of the installed geist package in the
// project's node_modules or those of its parent directories (workspaces).
func findGeistFont(root, rel string) string {
	dir := root
	for i := 0; i < 5; i++ {
		cand := filepath.Join(dir, "node_modules", "geist", filepath.FromSlash(rel))
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// publicFont plans data as public/migrated-fonts/<name> and returns its URL.
// Two different files with the same name keep distinct names.
func (l *localFonts) publicFont(name string, data []byte) string {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if prev, ok := l.files[name]; ok && prev != hash {
		ext := path.Ext(name)
		name = strings.TrimSuffix(name, ext) + "-" + hash[:8] + ext
	}
	if _, ok := l.files[name]; !ok {
		l.files[name] = hash
		l.pending = append(l.pending, PlannedFile{Kind: KindPublic, Target: "public/" + publicFontDir + "/" + name, Content: data, Rewrites: []string{"copied font file for @font-face"}})
	}
	return "/" + publicFontDir + "/" + name
}

// matchParen returns the index of the parenthesis closing the one at open, or
// -1. Strings, template literals and comments are skipped.
func matchParen(src string, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch c := src[i]; c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		case '"', '\'', '`':
			for i++; i < len(src) && src[i] != c; i++ {
				if src[i] == '\\' {
					i++
				}
			}
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					i++
				}
			} else if i+1 < len(src) && src[i+1] == '*' {
				end := strings.Index(src[i+2:], "*/")
				if end < 0 {
					return -1
				}
				i += end + 3
			}
		}
	}
	return -1
}

// parseLocalFontCall reads the options object passed to localFont().
func parseLocalFontCall(arg string) (localFontSpec, error) {
	var spec localFontSpec
	p := &litParser{s: arg}
	v, err := p.value()
	if err != nil {
		return spec, err
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return spec, fmt.Errorf("the argument is not an object literal")
	}
	str := func(k string) string {
		switch x := obj[k].(type) {
		case string:
			return x
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
		return ""
	}
	spec.weight, spec.style, spec.variable, spec.display = str("weight"), str("style"), str("variable"), str("display")
	switch src := obj["src"].(type) {
	case string:
		spec.faces = []fontFace{{file: src}}
	case []any:
		for _, it := range src {
			m, ok := it.(map[string]any)
			if !ok {
				return spec, fmt.Errorf("src entries must be { path, weight, style } objects")
			}
			file, _ := m["path"].(string)
			if file == "" {
				return spec, fmt.Errorf("src entry has no path")
			}
			face := fontFace{file: file}
			if w, ok := m["weight"].(string); ok {
				face.weight = w
			} else if w, ok := m["weight"].(float64); ok {
				face.weight = strconv.FormatFloat(w, 'f', -1, 64)
			}
			face.style, _ = m["style"].(string)
			spec.faces = append(spec.faces, face)
		}
	default:
		return spec, fmt.Errorf("src must be a string or an array of { path, weight, style } literals")
	}
	if fb, ok := obj["fallback"].([]any); ok {
		for _, x := range fb {
			s, ok := x.(string)
			if !ok {
				return spec, fmt.Errorf("fallback must list strings")
			}
			spec.fallback = append(spec.fallback, s)
		}
	}
	if ds, ok := obj["declarations"].([]any); ok {
		for _, x := range ds {
			m, ok := x.(map[string]any)
			if !ok {
				return spec, fmt.Errorf("declarations must be { prop, value } objects")
			}
			prop, _ := m["prop"].(string)
			val, _ := m["value"].(string)
			spec.declaration = append(spec.declaration, [2]string{prop, val})
		}
	}
	return spec, nil
}

// litParser reads a JSON-like JS literal: strings, numbers, booleans, null,
// arrays and objects, with comments and trailing commas.
type litParser struct {
	s string
	i int
}

func (p *litParser) skip() {
	for p.i < len(p.s) {
		switch {
		case strings.ContainsRune(" \t\r\n", rune(p.s[p.i])):
			p.i++
		case strings.HasPrefix(p.s[p.i:], "//"):
			for p.i < len(p.s) && p.s[p.i] != '\n' {
				p.i++
			}
		case strings.HasPrefix(p.s[p.i:], "/*"):
			end := strings.Index(p.s[p.i+2:], "*/")
			if end < 0 {
				p.i = len(p.s)
				return
			}
			p.i += end + 4
		default:
			return
		}
	}
}

func (p *litParser) value() (any, error) {
	p.skip()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("unexpected end of options")
	}
	switch c := p.s[p.i]; {
	case c == '"' || c == '\'' || c == '`':
		return p.str()
	case c == '[':
		p.i++
		arr := []any{}
		for {
			p.skip()
			if p.i < len(p.s) && p.s[p.i] == ']' {
				p.i++
				return arr, nil
			}
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
			p.skip()
			if p.i < len(p.s) && p.s[p.i] == ',' {
				p.i++
			}
		}
	case c == '{':
		p.i++
		obj := map[string]any{}
		for {
			p.skip()
			if p.i < len(p.s) && p.s[p.i] == '}' {
				p.i++
				return obj, nil
			}
			key, err := p.key()
			if err != nil {
				return nil, err
			}
			p.skip()
			if p.i >= len(p.s) || p.s[p.i] != ':' {
				return nil, fmt.Errorf("option %q is not a plain key: value pair", key)
			}
			p.i++
			v, err := p.value()
			if err != nil {
				return nil, fmt.Errorf("option %q is not a literal: %v", key, err)
			}
			obj[key] = v
			p.skip()
			if p.i < len(p.s) && p.s[p.i] == ',' {
				p.i++
			}
		}
	case c == '-' || c == '.' || (c >= '0' && c <= '9'):
		start := p.i
		for p.i < len(p.s) && strings.ContainsRune("-+.eE0123456789", rune(p.s[p.i])) {
			p.i++
		}
		n, err := strconv.ParseFloat(p.s[start:p.i], 64)
		if err != nil {
			return nil, err
		}
		return n, nil
	}
	start := p.i
	for p.i < len(p.s) && (p.s[p.i] == '_' || p.s[p.i] == '$' || p.s[p.i] >= 'a' && p.s[p.i] <= 'z' || p.s[p.i] >= 'A' && p.s[p.i] <= 'Z' || p.s[p.i] >= '0' && p.s[p.i] <= '9') {
		p.i++
	}
	switch word := p.s[start:p.i]; word {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null", "undefined":
		return nil, nil
	default:
		return nil, fmt.Errorf("%q is not a literal (an imported or computed value cannot be migrated)", word)
	}
}

func (p *litParser) key() (string, error) {
	p.skip()
	if p.i < len(p.s) && (p.s[p.i] == '"' || p.s[p.i] == '\'') {
		v, err := p.str()
		s, _ := v.(string)
		return s, err
	}
	start := p.i
	for p.i < len(p.s) && (p.s[p.i] == '_' || p.s[p.i] == '$' || p.s[p.i] >= 'a' && p.s[p.i] <= 'z' || p.s[p.i] >= 'A' && p.s[p.i] <= 'Z' || p.s[p.i] >= '0' && p.s[p.i] <= '9') {
		p.i++
	}
	if start == p.i {
		return "", fmt.Errorf("unsupported option syntax (spread or computed key)")
	}
	return p.s[start:p.i], nil
}

func (p *litParser) str() (any, error) {
	q := p.s[p.i]
	p.i++
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != q {
		if p.s[p.i] == '\\' {
			p.i++
		}
		p.i++
	}
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("unterminated string")
	}
	raw := p.s[start:p.i]
	p.i++
	if q == '`' && strings.Contains(raw, "${") {
		return nil, fmt.Errorf("template literal with ${} is not a literal")
	}
	return escape.UnescapeJSString(raw), nil
}
