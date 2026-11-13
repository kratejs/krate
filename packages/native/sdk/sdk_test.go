package sdk_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

type rig struct {
	app  *fakeplat.App
	host *sdk.Host
	main *fakeplat.Window
}

func newRig(t *testing.T) *rig {
	t.Helper()
	fa := fakeplat.New()
	h := sdk.NewHost()
	h.Attach(fa)
	w, err := fa.NewWindow(platform.WindowOptions{Title: "main", Width: 800, Height: 600, Resizable: true})
	if err != nil {
		t.Fatal(err)
	}
	h.AddWindow("main", w)
	t.Cleanup(h.Close)
	return &rig{app: fa, host: h, main: w.(*fakeplat.Window)}
}

// ui returns the page-facing handler with only the named permissions.
func (r *rig) ui(allowed ...string) http.Handler {
	reg := scheme.NewRegistry()
	r.host.RegisterUI(reg, func(p string) bool {
		for _, a := range allowed {
			if a == p {
				return true
			}
		}
		return false
	}, permission.Denied)
	return scheme.New(scheme.Options{Registry: reg})
}

func uiCall(t *testing.T, h http.Handler, fn string, args func(*wire.Encoder)) string {
	t.Helper()
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String(fn)
	e.Key("args")
	e.BeginArray()
	if args != nil {
		args(e)
	}
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/__krate/call", bytes.NewReader(e.Frame()))
	req.Header.Set("x-krate-window", "ui")
	h.ServeHTTP(rec, req)
	d, err := wire.ReadFrame(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := d.RawValue()
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPermissionGating(t *testing.T) {
	r := newRig(t)
	r.app.Text = "secret"
	// Write permission without read: ReadText is denied by name.
	h := r.ui("clipboard")
	if got := uiCall(t, h, "krate/clipboard.writeText", func(e *wire.Encoder) { e.String("hi") }); !strings.Contains(got, `"ok":null`) {
		t.Fatalf("write: %s", got)
	}
	if r.app.Text != "hi" {
		t.Fatal("write did not reach the platform")
	}
	if got := uiCall(t, h, "krate/clipboard.readText", nil); !strings.Contains(got, "permission_denied") || !strings.Contains(got, `"permission":"clipboard:read"`) {
		t.Fatalf("read without clipboard:read: %s", got)
	}
	if got := uiCall(t, h, "krate/dialog.message", func(e *wire.Encoder) { e.BeginObject(); e.EndObject() }); !strings.Contains(got, "permission_denied") || !strings.Contains(got, `"permission":"dialog"`) {
		t.Fatalf("dialog without permission: %s", got)
	}
	h = r.ui("clipboard", "clipboard:read")
	if got := uiCall(t, h, "krate/clipboard.readText", nil); !strings.Contains(got, `"ok":"hi"`) {
		t.Fatalf("read: %s", got)
	}
	// Go-only operations never reach the page.
	h = r.ui("clipboard", "clipboard:read", "app", "window", "dialog", "menu", "tray", "notify", "shortcut")
	for _, id := range []string{"krate/app.singleInstance", "krate/app.launchArgs"} {
		if got := uiCall(t, h, id, nil); !strings.Contains(got, "not_found") {
			t.Fatalf("%s reachable from the page: %s", id, got)
		}
	}
	// Malformed arguments are bad_args, not a crash.
	if got := uiCall(t, h, "krate/dialog.message", func(e *wire.Encoder) { e.Int(3) }); !strings.Contains(got, "bad_args") {
		t.Fatalf("bad args: %s", got)
	}
}

var _ = context.Background
