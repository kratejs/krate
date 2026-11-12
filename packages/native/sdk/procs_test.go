package sdk_test

import (
	"errors"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/automation"
	"github.com/kratejs/krate/packages/native/sdk/extension"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/offscreen"
)

// procApp adds the process capabilities to the fake platform.
type procApp struct {
	*fakeplat.App
	exts []caps.ExtensionInfo
}

func (a *procApp) AutomationEndpoint() caps.Endpoint {
	return caps.Endpoint{Kind: "cdp", URL: "http://127.0.0.1:9222", Port: 9222}
}
func (a *procApp) CDP(v caps.ViewRef, method, params string) (string, error) {
	return `{"method":"` + method + `","params":` + params + `}`, nil
}
func (a *procApp) OnCDPEvent(caps.ViewRef, string, func(string)) (func(), error) {
	return func() {}, nil
}
func (a *procApp) SnapshotView(caps.ViewRef, caps.SnapshotOptions) (caps.Snapshot, error) {
	pix := []byte{0, 0, 255, 255, 0, 0, 255, 255}
	return caps.Snapshot{Width: 2, Height: 1, Stride: 8, Pix: pix}, nil
}
func (a *procApp) ExtensionsSupported() bool { return true }
func (a *procApp) LoadExtension(dir string) (caps.ExtensionInfo, error) {
	i := caps.ExtensionInfo{ID: "e1", Name: dir, Enabled: true}
	a.exts = append(a.exts, i)
	return i, nil
}
func (a *procApp) ListExtensions() ([]caps.ExtensionInfo, error) { return a.exts, nil }
func (a *procApp) RemoveExtension(id string) error {
	a.exts = nil
	return nil
}

func attachProc(t *testing.T) *procApp {
	t.Helper()
	pa := &procApp{App: fakeplat.New()}
	h := sdk.NewHost()
	h.Attach(pa)
	w, err := pa.NewWindow(platform.WindowOptions{Title: "main"})
	if err != nil {
		t.Fatal(err)
	}
	h.AddWindow("main", w)
	t.Cleanup(h.Close)
	return pa
}

func TestOffscreenCaptureThroughTheSDK(t *testing.T) {
	attachProc(t)
	f, err := offscreen.Capture("main", offscreen.CaptureOptions{Format: "bgra"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Width != 2 || f.Height != 1 || f.Format != "bgra" || len(f.Data) != 8 || f.Data[2] != 255 {
		t.Fatalf("%+v", f)
	}
	p, err := offscreen.Capture("main", offscreen.CaptureOptions{})
	if err != nil || p.Format != "png" || len(p.Data) < 8 || string(p.Data[1:4]) != "PNG" {
		t.Fatalf("png frame: %+v %v", p, err)
	}
	if _, err := offscreen.Capture("nope", offscreen.CaptureOptions{}); err == nil {
		t.Fatal("a missing window must fail")
	}
	if _, err := offscreen.Capture("main", offscreen.CaptureOptions{Format: "jpeg"}); err == nil {
		t.Fatal("an unknown format must fail")
	}
}

func TestAutomationOffUntilConfigured(t *testing.T) {
	attachProc(t)
	automation.Configure(false)
	if _, err := automation.CDP("", "Browser.getVersion", nil); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("cdp while off = %v", err)
	}
	ep, err := automation.GetEndpoint()
	if err != nil || ep.Enabled || ep.Kind != "" {
		t.Fatalf("endpoint while off = %+v %v", ep, err)
	}
	automation.Configure(true)
	t.Cleanup(func() { automation.Configure(false) })
	res, err := automation.CDP("main", "Page.reload", map[string]any{"ignoreCache": true})
	if err != nil || string(res) != `{"method":"Page.reload","params":{"ignoreCache":true}}` {
		t.Fatalf("cdp = %s %v", res, err)
	}
	ep, _ = automation.GetEndpoint()
	if !ep.Enabled || ep.Kind != "cdp" || !ep.CDP || ep.Port != 9222 {
		t.Fatalf("endpoint = %+v", ep)
	}
}

func TestExtensionsAreLoadedByDeclaredName(t *testing.T) {
	pa := attachProc(t)
	extension.Configure([]extension.Declared{{Name: "dark", Dir: "/app/extensions/dark"}})
	t.Cleanup(func() { extension.Configure(nil) })
	if names, err := extension.Names(); err != nil || len(names) != 1 || names[0] != "dark" {
		t.Fatalf("names %v %v", names, err)
	}
	if _, err := extension.Load("../../etc"); err == nil {
		t.Fatal("a page must not load an undeclared path")
	}
	info, err := extension.Load("dark")
	if err != nil || info.Name != "/app/extensions/dark" {
		t.Fatalf("load %+v %v", info, err)
	}
	if l, _ := extension.List(); len(l) != 1 {
		t.Fatalf("list %v", l)
	}
	if err := extension.Remove("e1"); err != nil || len(pa.exts) != 0 {
		t.Fatalf("remove %v", err)
	}
}

func TestProcessAreasWithoutPlatformSupport(t *testing.T) {
	h := sdk.NewHost()
	h.Attach(fakeplat.New())
	t.Cleanup(h.Close)
	if _, err := extension.List(); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("extensions on a platform without them = %v", err)
	}
	if _, err := offscreen.Capture("", offscreen.CaptureOptions{}); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("capture without snapshots = %v", err)
	}
	if _, err := offscreen.Open(offscreen.OpenOptions{}); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("offscreen open = %v", err)
	}
}
