package windows

import (
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestFrameNavAllowed(t *testing.T) {
	p, err := caps.RemoteOptions{URL: "https://a.test/"}.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	for uri, want := range map[string]bool{
		"https://a.test/x": true, "https://b.test/": false, "about:srcdoc": true, "data:text/html,x": true,
		"blob:https://a.test/u": true, "krate://app/": false, "https://app.localhost/": false, "file:///x": false,
	} {
		if got := frameNavAllowed(p, uri); got != want {
			t.Errorf("%q = %v", uri, got)
		}
	}
}
