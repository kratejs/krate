package check

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// a11y: duplicate ids

// ruleDuplicateID flags repeated id attributes, which break label/aria
// references and fragment links.
func ruleDuplicateID(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	seen := map[string]int{}
	d.walk(func(n *html.Node) {
		if v, ok := attr(n, "id"); ok && strings.TrimSpace(v) != "" {
			seen[v]++
		}
	})
	var out []Finding
	for id, count := range seen {
		if count > 1 {
			out = append(out, Finding{
				Message: fmt.Sprintf("duplicate id %q appears %d times", id, count),
				Hint:    "IDs must be unique; use a class or data attribute for repeated styling.",
			})
		}
	}
	return out
}

// a11y: landmarks

// ruleLandmark flags pages whose entire content lives outside a landmark
// region, which makes screen-reader navigation harder.
func ruleLandmark(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	found := false
	d.walk(func(n *html.Node) {
		if found {
			return
		}
		switch strings.ToLower(n.Data) {
		case "main", "nav", "header", "footer", "aside":
			found = true
			return
		}
		if hasNonEmptyAttr(n, "role") {
			switch strings.ToLower(attrValue(n, "role")) {
			case "main", "navigation", "banner", "contentinfo", "complementary", "search":
				found = true
			}
		}
	})
	if found {
		return nil
	}
	return []Finding{{
		Message: "page has no landmark region",
		Hint:    "Wrap primary content in <main> (and navigation in <nav>) so assistive tech can skip around.",
	}}
}

// a11y: form labels

// ruleFormLabel flags form controls without an associated label (a <label>
// wrapping or referencing them, aria-label, aria-labelledby, or a title).
func ruleFormLabel(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	labelFor := map[string]bool{}
	var labels []*html.Node
	d.walk(func(n *html.Node) {
		if strings.EqualFold(n.Data, "label") {
			labels = append(labels, n)
			if v, ok := attr(n, "for"); ok {
				labelFor[v] = true
			}
		}
	})
	inLabel := map[*html.Node]bool{}
	for _, l := range labels {
		for _, c := range descendantControls(l) {
			inLabel[c] = true
		}
	}

	var out []Finding
	for _, tag := range []string{"input", "select", "textarea"} {
		for _, c := range d.elements(tag) {
			typ, _ := attr(c, "type")
			switch strings.ToLower(typ) {
			case "hidden", "submit", "button", "reset", "image":
				continue
			}
			if hasNonEmptyAttr(c, "aria-label") || hasNonEmptyAttr(c, "aria-labelledby") || hasNonEmptyAttr(c, "title") {
				continue
			}
			if id, ok := attr(c, "id"); ok && labelFor[id] {
				continue
			}
			if inLabel[c] {
				continue
			}
			out = append(out, Finding{
				Message: fmt.Sprintf("<%s> has no associated label", tag),
				Hint:    "Wrap the control in <label>, use label[for=id], or add aria-label.",
			})
		}
	}
	return out
}

func descendantControls(n *html.Node) []*html.Node {
	var out []*html.Node
	var visit func(*html.Node)
	visit = func(x *html.Node) {
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				switch strings.ToLower(c.Data) {
				case "input", "select", "textarea":
					out = append(out, c)
				}
			}
			visit(c)
		}
	}
	visit(n)
	return out
}

// a11y: positive tabindex

// rulePositiveTabindex flags tabindex > 0, which overrides natural focus order
// and is almost always a mistake.
func rulePositiveTabindex(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	var out []Finding
	d.walk(func(n *html.Node) {
		v, ok := attr(n, "tabindex")
		if !ok {
			return
		}
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && i > 0 {
			out = append(out, Finding{
				Message: fmt.Sprintf("positive tabindex=%d on <%s>", i, strings.ToLower(n.Data)),
				Hint:    "Use tabindex=\"0\" or \"-1\"; positive values break the natural focus order.",
			})
		}
	})
	return out
}

// a11y: valid ARIA roles

