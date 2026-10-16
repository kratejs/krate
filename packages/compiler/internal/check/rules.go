package check

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// a11y

// ruleImgAlt flags <img> elements with a missing or empty alt attribute. An
// explicit alt="" is valid for decorative images, but a *missing* alt is the
// most common screen-reader failure, so require the attribute to be present.
func ruleImgAlt(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	var out []Finding
	for _, img := range d.elements("img") {
		if _, ok := attr(img, "alt"); !ok {
			out = append(out, Finding{
				Message: "`<img>` is missing an `alt` attribute",
				Hint:    "Add alt text, or alt=\"\" if the image is purely decorative.",
			})
		}
	}
	return out
}

// ruleHeadingOrder checks that heading levels do not skip (e.g. h1 -> h3) and
// that a page has exactly one h1.
func ruleHeadingOrder(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	levels := headingLevels(d)
	if len(levels) == 0 {
		return []Finding{{
			Message: "page has no headings",
			Hint:    "Add an <h1> describing the page.",
		}}
	}

	var out []Finding
	h1 := 0
	prev := 0
	for _, lvl := range levels {
		if lvl == 1 {
			h1++
		}
		if prev != 0 && lvl > prev+1 {
			out = append(out, Finding{
				Message: fmt.Sprintf("heading level jumps from h%d to h%d", prev, lvl),
				Hint:    "Use sequential heading levels so assistive tech can navigate.",
			})
		}
		prev = lvl
	}
	if h1 == 0 {
		out = append(out, Finding{
			Message: "page has no <h1>",
			Hint:    "Add a single top-level <h1>.",
		})
	} else if h1 > 1 {
		out = append(out, Finding{
			Message: fmt.Sprintf("page has %d <h1> elements", h1),
			Hint:    "Keep a single <h1> per page; use h2+ for sections.",
		})
	}
	return out
}

// ruleAccessibleName checks that links and buttons expose an accessible name:
// visible text, aria-label, aria-labelledby, or a titled child image.
func ruleAccessibleName(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	var out []Finding

	check := func(n *interfaceNode) {
		if hasNonEmptyAttr(n.node, "aria-label") || hasNonEmptyAttr(n.node, "aria-labelledby") || hasNonEmptyAttr(n.node, "title") {
			return
		}
		if strings.TrimSpace(textContent(n.node)) != "" {
			return
		}
		// A child <img alt="..."> can name the control.
		for _, img := range childElements(n.node, "img") {
			if v, ok := attr(img, "alt"); ok && strings.TrimSpace(v) != "" {
				return
			}
		}
		out = append(out, Finding{
			Message: fmt.Sprintf("<%s> has no accessible name", n.tag),
			Hint:    "Add visible text, aria-label, or aria-labelledby.",
		})
	}

	for _, a := range d.elements("a") {
		check(&interfaceNode{tag: "a", node: a})
	}
	for _, b := range d.elements("button") {
		check(&interfaceNode{tag: "button", node: b})
	}
	return out
}

// seo

func ruleTitle(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	t := d.firstElement("title")
	title := textContent(t)
	if title == "" {
		return []Finding{{
			Message: "page has no <title>",
			Hint:    "Export a <Head><title>…</title></Head> or rely on seo.siteName.",
		}}
	}
	if len([]rune(title)) > 60 {
		return []Finding{{
			Message: fmt.Sprintf("<title> is %d characters (recommended ≤ 60)", len([]rune(title))),
			Hint:    "Shorten the title so it is not truncated in search results.",
		}}
	}
	return nil
}

func ruleDescription(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	desc, ok := d.metaByName("description")
	if !ok || strings.TrimSpace(desc) == "" {
		return []Finding{{
			Message: "page has no meta description",
			Hint:    "Set seo.description, or add <meta name=\"description\" content=\"…\">.",
		}}
	}
	if len([]rune(desc)) > 160 {
		return []Finding{{
			Message: fmt.Sprintf("meta description is %d characters (recommended ≤ 160)", len([]rune(desc))),
			Hint:    "Tighten the description; search engines truncate around 160.",
		}}
	}
	return nil
}

func ruleCanonical(p *Page, cfg *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	if d.hasCanonical() {
		return nil
	}
	return []Finding{{
		Message: "page has no canonical link",
		Hint:    "Set seo.baseUrl so Krate emits <link rel=\"canonical\">. (custom rule may override)",
	}}
}

func ruleOpenGraph(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	var out []Finding
	required := []string{"og:title", "og:type", "og:url"}
	for _, prop := range required {
		if _, ok := d.metaByProperty(prop); !ok {
			out = append(out, Finding{
				Message: "missing Open Graph tag: " + prop,
				Hint:    "Set seo.baseUrl/siteName so Krate emits OG tags.",
			})
		}
	}
	return out
}

func ruleLang(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	ht := d.firstElement("html")
	if ht == nil {
		return nil
	}
	if v, ok := attr(ht, "lang"); !ok || strings.TrimSpace(v) == "" {
		return []Finding{{
			Message: "<html> is missing a lang attribute",
			Hint:    "Set lang on the document so screen readers pick the right voice.",
		}}
	}
	return nil
}

// perf

func ruleJSBudget(p *Page, cfg *Config) []Finding {
	if cfg.JSBudgetBytes <= 0 || p.JSBytes <= cfg.JSBudgetBytes {
		return nil
	}
	return []Finding{{
		Message: fmt.Sprintf("route ships %s of JS (budget %s)", humanBytes(p.JSBytes), humanBytes(cfg.JSBudgetBytes)),
		Hint:    "Move logic to a server component, or raise checks.budget.js.",
	}}
}

// ruleImageDims flags <img> without both width and height, which causes layout
// shift (CLS). The compiled <Image> component always sets them.
func ruleImageDims(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	var out []Finding
	for _, img := range d.elements("img") {
		// Skip decorative/unknown-size images explicitly opted out.
		if hasAttr(img, "data-krate-no-dims") {
			continue
		}
		_, hasW := attr(img, "width")
		_, hasH := attr(img, "height")
		if !hasW || !hasH {
			out = append(out, Finding{
				Message: "`<img>` is missing width/height (causes layout shift)",
				Hint:    "Use <Image width height> so intrinsic dimensions are emitted.",
			})
		}
	}
	return out
}

// helpers

type interfaceNode struct {
	tag  string
	node *html.Node
}

func hasNonEmptyAttr(n *html.Node, name string) bool {
	v, ok := attr(n, name)
	return ok && strings.TrimSpace(v) != ""
}

func childElements(n *html.Node, tag string) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && strings.EqualFold(c.Data, tag) {
			out = append(out, c)
		}
	}
	return out
}

func humanBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	return fmt.Sprintf("%.1fKB", float64(n)/1024)
}
