package css

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestCustomUtilityMatchesTailwind compares @utility output with real
// Tailwind v4 (testdata/tailwind_utility_golden.json, produced by
// testdata/tailwind-utility/gen.mjs). Both sides are flattened to the same
// canonical form first, because Tailwind prints nested rules the way they were
// written, spacing as calc(var(--spacing) * n) and colors as theme variables.
func TestCustomUtilityMatchesTailwind(t *testing.T) {
	raw, err := os.ReadFile("testdata/tailwind_utility_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Tailwind string
		Cases    []struct {
			Name    string
			CSS     string
			Classes map[string]string
		}
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, c := range golden.Cases {
		d := NewTailwindDirectives(t.TempDir())
		d.Add(c.CSS, "")
		g := NewTailwindGenerator()
		d.ApplyTo(&g.Theme)
		classes := make([]string, 0, len(c.Classes))
		for cls := range c.Classes {
			classes = append(classes, cls)
		}
		sort.Strings(classes)
		for _, cls := range classes {
			want := canonCSS(c.Classes[cls], &g.Theme)
			got := canonCSS(g.Generate(map[string]bool{cls: true}), &g.Theme)
			checked++
			if want != got {
				t.Errorf("%s: %s\n  css: %s\n  tailwind %s: %s\n  krate:       %s", c.Name, cls, strings.Join(strings.Fields(c.CSS), " "), golden.Tailwind, want, got)
			}
		}
	}
	if checked < 200 {
		t.Fatalf("only %d golden classes checked", checked)
	}
}

type canonEntry struct {
	ats   []string
	sel   string
	decls []string
}

// canonCSS flattens CSS (nesting resolved) into one comparable line per rule.
func canonCSS(text string, theme *TailwindTheme) string {
	var entries []canonEntry
	canonWalk(splitCSSNodes(text), "", nil, theme, &entries)
	// Adjacent rules for the same selector and at-rules are one rule.
	var merged []canonEntry
	for _, e := range entries {
		if n := len(merged); n > 0 && merged[n-1].sel == e.sel && strings.Join(merged[n-1].ats, ">") == strings.Join(e.ats, ">") {
			merged[n-1].decls = append(append([]string(nil), merged[n-1].decls...), e.decls...)
			continue
		}
		merged = append(merged, e)
	}
	var lines []string
	for _, e := range merged {
		line := strings.Join(e.ats, " > ")
		if line != "" {
			line += " :: "
		}
		lines = append(lines, line+e.sel+" {"+strings.Join(e.decls, "; ")+"}")
	}
	return strings.Join(lines, "\n")
}

func canonWalk(nodes []cssNode, parent string, ats []string, theme *TailwindTheme, out *[]canonEntry) {
	var decls []string
	flush := func() {
		if len(decls) > 0 && parent != "" {
			*out = append(*out, canonEntry{ats: ats, sel: parent, decls: decls})
		}
		decls = nil
	}
	for _, n := range nodes {
		if !n.Block {
			name, val, ok := strings.Cut(n.Text, ":")
			if ok {
				decls = append(decls, strings.TrimSpace(name)+": "+canonValue(val, theme))
			}
			continue
		}
		flush()
		if strings.HasPrefix(n.Prelude, "@") {
			canonWalk(splitCSSNodes(n.Body), parent, append(append([]string(nil), ats...), canonAtRule(n.Prelude)), theme, out)
			continue
		}
		var sels []string
		for _, s := range topLevelSplit(n.Prelude, ',') {
			s = strings.TrimSpace(s)
			switch {
			case parent == "":
				sels = append(sels, s)
			case strings.Contains(s, "&"):
				sels = append(sels, strings.ReplaceAll(s, "&", parent))
			default:
				sels = append(sels, parent+" "+s)
			}
		}
		canonWalk(splitCSSNodes(n.Body), strings.Join(sels, ", "), ats, theme, out)
	}
	flush()
}

var (
	canonSpaceRe = regexp.MustCompile(`\s*([,/])\s*`)
	canonVarRe   = regexp.MustCompile(`var\(--([A-Za-z0-9_-]+)\)`)
	canonCalcRe  = regexp.MustCompile(`calc\(([0-9.]+)rem \* ([0-9.]+)\)`)
)

func canonValue(v string, theme *TailwindTheme) string {
	v = strings.Join(strings.Fields(v), " ")
	v = canonVarRe.ReplaceAllStringFunc(v, func(m string) string {
		name := canonVarRe.FindStringSubmatch(m)[1]
		if val, ok := utilityThemeVar(theme, name); ok {
			return val
		}
		return m
	})
	v = canonCalcRe.ReplaceAllStringFunc(v, func(m string) string {
		p := canonCalcRe.FindStringSubmatch(m)
		a, _ := strconv.ParseFloat(p[1], 64)
		b, _ := strconv.ParseFloat(p[2], 64)
		return formatNum(a*b) + "rem"
	})
	v = canonSpaceRe.ReplaceAllString(v, "$1")
	if v == "0rem" {
		v = "0"
	}
	return v
}

func canonAtRule(p string) string {
	p = strings.Join(strings.Fields(p), " ")
	p = strings.NewReplacer("(width >= 48rem)", "(min-width: 768px)", "(width >= 40rem)", "(min-width: 640px)", "(width < 48rem)", "(max-width: 767px)", "(hover: hover)", "(hover: hover)").Replace(p)
	return p
}
