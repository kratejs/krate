package toastscheme

import (
	"strings"
	"testing"
)

func TestURIRoundTrip(t *testing.T) {
	s := Scheme("x.y")
	for _, c := range [][2]string{{"t1", ""}, {"t1", "ok"}, {"id with space&=?", "a/b"}, {"", ""}} {
		raw := URI(s, c[0], c[1])
		id, act, ok := ParseURI(s, raw)
		if !ok || id != c[0] || act != c[1] {
			t.Errorf("%q: got %q %q %v", raw, id, act, ok)
		}
		// Windows appends a slash to the authority of some launches.
		id, act, ok = ParseURI(s, strings.Replace(raw, "activate?", "activate/?", 1))
		if !ok || id != c[0] || act != c[1] {
			t.Errorf("%q with slash: got %q %q %v", raw, id, act, ok)
		}
	}
	for _, bad := range []string{"", "https://x", s + "://activate", s + "://activate?id=1", Scheme("other") + "://activate?krate=1&id=1"} {
		if _, _, ok := ParseURI(s, bad); ok {
			t.Errorf("%q must not parse", bad)
		}
	}
}
