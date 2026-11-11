//go:build linux

package sdk_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/tray"
)

// fakeXEmbed stands in for the platform's GtkStatusIcon fallback.
type fakeXEmbed struct {
	platform.App
	mu        sync.Mutex
	available bool
	shown     *caps.TrayIcon
	sets      int
	fn        func(caps.TrayEvent)
}

func (f *fakeXEmbed) XEmbedTrayAvailable() bool { return f.available }
func (f *fakeXEmbed) OnXEmbedTray(fn func(caps.TrayEvent)) {
	f.mu.Lock()
	f.fn = fn
	f.mu.Unlock()
}
func (f *fakeXEmbed) SetXEmbedTray(i *caps.TrayIcon) error {
	f.mu.Lock()
	f.shown = i
	f.sets++
	f.mu.Unlock()
	return nil
}

func xembedRig(t *testing.T, available bool) (*fakeXEmbed, *sdk.Host) {
	fa := fakeplat.New()
	f := &fakeXEmbed{App: fa, available: available}
	h := sdk.NewHost()
	h.Attach(f)
	t.Cleanup(h.Close)
	w, _ := fa.NewWindow(platform.WindowOptions{})
	h.AddWindow("main", w)
	return f, h
}

func TestTrayFallsBackToXEmbedWithoutAStatusNotifierHost(t *testing.T) {
	session(t) // a bus without a watcher
	f, h := xembedRig(t, true)
	if !h.SupportsForTest("tray") {
		t.Fatal("tray should be supported with an XEmbed tray manager")
	}
	tr, err := tray.New(tray.Options{Tooltip: "tip", Title: "T", Icon: testPNG(t)})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.shown == nil || f.shown.Tooltip != "tip" || len(f.shown.PNG) == 0 || f.fn == nil {
		f.mu.Unlock()
		t.Fatalf("the XEmbed tray was not used: %+v", f.shown)
	}
	f.mu.Unlock()
	if err := tr.SetTooltip("changed"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.shown.Tooltip != "changed" || f.sets != 2 {
		f.mu.Unlock()
		t.Fatalf("update did not reach the XEmbed icon: %+v (%d sets)", f.shown, f.sets)
	}
	f.mu.Unlock()
	if err := tr.Remove(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.shown != nil {
		t.Fatal("Remove left the XEmbed icon")
	}
}

func TestTrayPrefersStatusNotifierOverXEmbed(t *testing.T) {
	addr := session(t)
	watcher := dial(t, addr)
	if err := watcher.RequestName("org.kde.StatusNotifierWatcher"); err != nil {
		t.Fatal(err)
	}
	registered := make(chan string, 1)
	watcher.Export("/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher", map[string]dbusiface.Method{
		"RegisterStatusNotifierItem": func(a []any) ([]any, error) { registered <- a[0].(string); return nil, nil },
	})
	f, _ := xembedRig(t, true)
	if _, err := tray.New(tray.Options{Tooltip: "x"}); err != nil {
		t.Fatal(err)
	}
	<-registered
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sets != 0 {
		t.Fatal("the XEmbed tray was used although a StatusNotifier host exists")
	}
}

func TestNoTrayAtAllNamesTheAppIndicatorExtension(t *testing.T) {
	session(t)
	_, h := xembedRig(t, false)
	if h.SupportsForTest("tray") {
		t.Fatal("tray supported with neither kind of tray")
	}
	_, err := tray.New(tray.Options{Tooltip: "x"})
	if !krate.IsUnsupported(err) || !strings.Contains(err.Error(), "AppIndicator") {
		t.Fatalf("err = %v", err)
	}
}
