package renderer

import (
	"strconv"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// RouteScopeBlockers returns, for every layout/template route scope present
// in the page's signatures, the reason its hydration cannot be skipped when
// the SPA router keeps the layout mounted ("" when it can). A blocked scope
// is always re-hydrated, so the router must swap (not keep) its DOM.
func RouteScopeBlockers(sigs []irtree.ComponentSignature) map[string]string {
	out := map[string]string{}
	owned := needsOwnerTree(sigs)
	for _, sig := range sigs {
		if sig.RouteScope == "" {
			continue
		}
		if _, ok := out[sig.RouteScope]; !ok {
			out[sig.RouteScope] = ""
		}
		if out[sig.RouteScope] != "" || sig.Tier != irtree.TierClient {
			continue
		}
		switch {
		case owned:
			out[sig.RouteScope] = "the page uses class components or error boundaries (owner tree hydration)"
		case sig.Lazy:
			out[sig.RouteScope] = "the layout renders a lazy() component"
		case sig.Boundary != "":
			out[sig.RouteScope] = "the layout renders inside a <Suspense> boundary"
		case len(sig.IslandBoot) > 0:
			out[sig.RouteScope] = "the layout passes signals to an external island"
		}
	}
	return out
}

// routeScopeOpen and routeScopeClose bracket the hydration of a layout's
// components: krateEnterScope returns false when the router kept the
// layout's DOM (its bindings and effects are still live), and otherwise tags
// every effect and cleanup created inside with the scope so a later soft
// navigation can keep or dispose them as a unit.
func routeScopeOpen(scope string) string {
	return "if(typeof krateEnterScope!=='function'||krateEnterScope(" + strconv.Quote(scope) + ")){\n"
}

const routeScopeClose = "if(typeof krateEnterScope==='function')krateLeaveScope();}\n"

// routeScoped reports whether sig's hydration is bracketed by its route scope.
func routeScoped(sig irtree.ComponentSignature, blockers map[string]string) bool {
	return sig.RouteScope != "" && blockers[sig.RouteScope] == ""
}
