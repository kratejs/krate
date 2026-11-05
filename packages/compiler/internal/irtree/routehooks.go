package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Router hooks at build time.
//
// usePathname(), useParams() and the other router hooks of @krate/runtime read
// the page's own route, which the build knows: the concrete URL of a page
// expanded from generateStaticParams, or the URL with the template's
// placeholders for a dynamic route (the server substitutes those per request).
// The hooks fold to those values so the static HTML is right before any script
// runs; the hydration code still calls the real hooks, which follow the URL.

// routeHookValues returns the hook functions for a page at path with params.
// path is "" when the build has no page URL (a layout), in which case the
// hooks yield unknown values and their slots are filled in the browser.
func routeHookValues(path string, params map[string]string) valueMap {
	out := valueMap{}
	if path == "" {
		return out
	}
	clean := path
	if len(clean) > 1 {
		clean = strings.TrimRight(clean, "/")
		if clean == "" {
			clean = "/"
		}
	}
	segments := func(depth int) []jsvalue.Value {
		var vals []jsvalue.Value
		parts := strings.Split(strings.Trim(clean, "/"), "/")
		if len(parts) == 1 && parts[0] == "" {
			parts = nil
		}
		for i, p := range parts {
			if i >= depth {
				vals = append(vals, jsvalue.String(p))
			}
		}
		return vals
	}
	depthOf := func(args []jsvalue.Value) int {
		if len(args) > 1 {
			if n := args[1]; !n.IsUnknown() {
				return int(n.ToNumber())
			}
		}
		return 0
	}
	out["usePathname"] = jsvalue.Func("usePathname", func([]jsvalue.Value) jsvalue.Value { return jsvalue.String(clean) })
	out["useParams"] = jsvalue.Func("useParams", func([]jsvalue.Value) jsvalue.Value {
		obj := jsvalue.NewObject()
		for k, v := range params {
			obj.Set(k, jsvalue.String(v))
		}
		return obj
	})
	out["useSearchParams"] = jsvalue.Func("useSearchParams", func([]jsvalue.Value) jsvalue.Value {
		// The query string is a request value: the static page sees none.
		obj := jsvalue.NewObject()
		obj.Set("get", jsvalue.Func("get", func([]jsvalue.Value) jsvalue.Value { return jsvalue.Null() }))
		obj.Set("getAll", jsvalue.Func("getAll", func([]jsvalue.Value) jsvalue.Value { return jsvalue.NewArray() }))
		obj.Set("has", jsvalue.Func("has", func([]jsvalue.Value) jsvalue.Value { return jsvalue.Bool(false) }))
		obj.Set("toString", jsvalue.Func("toString", func([]jsvalue.Value) jsvalue.Value { return jsvalue.String("") }))
		return obj
	})
	out["useRouter"] = jsvalue.Func("useRouter", func([]jsvalue.Value) jsvalue.Value {
		obj := jsvalue.NewObject()
		for _, m := range []string{"push", "replace", "back", "forward", "refresh", "prefetch"} {
			obj.Set(m, jsvalue.Func(m, func([]jsvalue.Value) jsvalue.Value { return jsvalue.Undefined() }))
		}
		return obj
	})
	out["useSelectedLayoutSegments"] = jsvalue.Func("useSelectedLayoutSegments", func(args []jsvalue.Value) jsvalue.Value {
		return jsvalue.ArrayOf(segments(depthOf(args)))
	})
	out["useSelectedLayoutSegment"] = jsvalue.Func("useSelectedLayoutSegment", func(args []jsvalue.Value) jsvalue.Value {
		segs := segments(depthOf(args))
		if len(segs) == 0 {
			return jsvalue.Null()
		}
		return segs[0]
	})
	return out
}
