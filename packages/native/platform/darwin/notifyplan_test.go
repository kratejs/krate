package darwin

import (
	"errors"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestBundleProblem(t *testing.T) {
	if err := bundleProblem("com.acme.app", "/Applications/Acme.app"); err != nil {
		t.Errorf("bundled: %v", err)
	}
	if err := bundleProblem("com.acme.app", "/Applications/Acme.app/"); err != nil {
		t.Errorf("trailing slash: %v", err)
	}
	for _, c := range []struct{ id, path string }{
		{"", "/usr/local/bin"},
		{"com.acme.app", "/usr/local/bin"},
	} {
		err := bundleProblem(c.id, c.path)
		if err == nil {
			t.Fatalf("%+v: no error", c)
		}
		if !errors.Is(err, krate.ErrUnsupported) {
			t.Errorf("%+v: not ErrUnsupported: %v", c, err)
		}
		for _, want := range []string{"app bundle", "krate native dev", c.path} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%+v: %q lacks %q", c, err, want)
			}
		}
	}
}

func TestNotifyDeniedError(t *testing.T) {
	err := errNotifyDenied("com.acme.app")
	var ke *krate.Error
	if !errors.As(err, &ke) || ke.Code != "permission_denied" || !strings.Contains(err.Error(), "System Settings") {
		t.Errorf("denied = %#v", err)
	}
}

func TestCategoryID(t *testing.T) {
	a := []caps.NotificationAction{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}
	if categoryID(nil) != plainCategory {
		t.Error("no actions should use the plain category")
	}
	id := categoryID(a)
	if id == plainCategory || !strings.HasPrefix(id, "krate.actions.") {
		t.Errorf("id = %q", id)
	}
	if categoryID(append([]caps.NotificationAction(nil), a...)) != id {
		t.Error("equal lists must share a category")
	}
	if categoryID(a[:1]) == id {
		t.Error("different lists must differ")
	}
	if categoryID([]caps.NotificationAction{{ID: "ab", Label: ""}}) == categoryID([]caps.NotificationAction{{ID: "a", Label: "b"}}) {
		t.Error("field boundaries must matter")
	}
}

func TestRequestAndEventID(t *testing.T) {
	if requestID("job-1", 7) != "job-1" || eventID("job-1") != "job-1" {
		t.Error("explicit id must round trip")
	}
	g := requestID("", 7)
	if g == "" || eventID(g) != "" {
		t.Errorf("generated %q -> %q", g, eventID(g))
	}
	if requestID("", 7) == requestID("", 8) {
		t.Error("generated ids must differ")
	}
}

func TestNotifyEvent(t *testing.T) {
	cases := []struct {
		action, req string
		want        caps.NotifyEvent
	}{
		{unDefaultActionID, "n1", caps.NotifyEvent{ID: "n1"}},
		{unDismissActionID, "n1", caps.NotifyEvent{ID: "n1", Closed: true}},
		{"reply", "n1", caps.NotifyEvent{ID: "n1", Action: "reply"}},
		{unDefaultActionID, "krate-auto-3", caps.NotifyEvent{}},
	}
	for _, c := range cases {
		if got := notifyEvent(c.action, c.req); got != c.want {
			t.Errorf("notifyEvent(%q,%q) = %+v, want %+v", c.action, c.req, got, c.want)
		}
	}
}

func TestInterruptionLevel(t *testing.T) {
	if l, ok := interruptionLevel(0); !ok || l != unLevelPassive {
		t.Errorf("low = %d %v", l, ok)
	}
	if _, ok := interruptionLevel(1); ok {
		t.Error("normal must leave the default")
	}
	if l, ok := interruptionLevel(2); !ok || l != unLevelTimeSensitive {
		t.Errorf("critical = %d %v", l, ok)
	}
}

func TestForegroundPresentation(t *testing.T) {
	loud, quiet := foregroundPresentation(false), foregroundPresentation(true)
	if loud&unPresentBanner == 0 || loud&unPresentSound == 0 || quiet&unPresentSound != 0 || quiet&unPresentBanner == 0 {
		t.Errorf("loud %b quiet %b", loud, quiet)
	}
}
