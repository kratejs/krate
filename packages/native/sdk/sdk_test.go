package sdk_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/app"
	"github.com/kratejs/krate/packages/native/sdk/clipboard"
	"github.com/kratejs/krate/packages/native/sdk/dialog"
	sdkfs "github.com/kratejs/krate/packages/native/sdk/fs"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/menu"
	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/sdk/shortcut"
	"github.com/kratejs/krate/packages/native/sdk/window"
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

func TestDialogRoundTrip(t *testing.T) {
	r := newRig(t)
	r.app.FileResult = []string{"/a/b.png", "/c d/e.png"}
	got, err := dialog.OpenFile(krate.Background(), dialog.OpenOptions{
		Title: "Pick", Dir: "/home", Multiple: true, ShowHidden: true,
		Filters: []dialog.Filter{{Name: "Images", Patterns: []string{"*.png", "*.jpg"}}},
	})
	if err != nil || !reflect.DeepEqual(got, r.app.FileResult) {
		t.Fatalf("OpenFile = %v, %v", got, err)
	}
	o := r.app.FileOpts[0]
	if o.Mode != caps.OpenFile || o.Title != "Pick" || o.Dir != "/home" || !o.Multiple || !o.ShowHidden ||
		!reflect.DeepEqual(o.Filters, []caps.FileFilter{{Name: "Images", Patterns: []string{"*.png", "*.jpg"}}}) {
		t.Fatalf("platform options %+v", o)
	}
	r.app.FileResult = nil
	p, err := dialog.SaveFile(krate.Background(), dialog.SaveOptions{Name: "x.txt", Overwrite: true})
	if err != nil || p != "" {
		t.Fatalf("cancelled SaveFile = %q, %v", p, err)
	}
	if o := r.app.FileOpts[1]; o.Mode != caps.SaveFile || o.Name != "x.txt" || !o.ConfirmWrite {
		t.Fatalf("save options %+v", o)
	}
	r.app.FileResult = []string{"/out.txt"}
	if p, _ = dialog.SaveFile(krate.Background(), dialog.SaveOptions{}); p != "/out.txt" {
		t.Fatalf("SaveFile = %q", p)
	}
	if _, err := dialog.OpenFolder(krate.Background(), dialog.OpenOptions{}); err != nil || r.app.FileOpts[3].Mode != caps.OpenFolder {
		t.Fatalf("OpenFolder %v %+v", err, r.app.FileOpts)
	}
	r.app.MessageReply = 1
	yes, err := dialog.Confirm(krate.Background(), "Sure?", "Delete?")
	if err != nil || !yes {
		t.Fatalf("Confirm = %v, %v", yes, err)
	}
	if m := r.app.MessageOpts[0]; m.Kind != caps.MessageQuestion || m.Title != "Sure?" || !reflect.DeepEqual(m.Buttons, []string{"No", "Yes"}) || m.Default != 1 {
		t.Fatalf("message options %+v", m)
	}
}

