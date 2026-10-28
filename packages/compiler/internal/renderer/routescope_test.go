package renderer

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func TestRouteScopeBlockers(t *testing.T) {
	sigs := []irtree.ComponentSignature{
		{ComponentID: "p", Tier: irtree.TierClient},
		{ComponentID: "_la_x", Tier: irtree.TierClient, RouteScope: "_la_"},
		{ComponentID: "_lb_x", Tier: irtree.TierClient, RouteScope: "_lb_", Lazy: true},
		{ComponentID: "_lc_x", Tier: irtree.TierClient, RouteScope: "_lc_", Boundary: "s1"},
	}
	got := RouteScopeBlockers(sigs)
	if got["_la_"] != "" || got["_lb_"] == "" || got["_lc_"] == "" {
		t.Errorf("blockers = %v", got)
	}
	sigs = append(sigs, irtree.ComponentSignature{ComponentID: "k", Tier: irtree.TierClient, Class: true})
	if RouteScopeBlockers(sigs)["_la_"] == "" {
		t.Errorf("owner-tree pages must block every scope")
	}
}

func TestHydrationBracketsRouteScopes(t *testing.T) {
	res := &EmitResult{Signatures: []irtree.ComponentSignature{
		{ComponentID: "p", Tier: irtree.TierClient, Effects: []string{"createEffect(()=>{})"}},
		{ComponentID: "_la_x", Tier: irtree.TierClient, RouteScope: "_la_", Effects: []string{"createEffect(()=>{})"}},
	}}
	js := GenerateNewHydrationJS(res)
	open := strings.Index(js, `krateEnterScope("_la_")`)
	layout := strings.Index(js, "// component _la_x")
	page := strings.Index(js, "// component p")
	if open < 0 || layout < open || page > open {
		t.Errorf("layout scope not bracketed (open %d layout %d page %d):\n%s", open, layout, page, js)
	}
	if !strings.Contains(js[layout:], "krateLeaveScope();}") {
		t.Errorf("scope not closed:\n%s", js)
	}
}
