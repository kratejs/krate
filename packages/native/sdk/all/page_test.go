package all

import (
	"testing"

	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

type pageRig struct {
	host *sdk.Host
	plat *fakeplat.App
	main *fakeplat.Window
	c    *sdktest.Caller
}

func newPageRig(t *testing.T, perms ...string) *pageRig {
	t.Helper()
	isolate(t)
	set, err := permission.Parse(append([]string{"window"}, perms...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "com.acme.page"}); err != nil {
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

func str(s string) func(e *wire.Encoder) { return func(e *wire.Encoder) { e.String(s) } }

func TestPageNeedsPermission(t *testing.T) {
	r := newPageRig(t, "kv")
	for _, id := range []string{"krate/page.zoom", "krate/page.capture", "krate/page.stopFind", "krate/page.print"} {
		if res := r.c.CallFrom("main", id, str("")); res.ErrCode != "permission_denied" {
			t.Errorf("%s: %+v", id, res)
		}
	}
	for _, id := range []string{"krate/downloads.list", "krate/cookies.clearSiteData"} {
		if res := r.c.CallFrom("main", id, nil); res.ErrCode != "permission_denied" {
			t.Errorf("%s: %+v", id, res)
		}
	}
}
