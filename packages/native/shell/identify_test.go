package shell

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/permission"
)

// A request without a native view is identified by what the page claims (browser
// mode); a request from a native view the shell cannot place gets the scope that
// allows nothing, whatever header it carries.
func TestIdentify(t *testing.T) {
	set, err := permission.Parse([]string{"kv", "window"})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Scope(unknownView, []string{}); err != nil {
		t.Fatal(err)
	}
	a := &app{sdk: sdk.NewHost(), perms: set}
	a.sdk.Attach(nil) // no platform: no view is known

	plain := httptest.NewRequest(http.MethodPost, "/__krate/call", nil)
	plain.Header.Set("x-krate-window", "w3")
	if id, native := a.identify(plain); native || id != "" {
		t.Errorf("no native view: %q native=%v, want the header to decide", id, native)
	}

	for _, view := range []caps.ViewRef{0, 7} {
		r := httptest.NewRequest(http.MethodPost, "/__krate/call", nil)
		r.Header.Set("x-krate-window", "main")
		r = r.WithContext(caps.WithView(r.Context(), view))
		id, native := a.identify(r)
		if !native || id != unknownView {
			t.Errorf("view %d: got %q native=%v, want the unknown-view scope", view, id, native)
		}
		if set.AllowsFor(id, "kv") || set.AllowsFor(id, "window") {
			t.Errorf("the unknown-view scope allows something")
		}
		if !set.AllowsFor("main", "kv") {
			t.Error("the app's own windows must keep their permissions")
		}
	}
}
