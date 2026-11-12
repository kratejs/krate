package sdk_test

import (
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/sdk/menu"
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
