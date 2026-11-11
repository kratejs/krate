package sdk_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/gohost"
	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/window"
	"github.com/kratejs/krate/packages/native/wire"
)

// childSrc is a Go host child whose functions use the SDK. It has no
// platform: every call is forwarded to the parent over KRATE_GOHOST_PARENT.
const childSrc = `package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/kratejs/krate/packages/native/gohost"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/app"
	"github.com/kratejs/krate/packages/native/sdk/clipboard"
	"github.com/kratejs/krate/packages/native/sdk/dialog"
	"github.com/kratejs/krate/packages/native/sdk/menu"
	"github.com/kratejs/krate/packages/native/sdk/shortcut"
	"github.com/kratejs/krate/packages/native/sdk/window"
	"github.com/kratejs/krate/packages/native/wire"
)

func str(out *wire.Encoder, s string) error { out.String(s); return nil }

func noArgs(a *wire.Decoder) error {
	if err := a.BeginArray(); err != nil {
		return err
	}
	return a.EndArray()
}

func main() {
	gohost.Serve(func(r *scheme.Registry) {
		r.Func("t.Clip", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			if err := noArgs(a); err != nil { return err }
			if err := clipboard.WriteText("from child é"); err != nil { return err }
			if err := clipboard.WriteImage([]byte{1, 2, 3, 0, 255}); err != nil { return err }
			s, err := clipboard.ReadText()
			if err != nil { return err }
			img, err := clipboard.ReadImage()
			if err != nil { return err }
			return str(out, fmt.Sprintf("%s|%v", s, img))
		})
		r.Func("t.Dialog", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			if err := noArgs(a); err != nil { return err }
			paths, err := dialog.OpenFile(ctx, dialog.OpenOptions{Title: "child", Multiple: true})
			if err != nil { return err }
			return str(out, strings.Join(paths, ","))
		})
		r.Func("t.Window", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			if err := noArgs(a); err != nil { return err }
			w, err := window.Open(window.Options{Title: "from child", Path: "/child", Width: 321, Height: 123})
			if err != nil { return err }
			if err := w.SetAlwaysOnTop(true); err != nil { return err }
			st, err := w.State()
			if err != nil { return err }
			return str(out, fmt.Sprintf("%s %v %d", w.ID, st.AlwaysOnTop, st.Width))
		})
		r.Func("t.Unsupported", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			if err := noArgs(a); err != nil { return err }
			err := window.Get("main").Navigate("/x")
			_ = err
			_, err = window.Get("nope").State()
			if err == nil { return krate.Errorf("test", "expected an error") }
			return str(out, err.Error())
		})
		r.Func("t.WaitMenu", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			if err := noArgs(a); err != nil { return err }
			got := make(chan menu.Event, 1)
			cancel, err := menu.OnItem("go", func(e menu.Event) { got <- e })
			if err != nil { return err }
			defer cancel()
			select {
			case e := <-got:
				return str(out, e.ID+"@"+e.Window)
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		r.Func("t.WaitShortcut", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			if err := noArgs(a); err != nil { return err }
			fired := make(chan struct{}, 1)
			unreg, err := shortcut.Register("Ctrl+Alt+J", func() { fired <- struct{}{} })
			if err != nil { return err }
			defer unreg()
			select {
			case <-fired:
				return str(out, "fired")
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		r.Func("t.Supports", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			if err := noArgs(a); err != nil { return err }
			return str(out, fmt.Sprintf("%v %v %s", app.Supports("dialog"), app.Supports("bogus"), os.Getenv("KRATE_APP_ID")))
		})
		r.Func("t.Quit", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			if err := noArgs(a); err != nil { return err }
			if err := app.Quit(); err != nil { return err }
			return str(out, "quit")
		})
	})
}
`

