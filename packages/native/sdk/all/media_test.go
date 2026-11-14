package all

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/media"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

type mediaRig struct {
	set  permission.Set
	host *sdk.Host
	plat *fakeplat.Media
	main *fakeplat.Window
	c    *sdktest.Caller
}

func newMediaRig(t *testing.T, policy map[string]string, perms ...string) *mediaRig {
	t.Helper()
	isolate(t)
	set, err := permission.Parse(perms)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "com.acme.media", Media: MediaOptions{Policy: policy}}); err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterAreas(reg, set)
	host := sdk.NewHost()
	plat := fakeplat.NewMedia()
	host.Attach(plat)
	t.Cleanup(host.Close)
	RegisterUI(reg, host, set, nil)
	w, err := plat.NewWindow(platform.WindowOptions{Title: "main", Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	host.AddWindow("main", w)
	return &mediaRig{set: set, host: host, plat: plat, main: w.(*fakeplat.Window), c: sdktest.New(t, reg)}
}

func (r *mediaRig) call(from, fn string, args ...any) sdktest.Result {
	return r.c.CallFrom(from, fn, func(e *wire.Encoder) {
		for _, a := range args {
			switch v := a.(type) {
			case string:
				e.String(v)
			case bool:
				e.Bool(v)
			case int:
				e.Int(int64(v))
			}
		}
	})
}

func TestMediaScopeAndPermissionGates(t *testing.T) {
	// The app has "media" and "location", but the new view is scoped to window
	// only: its camera request is denied even though the policy allows it.
	r := newMediaRig(t, map[string]string{"camera": "allow", "location": "allow"}, "media", "location", "window")
	res := r.c.CallFrom("main", "krate/window.addView", func(e *wire.Encoder) {
		e.String("main")
		e.BeginObject()
		e.Key("id")
		e.String("narrow")
		perms("window")(e)
		e.EndObject()
	})
	if res.Failed {
		t.Fatalf("addView: %+v", res)
	}
	var narrow caps.ViewRef
	for _, v := range r.main.Views() {
		if id, ok := r.host.ViewWindow(v.Ref); ok && id == "narrow" {
			narrow = v.Ref
		}
	}
	if narrow == 0 {
		t.Fatal("the new view has no platform ref")
	}
	if r.plat.Request(narrow, caps.PermCamera) {
		t.Error("a view whose scope lacks media got the camera")
	}
	if r.plat.Request(narrow, caps.PermLocation) {
		t.Error("a view whose scope lacks location got the location")
	}
	if !r.plat.Request(r.main.PrimaryView(), caps.PermCamera) || !r.plat.Request(r.main.PrimaryView(), caps.PermLocation) {
		t.Error("the main view lost its permissions")
	}
	// Without media:manage the page can read but not change the policy.
	if res := r.call("main", "krate/media.policy"); res.Failed {
		t.Errorf("reading the policy needs only media: %+v", res)
	}
	if res := r.call("main", "krate/media.setPolicy", "camera", "deny"); res.ErrCode != "permission_denied" || !strings.Contains(res.ErrData, "media:manage") {
		t.Errorf("setPolicy without media:manage: %+v", res)
	}
	if res := r.call("main", "krate/media.sources"); res.ErrCode != "permission_denied" {
		t.Errorf("sources without media:manage: %+v", res)
	}
}

func TestMediaSources(t *testing.T) {
	r := newMediaRig(t, nil, "media", "media:manage")
	r.plat.Sources = []caps.CaptureSource{
		{ID: "screen:1", Kind: "screen", Name: "Display 1", Width: 1920, Height: 1080, Thumbnail: []byte{1, 2, 3}},
		{ID: "window:7", Kind: "window", Name: "Editor", App: "Code", Width: 800, Height: 600},
	}
	list, err := media.Sources(160)
	if err != nil || len(list) != 2 || list[0].ID != "screen:1" || len(list[0].Thumbnail) != 3 || list[1].App != "Code" || list[1].Thumbnail != nil {
		t.Fatalf("sources: %+v %v", list, err)
	}
	list, err = media.Sources(0)
	if err != nil || list[0].Thumbnail != nil {
		t.Fatalf("no thumbnails: %+v %v", list, err)
	}
	if res := r.call("main", "krate/media.sources"); res.Failed {
		t.Errorf("sources from the UI without options: %+v", res)
	}
	// No engine can be told the source.
	res := r.call("main", "krate/media.chooseSource", "screen:1")
	if res.ErrCode != "unsupported" {
		t.Errorf("chooseSource: %+v", res)
	}
}
