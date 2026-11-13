package sdk_test

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/sdk/window"
	"github.com/kratejs/krate/packages/native/wire"
)

func TestViewOps(t *testing.T) {
	r := newRig(t)
	w := window.Main()

	side, err := w.AddView(window.ViewOptions{ID: "side", Path: "/side", Side: window.Left, Size: 0.25, MinWidth: 150})
	if err != nil {
		t.Fatal(err)
	}
	logs, err := w.AddView(window.ViewOptions{Path: "/logs", Beside: "main", Side: window.Bottom, Size: 0.3, MinHeight: 80})
	if err != nil {
		t.Fatal(err)
	}
	if side.ID != "side" || logs.ID != "v1" {
		t.Fatalf("ids %q %q", side.ID, logs.ID)
	}
	vs, err := w.Views()
	if err != nil || len(vs) != 3 || vs[0].ID != "main" || !vs[0].Primary || vs[1].ID != "side" || vs[1].Path != "/side" || vs[2].ID != "v1" {
		t.Fatalf("views %+v %v", vs, err)
	}
	all, _ := window.Views()
	if len(all) != 3 {
		t.Fatalf("all views %+v", all)
	}

	// The layout reads back with ids: side | (main over logs).
	l, err := w.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if l.Direction != "horizontal" || l.Ratio != 0.25 || l.First.View != "side" || l.First.MinWidth != 150 ||
		l.Second.Direction != "vertical" || l.Second.First.View != "main" || l.Second.Second.View != "v1" || l.Second.Second.MinHeight != 80 {
		t.Fatalf("layout %+v / %+v / %+v", l, l.First, l.Second)
	}
	// Replace it: three stacked panes.
	want := window.Split("vertical", 0.2, window.Pane("v1"), window.Split("vertical", 0.5, window.Pane("main"), window.Pane("side")))
	if err := w.SetLayout(want); err != nil {
		t.Fatal(err)
	}
	if l, _ = w.Layout(); l.Direction != "vertical" || l.First.View != "v1" || l.Ratio != 0.2 {
		t.Fatalf("layout after SetLayout: %+v", l)
	}
	for name, bad := range map[string]*window.Layout{
		"missing":  window.Split("vertical", 0, window.Pane("main"), window.Pane("side")),
		"unknown":  window.Split("vertical", 0, window.Pane("main"), window.Split("vertical", 0, window.Pane("side"), window.Pane("ghost"))),
		"axis":     window.Split("sideways", 0, window.Pane("main"), window.Split("vertical", 0, window.Pane("side"), window.Pane("v1"))),
		"half":     {Direction: "vertical", First: window.Pane("main")},
		"too deep": deep(40),
	} {
		if err := w.SetLayout(bad); err == nil {
			t.Errorf("%s layout accepted", name)
		}
	}

	// Bad options.
	for name, o := range map[string]window.ViewOptions{
		"side":      {Side: "diagonal"},
		"size":      {Size: 1.5},
		"id":        {ID: "has space"},
		"id first":  {ID: "1abc"},
		"duplicate": {ID: "side"},
		"window id": {ID: "main"},
		"beside":    {Beside: "ghost"},
		"min":       {MinWidth: -1},
	} {
		if _, err := w.AddView(o); err == nil {
			t.Errorf("%s: AddView accepted %+v", name, o)
		}
	}
	if _, err := window.Get("ghost").AddView(window.ViewOptions{}); err == nil || !strings.Contains(err.Error(), "no window") {
		t.Errorf("AddView on an unknown window: %v", err)
	}
	if err := (&window.View{ID: "ghost"}).Focus(); err == nil || !strings.Contains(err.Error(), "no view") {
		t.Errorf("focus of an unknown view: %v", err)
	}

	// Focus and navigate reach the platform.
	if err := side.Focus(); err != nil {
		t.Fatal(err)
	}
	if err := side.Navigate("/elsewhere"); err != nil {
		t.Fatal(err)
	}
	if vs, _ = w.Views(); vs[1].Path != "/elsewhere" {
		t.Fatalf("navigate: %+v", vs)
	}
	called := strings.Join(r.main.Called(), ",")
	if !strings.Contains(called, "focusview") {
		t.Errorf("calls: %s", called)
	}

	// Posting runs a script in the target view. The message cannot break out
	// of the string it is carried in.
	nasty := map[string]any{"s": "</script><script>alert(1)</script> \"'`${x}", "n": 1.5}
	if err := logs.Post(nasty); err != nil {
		t.Fatal(err)
	}
	var logsRef = r.main.Views()[2].Ref
	ev := r.app.Evals(logsRef)
	if len(ev) != 1 {
		t.Fatalf("evals %v", ev)
	}
	js := ev[0]
	if !strings.Contains(js, "krate:view-message") || !strings.Contains(js, "JSON.parse(") {
		t.Errorf("script %q", js)
	}
	if strings.Contains(js, "</script>") || strings.Contains(js, "alert(1)</") {
		t.Errorf("the message is not contained in a string literal: %q", js)
	}
	if !strings.Contains(js, `\"from\":\"\"`) {
		t.Errorf("host-sent message should have an empty from: %q", js)
	}
	if err := (&window.View{ID: "ghost"}).Post(1); err == nil {
		t.Error("post to an unknown view")
	}
	if err := logs.Post(strings.Repeat("x", window.MaxMessage)); err == nil {
		t.Error("an oversized message was accepted")
	}
	// A page sends with its own native identity as the sender.
	h := r.ui("window")
	got := uiCall(t, h, "krate/window.postMessage", func(e *wire.Encoder) { e.String("v1"); e.BeginObject(); e.Key("k"); e.Int(1); e.EndObject() })
	if got != `{"id":1,"ok":null}` && !strings.Contains(got, "null") {
		t.Fatalf("page post: %s", got)
	}
	ev = r.app.Evals(logsRef)
	if len(ev) != 2 || !strings.Contains(ev[1], `\"from\":\"ui\"`) {
		t.Errorf("a message from the page should carry the page's window id as from: %v", ev)
	}

	// Removal.
	if err := side.Remove(); err != nil {
		t.Fatal(err)
	}
	if err := side.Remove(); err == nil {
		t.Error("removing twice succeeded")
	}
	if err := (&window.View{ID: "main"}).Remove(); err == nil || !strings.Contains(err.Error(), "primary") {
		t.Errorf("removing the primary view: %v", err)
	}
	if vs, _ = w.Views(); len(vs) != 2 {
		t.Fatalf("views after removal: %+v", vs)
	}
}

func deep(n int) *window.Layout {
	l := window.Pane("main")
	for i := 0; i < n; i++ {
		l = window.Split("vertical", 0, l, window.Pane("x"))
	}
	return l
}
