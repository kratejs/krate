package caps

import (
	"strings"
	"testing"
)

func TestParseViewState(t *testing.T) {
	cases := []struct {
		name, raw, fallback string
		want                ViewState
	}{
		{"route and scroll", `{"r":"/tasks/4?tab=2#top","x":0,"y":812.5,"w":""}`, "/", ViewState{"/tasks/4?tab=2#top", 0, 812.5}},
		{"window id put back", `{"r":"/a?x=1","x":0,"y":3,"w":"w7"}`, "/", ViewState{"/a?x=1&__krate_win=w7", 0, 3}},
		{"window id put back without query", `{"r":"/a#f","x":0,"y":0,"w":"w7"}`, "/", ViewState{"/a?__krate_win=w7#f", 0, 0}},
		{"window id kept", `{"r":"/a?__krate_win=w9","x":0,"y":0,"w":"w7"}`, "/", ViewState{"/a?__krate_win=w9", 0, 0}},
		{"no answer", ``, "/settings?z=1", ViewState{"/settings?z=1", 0, 0}},
		{"garbage", `not json`, "settings", ViewState{"/settings", 0, 0}},
		{"other origin is refused", `{"r":"//evil.example/x","y":5}`, "/ok", ViewState{"/ok", 0, 0}},
		{"relative route is refused", `{"r":"x","y":5}`, "", ViewState{"/", 0, 0}},
		{"absurd scroll is dropped", `{"r":"/a","x":-4,"y":1e12}`, "/", ViewState{"/a", 0, 0}},
	}
	for _, c := range cases {
		if got := ParseViewState(c.raw, c.fallback); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestScrollScript(t *testing.T) {
	if ScrollScript(0, 0) != "" {
		t.Error("nothing to restore must produce no script")
	}
	js := ScrollScript(0, 812.5)
	if !strings.Contains(js, "(0,812.5)") || !strings.Contains(js, "scrollTo") {
		t.Errorf("script %q", js)
	}
}

// The quit decision of every platform: the app quits when the last window is
// gone unless keepAlive is set.
func TestQuitWhenEmpty(t *testing.T) {
	for _, c := range []struct {
		keep      bool
		remaining int
		want      bool
	}{
		{false, 0, true}, {false, 1, false}, {true, 0, false}, {true, 3, false},
	} {
		if got := QuitWhenEmpty(c.keep, c.remaining); got != c.want {
			t.Errorf("QuitWhenEmpty(%v, %d) = %v, want %v", c.keep, c.remaining, got, c.want)
		}
	}
}

func TestParkOnClose(t *testing.T) {
	for _, c := range []struct {
		keep         bool
		live, parked int
		want         bool
		scenario     string
	}{
		{true, 0, 0, true, "last live window of a keepAlive app is parked"},
		{true, 1, 0, false, "another live window remains: close for real"},
		{true, 0, 1, false, "one window is parked already: do not collect more"},
		{false, 0, 0, false, "without keepAlive the window closes and the app quits"},
	} {
		if got := ParkOnClose(c.keep, c.live, c.parked); got != c.want {
			t.Errorf("%s: got %v", c.scenario, got)
		}
	}
}
