package css

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"krate-compiler/internal/fsutil"
)

// classAttrRe matches className="..." / class="..." string-literal attributes.
var classAttrRe = regexp.MustCompile(`(?:className|class)\s*=\s*["']([^"']*)["']`)

// classAttrTemplateRe matches className={`...`} template-literal attributes.
// The template body may span multiple lines and contain ${...} interpolations.
var classAttrTemplateRe = regexp.MustCompile("(?:className|class)\\s*=\\s*\\{\\s*`([^`]*)`\\s*}")

// TailwindScanner extracts Tailwind utility class names from source files.
type TailwindScanner struct {
	Root string
}

// NewTailwindScanner creates a scanner for the given project root.
func NewTailwindScanner(root string) *TailwindScanner {
	return &TailwindScanner{Root: root}
}

// ScanClasses walks the project and extracts all Tailwind class names used in className attributes.
func (s *TailwindScanner) ScanClasses(dirs []string) map[string]bool {
	classes := make(map[string]bool)
	exts := map[string]bool{".tsx": true, ".jsx": true, ".mdx": true, ".html": true, ".ts": true}
	skipDirs := map[string]bool{"node_modules": true, ".git": true, "dist": true}

	for _, dir := range dirs {
		fsutil.WalkExt(dir, exts, skipDirs, func(path string, _ os.FileInfo) error {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			matches := classAttrRe.FindAllStringSubmatch(string(data), -1)
			for _, m := range matches {
				for _, c := range strings.Fields(m[1]) {
					if strings.HasPrefix(c, "{") || strings.HasPrefix(c, "`") {
						continue
					}
					classes[c] = true
				}
			}
			tplMatches := classAttrTemplateRe.FindAllStringSubmatch(string(data), -1)
			for _, m := range tplMatches {
				for _, c := range strings.Fields(stripTemplateInterpolations(m[1])) {
					if strings.HasPrefix(c, "{") || strings.HasPrefix(c, "`") {
						continue
					}
					classes[c] = true
				}
			}
			return nil
		})
	}

	return classes
}

