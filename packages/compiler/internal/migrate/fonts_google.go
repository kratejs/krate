package migrate

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// googleFontSpec is the literal option set of one next/font/google call.
type googleFontSpec struct {
	family   string // display name, e.g. "Noto Sans Display"
	weights  []string
	styles   []string
	display  string
	variable string
	fallback []string
}

type googleFontOut struct {
	spec  googleFontSpec
	id    string
	url   string
	class string
	// Rendered literal that replaces the call.
	literal string
}

type googleFonts struct {
	byKey map[string]*googleFontOut
	ids   map[string]bool
	order []*googleFontOut
}

func newGoogleFonts() *googleFonts {
	return &googleFonts{byKey: map[string]*googleFontOut{}, ids: map[string]bool{}}
}

var googleWeightOK = regexp.MustCompile(`^\d{1,4}$`)

// googleFamilyName turns a next/font/google export name (Noto_Sans_Display) into
// the family name Google Fonts uses (Noto Sans Display).
func googleFamilyName(ident string) string {
	return strings.ReplaceAll(ident, "_", " ")
}

func litStrings(v any) ([]string, bool) {
	switch x := v.(type) {
	case nil:
		return nil, true
	case string:
		return []string{x}, true
	case float64:
		return []string{strconv.FormatFloat(x, 'f', -1, 64)}, true
	case []any:
		var out []string
		for _, it := range x {
			s, ok := litStrings(it)
			if !ok || len(s) != 1 {
				return nil, false
			}
			out = append(out, s[0])
		}
		return out, true
	}
	return nil, false
}

// parseGoogleFontCall reads the options object of a next/font/google call.
func parseGoogleFontCall(family, arg string) (googleFontSpec, error) {
	spec := googleFontSpec{family: family}
	var obj map[string]any
	if strings.TrimSpace(arg) == "" {
		obj = map[string]any{}
	} else {
		p := &litParser{s: arg}
		v, err := p.value()
		if err != nil {
			return spec, err
		}
		var ok bool
		if obj, ok = v.(map[string]any); !ok {
			return spec, fmt.Errorf("the argument is not an object literal")
		}
	}
	weights, ok := litStrings(obj["weight"])
	if !ok {
		return spec, fmt.Errorf("weight must be a string, number or array of them")
	}
	for _, w := range weights {
		if w == "variable" {
			continue
		}
		if !googleWeightOK.MatchString(w) {
			return spec, fmt.Errorf("unsupported weight %q", w)
		}
		spec.weights = append(spec.weights, w)
	}
	styles, ok := litStrings(obj["style"])
	if !ok {
		return spec, fmt.Errorf("style must be a string or array of strings")
	}
	for _, s := range styles {
		if s != "normal" && s != "italic" {
			return spec, fmt.Errorf("unsupported style %q", s)
		}
		spec.styles = append(spec.styles, s)
	}
	if d, ok := obj["display"].(string); ok {
		if !cssDisplayOK.MatchString(d) {
			return spec, fmt.Errorf("unsupported display %q", d)
		}
		spec.display = d
	}
	if v, ok := obj["variable"].(string); ok {
		if !cssVarNameOK.MatchString(v) {
			return spec, fmt.Errorf("unsupported variable name %q", v)
		}
		spec.variable = v
	}
	if fb, ok := obj["fallback"].([]any); ok {
		for _, x := range fb {
			s, ok := x.(string)
			if !ok || !cssFamilyOK.MatchString(s) {
				return spec, fmt.Errorf("unsupported fallback font")
			}
			spec.fallback = append(spec.fallback, s)
		}
	}
	return spec, nil
}

// googleCSSURL builds the css2 URL for spec. A font with no weight asks for no
// weight axis, so Google serves its default (the full range for variable fonts
// is not known to the migrator).
func googleCSSURL(s googleFontSpec) string {
	display := s.display
	if display == "" {
		display = "swap"
	}
	hasItalic, hasNormal := false, false
	for _, st := range s.styles {
		if st == "italic" {
			hasItalic = true
		} else {
			hasNormal = true
		}
	}
	if !hasItalic {
		hasNormal = true
	}
	seen := map[string]bool{}
	var weights []string
	for _, w := range s.weights {
		if !seen[w] {
			seen[w] = true
			weights = append(weights, w)
		}
	}
	sort.Slice(weights, func(i, j int) bool {
		a, _ := strconv.Atoi(weights[i])
		b, _ := strconv.Atoi(weights[j])
		return a < b
	})
	fam := url.QueryEscape(s.family)
	switch {
	case len(weights) == 0 && hasItalic && hasNormal:
		fam += ":ital@0;1"
	case len(weights) == 0 && hasItalic:
		fam += ":ital@1"
	case len(weights) > 0 && !hasItalic:
		fam += ":wght@" + strings.Join(weights, ";")
	case len(weights) > 0:
		var tuples []string
		for _, ital := range []string{"0", "1"} {
			if (ital == "0" && !hasNormal) || (ital == "1" && !hasItalic) {
				continue
			}
			for _, w := range weights {
				tuples = append(tuples, ital+","+w)
			}
		}
		fam += ":ital,wght@" + strings.Join(tuples, ";")
	}
	return "https://fonts.googleapis.com/css2?family=" + fam + "&display=" + display
}