// validARIALandmarkRoles is a subset of the ARIA role list used for validation.
// Only roles commonly misused on generic elements are checked; unknown roles
// are not flagged to avoid false positives as ARIA evolves.
var knownARIARoles = map[string]bool{
	"alert": true, "alertdialog": true, "application": true, "article": true,
	"banner": true, "button": true, "cell": true, "checkbox": true,
	"columnheader": true, "combobox": true, "complementary": true,
	"contentinfo": true, "definition": true, "dialog": true, "directory": true,
	"document": true, "feed": true, "figure": true, "form": true, "grid": true,
	"gridcell": true, "group": true, "heading": true, "img": true, "link": true,
	"list": true, "listbox": true, "listitem": true, "log": true, "main": true,
	"marquee": true, "math": true, "menu": true, "menubar": true,
	"menuitem": true, "menuitemcheckbox": true, "menuitemradio": true,
	"navigation": true, "none": true, "note": true, "option": true,
	"presentation": true, "progressbar": true, "radio": true, "radiogroup": true,
	"region": true, "row": true, "rowgroup": true, "rowheader": true,
	"scrollbar": true, "search": true, "searchbox": true, "separator": true,
	"slider": true, "spinbutton": true, "status": true, "switch": true,
	"tab": true, "table": true, "tablist": true, "tabpanel": true,
	"term": true, "textbox": true, "timer": true, "toolbar": true,
	"tooltip": true, "tree": true, "treegrid": true, "treeitem": true,
}

// ruleARIARole flags role attributes whose value is not a known ARIA role.
func ruleARIARole(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	var out []Finding
	d.walk(func(n *html.Node) {
		v, ok := attr(n, "role")
		if !ok {
			return
		}
		// A role attribute may list fallbacks separated by spaces.
		for _, role := range strings.Fields(strings.ToLower(v)) {
			if !knownARIARoles[role] {
				out = append(out, Finding{
					Message: fmt.Sprintf("unknown ARIA role %q on <%s>", role, strings.ToLower(n.Data)),
					Hint:    "Use a valid ARIA role or remove the attribute (a <div> maps to no role).",
				})
				break
			}
		}
	})
	return out
}

// a11y: inline style contrast (heuristic)

// ruleColorContrast is a best-effort check for the common inline case: a
// `color` and a `background` on the same element whose contrast ratio is below
// WCAG AA (4.5:1). It only evaluates literal hex colors; anything using CSS
// variables or classes is skipped (the cascade isn't available here).
func ruleColorContrast(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	var out []Finding
	d.walk(func(n *html.Node) {
		v, ok := attr(n, "style")
		if !ok {
			return
		}
		fg, fgOK := cssHexColor(v, "color")
		bg, bgOK := cssHexColor(v, "background-color")
		if !bgOK {
			bg, bgOK = cssHexColor(v, "background")
		}
		if !fgOK || !bgOK {
			return
		}
		if ratio := contrastRatio(fg, bg); ratio < 4.5 {
			out = append(out, Finding{
				Message: fmt.Sprintf("low text contrast (%.2f:1) on <%s>", ratio, strings.ToLower(n.Data)),
				Hint:    "Increase contrast to at least 4.5:1 for body text (3:1 for large text).",
			})
		}
	})
	return out
}

// cssHexColor extracts a #rgb/#rrggbb value for the given property from an
// inline style string. Returns false when absent or not a literal hex color.
func cssHexColor(style, prop string) ([3]int, bool) {
	for _, decl := range strings.Split(style, ";") {
		kv := strings.SplitN(decl, ":", 2)
		if len(kv) != 2 {
			continue
		}
		if strings.TrimSpace(strings.ToLower(kv[0])) != prop {
			continue
		}
		return parseHexColor(strings.TrimSpace(kv[1]))
	}
	return [3]int{}, false
}

func parseHexColor(s string) ([3]int, bool) {
	if !strings.HasPrefix(s, "#") {
		return [3]int{}, false
	}
	h := strings.TrimPrefix(s, "#")
	switch len(h) {
	case 3:
		var c [3]int
		for i := 0; i < 3; i++ {
			v, err := strconv.ParseInt(strings.Repeat(string(h[i]), 2), 16, 32)
			if err != nil {
				return [3]int{}, false
			}
			c[i] = int(v)
		}
		return c, true
	case 6:
		var c [3]int
		for i := 0; i < 3; i++ {
			v, err := strconv.ParseInt(h[i*2:i*2+2], 16, 32)
			if err != nil {
				return [3]int{}, false
			}
			c[i] = int(v)
		}
		return c, true
	}
	return [3]int{}, false
}

// contrastRatio computes the WCAG contrast ratio between two sRGB colors.
func contrastRatio(a, b [3]int) float64 {
	la := relativeLuminance(a)
	lb := relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func relativeLuminance(c [3]int) float64 {
	channel := func(v int) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c[0]) + 0.7152*channel(c[1]) + 0.0722*channel(c[2])
}

func attrValue(n *html.Node, name string) string {
	v, _ := attr(n, name)
	return v
}
