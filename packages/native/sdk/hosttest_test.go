package sdk_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/sdk/menu"
	"github.com/kratejs/krate/packages/native/sdk/shortcut"
)

func TestHostInjectMenuAndViews(t *testing.T) {
	r := newRig(t)
	views := r.host.Views()
	if len(views) != 1 || views[0].ID != "main" || views[0].Window != "main" || !views[0].Primary {
		t.Fatalf("views = %+v", views)
	}
	got := make(chan menu.Event, 2)
	cancel, err := menu.OnItem("file.open", func(e menu.Event) { got <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitSubs(t, r, "menu")
	if err := r.host.Inject("menu", []byte(`{"id":"file.open","window":"main","checked":true}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-got:
		if e.ID != "file.open" || e.Window != "main" || !e.Checked {
			t.Fatalf("event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
}

func TestHostInjectRejects(t *testing.T) {
	r := newRig(t)
	for area, payload := range map[string]string{
		"tray":     `{"kind":"hover"}`,
		"menu":     `{}`,
		"shortcut": `{}`,
		"window":   `{}`,
		"menu ":    `nope`,
	} {
		if err := r.host.Inject(area, []byte(payload)); err == nil {
			t.Errorf("Inject(%q, %s) = nil, want an error", area, payload)
		}
	}
	if err := r.host.EvalView("nope", "1"); err == nil {
		t.Error("EvalView of an unknown view succeeded")
	}
}

func TestHostInjectShortcut(t *testing.T) {
	r := newRig(t)
	fired := make(chan struct{}, 4)
	unreg, err := shortcut.Register("Ctrl+Shift+K", func() { fired <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	waitSubs(t, r, "shortcut")
	if err := r.host.Inject("shortcut", []byte(`{"accelerator":"Shift+Ctrl+K"}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
	case <-time.After(3 * time.Second):
		t.Fatal("the registered handler did not run")
	}
	err = r.host.Inject("shortcut", []byte(`{"accelerator":"Ctrl+Shift+J"}`))
	if err == nil || !strings.Contains(err.Error(), "no shortcut is registered") {
		t.Errorf("unregistered accelerator: %v", err)
	}
	if err := unreg(); err != nil {
		t.Fatal(err)
	}
	if err := r.host.Inject("shortcut", []byte(`{"accelerator":"Ctrl+Shift+K"}`)); err == nil {
		t.Error("an unregistered shortcut was delivered")
	}
}
