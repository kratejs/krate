package migrate

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// imagesConfig maps the static parts of Next's images config onto Krate's
// image pipeline. Custom loaders and unsupported formats are reported.
func imagesConfig(e ast.Expr) (map[string]any, []string) {
	obj, ok := e.(*ast.ObjectExpr)
	if !ok {
		return nil, []string{"next.config images is not a static object"}
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
	if v := prop(obj, "remotePatterns"); v != nil {
		if arr, ok := v.(*ast.ArrayExpr); ok {
			for _, el := range arr.Elements {
				p, ok := el.(*ast.ObjectExpr)
				if !ok {
					warnings = append(warnings, "next.config images.remotePatterns contains a non-static entry")
					continue
				}
				host, _ := stringValue(prop(p, "hostname"))
				if host == "" {
					continue
				}
				entry := map[string]string{"hostname": host}
				if path, ok := stringValue(prop(p, "pathname")); ok && path != "" {
					entry["pathname"] = path
				}
				patterns = append(patterns, entry)
			}
		} else {
			warnings = append(warnings, "next.config images.remotePatterns is not a static array")
		}
	}
	if v := prop(obj, "domains"); v != nil {
		if vals, ok := stringArray(v); ok {
			for _, host := range vals {
				patterns = append(patterns, map[string]string{"hostname": host})
			}
		}
	}
	if len(patterns) > 0 {
		out["remotePatterns"] = patterns
	}
	return out, warnings
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
