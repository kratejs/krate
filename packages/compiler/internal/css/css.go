package css

import (
	"fmt"
	"hash/fnv"
	"os"
	"regexp"
	"sort"
	"strings"

	"krate-compiler/internal/fsutil"
)

var classSelector = regexp.MustCompile(`\.([a-zA-Z_][a-zA-Z0-9_-]*)`)

type Asset struct {
	Path     string
	Content  string
	IsModule bool
}

type ModuleMapping struct {
	LocalVar string
	Mappings map[string]string
}

func Collect(dir string) ([]*Asset, error) {
	var assets []*Asset

	err := fsutil.WalkExt(dir, map[string]bool{".css": true}, nil, func(path string, info os.FileInfo) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		isModule := strings.Contains(info.Name(), ".module.")

		assets = append(assets, &Asset{
			Path:     path,
			Content:  string(data),
			IsModule: isModule,
		})
		return nil
	})

	return assets, err
}

func ProcessModule(path, content string) (scopedCSS string, mapping map[string]string, err error) {
	hash := hashPath(path)
	seen := make(map[string]bool)
	mapping = make(map[string]string)

	scoped := classSelector.ReplaceAllStringFunc(content, func(match string) string {
		name := match[1:]
		if seen[name] {
			return "." + mapping[name]
		}
		seen[name] = true
		scoped := name + "_" + hash
		mapping[name] = scoped
		return "." + scoped
	})

	return scoped, mapping, nil
}

func hashPath(path string) string {
	h := fnv.New32a()
	h.Write([]byte(path))
	v := h.Sum32()
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var buf [6]byte
	for i := 5; i >= 0; i-- {
		buf[i] = chars[v%36]
		v /= 36
	}
	return string(buf[:])
}

func Minify(css string) string {
	var b strings.Builder
	b.Grow(len(css))
	i := 0
	inBlock := false
	wasSpace := false
	n := len(css)

	for i < n {
		ch := css[i]
		switch {
		case ch == '/' && i+1 < n && css[i+1] == '*':
			inBlock = true
			i += 2
		case ch == '*' && i+1 < n && css[i+1] == '/':
			inBlock = false
			i += 2
		case inBlock:
			i++
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			if !wasSpace {
				b.WriteByte(' ')
				wasSpace = true
			}
			i++
		case ch == '{' || ch == '}' || ch == ';' || ch == ',':
			b.WriteByte(ch)
			wasSpace = false
			i++
		default:
			b.WriteByte(ch)
			wasSpace = false
			i++
		}
	}

	minified := strings.TrimSpace(b.String())

	minified = shortenHexColors(minified)
	minified = rgbaToHex(minified)
	minified = removeZeroUnits(minified)
	minified = simplifyCalc(minified)
	minified = removeTrailingSemicolons(minified)
	minified = removeEmptyRules(minified)
	minified = removeDuplicateDeclarations(minified)

	return minified
}

// shortenHexColors shortens #rrggbb to #rgb when possible, and #rrggbbaa to #rgba.
func shortenHexColors(css string) string {
	return hexColorRe.ReplaceAllStringFunc(css, func(match string) string {
		if len(match) == 7 { // #rrggbb
			if match[1] == match[2] && match[3] == match[4] && match[5] == match[6] {
				return "#" + string(match[1]) + string(match[3]) + string(match[5])
			}
		}
		if len(match) == 9 { // #rrggbbaa
			if match[1] == match[2] && match[3] == match[4] && match[5] == match[6] && match[7] == match[8] {
				return "#" + string(match[1]) + string(match[3]) + string(match[5]) + string(match[7])
			}
		}
		return match
	})
}

// removeZeroUnits removes units from 0 values (0px → 0).
func removeZeroUnits(css string) string {
	return zeroUnitRe.ReplaceAllString(css, "0")
}

// removeEmptyRules removes empty rulesets (selectors with no declarations).
func removeEmptyRules(css string) string {
	return emptyRuleRe.ReplaceAllString(css, "")
}

// rgbaToHex converts rgba(r,g,b,1) and rgb(r,g,b) to #hex.
func rgbaToHex(css string) string {
	css = rgbaRe.ReplaceAllStringFunc(css, func(match string) string {
		parts := rgbaRe.FindStringSubmatch(match)
		if len(parts) < 5 {
			return match
		}
		a := parts[4]
		if a != "1" && a != "1.0" && a != "1.00" {
			return match
		}
		r := parseByte(parts[1])
		g := parseByte(parts[2])
		b := parseByte(parts[3])
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	})
	css = rgbRe.ReplaceAllStringFunc(css, func(match string) string {
		parts := rgbRe.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}
		r := parseByte(parts[1])
		g := parseByte(parts[2])
		b := parseByte(parts[3])
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	})
	return css
}

func parseByte(s string) uint8 {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if n > 255 {
		n = 255
	}
	return uint8(n)
}

