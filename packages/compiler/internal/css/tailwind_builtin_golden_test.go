package css

import (
	"encoding/json"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// builtinKnownDifferences are classes whose Krate output is written
// differently from Tailwind's but renders the same, with the reason.
var builtinKnownDifferences = map[string]string{
	"blur-none":          "--tw-blur: initial instead of an empty value; both make var(--tw-blur,) fall back to nothing",
	"backdrop-blur-none": "--tw-backdrop-blur: initial instead of an empty value; same fallback",
	"drop-shadow-none":   "--tw-drop-shadow: initial instead of an empty value; same fallback",
	"flex-auto":          "flex: 1 1 auto is the longhand of flex: auto",
	"flex-initial":       "flex: 0 1 auto is the longhand of flex: 0 auto",
	"bg-clip-text":       "Krate also emits -webkit-background-clip: text for older Safari",
	"shadow-[#f00]/50":   "rgb(255 0 0 / 0.5) is the same color as color-mix(in oklab, #f00 50%, transparent)",
	"shadow-sm/[0.4]":    "--tw-shadow-alpha: 40% instead of 0.4, which the <percentage> registration rejects",
	"origin-left":        "Tailwind writes left as 0 (0 center); Krate keeps the keyword",
	"origin-right":       "Tailwind writes right as 100% (100% center); Krate keeps the keyword",
}

// TestBuiltinUtilitiesMatchTailwind compares built-in utilities and variants
// with real Tailwind v4 (testdata/tailwind_builtin_golden.json, produced by
// testdata/tailwind-builtin/gen.mjs). Both sides are canonicalized: nesting
// resolved, theme values inlined, calc() products computed and fallbacks that
// only restate a registered initial value dropped.
func TestBuiltinUtilitiesMatchTailwind(t *testing.T) {
	raw, err := os.ReadFile("testdata/tailwind_builtin_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Tailwind   string
		Properties map[string]struct {
			Syntax  string
			Initial *string
		}
		Classes map[string]string
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	g := NewTailwindGenerator()
	classes := make([]string, 0, len(golden.Classes))
	for cls := range golden.Classes {
		classes = append(classes, cls)
	}
	sort.Strings(classes)
	checked := 0
	for _, cls := range classes {
		if _, known := builtinKnownDifferences[cls]; known {
			continue
		}
		want := golden.Classes[cls]
		got := g.Generate(map[string]bool{cls: true})
		w, k := builtinCanon(want, got, &g.Theme)
		checked++
		if w != k {
			t.Errorf("%s\n  tailwind %s: %s\n  krate:        %s", cls, golden.Tailwind, w, k)
		}
	}
	if checked < 800 {
		t.Fatalf("only %d golden classes checked", checked)
	}

	for name, want := range golden.Properties {
		p, ok := twPropertyByName[name]
		if !ok {
			t.Errorf("@property %s: Tailwind registers it, Krate does not", name)
			continue
		}
		syntax := p.syntax
		if syntax == "" {
			syntax = "*"
		}
		if syntax != want.Syntax {
			t.Errorf("@property %s: syntax %q, Tailwind %q", name, syntax, want.Syntax)
		}
		switch {
		case want.Initial == nil && p.hasInit:
			t.Errorf("@property %s: initial %q, Tailwind has none", name, p.initial)
		case want.Initial != nil && (!p.hasInit || p.initial != *want.Initial):
			t.Errorf("@property %s: initial %q, Tailwind %q", name, p.initial, *want.Initial)
		}
	}
}

func TestBuiltinKnownDifferencesStillDiffer(t *testing.T) {
	raw, err := os.ReadFile("testdata/tailwind_builtin_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct{ Classes map[string]string }
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	g := NewTailwindGenerator()
	for cls := range builtinKnownDifferences {
		want, ok := golden.Classes[cls]
		if !ok {
			t.Errorf("%s is listed as a known difference but is not in the golden file", cls)
			continue
		}
		w, k := builtinCanon(want, g.Generate(map[string]bool{cls: true}), &g.Theme)
		if w == k {
			t.Errorf("%s now matches Tailwind; remove it from builtinKnownDifferences", cls)
		}
	}
}

// builtinCanon canonicalizes Tailwind's and Krate's CSS for one class.
func builtinCanon(tw, kr string, theme *TailwindTheme) (string, string) {
	w := canonCSS(tw, theme)
	k := canonCSS(kr, theme)
	k = dropRestatedFallbacks(w, k)
	return builtinNorm(w), builtinNorm(k)
}

var (
	goldenBareTwVarRe = regexp.MustCompile(`var\((--tw-[a-z0-9-]+)\)`)
	calcProductRe     = regexp.MustCompile(`calc\((-?[0-9.]+)(rem|px|%|em|deg|ms|s|) \* (-?[0-9.]+)\)`)
	calcZeroRe        = regexp.MustCompile(`calc\(0 \* [^()]*(\([^()]*(\([^()]*\))?[^()]*\))?[^()]*\)`)
	calcFractionRe    = regexp.MustCompile(`calc\(([0-9]+)/([0-9]+) \* (-?[0-9.]+)%\)`)
	decimalRe         = regexp.MustCompile(`(^|[ (,:/])(-?[0-9]*\.[0-9]+)(%|rem|px|em|deg|)`)
	zeroUnitRe        = regexp.MustCompile(`(^|[ (,:])-?0(px|rem|%|deg)([ ),;}]|$)`)
	opacityPercentR   = regexp.MustCompile(`((?:^|\{|; )(?:opacity|zoom): )([0-9.]+)%`)
)

// dropRestatedFallbacks removes the var() fallbacks Krate adds on --tw-*
// variables that Tailwind reads without one; the fallback restates the
// registered initial value, for browsers without @property.
func dropRestatedFallbacks(tw, kr string) string {
	bare := map[string]bool{}
	for _, m := range goldenBareTwVarRe.FindAllStringSubmatch(tw, -1) {
		bare[m[1]] = true
	}
	var b strings.Builder
	for i := 0; i < len(kr); {
		j := strings.Index(kr[i:], "var(--tw-")
		if j < 0 {
			b.WriteString(kr[i:])
			break
		}
		j += i
		b.WriteString(kr[i:j])
		n := j + len("var(")
		end := n
		for end < len(kr) && (kr[end] == '-' || kr[end] >= 'a' && kr[end] <= 'z' || kr[end] >= '0' && kr[end] <= '9') {
			end++
		}
		name := kr[n:end]
		if end < len(kr) && kr[end] == ',' && bare[name] {
			depth, k := 1, end
			for k < len(kr) && depth > 0 {
				k++
				switch kr[k] {
				case '(':
					depth++
				case ')':
					depth--
				}
			}
			b.WriteString("var(" + name + ")")
			i = k + 1
			continue
		}
		b.WriteString(kr[j:end])
		i = end
	}
	return b.String()
}

func builtinNum(f float64) string {
	return strconv.FormatFloat(math.Round(f*10000)/10000, 'f', -1, 64)
}

func builtinNorm(s string) string {
	s = strings.ReplaceAll(s, "'", `"`)
	s = strings.ReplaceAll(s, "( ", "(")
	s = strings.ReplaceAll(s, "currentColor", "currentcolor")
	s = strings.ReplaceAll(s, "color-mix(in srgb,", "color-mix(in oklab,")
	s = calcFractionRe.ReplaceAllStringFunc(s, func(m string) string {
		p := calcFractionRe.FindStringSubmatch(m)
		a, _ := strconv.ParseFloat(p[1], 64)
		b, _ := strconv.ParseFloat(p[2], 64)
		c, _ := strconv.ParseFloat(p[3], 64)
		return builtinNum(a/b*c) + "%"
	})
	s = calcProductRe.ReplaceAllStringFunc(s, func(m string) string {
		p := calcProductRe.FindStringSubmatch(m)
		a, _ := strconv.ParseFloat(p[1], 64)
		b, _ := strconv.ParseFloat(p[3], 64)
		return builtinNum(a*b) + p[2]
	})
	s = calcZeroRe.ReplaceAllString(s, "0")
	s = decimalRe.ReplaceAllStringFunc(s, func(m string) string {
		p := decimalRe.FindStringSubmatch(m)
		f, _ := strconv.ParseFloat(p[2], 64)
		return p[1] + builtinNum(f) + p[3]
	})
	s = zeroUnitRe.ReplaceAllString(s, "${1}0$3")
	s = opacityPercentR.ReplaceAllStringFunc(s, func(m string) string {
		p := opacityPercentR.FindStringSubmatch(m)
		f, _ := strconv.ParseFloat(p[2], 64)
		return p[1] + builtinNum(f/100)
	})
	// One entry per (at-rules, selector), declarations merged: Tailwind may
	// split a rule's declarations around a nested @supports.
	var keys []string
	decls := map[string][]string{}
	for _, line := range strings.Split(s, "\n") {
		ats, rule, hasAts := strings.Cut(line, " :: ")
		if !hasAts {
			ats, rule = "", line
		}
		sel, body, _ := strings.Cut(rule, " {")
		body = strings.TrimSuffix(body, "}")
		for _, one := range topLevelSplit(sel, ',') {
			key := ats + " :: " + builtinSelector(strings.TrimSpace(one))
			if _, ok := decls[key]; !ok {
				keys = append(keys, key)
			}
			decls[key] = append(decls[key], body)
		}
	}
	var lines []string
	base := map[string]string{}
	for _, key := range keys {
		body := strings.Join(decls[key], "; ")
		if ats, sel, _ := strings.Cut(key, " :: "); ats == "" {
			base[sel] = body
		}
		lines = append(lines, key+" {"+body+"}")
	}
	// Tailwind's color-mix() polyfill repeats a declaration inside
	// @supports (color: color-mix(in lab, red, red)); once the srgb fallback
	// is normalized the repeat is a no-op.
	const mixSupports = "@supports (color: color-mix(in lab, red, red))"
	out := lines[:0]
	for _, line := range lines {
		ats, rest, _ := strings.Cut(line, " :: ")
		if ats == mixSupports {
			sel, body, _ := strings.Cut(rest, " {")
			if b, ok := base[sel]; ok && strings.Contains(b, strings.TrimSuffix(body, "}")) {
				continue
			}
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// builtinSelector rewrites Tailwind's `:is(<ctx> .cls)` group/peer form to
// Krate's equivalent `.cls:is(<ctx> *)` (same specificity), and drops the `*`
// Krate puts before a descendant pseudo-element.
func builtinSelector(sel string) string {
	sel = strings.ReplaceAll(sel, " *::", " ::")
	if !strings.HasPrefix(sel, ":is(") || !strings.HasSuffix(sel, ")") {
		return sel
	}
	inner := sel[len(":is(") : len(sel)-1]
	sp := strings.LastIndexByte(inner, ' ')
	if sp < 0 || !strings.HasPrefix(inner[sp+1:], ".") {
		return sel
	}
	return inner[sp+1:] + ":is(" + inner[:sp] + " *)"
}
