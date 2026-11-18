package migrate

import (
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// images maps the static parts of Next's images config onto Krate's image
// pipeline. Custom loaders and unsupported formats are reported. A
// remotePatterns list built in code is carried over as code pieces.
func (rc *ruleCtx) images(e ast.Expr) (map[string]any, []string, []*codePiece) {
	obj, ok := e.(*ast.ObjectExpr)
	if !ok {
		return nil, []string{"next.config images is not a static object"}, nil
	}
	out := map[string]any{}
	var warnings []string
	if v := prop(obj, "deviceSizes"); v != nil {
		if arr, ok := intArray(v); ok && len(arr) > 0 {
			out["breakpoints"] = arr
		}
	}
	if v := prop(obj, "formats"); v != nil {
		if vals, ok := stringArray(v); ok {
			var formats []string
			for _, f := range vals {
				switch strings.ToLower(f) {
				case "image/webp", "webp":
					formats = append(formats, "webp")
				case "image/jpeg", "image/png", "image/gif":
					formats = append(formats, "original")
				case "image/avif", "avif":
					warnings = append(warnings, "next.config images uses AVIF, which Krate does not encode")
				}
			}
			if len(formats) > 0 {
				out["formats"] = uniqueStrings(formats)
			}
		}
	}
	if v := prop(obj, "qualities"); v != nil {
		if arr, ok := intArray(v); ok && len(arr) == 1 {
			out["quality"] = arr[0]
		} else if ok {
			warnings = append(warnings, "next.config images.qualities has multiple values; Krate uses one base quality")
		}
	}
	if v := prop(obj, "unoptimized"); v != nil {
		if b, ok := boolValue(v); ok {
			out["unoptimized"] = b
		}
	}
	if prop(obj, "loader") != nil {
		warnings = append(warnings, "next.config images.loader needs a manual image pipeline")
	}
	if prop(obj, "loaderFile") != nil {
		warnings = append(warnings, "next.config images.loaderFile needs a manual image pipeline")
	}

	var patterns []map[string]string
	var pieces []*codePiece
	if v := prop(obj, "remotePatterns"); v != nil {
		var w []string
		patterns, pieces, w = rc.remotePatterns(obj, v)
		warnings = append(warnings, w...)
	}
	if v := prop(obj, "domains"); v != nil {
		if vals, ok := stringArray(v); ok {
			for _, host := range vals {
				patterns = append(patterns, map[string]string{"hostname": host})
			}
		} else {
			warnings = append(warnings, "next.config images.domains is not a static string array and was not translated")
		}
	}
	if len(patterns) > 0 {
		out["remotePatterns"] = patterns
	}
	return out, warnings, pieces
}

// staticPattern reads one remotePatterns entry whose fields are all string
// literals. hostname is required; pathname, protocol, port and search are
// kept when present.
func staticPattern(p *ast.ObjectExpr) (map[string]string, bool) {
	for _, q := range p.Properties {
		if q == nil || q.Spread || q.Computed {
			return nil, false
		}
	}
	host, ok := stringValue(prop(p, "hostname"))
	if !ok || host == "" {
		return nil, false
	}
	entry := map[string]string{"hostname": host}
	for _, key := range []string{"pathname", "protocol", "port", "search"} {
		v := prop(p, key)
		if v == nil {
			continue
		}
		s, ok := stringValue(v)
		if !ok {
			return nil, false
		}
		if s != "" || key == "port" || key == "search" {
			entry[key] = s
		}
	}
	return entry, true
}

// remotePatterns translates the value of images.remotePatterns: static
// entries become config entries, computed entries (an env-built hostname, a
// list another statement pushes to) become code pieces.
func (rc *ruleCtx) remotePatterns(obj *ast.ObjectExpr, v ast.Expr) (patterns []map[string]string, pieces []*codePiece, warnings []string) {
	switch val := v.(type) {
	case *ast.ArrayExpr:
		return rc.patternArray(val)
	case *ast.Identifier:
		if arr := rc.resolveArray(val); arr != nil && !rc.pre.mutatedElsewhere(val.Name) {
			return rc.patternArray(arr)
		}
		if rc.pre.declares(val.Name) {
			return nil, []*codePiece{{Label: "images.remotePatterns", Text: "..." + val.Name}}, nil
		}
	default:
		if text, ok := rc.propText(obj, "remotePatterns"); ok && rc.pre != nil {
			return nil, []*codePiece{{Label: "images.remotePatterns", Text: "...(" + text + ")"}}, nil
		}
	}
	return nil, nil, []string{"next.config images.remotePatterns is not a static array"}
}

func (rc *ruleCtx) patternArray(arr *ast.ArrayExpr) (patterns []map[string]string, pieces []*codePiece, warnings []string) {
	parts := rc.arrayParts(arr)
	for i, el := range arr.Elements {
		p, ok := el.(*ast.ObjectExpr)
		if ok {
			if entry, static := staticPattern(p); static {
				patterns = append(patterns, entry)
				continue
			}
		}
		if parts != nil && rc.pre != nil {
			pieces = append(pieces, &codePiece{Label: fmt.Sprintf("images.remotePatterns[%d]", i), Text: parts[i]})
			continue
		}
		if !ok {
			warnings = append(warnings, "next.config images.remotePatterns contains a non-static entry")
		} else {
			warnings = append(warnings, "next.config images.remotePatterns has an entry whose hostname is computed (for example from an environment variable) and was not translated")
		}
	}
	return patterns, pieces, warnings
}

func intArray(e ast.Expr) ([]int, bool) {
	arr, ok := e.(*ast.ArrayExpr)
	if !ok {
		return nil, false
	}
	out := make([]int, 0, len(arr.Elements))
	for _, el := range arr.Elements {
		n, ok := intValue(el)
		if !ok {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

func stringArray(e ast.Expr) ([]string, bool) {
	arr, ok := e.(*ast.ArrayExpr)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr.Elements))
	for _, el := range arr.Elements {
		s, ok := stringValue(el)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
