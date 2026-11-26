package routeconv

import "testing"

func TestReservedBase(t *testing.T) {
	for _, b := range []string{"template", "_template", "_layout", "loading", "error", "404", "not-found", "500", "403", "forbidden", "401", "unauthorized", "_anything"} {
		if !ReservedBase(b) {
			t.Errorf("%q should be reserved", b)
		}
	}
	for _, b := range []string{"index", "other", "templates", "layout", "errors"} {
		if ReservedBase(b) {
			t.Errorf("%q should not be reserved", b)
		}
	}
}
