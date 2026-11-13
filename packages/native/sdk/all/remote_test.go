package all

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

func newRemoteRig(t *testing.T, perms ...string) *pageRig {
	t.Helper()
	return newPageRigWith(t, append([]string{"kv"}, perms...)...)
}

// newPageRigWith is newPageRig under another app id, so these tests do not
// share state with the page tests.
func newPageRigWith(t *testing.T, perms ...string) *pageRig {
	t.Helper()
	isolate(t)
	set, err := permission.Parse(append([]string{"window"}, perms...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "com.acme.remote"}); err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterAreas(reg, set)
	host := sdk.NewHost()
	plat := fakeplat.New()
	host.Attach(plat)
	t.Cleanup(host.Close)
	RegisterUI(reg, host, set, nil)
	w, err := plat.NewWindow(platform.WindowOptions{Title: "main", Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	host.AddWindow("main", w)
	return &pageRig{host: host, plat: plat, main: w.(*fakeplat.Window), c: sdktest.New(t, reg)}
}

func remoteOpts(url string, more func(e *wire.Encoder)) func(e *wire.Encoder) {
	return func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("url")
		e.String(url)
		if more != nil {
			more(e)
		}
		e.EndObject()
	}
}

func TestRemoteNeedsPermission(t *testing.T) {
	r := newRemoteRig(t)
	for _, id := range []string{"krate/remote.open"} {
		res := r.c.CallFrom("main", id, remoteOpts("https://a.test/", nil))
		if res.ErrCode != "permission_denied" || !strings.Contains(res.ErrData, "remote") {
			t.Errorf("%s: %+v", id, res)
		}
	}
	// remote alone is not enough for rules, the network settings or the answers.
	r = newRemoteRig(t, "remote")
	for id, perm := range map[string]string{
		"krate/remote.setRules":          "remote:intercept",
		"krate/remote.setUserAgent":      "remote:network",
		"krate/remote.setProxy":          "remote:network",
		"krate/remote.clearData":         "remote:network",
		"krate/remote.answerCertificate": "remote:network",
	} {
		res := r.c.CallFrom("main", id, str(""))
		if res.ErrCode != "permission_denied" || !strings.Contains(res.ErrData, perm) {
			t.Errorf("%s: %+v", id, res)
		}
	}
}

// A remote window and a remote view have an empty scope, whatever the caller
// holds.
func TestRemoteViewsHaveNoScope(t *testing.T) {
	r := newRemoteRig(t, "remote")
	if res := r.c.CallFrom("main", "krate/kv.get", str("k")); res.Failed {
		t.Fatalf("the app itself cannot use kv: %+v", res)
	}
	res := r.c.CallFrom("main", "krate/remote.open", remoteOpts("https://a.test/", func(e *wire.Encoder) { e.Key("id"); e.String("shop") }))
	if res.Failed {
		t.Fatalf("open: %+v", res)
	}
	for _, id := range []string{"krate/kv.get", "krate/remote.open", "krate/window.list", "krate/os.info", "krate/remote.capabilities"} {
		res := r.c.CallFrom("shop", id, str("k"))
		if res.ErrCode != "permission_denied" {
			t.Errorf("a remote window called %s: %+v", id, res)
		}
	}
	res = r.c.CallFrom("main", "krate/remote.addView", func(e *wire.Encoder) {
		e.String("")
		remoteOpts("https://b.test/", func(e *wire.Encoder) { e.Key("id"); e.String("pane") })(e)
	})
	if res.Failed {
		t.Fatalf("addView: %+v", res)
	}
	if res := r.c.CallFrom("pane", "krate/kv.get", str("k")); res.ErrCode != "permission_denied" {
		t.Errorf("a remote view called kv: %+v", res)
	}
	// An unknown view and the forged header of a remote page get nothing either.
	if res := r.c.CallFrom("main", "krate/kv.get", str("k")); res.Failed {
		t.Fatalf("main lost its scope: %+v", res)
	}
	if got := len(r.plat.Windows); got != 2 {
		t.Fatalf("%d windows", got)
	}
}