func startChild(t *testing.T, r *rig) *childRig {
	t.Helper()
	dir, cleanup, err := gohosttest.TempDir("sdkui-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	bin, err := gohosttest.Build(dir, "child", childSrc)
	if err != nil {
		if strings.Contains(err.Error(), "toolchain not found") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	parent, err := r.host.ServeParent()
	if err != nil {
		t.Fatal(err)
	}
	c := gohost.NewChild(gohost.ChildOptions{Logf: t.Logf})
	if err := c.Start(context.Background(), bin, []string{gohost.ParentEnv + "=" + parent, "KRATE_APP_ID=test.child"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Stop(); err != nil {
			t.Error(err)
		}
	})
	return &childRig{c: c, rig: r}
}

type childRig struct {
	c   *gohost.Child
	rig *rig
}

var childFuncs = []string{"t.Clip", "t.Dialog", "t.Window", "t.Unsupported", "t.WaitMenu", "t.WaitShortcut", "t.Supports", "t.Quit"}

// callRemote calls a function of the child through a scheme handler whose
// registry forwards to it, as the shell does for the page.
func callRemote(c *gohost.Child, fn string) (string, error) {
	reg := scheme.NewRegistry()
	c.Remote().RegisterInto(reg, childFuncs, nil, nil)
	h := scheme.New(scheme.Options{Registry: reg})
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
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/__krate/call", bytes.NewReader(e.Frame())))
	d, err := wire.ReadFrame(rec.Body)
	if err != nil {
		return "", err
	}
	raw, err := d.RawValue()
	if err != nil {
		return "", err
	}
	var out struct {
		OK  *string                         `json:"ok"`
		Err *struct{ Code, Message string } `json:"err"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("%s: %w", raw, err)
	}
	if out.Err != nil {
		return "", fmt.Errorf("%s: %s", out.Err.Code, out.Err.Message)
	}
	if out.OK == nil {
		return "", fmt.Errorf("no result: %s", raw)
	}
	return *out.OK, nil
}

func (cr *childRig) call(t *testing.T, fn string) string {
	t.Helper()
	got, err := callRemote(cr.c, fn)
	if err != nil {
		t.Fatalf("%s: %v", fn, err)
	}
	return got
}

func TestTwoProcessBridge(t *testing.T) {
	if os.Getenv(bridge.ParentEnv) != "" {
		t.Skip("running inside a Krate child")
	}
	r := newRig(t)
	cr := startChild(t, r)

	// Request/response through the second process.
	if got := cr.call(t, "t.Clip"); got != "from child é|[1 2 3 0 255]" {
		t.Fatalf("t.Clip = %q", got)
	}
	if r.app.Text != "from child é" {
		t.Fatalf("the platform clipboard holds %q", r.app.Text)
	}

	r.app.FileResult = []string{"/one", "/two words"}
	if got := cr.call(t, "t.Dialog"); got != "/one,/two words" {
		t.Fatalf("t.Dialog = %q", got)
	}
	if o := r.app.FileOpts[0]; o.Title != "child" || !o.Multiple {
		t.Fatalf("options through the bridge: %+v", o)
	}

	if got := cr.call(t, "t.Window"); !strings.HasSuffix(got, " true 321") {
		t.Fatalf("t.Window = %q", got)
	}
	if w := r.app.Windows[len(r.app.Windows)-1]; w.Opts.Path != "/child" || !w.State().AlwaysOnTop {
		t.Fatalf("window %+v %+v", w.Opts, w.State())
	}

	// A coded error survives both hops.
	if got := cr.call(t, "t.Unsupported"); !strings.Contains(got, "not_found") || !strings.Contains(got, "no window") {
		t.Fatalf("t.Unsupported = %q", got)
	}

	if got := cr.call(t, "t.Supports"); got != "true false test.child" {
		t.Fatalf("t.Supports = %q", got)
	}

	// Events travel back: the child waits for a menu click and a shortcut.
	type res struct {
		s   string
		err error
	}
	menuRes := make(chan res, 1)
	go func() {
		s, err := callRemote(cr.c, "t.WaitMenu")
		menuRes <- res{s, err}
	}()
	waitSubs(t, r, "menu")
	r.app.EmitMenu(caps.MenuEvent{ID: "go", Window: r.main.Ref()})
	select {
	case x := <-menuRes:
		if x.err != nil || x.s != "go@main" {
			t.Fatalf("t.WaitMenu = %q, %v", x.s, x.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the menu click never reached the child")
	}

	scRes := make(chan res, 1)
	go func() {
		s, err := callRemote(cr.c, "t.WaitShortcut")
		scRes <- res{s, err}
	}()
	var handle string
	deadline := time.Now().Add(5 * time.Second)
	for handle == "" && time.Now().Before(deadline) {
		if hs := r.app.ShortcutHandles(); len(hs) > 0 {
			handle = hs[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	if handle == "" {
		t.Fatal("the child's shortcut never reached the platform")
	}
	waitSubs(t, r, "shortcut")
	r.app.EmitShortcut(handle)
	select {
	case x := <-scRes:
		if x.err != nil || x.s != "fired" {
			t.Fatalf("t.WaitShortcut = %q, %v", x.s, x.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the shortcut press never reached the child")
	}

	if got := cr.call(t, "t.Quit"); got != "quit" || r.app.QuitCalls() != 1 {
		t.Fatalf("t.Quit = %q, quits %d", got, r.app.QuitCalls())
	}
}

var _ = window.Main
