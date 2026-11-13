package windows

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/toastscheme"
)

func TestActivationPayloadRoundTrip(t *testing.T) {
	s := toastscheme.Scheme("com.acme.app")
	for _, c := range [][2]string{{"n1", ""}, {"n1", "reply"}, {"unicode é\U0001F600", "a b"}} {
		units := encodeActivation(toastscheme.URI(s, c[0], c[1]))
		if units[len(units)-1] != 0 {
			t.Fatal("payload must end with NUL")
		}
		id, act, ok := decodeActivation(s, units)
		if !ok || id != c[0] || act != c[1] {
			t.Errorf("%v: got %q %q %v", c, id, act, ok)
		}
	}
}

func TestActivationPayloadRejectsForeignData(t *testing.T) {
	s := toastscheme.Scheme("com.acme.app")
	other := toastscheme.Scheme("com.other.app")
	for name, units := range map[string][]uint16{
		"empty":         nil,
		"other app":     encodeActivation(toastscheme.URI(other, "n", "")),
		"not a url":     encodeActivation("hello"),
		"huge":          encodeActivation(strings.Repeat("x", 9000)),
		"no krate flag": encodeActivation(s + "://activate?id=1"),
	} {
		if _, _, ok := decodeActivation(s, units); ok {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestSinkWindowNameIsPerApp(t *testing.T) {
	if sinkWindowName(toastscheme.Scheme("a.b")) == sinkWindowName(toastscheme.Scheme("c.d")) {
		t.Error("two apps share a sink window name")
	}
}