func TestDialogPicksAreGrantedToFS(t *testing.T) {
	r := newRig(t)
	home := t.TempDir()
	for _, k := range []string{"XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "r"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "l"))
	if err := sdkos.Configure(sdkos.App{ID: "com.acme.picked", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "picked.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(f)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sdkfs.Revoke(f); sdkfs.Revoke(filepath.Join(dir, "saved.txt")) })
	if _, err := sdkfs.Default.Check(f); err == nil {
		t.Fatal("a file outside the app directories was readable before the user picked it")
	}
	r.app.FileResult = []string{f}
	if _, err := dialog.OpenFile(krate.Background(), dialog.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, err := sdkfs.Default.Check(f); err != nil || got != real {
		t.Fatalf("picked file: %q, %v", got, err)
	}
	if _, err := sdkfs.Default.Check(filepath.Join(dir, "other.txt")); err == nil {
		t.Fatal("a grant for one file must not cover its siblings")
	}
	r.app.FileResult = []string{filepath.Join(dir, "saved.txt")}
	if _, err := dialog.SaveFile(krate.Background(), dialog.SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := sdkfs.Default.Check(filepath.Join(dir, "saved.txt")); err != nil {
		t.Fatalf("save target: %v", err)
	}
}

func TestClipboardRoundTrip(t *testing.T) {
	newRig(t)
	if err := clipboard.WriteText("héllo   \"q\""); err != nil {
		t.Fatal(err)
	}
	if s, _ := clipboard.ReadText(); s != "héllo   \"q\"" {
		t.Fatalf("text %q", s)
	}
	png := []byte{0x89, 'P', 'N', 'G', 0, 1, 2, 255}
	if err := clipboard.WriteImage(png); err != nil {
		t.Fatal(err)
	}
	if b, _ := clipboard.ReadImage(); !bytes.Equal(b, png) {
		t.Fatalf("image %v", b)
	}
	files := []string{"/x y/a.txt", "/b"}
	if err := clipboard.WriteFiles(files); err != nil {
		t.Fatal(err)
	}
	if f, _ := clipboard.ReadFiles(); !reflect.DeepEqual(f, files) {
		t.Fatalf("files %v", f)
	}
	if err := clipboard.Clear(); err != nil {
		t.Fatal(err)
	}
	if s, _ := clipboard.ReadText(); s != "" {
		t.Fatal("not cleared")
	}
	if b, err := clipboard.ReadImage(); err != nil || b != nil {
		t.Fatalf("empty image = %v, %v", b, err)
	}
}

func TestClipboardRichFormats(t *testing.T) {
	r := newRig(t)
	if err := clipboard.Write(clipboard.Data{Text: "plain", HTML: "<b>bold</b>", RTF: "{\\rtf1 x}"}); err != nil {
		t.Fatal(err)
	}
	if h, _ := clipboard.ReadHTML(); h != "<b>bold</b>" {
		t.Fatalf("html %q", h)
	}
	if v, _ := clipboard.ReadRTF(); v != "{\\rtf1 x}" {
		t.Fatalf("rtf %q", v)
	}
	if s, _ := clipboard.ReadText(); s != "plain" {
		t.Fatalf("text %q", s)
	}
	fm, _ := clipboard.Formats()
	if !reflect.DeepEqual(fm, []string{"text/plain", "text/html", "text/rtf"}) {
		t.Fatalf("formats %v", fm)
	}
	if err := clipboard.WriteFormat("application/x-acme", []byte{1, 0, 2}); err != nil {
		t.Fatal(err)
	}
	if b, _ := clipboard.ReadFormat("application/x-acme"); !bytes.Equal(b, []byte{1, 0, 2}) {
		t.Fatalf("custom %v", b)
	}
	if b, err := clipboard.ReadFormat("text/html"); err != nil || b != nil {
		t.Fatalf("absent = %v, %v", b, err)
	}
	// The page needs clipboard to write and clipboard:read for every read.
	h := r.ui("clipboard")
	call := func(id string, enc func(e *wire.Encoder)) string { return uiCall(t, h, id, enc) }
	if got := call("krate/clipboard.write", func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("html")
		e.String("<i>x</i>")
		e.EndObject()
	}); !strings.Contains(got, `"ok":null`) {
		t.Fatalf("write: %s", got)
	}
	for _, id := range []string{"krate/clipboard.readHtml", "krate/clipboard.readRtf", "krate/clipboard.formats"} {
		if got := call(id, nil); !strings.Contains(got, `"permission":"clipboard:read"`) {
			t.Fatalf("%s without clipboard:read: %s", id, got)
		}
	}
	if got := call("krate/clipboard.readFormat", func(e *wire.Encoder) { e.String("text/html") }); !strings.Contains(got, "permission_denied") {
		t.Fatalf("readFormat without clipboard:read: %s", got)
	}
	h = r.ui("clipboard", "clipboard:read")
	if got := uiCall(t, h, "krate/clipboard.readHtml", nil); !strings.Contains(got, `"ok":"\u003ci\u003ex\u003c/i\u003e"`) && !strings.Contains(got, `"ok":"<i>x</i>"`) {
		t.Fatalf("readHtml: %s", got)
	}
	if got := uiCall(t, h, "krate/clipboard.write", func(e *wire.Encoder) { e.Int(3) }); !strings.Contains(got, "bad_args") && !strings.Contains(got, "permission_denied") {
		t.Fatalf("bad write args: %s", got)
	}
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

// The Host's operations and the id table in sdk/ids.go list the same UI ids
// with the same permissions, and every permission is defined in
// sdk/permission.
func TestUIIDsMatchTable(t *testing.T) {
	r := newRig(t)
	ids := r.host.IDs()
	if len(ids) < 40 {
		t.Fatalf("only %d ids registered", len(ids))
	}
	known := map[string]bool{}
	for _, p := range permission.All() {
		known[p.Name] = true
	}
	goOnly := map[string]bool{"krate/app.launchArgs": true, "krate/app.singleInstance": true}
	var bad []string
	for id, perm := range ids {
		if !strings.HasPrefix(id, "krate/") {
			bad = append(bad, id)
		}
		if perm != "" && !known[perm] {
			bad = append(bad, id+" needs unknown permission "+perm)
		}
		row, inTable := sdk.Lookup(id)
		switch {
		case goOnly[id]:
			if inTable {
				bad = append(bad, id+" is Go-only but listed in sdk/ids.go")
			}
		case !inTable:
			bad = append(bad, id+" is not in sdk/ids.go")
		case row.Permission != perm:
			bad = append(bad, id+": table says "+row.Permission+", registered with "+perm)
		}
	}
	uiAreas := map[string]bool{"window": true, "dialog": true, "clipboard": true, "menu": true, "tray": true, "shortcut": true, "notify": true, "app": true}
	for _, row := range sdk.IDs {
		if !uiAreas[row.TS] && row.TS != "lifecycle" {
			continue
		}
		if _, ok := ids[row.Name]; !ok {
			bad = append(bad, row.Name+" is in sdk/ids.go but not registered by the Host")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("%v", bad)
	}
}

func TestMenuEvents(t *testing.T) {
	r := newRig(t)
	m := menu.New(menu.Sub("&File", menu.Action("new", "New", "CmdOrCtrl+N"), menu.Separator(), menu.Check("auto", "Autosave", true)))
	if err := menu.SetApp(m); err != nil {
		t.Fatal(err)
	}
	if len(r.app.AppMenu) != 1 || r.app.AppMenu[0].Label != "&File" || len(r.app.AppMenu[0].Submenu) != 3 ||
		r.app.AppMenu[0].Submenu[0].Accelerator != "CmdOrCtrl+N" || r.app.AppMenu[0].Submenu[2].Kind != caps.MenuItemCheck || !r.app.AppMenu[0].Submenu[2].Checked {
		t.Fatalf("platform menu %+v", r.app.AppMenu)
	}
	got := make(chan menu.Event, 4)
	cancel, err := menu.OnItem("new", func(e menu.Event) { got <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitSubs(t, r, "menu")
	ref := r.main.Ref()
	r.app.EmitMenu(caps.MenuEvent{ID: "other", Window: ref})
	r.app.EmitMenu(caps.MenuEvent{ID: "new", Window: ref})
	select {
	case e := <-got:
		if e.ID != "new" || e.Window != "main" {
			t.Fatalf("event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
	if err := menu.Popup(menu.New(menu.Action("c", "Copy", "")), "", -1, -1); err != nil || len(r.app.Popups) != 1 {
		t.Fatalf("popup %v %v", err, r.app.Popups)
	}
	if err := menu.SetApp(nil); err != nil || len(r.app.AppMenu) != 0 {
		t.Fatalf("clear %v %v", err, r.app.AppMenu)
	}
}

func TestContextMenuSDK(t *testing.T) {
	r := newRig(t)
	if err := menu.SetContextMenu("", "", menu.ContextOptions{Mode: menu.ContextCustom}); err != nil {
		t.Fatal(err)
	}
	if err := menu.SetContextMenu("*", "", menu.ContextOptions{Mode: menu.ContextExtend, Items: []menu.Item{menu.Action("x", "Extra", "")}}); err != nil {
		t.Fatal(err)
	}
	if err := menu.SetContextMenu("main", "main", menu.ContextOptions{Mode: menu.ContextNone}); err != nil {
		t.Fatal(err)
	}
	ps := r.app.ContextPolicies
	if len(ps) != 3 || ps[0].Window != r.main.Ref() || ps[0].View != 0 || ps[0].Policy.Mode != caps.ContextMenuCustom ||
		ps[1].Window != 0 || ps[1].View != 0 || ps[1].Policy.Mode != caps.ContextMenuExtend || len(ps[1].Policy.Items) != 1 || ps[1].Policy.Items[0].ID != "x" ||
		ps[2].View == 0 || ps[2].Policy.Mode != caps.ContextMenuNone {
		t.Fatalf("policies %+v", ps)
	}
	if err := menu.SetContextMenu("nope", "", menu.ContextOptions{Mode: menu.ContextNone}); err == nil {
		t.Fatal("an unknown window is an error")
	}
	if err := menu.SetContextMenu("", "", menu.ContextOptions{Mode: menu.ContextNone, Items: []menu.Item{menu.Action("x", "X", "")}}); err == nil {
		t.Fatal("items outside extend mode are an error")
	}

	got := make(chan menu.ContextInfo, 2)
	cancel, err := menu.OnContextMenu(func(i menu.ContextInfo) []menu.Item {
		got <- i
		return append(menu.DefaultContextItems(i), menu.Action("mine", "Mine", ""))
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitSubs(t, r, "menu.context")
	r.app.EmitContext(caps.ContextMenuInfo{
		Window: r.main.Ref(), View: 1, X: 5, Y: 6, WindowX: 15, WindowY: 26,
		Kinds: []string{"page", "link", "editable", "selection"}, LinkURL: "https://e.x/", SelectionText: "sel",
		Misspelled: "teh", Suggestions: []string{"the", "tech"}, Inspectable: true,
	})
	var info menu.ContextInfo
	select {
	case info = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("no context event")
	}
	if info.Window != "main" || info.X != 5 || info.WindowY != 26 || !info.Has("link") || info.LinkURL != "https://e.x/" ||
		info.SelectionText != "sel" || info.Misspelled != "teh" || !reflect.DeepEqual(info.Suggestions, []string{"the", "tech"}) || !info.Inspectable {
		t.Fatalf("event %+v", info)
	}
	// The answer is popped up at the click.
	deadline := time.Now().Add(3 * time.Second)
	for len(r.app.Popups) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(r.app.Popups) != 1 {
		t.Fatalf("popups %v", r.app.Popups)
	}
	var ids []string
	for _, it := range r.app.Popups[0] {
		ids = append(ids, it.ID+"/"+it.Label)
	}
	want := []string{"krate:replaceMisspelling:the/the", "krate:replaceMisspelling:tech/tech", "krate:addToDictionary/Add to Dictionary", "/",
		"krate:copyLink:https://e.x//Copy Link Address", "/", "krate:undo/Undo", "krate:redo/Redo", "/", "krate:cut/Cut", "krate:copy/Copy",
		"krate:paste/Paste", "krate:selectAll/Select All", "/", "krate:inspectElement/Inspect Element", "mine/Mine"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("default items:\n got %q\nwant %q", ids, want)
	}

	// Built-in ids run on the view instead of reaching the page.
	menuGot := make(chan menu.Event, 4)
	cancel2, _ := menu.OnClick(func(e menu.Event) { menuGot <- e })
	defer cancel2()
	waitSubs(t, r, "menu")
	ref := r.main.Ref()
	r.app.EmitMenu(caps.MenuEvent{ID: "krate:copy", Window: ref})
	r.app.EmitMenu(caps.MenuEvent{ID: menu.ActionReplace("the"), Window: ref})
	r.app.EmitMenu(caps.MenuEvent{ID: menu.ActionCopyLink("https://e.x/a:b"), Window: ref})
	r.app.EmitMenu(caps.MenuEvent{ID: "mine", Window: ref})
	select {
	case e := <-menuGot:
		if e.ID != "mine" {
			t.Fatalf("a built-in id was reported: %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no click")
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := r.app.ReadText(); s == "https://e.x/a:b" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s, _ := r.app.ReadText(); s != "https://e.x/a:b" {
		t.Fatalf("copyLink put %q on the clipboard", s)
	}
	// The commands run on the main thread as they arrive; their order is not
	// part of the contract.
	cmds := append([]string(nil), r.app.ContextCommands...)
	sort.Strings(cmds)
	if !reflect.DeepEqual(cmds, []string{"copy:", "replaceMisspelling:the"}) {
		t.Fatalf("commands %v", r.app.ContextCommands)
	}

	// The page needs only the menu permission.
	h := r.ui("menu")
	if got := uiCall(t, h, "krate/menu.setContextMenu", func(e *wire.Encoder) {
		e.String("")
		e.String("")
		e.BeginObject()
		e.Key("mode")
		e.String("none")
		e.EndObject()
	}); !strings.Contains(got, `"ok":null`) {
		t.Fatalf("setContextMenu from the page: %s", got)
	}
	h = r.ui("dialog")
	if got := uiCall(t, h, "krate/menu.setContextMenu", nil); !strings.Contains(got, "permission_denied") {
		t.Fatalf("without menu: %s", got)
	}
}

// waitSubs waits until a topic has a subscriber, so an event is not lost.
func waitSubs(t *testing.T, r *rig, topic string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r.host.Subscribers(topic) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("nobody subscribed to %s", topic)
}

func TestMenuEventsReachThePage(t *testing.T) {
	r := newRig(t)
	srv := httptest.NewServer(r.ui("menu"))
	defer srv.Close()
	// The page opens the event stream with a call, then reads it.
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String("krate/menu.events")
	e.Key("args")
	e.BeginArray()
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	resp, err := http.Post(srv.URL+"/__krate/call", "application/x-krate-frames", bytes.NewReader(e.Frame()))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := wire.ReadFrame(resp.Body)
	raw, _ := d.RawValue()
	resp.Body.Close()
	var res struct{ Stream string }
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), `"stream"`, `"Stream"`)), &res); err != nil {
		t.Fatal(err)
	}
	if res.Stream == "" {
		t.Fatalf("no stream: %s", raw)
	}
	sresp, err := http.Get(srv.URL + "/__krate/stream/" + res.Stream)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	waitSubs(t, r, "menu")
	r.app.EmitMenu(caps.MenuEvent{ID: "save", Window: r.main.Ref(), Checked: true})
	fd, err := wire.ReadFrame(sresp.Body)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = fd.RawValue()
	if string(raw) != `{"v":{"id":"save","window":"main","checked":true}}` {
		t.Fatalf("stream frame %s", raw)
	}
}

// A denied event stream opens (the page gets a stream id) and then fails with
// the same structured permission_denied error as a denied function.
func TestDeniedStreamReportsPermission(t *testing.T) {
	r := newRig(t)
	srv := httptest.NewServer(r.ui("window"))
	defer srv.Close()
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String("krate/menu.events")
	e.Key("args")
	e.BeginArray()
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	resp, err := http.Post(srv.URL+"/__krate/call", "application/x-krate-frames", bytes.NewReader(e.Frame()))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := wire.ReadFrame(resp.Body)
	raw, _ := d.RawValue()
	resp.Body.Close()
	if strings.Contains(string(raw), "not_found") {
		t.Fatalf("denied stream is not_found: %s", raw)
	}
	var res struct{ Stream string }
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), `"stream"`, `"Stream"`)), &res); err != nil {
		t.Fatal(err)
	}
	if res.Stream == "" {
		// Denial may also be reported on the call itself.
		if !strings.Contains(string(raw), "permission_denied") {
			t.Fatalf("unexpected reply %s", raw)
		}
		return
	}
	sresp, err := http.Get(srv.URL + "/__krate/stream/" + res.Stream)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	fd, err := wire.ReadFrame(sresp.Body)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = fd.RawValue()
	if !strings.Contains(string(raw), "permission_denied") || !strings.Contains(string(raw), `"permission":"menu"`) {
		t.Fatalf("stream frame %s", raw)
	}
}

func TestWindowOps(t *testing.T) {
	r := newRig(t)
	w, err := window.Open(window.Options{Title: "Settings", Path: "/settings", Width: 500, Height: 400, Frameless: true, AlwaysOnTop: true, HasPosition: true, X: 10, Y: 20, PreventClose: true})
	if err != nil {
		t.Fatal(err)
	}
	if w.ID == "" || w.ID == "main" {
		t.Fatalf("id %q", w.ID)
	}
	nw := r.app.Windows[1]
	if nw.Opts.Path != "/settings" || nw.Opts.Width != 500 || nw.Opts.Title != "Settings" {
		t.Fatalf("opts %+v", nw.Opts)
	}
	st, err := w.State()
	if err != nil || st.Decorated || !st.AlwaysOnTop || st.X != 10 || st.Y != 20 || st.Width != 500 {
		t.Fatalf("state %+v %v", st, err)
	}
	if err := w.SetFullscreen(true); err != nil {
		t.Fatal(err)
	}
	if st, _ = w.State(); !st.Fullscreen {
		t.Fatal("fullscreen not applied")
	}
	if err := w.Restore(); err != nil {
		t.Fatal(err)
	}
	if st, _ = w.State(); st.Fullscreen {
		t.Fatal("restore ignored")
	}
	if err := w.SetTitle("T2"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetSize(300, 200); err != nil {
		t.Fatal(err)
	}
	if err := w.Navigate("/x"); err != nil {
		t.Fatal(err)
	}
	if st, _ = w.State(); st.Title != "T2" || st.Width != 300 {
		t.Fatalf("state %+v", st)
	}
	ids, _ := window.List()
	sort.Strings(ids)
	if len(ids) != 2 {
		t.Fatalf("list %v", ids)
	}
	if err := window.Get("nope").Maximize(); err == nil || !strings.Contains(err.Error(), "no window") {
		t.Fatalf("unknown window: %v", err)
	}

	events := make(chan window.Event, 4)
	cancel, err := window.OnEvent(func(e window.Event) { events <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitSubs(t, r, "window")
	r.app.EmitWindow(nw, caps.WindowCloseRequested)
	if e := <-events; e.Window != w.ID || e.Kind != window.CloseRequested {
		t.Fatalf("event %+v", e)
	}
	r.app.EmitWindow(nw, caps.WindowClosed)
	if e := <-events; e.Kind != window.Closed {
		t.Fatalf("event %+v", e)
	}
	if ids, _ = window.List(); len(ids) != 1 {
		t.Fatalf("closed window still listed: %v", ids)
	}
	main0 := window.Main()
	// Hide, with and without releasing the webview.
	if err := main0.Hide(); err != nil {
		t.Fatal(err)
	}
	if err := window.Hide("main", window.HideOptions{Release: true}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.main.Called(), ","); !strings.HasSuffix(got, "hide,release") || !r.main.Released() {
		t.Fatalf("hide calls %s released=%v", got, r.main.Released())
	}
	if err := window.Show("main"); err != nil {
		t.Fatal(err)
	}
	// Saved state round trip.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("KRATE_APP_ID", "test.app")
	main := window.Main()
	if err := main.SetSize(700, 500); err != nil {
		t.Fatal(err)
	}
	if err := main.SetPosition(30, 40); err != nil {
		t.Fatal(err)
	}
	if err := window.SaveState(main, "main"); err != nil {
		t.Fatal(err)
	}
	if err := main.SetSize(100, 100); err != nil {
		t.Fatal(err)
	}
	if err := main.SetPosition(0, 0); err != nil {
		t.Fatal(err)
	}
	if ok, err := window.RestoreState(main, "main"); err != nil || !ok {
		t.Fatalf("restore %v %v", ok, err)
	}
	if st, _ = main.State(); st.Width != 700 || st.Height != 500 || st.X != 30 || st.Y != 40 {
		t.Fatalf("restored %+v", st)
	}
	if ok, _ := window.RestoreState(main, "never-saved"); ok {
		t.Fatal("restored nothing")
	}
	if err := window.SaveState(main, "../evil"); err == nil {
		t.Fatal("path in state name accepted")
	}
}

func TestShortcutAndSupports(t *testing.T) {
	r := newRig(t)
	if _, err := shortcut.Register("K", func() {}); err == nil {
		t.Fatal("bare letter accepted")
	}
	if _, err := shortcut.Register("Ctrl+Hyper+K", func() {}); err == nil {
		t.Fatal("unknown modifier accepted")
	}
	var mu sync.Mutex
	n := 0
	unreg, err := shortcut.Register("Ctrl+Shift+K", func() { mu.Lock(); n++; mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	if len(r.app.Shortcuts) != 1 {
		t.Fatal("not registered on the platform")
	}
	var handle string
	for h, a := range r.app.Shortcuts {
		handle = h
		if !a.Ctrl || !a.Shift || a.Key != "K" {
			t.Fatalf("accelerator %+v", a)
		}
	}
	waitSubs(t, r, "shortcut")
	r.app.EmitShortcut(handle)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		c := n
		mu.Unlock()
		if c == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	if n != 1 {
		t.Fatalf("callback ran %d times", n)
	}
	mu.Unlock()
	if err := unreg(); err != nil || len(r.app.Shortcuts) != 0 {
		t.Fatalf("unregister %v %v", err, r.app.Shortcuts)
	}
	if !app.Supports("dialog") || !app.Supports("clipboard") || !app.Supports("window:alwaysOnTop") && app.Supports("nonsense") {
		t.Fatal("supports")
	}
	if app.Supports("nonsense") {
		t.Fatal("unknown capability supported")
	}
	if !krate.Supports("menu") {
		t.Fatal("krate.Supports does not see the SDK")
	}
}

func TestQuit(t *testing.T) {
	r := newRig(t)
	if err := app.Quit(); err != nil || r.app.QuitCalls() != 1 {
		t.Fatalf("quit %v %d", err, r.app.QuitCalls())
	}
}

func TestNoPlatformIsUnsupported(t *testing.T) {
	t.Setenv("KRATE_GOHOST_PARENT", "")
	_, err := clipboard.ReadText()
	if err == nil || !krate.IsUnsupported(err) || !strings.Contains(err.Error(), "KRATE_GOHOST_PARENT") {
		t.Fatalf("err = %v", err)
	}
	if !errorsIs(err, krate.ErrUnsupported) {
		t.Fatal("errors.Is(ErrUnsupported) is false")
	}
}

func TestBrowserModeIsUnsupported(t *testing.T) {
	h := sdk.NewHost()
	h.Attach(nil)
	defer h.Close()
	if _, err := clipboard.ReadText(); !krate.IsUnsupported(err) {
		t.Fatalf("err = %v", err)
	}
	if _, err := window.Open(window.Options{}); !krate.IsUnsupported(err) {
		t.Fatalf("err = %v", err)
	}
	if app.Supports("dialog") {
		t.Fatal("dialogs supported without a platform")
	}
}

var _ = context.Background

func errorsIs(err, target error) bool {
	type is interface{ Is(error) bool }
	for e := err; e != nil; {
		if e == target {
			return true
		}
		if i, ok := e.(is); ok && i.Is(target) {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// The JSON below is what @krate/app/{dialog,window,menu,tray,notify} send
// (see packages/app/test/sdk-ui.test.ts); the Go codecs must accept it.
func TestArgumentsAsTheTSWrappersSendThem(t *testing.T) {
	isolateSessionBus(t)
	r := newRig(t)
	h := r.ui("dialog", "window", "menu", "notify", "tray", "shortcut", "app", "clipboard")
	raw := func(js string) func(*wire.Encoder) { return func(e *wire.Encoder) { e.Raw([]byte(js)) } }
	cases := []struct{ fn, args string }{
		{"krate/dialog.openFile", `{"title":"Pick","multiple":true,"filters":[{"name":"Images","patterns":["*.png"]}]}`},
		{"krate/dialog.saveFile", `{"name":"x.txt","overwrite":true}`},
		{"krate/dialog.message", `{"message":"boom","buttons":["A","B"],"kind":2}`},
		{"krate/window.open", `{"title":"S","path":"/settings","frameless":true,"x":5,"y":6,"hasPosition":true}`},
		{"krate/menu.setApp", `[{"id":"","label":"&File","kind":4,"accelerator":"","disabled":false,"checked":false,"submenu":[{"id":"new","label":"New","kind":0,"accelerator":"CmdOrCtrl+N","disabled":false,"checked":false,"submenu":[]}]}]`},
		{"krate/notify.send", `{"id":"n","title":"Done","body":"","icon":"","actions":[{"id":"open","label":"Open"}],"urgency":2,"timeoutMs":3000,"silent":false}`},
	}
	for _, c := range cases {
		got := uiCall(t, h, c.fn, raw(c.args))
		if strings.Contains(got, `"err"`) && !strings.Contains(got, "unsupported") {
			t.Errorf("%s rejected the TS shape: %s", c.fn, got)
		}
	}
	// Positional forms.
	for _, c := range []struct {
		fn   string
		args func(*wire.Encoder)
	}{
		{"krate/window.setSize", func(e *wire.Encoder) { e.String("main"); e.Int(500); e.Int(400) }},
		{"krate/window.setAlwaysOnTop", func(e *wire.Encoder) { e.String("main"); e.Bool(true) }},
		{"krate/menu.popup", func(e *wire.Encoder) {
			e.String("")
			e.Int(-1)
			e.Int(-1)
			e.Raw([]byte(`[{"id":"c","label":"Copy","kind":0,"accelerator":"","disabled":false,"checked":false,"submenu":[]}]`))
		}},
		{"krate/shortcut.register", func(e *wire.Encoder) { e.String("ui-1-abc"); e.String("Ctrl+Shift+K") }},
		{"krate/shortcut.unregister", func(e *wire.Encoder) { e.String("ui-1-abc") }},
		{"krate/app.supports", func(e *wire.Encoder) { e.String("tray") }},
	} {
		if got := uiCall(t, h, c.fn, c.args); strings.Contains(got, "bad_args") {
			t.Errorf("%s: %s", c.fn, got)
		}
	}
	// And the tray shape, with binary icon bytes.
	got := uiCall(t, h, "krate/tray.set", func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("tooltip")
		e.String("T")
		e.Key("title")
		e.String("")
		e.Key("icon")
		e.Bytes([]byte{1, 2, 3})
		e.Key("menu")
		e.Raw([]byte(`[{"id":"quit","label":"Quit","kind":0,"accelerator":"","disabled":false,"checked":false,"submenu":[]}]`))
		e.EndObject()
	})
	if strings.Contains(got, "bad_args") {
		t.Errorf("tray.Set: %s", got)
	}
	if len(r.app.AppMenu) != 1 || r.app.AppMenu[0].Submenu[0].Accelerator != "CmdOrCtrl+N" {
		t.Errorf("menu from TS shape: %+v", r.app.AppMenu)
	}
	if r.main.State().Width != 500 || !r.main.State().AlwaysOnTop {
		t.Errorf("window ops from TS shape: %+v", r.main.State())
	}
}

// Every SDK id has a lowerCamel operation name after the area.
func TestIDsAreLowerCamel(t *testing.T) {
	for _, r := range sdk.IDs {
		i := strings.LastIndex(r.Name, ".")
		if !strings.HasPrefix(r.Name, "krate/") || i < 0 || i+1 >= len(r.Name) {
			t.Errorf("malformed id %q", r.Name)
			continue
		}
		if c := r.Name[i+1]; c < 'a' || c > 'z' {
			t.Errorf("id %q: operation name must start with a lower-case letter", r.Name)
		}
	}
}