// simplifyCalc simplifies calc() expressions: calc(0 + X) → X, calc(X + 0) → X, calc(X * 1) → X, etc.
func simplifyCalc(css string) string {
	return calcRe.ReplaceAllStringFunc(css, func(match string) string {
		parts := calcRe.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		expr := strings.TrimSpace(parts[1])
		lower := strings.ToLower(expr)
		// calc(0 + X) → X
		if strings.HasPrefix(lower, "0 + ") {
			return strings.TrimSpace(expr[4:])
		}
		// calc(X + 0) → X
		if strings.HasSuffix(lower, " + 0") {
			return strings.TrimSpace(expr[:len(expr)-4])
		}
		// calc(0 - X) → -X
		if strings.HasPrefix(lower, "0 - ") {
			return "-" + strings.TrimSpace(expr[4:])
		}
		// calc(X * 1) → X
		if strings.HasSuffix(lower, " * 1") {
			return strings.TrimSpace(expr[:len(expr)-4])
		}
		// calc(X * 0) → 0
		if strings.HasSuffix(lower, " * 0") {
			return "0"
		}
		// calc(1 * X) → X
		if strings.HasPrefix(lower, "1 * ") {
			return strings.TrimSpace(expr[4:])
		}
		return match
	})
}

// removeTrailingSemicolons removes semicolons right before closing braces.
func removeTrailingSemicolons(css string) string {
	return trailingSemiRe.ReplaceAllString(css, "}")
}

// removeDuplicateDeclarations removes duplicate property declarations within a rule,
// keeping only the last declaration for each property.
func removeDuplicateDeclarations(css string) string {
	var result strings.Builder
	remaining := css

	for {
		openIdx := strings.Index(remaining, "{")
		if openIdx == -1 {
			result.WriteString(remaining)
			break
		}

		depth := 1
		closeIdx := openIdx + 1
		for closeIdx < len(remaining) && depth > 0 {
			if remaining[closeIdx] == '{' {
				depth++
			} else if remaining[closeIdx] == '}' {
				depth--
			}
			closeIdx++
		}
		closeIdx--

		selector := remaining[:openIdx]
		body := remaining[openIdx+1 : closeIdx]
		rest := remaining[closeIdx+1:]

		deduped := deduplicateBody(body)
		result.WriteString(selector)
		result.WriteByte('{')
		result.WriteString(deduped)
		result.WriteByte('}')
		remaining = rest
	}

	return result.String()
}

func deduplicateBody(body string) string {
	props := make(map[string]string)
	var order []string
	decls := strings.Split(body, ";")

	for _, decl := range decls {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		colonIdx := strings.Index(decl, ":")
		if colonIdx == -1 {
			continue
		}
		prop := strings.TrimSpace(decl[:colonIdx])
		val := strings.TrimSpace(decl[colonIdx+1:])
		lowerProp := strings.ToLower(prop)

		if _, exists := props[lowerProp]; !exists {
			order = append(order, lowerProp)
		}
		props[lowerProp] = val
	}

	var b strings.Builder
	for i, prop := range order {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(prop)
		b.WriteByte(':')
		b.WriteString(props[prop])
	}
	return b.String()
}

var hexColorRe = regexp.MustCompile(`#[0-9a-fA-F]{6,8}\b`)
var zeroUnitRe = regexp.MustCompile(`\b0(px|em|rem|vh|vw|vmin|vmax|%|pt|pc|in|cm|mm|ex|ch|fr)\b`)
var emptyRuleRe = regexp.MustCompile(`[^}]*\{\s*\}`)
var rgbaRe = regexp.MustCompile(`rgba\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(0?\.?\d+|1\.0*)\s*\)`)
var rgbRe = regexp.MustCompile(`rgb\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*\)`)
var calcRe = regexp.MustCompile(`calc\(([^)]+)\)`)
var trailingSemiRe = regexp.MustCompile(`;\s*\}`)
var blockCommentRe = regexp.MustCompile(`/\*[\s\S]*?\*/`)

func Bundle(assets []*Asset) string {
	var b strings.Builder
	for _, a := range assets {
		if !a.IsModule {
			b.WriteString(a.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func ExtractClassNames(css string) []string {
	seen := make(map[string]bool)
	matches := classSelector.FindAllStringSubmatch(css, -1)
	var result []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			result = append(result, m[1])
		}
	}
	sort.Strings(result)
	return result
}

func GenerateMapping(path string, classNames []string) map[string]string {
	hash := hashPath(path)
	mapping := make(map[string]string, len(classNames))
	for _, name := range classNames {
		mapping[name] = name + "_" + hash
	}
	return mapping
}

func ScopeCSS(content string, mapping map[string]string) string {
	return classSelector.ReplaceAllStringFunc(content, func(match string) string {
		name := match[1:]
		if scoped, ok := mapping[name]; ok {
			return "." + scoped
		}
		return match
	})
}

func VerifyMapping(mapping map[string]string) error {
	for orig, scoped := range mapping {
		if !strings.HasPrefix(scoped, orig+"_") {
			return fmt.Errorf("invalid scoped name %q for class %q", scoped, orig)
		}
	}
	return nil
}