// stripTemplateInterpolations removes ${...} segments from a template literal
// body so only the static class-name text remains. Nested braces inside the
// interpolation (e.g. ${spanMap[props.label] ?? ""}) are skipped correctly.
func stripTemplateInterpolations(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			depth := 1
			i += 2
			for i < len(s) && depth > 0 {
				switch s[i] {
				case '{':
					depth++
				case '}':
					depth--
				}
				i++
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// TailwindGenerator converts class names to CSS rules.
type TailwindGenerator struct {
	Theme TailwindTheme
}

// NewTailwindGenerator creates a generator with default theme.
func NewTailwindGenerator() *TailwindGenerator {
	return &TailwindGenerator{
		Theme: DefaultTailwindTheme(),
	}
}

// Generate produces CSS for the given set of class names, deduplicated.
func (g *TailwindGenerator) Generate(classes map[string]bool) string {
	if len(classes) == 0 {
		return ""
	}

	seen := make(map[string]bool)
	var rules []string

	for cls := range classes {
		for _, c := range strings.Fields(cls) {
			if seen[c] {
				continue
			}
			seen[c] = true

			css := classToCSS(c, g.Theme)
			if css != "" {
				rules = append(rules, css)
			}
		}
	}

	if len(rules) == 0 {
		return ""
	}

	return strings.Join(rules, "\n") + "\n"
}

// classToCSS converts a single Tailwind class to its CSS declaration.
func classToCSS(cls string, theme TailwindTheme) string {
	variant := ""
	baseCls := cls

	if idx := strings.LastIndex(cls, ":"); idx > 0 {
		variant = cls[:idx]
		baseCls = cls[idx+1:]
	}

	css := generateCSS(baseCls, theme)
	if css == "" {
		return ""
	}

	if variant != "" {
		pseudo := variantToPseudo(variant)
		if pseudo != "" {
			return fmt.Sprintf(".%s{%s}", escapeSelector(cls), css)
		}
		bp := variantToBreakpoint(variant)
		if bp != "" {
			return fmt.Sprintf("@media (min-width: %s) { .%s{%s} }", bp, escapeSelector(cls), strings.TrimSpace(css))
		}
		if variant == "dark" {
			return fmt.Sprintf("@media (prefers-color-scheme: dark) { .%s{%s} }", escapeSelector(cls), strings.TrimSpace(css))
		}
	}

	return fmt.Sprintf(".%s {%s}", escapeSelector(cls), css)
}

func generateCSS(cls string, theme TailwindTheme) string {
	// Display
	if cls == "block" {
		return "display: block;"
	}
	if cls == "inline-block" {
		return "display: inline-block;"
	}
	if cls == "inline" {
		return "display: inline;"
	}
	if cls == "flex" {
		return "display: flex;"
	}
	if cls == "inline-flex" {
		return "display: inline-flex;"
	}
	if cls == "grid" {
		return "display: grid;"
	}
	if cls == "inline-grid" {
		return "display: inline-grid;"
	}
	if cls == "hidden" {
		return "display: none;"
	}
	if cls == "contents" {
		return "display: contents;"
	}

	// Flex direction
	if cls == "flex-row" {
		return "flex-direction: row;"
	}
	if cls == "flex-col" {
		return "flex-direction: column;"
	}
	if cls == "flex-row-reverse" {
		return "flex-direction: row-reverse;"
	}
	if cls == "flex-col-reverse" {
		return "flex-direction: column-reverse;"
	}
	if cls == "flex-wrap" {
		return "flex-wrap: wrap;"
	}
	if cls == "flex-wrap-reverse" {
		return "flex-wrap: wrap-reverse;"
	}
	if cls == "flex-nowrap" {
		return "flex-wrap: nowrap;"
	}
	if cls == "flex-1" {
		return "flex: 1 1 0%;"
	}
	if cls == "flex-auto" {
		return "flex: 1 1 auto;"
	}
	if cls == "flex-initial" {
		return "flex: 0 1 auto;"
	}
	if cls == "flex-none" {
		return "flex: none;"
	}

	// Flex order
	if match := regexp.MustCompile(`^order-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("order: %s;", match[1])
	}
	if cls == "order-first" {
		return "order: -9999;"
	}
	if cls == "order-last" {
		return "order: 9999;"
	}
	if cls == "order-none" {
		return "order: 0;"
	}

	// Justify content
	if cls == "justify-start" {
		return "justify-content: flex-start;"
	}
	if cls == "justify-end" {
		return "justify-content: flex-end;"
	}
	if cls == "justify-center" {
		return "justify-content: center;"
	}
	if cls == "justify-between" {
		return "justify-content: space-between;"
	}
	if cls == "justify-around" {
		return "justify-content: space-around;"
	}
	if cls == "justify-evenly" {
		return "justify-content: space-evenly;"
	}

	// Align items
	if cls == "items-start" {
		return "align-items: flex-start;"
	}
	if cls == "items-end" {
		return "align-items: flex-end;"
	}
	if cls == "items-center" {
		return "align-items: center;"
	}
	if cls == "items-baseline" {
		return "align-items: baseline;"
	}
	if cls == "items-stretch" {
		return "align-items: stretch;"
	}

	// Align self
	if cls == "self-auto" {
		return "align-self: auto;"
	}
	if cls == "self-start" {
		return "align-self: flex-start;"
	}
	if cls == "self-end" {
		return "align-self: flex-end;"
	}
	if cls == "self-center" {
		return "align-self: center;"
	}
	if cls == "self-baseline" {
		return "align-self: baseline;"
	}
	if cls == "self-stretch" {
		return "align-self: stretch;"
	}

	// Align content
	if cls == "content-center" {
		return "align-content: center;"
	}
	if cls == "content-start" {
		return "align-content: flex-start;"
	}
	if cls == "content-end" {
		return "align-content: flex-end;"
	}
	if cls == "content-between" {
		return "align-content: space-between;"
	}
	if cls == "content-around" {
		return "align-content: space-around;"
	}
	if cls == "content-evenly" {
		return "align-content: space-evenly;"
	}
	if cls == "content-baseline" {
		return "align-content: baseline;"
	}
	if cls == "content-stretch" {
		return "align-content: stretch;"
	}

	// Justify items
	if cls == "justify-items-start" {
		return "justify-items: start;"
	}
	if cls == "justify-items-end" {
		return "justify-items: end;"
	}
	if cls == "justify-items-center" {
		return "justify-items: center;"
	}
	if cls == "justify-items-stretch" {
		return "justify-items: stretch;"
	}

	// Justify self
	if cls == "justify-self-auto" {
		return "justify-self: auto;"
	}
	if cls == "justify-self-start" {
		return "justify-self: start;"
	}
	if cls == "justify-self-end" {
		return "justify-self: end;"
	}
	if cls == "justify-self-center" {
		return "justify-self: center;"
	}
	if cls == "justify-self-stretch" {
		return "justify-self: stretch;"
	}

	// Place items
	if cls == "place-items-center" {
		return "place-items: center;"
	}
	if cls == "place-items-start" {
		return "place-items: start;"
	}
	if cls == "place-items-end" {
		return "place-items: end;"
	}
	if cls == "place-items-stretch" {
		return "place-items: stretch;"
	}

	// Place content
	if cls == "place-content-center" {
		return "place-content: center;"
	}
	if cls == "place-content-start" {
		return "place-content: start;"
	}
	if cls == "place-content-end" {
		return "place-content: end;"
	}
	if cls == "place-content-between" {
		return "place-content: space-between;"
	}
	if cls == "place-content-around" {
		return "place-content: space-around;"
	}
	if cls == "place-content-evenly" {
		return "place-content: space-evenly;"
	}
	if cls == "place-content-stretch" {
		return "place-content: stretch;"
	}

	// Place self
	if cls == "place-self-auto" {
		return "place-self: auto;"
	}
	if cls == "place-self-start" {
		return "place-self: start;"
	}
	if cls == "place-self-end" {
		return "place-self: end;"
	}
	if cls == "place-self-center" {
		return "place-self: center;"
	}
	if cls == "place-self-stretch" {
		return "place-self: stretch;"
	}

	// Flex grow/shrink
	if cls == "flex-grow" || cls == "grow" {
		return "flex-grow: 1;"
	}
	if cls == "flex-grow-0" || cls == "grow-0" {
		return "flex-grow: 0;"
	}
	if cls == "flex-shrink" || cls == "shrink" {
		return "flex-shrink: 1;"
	}
	if cls == "flex-shrink-0" || cls == "shrink-0" {
		return "flex-shrink: 0;"
	}

	// Space between
	if match := regexp.MustCompile(`^space-x-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-left: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^space-y-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-top: %s;", spacingValue(match[1], theme))
	}
	if cls == "space-x-reverse" {
		return "direction: rtl;"
	}
	if cls == "space-y-reverse" {
		return "flex-direction: column-reverse;"
	}

	// Gap
	if match := regexp.MustCompile(`^gap-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("gap: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^gap-x-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("column-gap: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^gap-y-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("row-gap: %s;", spacingValue(match[1], theme))
	}

	// Grid
	if cls == "grid-cols-1" {
		return "grid-template-columns: repeat(1, minmax(0, 1fr));"
	}
	if cls == "grid-cols-2" {
		return "grid-template-columns: repeat(2, minmax(0, 1fr));"
	}
	if cls == "grid-cols-3" {
		return "grid-template-columns: repeat(3, minmax(0, 1fr));"
	}
	if cls == "grid-cols-4" {
		return "grid-template-columns: repeat(4, minmax(0, 1fr));"
	}
	if cls == "grid-cols-5" {
		return "grid-template-columns: repeat(5, minmax(0, 1fr));"
	}
	if cls == "grid-cols-6" {
		return "grid-template-columns: repeat(6, minmax(0, 1fr));"
	}
	if cls == "grid-cols-12" {
		return "grid-template-columns: repeat(12, minmax(0, 1fr));"
	}
	if cls == "col-span-1" {
		return "grid-column: span 1 / span 1;"
	}
	if cls == "col-span-2" {
		return "grid-column: span 2 / span 2;"
	}
	if cls == "col-span-3" {
		return "grid-column: span 3 / span 3;"
	}
	if cls == "col-span-full" {
		return "grid-column: 1 / -1;"
	}
	if match := regexp.MustCompile(`^grid-rows-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("grid-template-rows: repeat(%s, minmax(0, 1fr));", match[1])
	}
	if match := regexp.MustCompile(`^row-span-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("grid-row: span %s / span %s;", match[1], match[1])
	}
	if cls == "row-span-full" {
		return "grid-row: 1 / -1;"
	}

	// Padding
	if match := regexp.MustCompile(`^p-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^px-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding-left: %s; padding-right: %s;", spacingValue(match[1], theme), spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^py-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding-top: %s; padding-bottom: %s;", spacingValue(match[1], theme), spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^pt-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding-top: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^pr-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding-right: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^pb-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding-bottom: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^pl-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding-left: %s;", spacingValue(match[1], theme))
	}

	// Margin
	if match := regexp.MustCompile(`^m-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^mx-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-left: %s; margin-right: %s;", spacingValue(match[1], theme), spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^my-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-top: %s; margin-bottom: %s;", spacingValue(match[1], theme), spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^mt-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-top: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^mr-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-right: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^mb-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-bottom: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^ml-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-left: %s;", spacingValue(match[1], theme))
	}

	// Width
	if match := regexp.MustCompile(`^w-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("width: %s;", sizingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^min-w-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("min-width: %s;", sizingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^max-w-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("max-width: %s;", sizingValue(match[1], theme))
	}

	// Height
	if match := regexp.MustCompile(`^h-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("height: %s;", sizingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^min-h-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("min-height: %s;", sizingValue(match[1], theme))
	}
	if cls == "h-screen" {
		return "height: 100vh;"
	}
	if cls == "h-full" {
		return "height: 100%;"
	}
	if cls == "w-screen" {
		return "width: 100vw;"
	}
	if cls == "w-full" {
		return "width: 100%;"
	}
	if cls == "w-auto" {
		return "width: auto;"
	}
	if cls == "w-max" {
		return "width: max-content;"
	}
	if cls == "w-min" {
		return "width: min-content;"
	}

	// Min/max height
	if cls == "min-h-screen" {
		return "min-height: 100vh;"
	}
	if cls == "min-h-full" {
		return "min-height: 100%;"
	}
	if cls == "max-h-screen" {
		return "max-height: 100vh;"
	}
	if cls == "max-h-full" {
		return "max-height: 100%;"
	}
	if match := regexp.MustCompile(`^min-h-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("min-height: %s;", sizingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^max-h-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("max-height: %s;", sizingValue(match[1], theme))
	}

	// Text color with shade: text-gray-600
	if match := regexp.MustCompile(`^text-([a-z]+)-(\d+)$`).FindStringSubmatch(cls); match != nil {
		color := match[1]
		shade := match[2]
		if hex, ok := theme.Colors[color]; ok {
			if shadeVal, ok := hex[shade]; ok {
				return fmt.Sprintf("color: %s;", shadeVal)
			}
		}
	}
	// Named text color: text-white, text-black, text-current, text-transparent, text-inherit
	if match := regexp.MustCompile(`^text-([a-z]+)$`).FindStringSubmatch(cls); match != nil {
		name := match[1]
		// Check BgColors for named colors (white, black, transparent, current)
		if val, ok := theme.BgColors[name]; ok {
			return fmt.Sprintf("color: %s;", val)
		}
		// Check TextSizes for size names (xs, sm, base, lg, xl, etc.)
		if val, ok := theme.TextSizes[name]; ok {
			return val
		}
		if name == "inherit" {
			return "color: inherit;"
		}
	}

	// Text alignment
	if cls == "text-left" {
		return "text-align: left;"
	}
	if cls == "text-center" {
		return "text-align: center;"
	}
	if cls == "text-right" {
		return "text-align: right;"
	}
	if cls == "text-justify" {
		return "text-align: justify;"
	}

	// Text decoration
	if cls == "underline" {
		return "text-decoration: underline;"
	}
	if cls == "line-through" {
		return "text-decoration: line-through;"
	}
	if cls == "no-underline" {
		return "text-decoration: none;"
	}

	// Text transform
	if cls == "uppercase" {
		return "text-transform: uppercase;"
	}
	if cls == "lowercase" {
		return "text-transform: lowercase;"
	}
	if cls == "capitalize" {
		return "text-transform: capitalize;"
	}
	if cls == "normal-case" {
		return "text-transform: none;"
	}

	// Vertical align
	if cls == "align-baseline" {
		return "vertical-align: baseline;"
	}
	if cls == "align-top" {
		return "vertical-align: top;"
	}
	if cls == "align-middle" {
		return "vertical-align: middle;"
	}
	if cls == "align-bottom" {
		return "vertical-align: bottom;"
	}
	if cls == "align-text-top" {
		return "vertical-align: text-top;"
	}
	if cls == "align-text-bottom" {
		return "vertical-align: text-bottom;"
	}

	// Font weight
	if match := regexp.MustCompile(`^font-([a-z]+)$`).FindStringSubmatch(cls); match != nil {
		if val, ok := theme.FontWeights[match[1]]; ok {
			return fmt.Sprintf("font-weight: %s;", val)
		}
	}

	// Font family
	if cls == "font-sans" {
		return "font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, \"Noto Sans\", sans-serif, \"Apple Color Emoji\", \"Segoe UI Emoji\", \"Segoe UI Symbol\", \"Noto Color Emoji\";"
	}
	if cls == "font-serif" {
		return "font-family: ui-serif, Georgia, Cambria, \"Times New Roman\", Times, serif;"
	}
	if cls == "font-mono" {
		return "font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, \"Liberation Mono\", \"Courier New\", monospace;"
	}

	// Line height
	if match := regexp.MustCompile(`^leading-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		val := match[1]
		switch val {
		case "none":
			return "line-height: 1;"
		case "tight":
			return "line-height: 1.25;"
		case "snug":
			return "line-height: 1.375;"
		case "normal":
			return "line-height: 1.5;"
		case "relaxed":
			return "line-height: 1.625;"
		case "loose":
			return "line-height: 2;"
		default:
			return fmt.Sprintf("line-height: %srem;", val)
		}
	}

	// Letter spacing
	if match := regexp.MustCompile(`^tracking-([a-z]+)$`).FindStringSubmatch(cls); match != nil {
		switch match[1] {
		case "tighter":
			return "letter-spacing: -0.05em;"
		case "tight":
			return "letter-spacing: -0.025em;"
		case "normal":
			return "letter-spacing: 0em;"
		case "wide":
			return "letter-spacing: 0.025em;"
		case "wider":
			return "letter-spacing: 0.05em;"
		case "widest":
			return "letter-spacing: 0.1em;"
		}
	}

	// List style
	if cls == "list-none" {
		return "list-style: none;"
	}
	if cls == "list-disc" {
		return "list-style: disc;"
	}
	if cls == "list-decimal" {
		return "list-style: decimal;"
	}
	if cls == "list-inside" {
		return "list-style-position: inside;"
	}
	if cls == "list-outside" {
		return "list-style-position: outside;"
	}

	// Background attachment
	if cls == "bg-fixed" {
		return "background-attachment: fixed;"
	}
	if cls == "bg-local" {
		return "background-attachment: local;"
	}
	if cls == "bg-scroll" {
		return "background-attachment: scroll;"
	}

	// Background position
	if cls == "bg-bottom" {
		return "background-position: bottom;"
	}
	if cls == "bg-center" {
		return "background-position: center;"
	}
	if cls == "bg-left" {
		return "background-position: left;"
	}
	if cls == "bg-left-bottom" {
		return "background-position: left bottom;"
	}
	if cls == "bg-left-top" {
		return "background-position: left top;"
	}
	if cls == "bg-right" {
		return "background-position: right;"
	}
	if cls == "bg-right-bottom" {
		return "background-position: right bottom;"
	}
	if cls == "bg-right-top" {
		return "background-position: right top;"
	}
	if cls == "bg-top" {
		return "background-position: top;"
	}

	// Background size
	if cls == "bg-cover" {
		return "background-size: cover;"
	}
	if cls == "bg-contain" {
		return "background-size: contain;"
	}
	if cls == "bg-auto" {
		return "background-size: auto;"
	}

	// Background repeat
	if cls == "bg-repeat" {
		return "background-repeat: repeat;"
	}
	if cls == "bg-no-repeat" {
		return "background-repeat: no-repeat;"
	}
	if cls == "bg-repeat-x" {
		return "background-repeat: repeat-x;"
	}
	if cls == "bg-repeat-y" {
		return "background-repeat: repeat-y;"
	}
	if cls == "bg-repeat-round" {
		return "background-repeat: round;"
	}
	if cls == "bg-repeat-space" {
		return "background-repeat: space;"
	}

	// Background color
	if match := regexp.MustCompile(`^bg-([a-z]+)-(\d+)$`).FindStringSubmatch(cls); match != nil {
		color := match[1]
		shade := match[2]
		if hex, ok := theme.Colors[color]; ok {
			if shadeVal, ok := hex[shade]; ok {
				return fmt.Sprintf("background-color: %s;", shadeVal)
			}
		}
	}
	if match := regexp.MustCompile(`^bg-([a-z]+)$`).FindStringSubmatch(cls); match != nil {
		if val, ok := theme.BgColors[match[1]]; ok {
			return fmt.Sprintf("background-color: %s;", val)
		}
	}

	// Border color with shade
	if match := regexp.MustCompile(`^border-([a-z]+)-(\d+)$`).FindStringSubmatch(cls); match != nil {
		color := match[1]
		shade := match[2]
		if hex, ok := theme.Colors[color]; ok {
			if shadeVal, ok := hex[shade]; ok {
				return fmt.Sprintf("border-color: %s;", shadeVal)
			}
		}
	}
	// Named border color
	if match := regexp.MustCompile(`^border-([a-z]+)$`).FindStringSubmatch(cls); match != nil {
		name := match[1]
		if val, ok := theme.BgColors[name]; ok {
			return fmt.Sprintf("border-color: %s;", val)
		}
	}

	// Border width
	if match := regexp.MustCompile(`^border-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("border-width: %spx;", match[1])
	}
	if cls == "border" {
		return "border-width: 1px; border-style: solid;"
	}
	if cls == "border-0" {
		return "border-width: 0;"
	}
	if cls == "border-t" {
		return "border-top-width: 1px;"
	}
	if cls == "border-b" {
		return "border-bottom-width: 1px;"
	}
	if cls == "border-l" {
		return "border-left-width: 1px;"
	}
	if cls == "border-r" {
		return "border-right-width: 1px;"
	}
	if cls == "border-t-0" {
		return "border-top-width: 0;"
	}
	if cls == "border-b-0" {
		return "border-bottom-width: 0;"
	}
	if cls == "border-l-0" {
		return "border-left-width: 0;"
	}
	if cls == "border-r-0" {
		return "border-right-width: 0;"
	}

	// Border x/y shorthand
	if cls == "border-x" {
		return "border-left-width: 1px; border-right-width: 1px;"
	}
	if cls == "border-y" {
		return "border-top-width: 1px; border-bottom-width: 1px;"
	}
	if cls == "border-x-0" {
		return "border-left-width: 0; border-right-width: 0;"
	}
	if cls == "border-y-0" {
		return "border-top-width: 0; border-bottom-width: 0;"
	}

	// Border style
	if cls == "border-solid" {
		return "border-style: solid;"
	}
	if cls == "border-dashed" {
		return "border-style: dashed;"
	}
	if cls == "border-dotted" {
		return "border-style: dotted;"
	}
	if cls == "border-double" {
		return "border-style: double;"
	}
	if cls == "border-none" {
		return "border-style: none;"
	}

	// Outline
	if cls == "outline-none" {
		return "outline: 2px solid transparent; outline-offset: 2px;"
	}
	if cls == "outline" {
		return "outline-style: solid;"
	}
	if match := regexp.MustCompile(`^outline-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("outline-width: %spx;", match[1])
	}

	// Border radius
	if match := regexp.MustCompile(`^rounded-([a-zA-Z0-9]+)$`).FindStringSubmatch(cls); match != nil {
		if val, ok := theme.Radii[match[1]]; ok {
			return fmt.Sprintf("border-radius: %s;", val)
		}
	}
	if cls == "rounded" {
		return "border-radius: 0.25rem;"
	}
	if cls == "rounded-full" {
		return "border-radius: 9999px;"
	}
	if cls == "rounded-none" {
		return "border-radius: 0;"
	}

	// Rounded sides
	if match := regexp.MustCompile(`^rounded-(tl|tr|bl|br)-([a-zA-Z0-9]+)$`).FindStringSubmatch(cls); match != nil {
		corner := match[1]
		val := spacingValue(match[2], theme)
		props := map[string]string{
			"tl": "border-top-left-radius",
			"tr": "border-top-right-radius",
			"bl": "border-bottom-left-radius",
			"br": "border-bottom-right-radius",
		}
		return fmt.Sprintf("%s: %s;", props[corner], val)
	}

	// Opacity
	if match := regexp.MustCompile(`^opacity-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("opacity: 0.%s;", match[1])
	}
	if cls == "opacity-0" {
		return "opacity: 0;"
	}
	if cls == "opacity-100" {
		return "opacity: 1;"
	}

	// Shadow
	if match := regexp.MustCompile(`^shadow-([a-z]+)$`).FindStringSubmatch(cls); match != nil {
		if val, ok := theme.Shadows[match[1]]; ok {
			return fmt.Sprintf("box-shadow: %s;", val)
		}
	}
	if cls == "shadow" {
		return "box-shadow: 0 1px 3px 0 rgba(0, 0, 0, 0.1), 0 1px 2px 0 rgba(0, 0, 0, 0.06);"
	}
	if cls == "shadow-none" {
		return "box-shadow: none;"
	}

	// Text size (Tailwind numeric)
	if match := regexp.MustCompile(`^text-(\w+)$`).FindStringSubmatch(cls); match != nil {
		if val, ok := theme.TextSizes[match[1]]; ok {
			return val
		}
	}

	// Position
	if cls == "static" {
		return "position: static;"
	}
	if cls == "fixed" {
		return "position: fixed;"
	}
	if cls == "absolute" {
		return "position: absolute;"
	}
	if cls == "relative" {
		return "position: relative;"
	}
	if cls == "sticky" {
		return "position: sticky;"
	}

	// Inset (top/right/bottom/left)
	if match := regexp.MustCompile(`^inset-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("inset: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^inset-x-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("left: %s; right: %s;", spacingValue(match[1], theme), spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^inset-y-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("top: %s; bottom: %s;", spacingValue(match[1], theme), spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^top-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("top: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^right-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("right: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^bottom-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("bottom: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^left-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("left: %s;", spacingValue(match[1], theme))
	}

	// Overflow
	if cls == "overflow-hidden" {
		return "overflow: hidden;"
	}
	if cls == "overflow-auto" {
		return "overflow: auto;"
	}
	if cls == "overflow-scroll" {
		return "overflow: scroll;"
	}
	if cls == "overflow-visible" {
		return "overflow: visible;"
	}
	if cls == "overflow-x-auto" {
		return "overflow-x: auto;"
	}
	if cls == "overflow-y-auto" {
		return "overflow-y: auto;"
	}
	if cls == "overflow-x-hidden" {
		return "overflow-x: hidden;"
	}
	if cls == "overflow-y-hidden" {
		return "overflow-y: hidden;"
	}
	if cls == "overflow-x-scroll" {
		return "overflow-x: scroll;"
	}
	if cls == "overflow-y-scroll" {
		return "overflow-y: scroll;"
	}

	// Z-index
	if match := regexp.MustCompile(`^z-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("z-index: %s;", match[1])
	}
	if cls == "z-auto" {
		return "z-index: auto;"
	}

	// Cursor
	if cls == "cursor-pointer" {
		return "cursor: pointer;"
	}
	if cls == "cursor-default" {
		return "cursor: default;"
	}
	if cls == "cursor-not-allowed" {
		return "cursor: not-allowed;"
	}
	if cls == "cursor-grab" {
		return "cursor: grab;"
	}
	if cls == "cursor-grabbing" {
		return "cursor: grabbing;"
	}
	if cls == "cursor-wait" {
		return "cursor: wait;"
	}
	if cls == "cursor-text" {
		return "cursor: text;"
	}
	if cls == "cursor-move" {
		return "cursor: move;"
	}
	if cls == "cursor-help" {
		return "cursor: help;"
	}

	// User select
	if cls == "select-none" {
		return "user-select: none;"
	}
	if cls == "select-text" {
		return "user-select: text;"
	}
	if cls == "select-all" {
		return "user-select: all;"
	}
	if cls == "select-auto" {
		return "user-select: auto;"
	}

	// Whitespace
	if cls == "whitespace-normal" {
		return "white-space: normal;"
	}
	if cls == "whitespace-nowrap" {
		return "white-space: nowrap;"
	}
	if cls == "whitespace-pre" {
		return "white-space: pre;"
	}
	if cls == "whitespace-pre-line" {
		return "white-space: pre-line;"
	}
	if cls == "whitespace-pre-wrap" {
		return "white-space: pre-wrap;"
	}

	// Word break
	if cls == "break-normal" {
		return "overflow-wrap: normal; word-break: normal;"
	}
	if cls == "break-words" {
		return "overflow-wrap: break-word;"
	}
	if cls == "break-all" {
		return "word-break: break-all;"
	}
	if cls == "truncate" {
		return "overflow: hidden; text-overflow: ellipsis; white-space: nowrap;"
	}

	// Box sizing
	if cls == "box-border" {
		return "box-sizing: border-box;"
	}
	if cls == "box-content" {
		return "box-sizing: content-box;"
	}

	// Appearance
	if cls == "appearance-none" {
		return "appearance: none;"
	}

	// Transition property
	if cls == "transition" {
		return "transition-property: all; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}
	if cls == "transition-all" {
		return "transition-property: all; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}
	if cls == "transition-colors" {
		return "transition-property: color, background-color, border-color, text-decoration-color, fill, stroke; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}
	if cls == "transition-opacity" {
		return "transition-property: opacity; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}
	if cls == "transition-shadow" {
		return "transition-property: box-shadow; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}
	if cls == "transition-transform" {
		return "transition-property: transform; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}
	if cls == "transition-none" {
		return "transition-property: none;"
	}

	if match := regexp.MustCompile(`^duration-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("transition-duration: %sms;", match[1])
	}
	if match := regexp.MustCompile(`^delay-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("transition-delay: %sms;", match[1])
	}
	if cls == "ease-linear" {
		return "transition-timing-function: linear;"
	}
	if cls == "ease-in" {
		return "transition-timing-function: cubic-bezier(0.4, 0, 1, 1);"
	}
	if cls == "ease-out" {
		return "transition-timing-function: cubic-bezier(0, 0, 0.2, 1);"
	}
	if cls == "ease-in-out" {
		return "transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1);"
	}

	// Transform
	if cls == "scale-95" {
		return "transform: scale(0.95);"
	}
	if cls == "scale-100" {
		return "transform: scale(1);"
	}
	if cls == "scale-105" {
		return "transform: scale(1.05);"
	}
	if cls == "scale-110" {
		return "transform: scale(1.10);"
	}
	if cls == "rotate-45" {
		return "transform: rotate(45deg);"
	}
	if cls == "rotate-90" {
		return "transform: rotate(90deg);"
	}
	if cls == "rotate-180" {
		return "transform: rotate(180deg);"
	}

	// Visibility
	if cls == "visible" {
		return "visibility: visible;"
	}
	if cls == "invisible" {
		return "visibility: hidden;"
	}

	// Object fit
	if cls == "object-contain" {
		return "object-fit: contain;"
	}
	if cls == "object-cover" {
		return "object-fit: cover;"
	}
	if cls == "object-fill" {
		return "object-fit: fill;"
	}
	if cls == "object-none" {
		return "object-fit: none;"
	}
	if cls == "object-scale-down" {
		return "object-fit: scale-down;"
	}

	// Object position
	if cls == "object-bottom" {
		return "object-position: bottom;"
	}
	if cls == "object-center" {
		return "object-position: center;"
	}
	if cls == "object-left" {
		return "object-position: left;"
	}
	if cls == "object-left-bottom" {
		return "object-position: left bottom;"
	}
	if cls == "object-left-top" {
		return "object-position: left top;"
	}
	if cls == "object-right" {
		return "object-position: right;"
	}
	if cls == "object-right-bottom" {
		return "object-position: right bottom;"
	}
	if cls == "object-right-top" {
		return "object-position: right top;"
	}
	if cls == "object-top" {
		return "object-position: top;"
	}

	// Pointer events
	if cls == "pointer-events-none" {
		return "pointer-events: none;"
	}
	if cls == "pointer-events-auto" {
		return "pointer-events: auto;"
	}

	// Screen reader only
	if cls == "sr-only" {
		return "position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border-width: 0;"
	}

	// Background gradient direction
	if cls == "bg-gradient-to-t" {
		return "background-image: linear-gradient(to top, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-tr" {
		return "background-image: linear-gradient(to top right, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-r" {
		return "background-image: linear-gradient(to right, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-br" {
		return "background-image: linear-gradient(to bottom right, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-b" {
		return "background-image: linear-gradient(to bottom, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-bl" {
		return "background-image: linear-gradient(to bottom left, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-l" {
		return "background-image: linear-gradient(to left, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-tl" {
		return "background-image: linear-gradient(to top left, var(--tw-gradient-stops));"
	}

	// Gradient color stops: from-*, via-*, to-*
	if match := regexp.MustCompile(`^from-([a-z]+)-(\d+)$`).FindStringSubmatch(cls); match != nil {
		if hex, ok := theme.Colors[match[1]]; ok {
			if shadeVal, ok := hex[match[2]]; ok {
				return fmt.Sprintf("--tw-gradient-from: %s; --tw-gradient-stops: var(--tw-gradient-from), var(--tw-gradient-to);", shadeVal)
			}
		}
	}
	if match := regexp.MustCompile(`^from-([a-z]+)$`).FindStringSubmatch(cls); match != nil {
		if val, ok := theme.BgColors[match[1]]; ok {
			return fmt.Sprintf("--tw-gradient-from: %s; --tw-gradient-stops: var(--tw-gradient-from), var(--tw-gradient-to);", val)
		}
	}
	if match := regexp.MustCompile(`^via-([a-z]+)-(\d+)$`).FindStringSubmatch(cls); match != nil {
		if hex, ok := theme.Colors[match[1]]; ok {
			if shadeVal, ok := hex[match[2]]; ok {
				return fmt.Sprintf("--tw-gradient-stops: var(--tw-gradient-from), %s, var(--tw-gradient-to);", shadeVal)
			}
		}
	}
	if match := regexp.MustCompile(`^via-([a-z]+)$`).FindStringSubmatch(cls); match != nil {
		if val, ok := theme.BgColors[match[1]]; ok {
			return fmt.Sprintf("--tw-gradient-stops: var(--tw-gradient-from), %s, var(--tw-gradient-to);", val)
		}
	}
	if match := regexp.MustCompile(`^to-([a-z]+)-(\d+)$`).FindStringSubmatch(cls); match != nil {
		if hex, ok := theme.Colors[match[1]]; ok {
			if shadeVal, ok := hex[match[2]]; ok {
				return fmt.Sprintf("--tw-gradient-to: %s;", shadeVal)
			}
		}
	}
	if match := regexp.MustCompile(`^to-([a-z]+)$`).FindStringSubmatch(cls); match != nil {
		if val, ok := theme.BgColors[match[1]]; ok {
			return fmt.Sprintf("--tw-gradient-to: %s;", val)
		}
	}

	// Arbitrary values with bracket syntax: w-[30px], top-[117px], text-[#bada55]
	if match := regexp.MustCompile(`^([a-z-]+)-\[(.+)\]$`).FindStringSubmatch(cls); match != nil {
		prop := match[1]
		val := match[2]
		cssProp := arbitraryPropToCSS(prop, val)
		if cssProp != "" {
			return cssProp
		}
	}

	return ""
}

func spacingValue(key string, theme TailwindTheme) string {
	if val, ok := theme.Spacing[key]; ok {
		return val
	}
	if match := regexp.MustCompile(`^(\d+)$`).FindStringSubmatch(key); match != nil {
		n := match[1]
		if n == "0" {
			return "0"
		}
		if n == "0.5" {
			return "0.125rem"
		}
		if n == "1" {
			return "0.25rem"
		}
		if n == "1.5" {
			return "0.375rem"
		}
		if n == "2" {
			return "0.5rem"
		}
		if n == "2.5" {
			return "0.625rem"
		}
		if n == "3" {
			return "0.75rem"
		}
		if n == "3.5" {
			return "0.875rem"
		}
		if n == "4" {
			return "1rem"
		}
		if n == "5" {
			return "1.25rem"
		}
		if n == "6" {
			return "1.5rem"
		}
		if n == "7" {
			return "1.75rem"
		}
		if n == "8" {
			return "2rem"
		}
		if n == "9" {
			return "2.25rem"
		}
		if n == "10" {
			return "2.5rem"
		}
		if n == "11" {
			return "2.75rem"
		}
		if n == "12" {
			return "3rem"
		}
		if n == "14" {
			return "3.5rem"
		}
		if n == "16" {
			return "4rem"
		}
		if n == "20" {
			return "5rem"
		}
		if n == "24" {
			return "6rem"
		}
		if n == "28" {
			return "7rem"
		}
		if n == "32" {
			return "8rem"
		}
		if n == "36" {
			return "9rem"
		}
		if n == "40" {
			return "10rem"
		}
		if n == "44" {
			return "11rem"
		}
		if n == "48" {
			return "12rem"
		}
		if n == "52" {
			return "13rem"
		}
		if n == "56" {
			return "14rem"
		}
		if n == "60" {
			return "15rem"
		}
		if n == "64" {
			return "16rem"
		}
		if n == "72" {
			return "18rem"
		}
		if n == "80" {
			return "20rem"
		}
		if n == "96" {
			return "24rem"
		}
		return n + "px"
	}
	if strings.HasPrefix(key, "[") && strings.HasSuffix(key, "]") {
		return key[1 : len(key)-1]
	}
	return key
}

func sizingValue(key string, theme TailwindTheme) string {
	if val, ok := theme.Sizing[key]; ok {
		return val
	}
	switch key {
	case "1/2":
		return "50%"
	case "1/3":
		return "33.333333%"
	case "2/3":
		return "66.666667%"
	case "1/4":
		return "25%"
	case "3/4":
		return "75%"
	case "1/5":
		return "20%"
	case "2/5":
		return "40%"
	case "3/5":
		return "60%"
	case "4/5":
		return "80%"
	case "1/6":
		return "16.666667%"
	case "5/6":
		return "83.333333%"
	case "full":
		return "100%"
	case "screen":
		return "100vw"
	case "auto":
		return "auto"
	case "min":
		return "min-content"
	case "max":
		return "max-content"
	case "fit":
		return "fit-content"
	}
	if match := regexp.MustCompile(`^(\d+)$`).FindStringSubmatch(key); match != nil {
		return spacingValue(key, theme)
	}
	if strings.HasPrefix(key, "[") && strings.HasSuffix(key, "]") {
		return key[1 : len(key)-1]
	}
	return key
}

func arbitraryPropToCSS(prop, val string) string {
	switch prop {
	case "w":
		return "width: " + val + ";"
	case "h":
		return "height: " + val + ";"
	case "min-w":
		return "min-width: " + val + ";"
	case "min-h":
		return "min-height: " + val + ";"
	case "max-w":
		return "max-width: " + val + ";"
	case "max-h":
		return "max-height: " + val + ";"
	case "p":
		return "padding: " + val + ";"
	case "px":
		return "padding-left: " + val + "; padding-right: " + val + ";"
	case "py":
		return "padding-top: " + val + "; padding-bottom: " + val + ";"
	case "pt":
		return "padding-top: " + val + ";"
	case "pr":
		return "padding-right: " + val + ";"
	case "pb":
		return "padding-bottom: " + val + ";"
	case "pl":
		return "padding-left: " + val + ";"
	case "m":
		return "margin: " + val + ";"
	case "mx":
		return "margin-left: " + val + "; margin-right: " + val + ";"
	case "my":
		return "margin-top: " + val + "; margin-bottom: " + val + ";"
	case "mt":
		return "margin-top: " + val + ";"
	case "mr":
		return "margin-right: " + val + ";"
	case "mb":
		return "margin-bottom: " + val + ";"
	case "ml":
		return "margin-left: " + val + ";"
	case "gap":
		return "gap: " + val + ";"
	case "gap-x":
		return "column-gap: " + val + ";"
	case "gap-y":
		return "row-gap: " + val + ";"
	case "top":
		return "top: " + val + ";"
	case "right":
		return "right: " + val + ";"
	case "bottom":
		return "bottom: " + val + ";"
	case "left":
		return "left: " + val + ";"
	case "inset":
		return "inset: " + val + ";"
	case "text":
		return "color: " + val + ";"
	case "bg":
		return "background-color: " + val + ";"
	case "border":
		return "border-color: " + val + ";"
	case "rounded":
		return "border-radius: " + val + ";"
	case "opacity":
		return "opacity: " + val + ";"
	case "z":
		return "z-index: " + val + ";"
	case "shadow":
		return "box-shadow: " + val + ";"
	case "translate-x":
		return "transform: translateX(" + val + ");"
	case "translate-y":
		return "transform: translateY(" + val + ");"
	case "rotate":
		return "transform: rotate(" + val + ");"
	case "scale":
		return "transform: scale(" + val + ");"
	case "blur":
		return "filter: blur(" + val + ");"
	case "brightness":
		return "filter: brightness(" + val + ");"
	case "contrast":
		return "filter: contrast(" + val + ");"
	case "from":
		return "--tw-gradient-from: " + val + "; --tw-gradient-stops: var(--tw-gradient-from), var(--tw-gradient-to);"
	case "via":
		return "--tw-gradient-stops: var(--tw-gradient-from), " + val + ", var(--tw-gradient-to);"
	case "to":
		return "--tw-gradient-to: " + val + ";"
	}
	return ""
}

func variantToPseudo(variant string) string {
	switch variant {
	case "hover":
		return "&:hover"
	case "focus":
		return "&:focus"
	case "active":
		return "&:active"
	case "visited":
		return "&:visited"
	case "disabled":
		return "&:disabled"
	case "checked":
		return "&:checked"
	case "first":
		return "&:first-child"
	case "last":
		return "&:last-child"
	case "odd":
		return "&:nth-child(odd)"
	case "even":
		return "&:nth-child(even)"
	case "focus-within":
		return "&:focus-within"
	case "focus-visible":
		return "&:focus-visible"
	case "group-hover":
		return ".group:hover &"
	case "group-focus":
		return ".group:focus &"
	case "placeholder":
		return "&::placeholder"
	case "before":
		return "&::before"
	case "after":
		return "&::after"
	}
	return ""
}

func variantToBreakpoint(variant string) string {
	switch variant {
	case "sm":
		return "640px"
	case "md":
		return "768px"
	case "lg":
		return "1024px"
	case "xl":
		return "1280px"
	case "2xl":
		return "1536px"
	case "3xl":
		return "1920px"
	}
	return ""
}

func escapeSelector(s string) string {
	return strings.ReplaceAll(s, ":", "\\:")
}
