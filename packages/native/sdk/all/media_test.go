package all

import (
	"strings"
	"testing"
	"time"

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

func TestMediaHandlerIsInstalledAndPolicyApplies(t *testing.T) {
	r := newMediaRig(t, map[string]string{"camera": "allow", "microphone": "deny"}, "media", "media:manage", "window")
	if !r.plat.HasHandler() {
		t.Fatal("the SDK did not install the permission handler")
	}
	if !r.host.SupportsForTest("media") {
		t.Error("app.supports(media) is false on a platform with the handler")
	}
	view := r.main.PrimaryView()
	if !r.plat.Request(view, caps.PermCamera) {
		t.Error("camera allowed by policy was denied")
	}
	if r.plat.Request(view, caps.PermMicrophone) || r.plat.Request(view, caps.PermCamera, caps.PermMicrophone) {
		t.Error("microphone denied by policy was allowed")
	}
	res := r.call("main", "krate/media.policy")
	if res.Failed || !strings.Contains(res.OK, `"camera":"allow"`) || !strings.Contains(res.OK, `"microphone":"deny"`) || !strings.Contains(res.OK, `"location":"prompt"`) {
		t.Fatalf("policy: %+v", res)
	}
	if res := r.call("main", "krate/media.setPolicy", "microphone", "allow"); res.Failed {
		t.Fatalf("setPolicy: %+v", res)
	}
	if !r.plat.Request(view, caps.PermMicrophone) {
		t.Error("microphone after setPolicy(allow) was denied")
	}
	if res := r.call("main", "krate/media.setPolicy", "microphone", "sometimes"); res.ErrCode != "bad_args" {
		t.Errorf("a bad policy: %+v", res)
	}
}

func TestMediaPromptAndRememberedDecisions(t *testing.T) {
	r := newMediaRig(t, nil, "media", "media:manage")
	view := r.main.PrimaryView()
	r.plat.MessageReply = 0 // Allow
	if !r.plat.Request(view, caps.PermCamera, caps.PermMicrophone) {
		t.Fatal("the prompt answered Allow but the request was denied")
	}
	if n := len(r.plat.MessageOpts); n != 1 || !strings.Contains(r.plat.MessageOpts[0].Message, "camera and your microphone") {
		t.Fatalf("prompts: %+v", r.plat.MessageOpts)
	}
	if !r.plat.Request(view, caps.PermCamera) || len(r.plat.MessageOpts) != 1 {
		t.Fatal("a remembered decision must not prompt again")
	}
	res := r.call("main", "krate/media.decisions")
	if res.Failed || strings.Count(res.OK, `"allow":true`) != 2 {
		t.Fatalf("decisions: %+v", res)
	}
	if res := r.call("main", "krate/media.reset", "camera"); res.Failed {
		t.Fatalf("reset: %+v", res)
	}
	r.plat.MessageReply = 1 // Deny
	if r.plat.Request(view, caps.PermCamera) {
		t.Fatal("the prompt answered Deny but the request was allowed")
	}
	if r.plat.Request(view, caps.PermCamera) || len(r.plat.MessageOpts) != 2 {
		t.Fatal("the denial must be remembered")
	}
	if res := r.call("main", "krate/media.reset"); res.Failed {
		t.Fatalf("reset all: %+v", res)
	}
	if res := r.call("main", "krate/media.decisions"); res.OK != "[]" {
		t.Fatalf("after reset: %+v", res)
	}
	if res := r.call("main", "krate/media.decide", "location", "app", true); res.Failed {
		t.Fatalf("decide: %+v", res)
	}
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

func TestMediaWithoutPermissionDeniesRequests(t *testing.T) {
	r := newMediaRig(t, map[string]string{"camera": "allow"}, "window")
	if r.plat.Request(r.main.PrimaryView(), caps.PermCamera) {
		t.Fatal("an app without the media permission got the camera")
	}
	if res := r.call("main", "krate/media.policy"); res.ErrCode != "permission_denied" {
		t.Fatalf("policy: %+v", res)
	}
}

func TestMediaRequestHandlerStream(t *testing.T) {
	r := newMediaRig(t, map[string]string{"camera": "deny"}, "media", "media:manage")
	got := make(chan media.Request, 4)
	cancel, err := media.OnRequest(func(q media.Request) media.Verdict {
		got <- q
		return media.Verdict{Allow: q.Kinds[0] == media.Kind("camera"), Remember: "none"}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitFor(t, func() bool { return r.host.Subscribers("mediaRequests") == 1 })
	view := r.main.PrimaryView()
	if !r.plat.Request(view, caps.PermCamera) {
		t.Fatal("the handler allowed the camera but the request was denied")
	}
	q := <-got
	if q.View != "main" || q.Origin != "krate://app" || len(q.Kinds) != 1 || q.Kinds[0] != "camera" {
		t.Fatalf("handler saw %+v", q)
	}
	if r.plat.Request(view, caps.PermMicrophone) {
		t.Fatal("the handler denied the microphone but the request was allowed")
	}
	// "none" left no decisions behind.
	ds, err := media.Decisions()
	if err != nil || len(ds) != 0 {
		t.Fatalf("decisions %v %v", ds, err)
	}
	cancel()
	waitFor(t, func() bool { return r.host.Subscribers("mediaRequests") == 0 })
	// With no handler the policy decides again (camera: deny).
	if r.plat.Request(view, caps.PermCamera) {
		t.Fatal("policy deny ignored after the handler left")
	}
}

func TestMediaOSConsentIDs(t *testing.T) {
	r := newMediaRig(t, map[string]string{"camera": "allow", "microphone": "allow"}, "media", "media:manage")
	r.plat.Status[caps.PermCamera] = caps.PrivacyDenied
	r.plat.Status[caps.PermMicrophone] = caps.PrivacyNotDetermined
	view := r.main.PrimaryView()
	if r.plat.Request(view, caps.PermCamera) {
		t.Error("camera denied by the OS was allowed")
	}
	if !r.plat.Request(view, caps.PermMicrophone) || len(r.plat.Asked) != 1 {
		t.Errorf("microphone: asked %v", r.plat.Asked)
	}
	if res := r.call("main", "krate/media.osStatus", "camera"); unquote(res.OK) != "denied" {
		t.Errorf("osStatus: %+v", res)
	}
	if res := r.call("main", "krate/media.osStatus", "location"); unquote(res.OK) != "unknown" {
		t.Errorf("osStatus: %+v", res)
	}
	if res := r.call("main", "krate/media.osStatus", "clipboard"); res.ErrCode != "bad_args" {
		t.Errorf("osStatus of a kind without a switch: %+v", res)
	}
	r.plat.Status[caps.PermScreen] = caps.PrivacyNotDetermined
	r.plat.OnAsk = caps.PrivacyGranted
	if res := r.call("main", "krate/media.osRequest", "screen"); unquote(res.OK) != "granted" {
		t.Errorf("osRequest: %+v", res)
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

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
