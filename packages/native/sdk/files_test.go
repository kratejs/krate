package sdk_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/app"
	sdkfs "github.com/kratejs/krate/packages/native/sdk/fs"
	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
	"github.com/kratejs/krate/packages/native/sdk/window"
)

// filesFixture configures the app directories under a temp home and returns a
// directory outside them with some files.
func filesFixture(t *testing.T, names ...string) (paths []string) {
	t.Helper()
	home := t.TempDir()
	for _, k := range []string{"XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "r"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "l"))
	if err := sdkos.Configure(sdkos.App{ID: "com.acme.files", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("hello "+n), 0o600); err != nil {
			t.Fatal(err)
		}
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, real)
		t.Cleanup(func() { sdkfs.Revoke(p) })
	}
	return paths
}

func readable(t *testing.T, p string) bool {
	t.Helper()
	_, err := sdkfs.Default.Check(p)
	return err == nil
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLaunchFilesAreOpenedAndGranted(t *testing.T) {
	ps := filesFixture(t, "a.md", "b.md")
	if readable(t, ps[0]) {
		t.Fatal("the file was readable before it was opened")
	}
	restore := app.SetLaunchArgsForTest([]string{"--flag", ps[0], "acme://x", filepath.Join(ps[1], "..", "gone.md")})
	defer restore()
	r := newRig(t)
	got, err := app.OpenedFiles()
	if err != nil || !reflect.DeepEqual(got, []string{ps[0]}) {
		t.Fatalf("OpenedFiles = %v, %v", got, err)
	}
	if !readable(t, ps[0]) {
		t.Error("a file opened at launch is not readable through the fs scope")
	}
	if readable(t, ps[1]) {
		t.Error("a sibling of the opened file became readable")
	}
	// The page sees the same list, and only with the app permission.
	if raw := uiCall(t, r.ui("app"), "krate/app.openedFiles", nil); raw != `{"id":1,"ok":`+mustJSON(t, []string{ps[0]})+`}` {
		t.Errorf("page OpenedFiles = %s", raw)
	}
	if raw := uiCall(t, r.ui("window"), "krate/app.openedFiles", nil); !strings.Contains(raw, "permission") {
		t.Errorf("without the app permission: %s", raw)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOnOpenFileReplaysThenStreams(t *testing.T) {
	ps := filesFixture(t, "launch.md", "later.md", "system.md")
	restore := app.SetLaunchArgsForTest([]string{ps[0]})
	defer restore()
	r := newRig(t)
	var mu sync.Mutex
	var got []string
	if err := app.OnOpenFile(func(p string) { mu.Lock(); got = append(got, p); mu.Unlock() }); err != nil {
		t.Fatal(err)
	}
	snapshot := func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(got) }
	if !reflect.DeepEqual(snapshot(), []string{ps[0]}) {
		t.Fatalf("the launch file was not replayed: %v", snapshot())
	}
	waitFor(t, "the event stream", func() bool { return r.host.Subscribers("appopen") > 0 })
	r.app.EmitOpenFiles(ps[1], filepath.Join(filepath.Dir(ps[1]), "missing.md"))
	waitFor(t, "the system open", func() bool { return len(snapshot()) == 2 })
	r.app.EmitOpenFiles(ps[2])
	waitFor(t, "the second system open", func() bool { return len(snapshot()) == 3 })
	time.Sleep(50 * time.Millisecond)
	if !reflect.DeepEqual(snapshot(), ps) {
		t.Fatalf("got %v, want %v (each once, missing files dropped)", snapshot(), ps)
	}
	if !readable(t, ps[2]) {
		t.Error("a file the system opened is not readable through the fs scope")
	}
	all, _ := app.OpenedFiles()
	if !reflect.DeepEqual(all, ps) {
		t.Errorf("OpenedFiles = %v", all)
	}
}

func TestSecondInstanceOpensItsFiles(t *testing.T) {
	ps := filesFixture(t, "one.md", "two.md")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	restore := app.SetLaunchArgsForTest(nil)
	defer restore()
	r := newRig(t)
	got := make(chan string, 4)
	if err := app.OnOpenFile(func(p string) { got <- p }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the event stream", func() bool { return r.host.Subscribers("appopen") > 0 })
	first, err := app.SingleInstance("com.acme.files", nil)
	if err != nil || !first {
		t.Fatalf("SingleInstance = %v, %v", first, err)
	}
	// A later launch, as the operating system does for a double click.
	inst, isFirst, err := app.Acquire("", "com.acme.files", []string{ps[0], "--flag", ps[1]}, nil)
	if err != nil || isFirst || inst != nil {
		t.Fatalf("second launch: %v %v %v", inst, isFirst, err)
	}
	var seen []string
	for range 2 {
		select {
		case p := <-got:
			seen = append(seen, p)
		case <-time.After(5 * time.Second):
			t.Fatalf("the running instance heard only %v", seen)
		}
	}
	if !reflect.DeepEqual(seen, ps) {
		t.Fatalf("opened %v, want %v", seen, ps)
	}
	if !readable(t, ps[1]) {
		t.Error("a file from a second instance is not readable through the fs scope")
	}
	waitFor(t, "the window to be raised", func() bool {
		c := r.main.Called()
		return slices.Contains(c, "show") && slices.Contains(c, "focus")
	})
}

func TestFileDropsReachTheViewAndAreGranted(t *testing.T) {
	ps := filesFixture(t, "dropped.md")
	r := newRig(t)
	got := make(chan window.FileDropEvent, 2)
	cancel, err := window.OnFileDrop(func(e window.FileDropEvent) { got <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitFor(t, "the drop stream", func() bool { return r.host.Subscribers("windowdrops") > 0 })
	if readable(t, ps[0]) {
		t.Fatal("readable before the drop")
	}
	r.app.EmitFileDrop(caps.FileDrop{View: r.main.PrimaryView(), Window: r.main.Ref(), Paths: ps, X: 12, Y: 34})
	select {
	case e := <-got:
		if e.View != "main" || e.Window != "main" || !reflect.DeepEqual(e.Paths, ps) || e.X != 12 || e.Y != 34 {
			t.Fatalf("drop event %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no drop event")
	}
	if !readable(t, ps[0]) {
		t.Error("a dropped file is not readable through the fs scope")
	}
	// A drop with no paths, or on an unknown view and window, says nothing.
	r.app.EmitFileDrop(caps.FileDrop{View: r.main.PrimaryView(), Window: r.main.Ref()})
	r.app.EmitFileDrop(caps.FileDrop{View: 9999, Window: 9999, Paths: ps})
	select {
	case e := <-got:
		t.Fatalf("unexpected drop event %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
	// The page learns which view it is.
	if raw := uiCall(t, r.ui("window"), "krate/window.thisView", nil); raw != `{"id":1,"ok":"ui"}` {
		t.Errorf("thisView = %s", raw)
	}
}