func (g *googleFonts) add(spec googleFontSpec) *googleFontOut {
	key := fmt.Sprintf("%#v", spec)
	if out, ok := g.byKey[key]; ok {
		return out
	}
	id := fontSlug(strings.TrimPrefix(spec.variable, "--font-"))
	if spec.variable == "" || id == "" {
		id = fontSlug(spec.family)
	}
	base := id
	for n := 2; g.ids[id]; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	g.ids[id] = true
	out := &googleFontOut{spec: spec, id: id, url: googleCSSURL(spec), class: "krate-font-" + id}
	out.literal = out.makeLiteral()
	g.byKey[key] = out
	g.order = append(g.order, out)
	return out
}

func (o *googleFontOut) fallbackCSS() string {
	fallback := o.spec.fallback
	if len(fallback) == 0 {
		fallback = []string{"ui-sans-serif", "system-ui", "sans-serif"}
		if strings.Contains(o.spec.family, "Mono") || strings.Contains(o.spec.family, "Code") {
			fallback = []string{"ui-monospace", "SFMono-Regular", "Menlo", "monospace"}
		}
	}
	var fb []string
	for _, n := range fallback {
		if strings.Contains(n, " ") {
			n = `"` + n + `"`
		}
		fb = append(fb, n)
	}
	return strings.Join(fb, ", ")
}

func (o *googleFontOut) makeLiteral() string {
	style := "fontFamily: " + strconv.Quote(`"`+o.spec.family+`", `+o.fallbackCSS())
	if len(o.spec.weights) == 1 {
		style += ", fontWeight: " + strconv.Quote(o.spec.weights[0])
	}
	if len(o.spec.styles) == 1 && o.spec.styles[0] != "normal" {
		style += ", fontStyle: " + strconv.Quote(o.spec.styles[0])
	}
	variable := `""`
	if o.spec.variable != "" {
		variable = strconv.Quote(o.class + "-variable")
	}
	return "{ className: " + strconv.Quote(o.class) + ", variable: " + variable + ", style: { " + style + " } }"
}

// css renders the rules for o.
func (o *googleFontOut) css() string {
	var b strings.Builder
	fmt.Fprintf(&b, ".%s { font-family: \"%s\", %s; }\n", o.class, o.spec.family, o.fallbackCSS())
	if o.spec.variable != "" {
		fmt.Fprintf(&b, ".%s-variable { %s: \"%s\", %s; }\n", o.class, o.spec.variable, o.spec.family, o.fallbackCSS())
	}
	return b.String()
}

// rewriteCalls replaces every literal call of the imported google fonts in src
// with a className/variable/style object. bindings maps local name to export
// name. It returns the new source and the local names that still have a call
// or other use that needs the runtime helper.
func (g *googleFonts) rewriteCalls(rep *Report, source, src string, bindings map[string]string) (string, map[string]string) {
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	imp := fontGoogleImport.FindAllStringIndex(src, -1)
	inImport := func(i int) bool {
		for _, r := range imp {
			if i >= r[0] && i < r[1] {
				return true
			}
		}
		return false
	}
	replaced := map[string]int{}
	names := make([]string, 0, len(bindings))
	for local := range bindings {
		names = append(names, local)
	}
	sort.Strings(names)
	for _, local := range names {
		callRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(local) + `\s*\(`)
		for _, loc := range callRe.FindAllStringIndex(src, -1) {
			if inImport(loc[0]) || (loc[0] > 0 && (src[loc[0]-1] == '.' || src[loc[0]-1] == '$')) {
				continue
			}
			open := loc[1] - 1
			closeIdx := matchParen(src, open)
			if closeIdx < 0 {
				continue
			}
			spec, err := parseGoogleFontCall(googleFamilyName(bindings[local]), src[open+1:closeIdx])
			if err != nil {
				rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "next/font/google call could not be migrated: " + err.Error(), Hint: "the generated helper ignores its options; set the family, weights and CSS variable in src/migrated/next-fonts.css by hand"})
				continue
			}
			edits = append(edits, edit{loc[0], closeIdx + 1, g.add(spec).literal})
			replaced[local]++
		}
	}
	// A binding with uses beyond its replaced calls still needs the helper.
	rest := fontGoogleImport.ReplaceAllString(src, "")
	left := map[string]string{}
	for local, exp := range bindings {
		uses := len(regexp.MustCompile(`\b`+regexp.QuoteMeta(local)+`\b`).FindAllStringIndex(rest, -1))
		if uses > replaced[local] {
			left[local] = exp
		}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		src = src[:e.start] + e.text + src[e.end:]
	}
	return src, left
}
