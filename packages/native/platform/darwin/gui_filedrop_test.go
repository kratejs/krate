//go:build darwin

package darwin

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI tests for file drops and open-file requests. A real drag needs a human
// or the accessibility APIs (see MANUAL_TEST.md); these drive the same code
// with what AppKit would hand it: an NSDraggingInfo whose pasteboard holds
// file URLs, and the delegate's application:openURLs: call.

var (
	fakeDragOnce sync.Once
	fakeDragPB   objc.ID
)

// fakeDragInfo is an object that answers draggingPasteboard, which is all the
// drop watcher reads when the location is not available.
func fakeDragInfo(t *testing.T, pb objc.ID) objc.ID {
	t.Helper()
	fakeDragOnce.Do(func() {
		_, err := objc.RegisterClass("KrateFakeDragInfo", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: sel("draggingPasteboard"), Fn: func(self objc.ID, _ objc.SEL) objc.ID { return fakeDragPB }},
		})
		if err != nil {
			t.Fatalf("registering the fake dragging info: %v", err)
		}
	})
	fakeDragPB = pb
	return cls("KrateFakeDragInfo").Send(sel("new"))
}

func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func realPaths(t *testing.T, in []string) []string {
	out := make([]string, len(in))
	for i, p := range in {
		out[i] = realPath(t, p)
	}
	return out
}

func writeFiles(t *testing.T, names ...string) []string {
	t.Helper()
	dir := t.TempDir()
	var out []string
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func TestGUIWebViewWatchesDrops(t *testing.T) {
	requireGUI(t)
	if !fileState.dropClass {
		t.Fatal("the WKWebView subclass for file drops was not registered")
	}
	w, err := guiApp.NewWindow(WindowOptions{Title: "drop class", Width: 400, Height: 300, Resizable: true, Path: "/?drop=class"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitReport(t)
	win := w
	var name string
	var overridden bool
	runOnMain(func() {
		withPool(func() {
			web := win.pv.web
			name = goString(web.Send(sel("class")).Send(sel("description")))
			own := classMethodImp(objc.GetClass(webViewClass), sel("performDragOperation:"))
			wk := classMethodImp(objc.GetClass("WKWebView"), sel("performDragOperation:"))
			overridden = own != 0 && own != wk
		})
	})
	if name != webViewClass {
		t.Errorf("the window's web view is a %s, want %s", name, webViewClass)
	}
	if !overridden {
		t.Error("KrateWebView does not override performDragOperation:")
	}
	if fileState.superDrag == 0 {
		t.Error("WKWebView's own performDragOperation: was not found")
	}
}

func TestGUIDropReadsFilePathsFromThePasteboard(t *testing.T) {
	requireGUI(t)
	w, err := guiApp.NewWindow(WindowOptions{Title: "drop pb", Width: 400, Height: 300, Resizable: true, Path: "/?drop=pb"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitReport(t)
	win := w
	files := writeFiles(t, "a file.txt", "b.md")
	got := make(chan caps.FileDrop, 2)
	guiApp.OnFileDrop(func(d caps.FileDrop) { got <- d })
	defer guiApp.OnFileDrop(nil)

	runOnMain(func() {
		withPool(func() {
			pb := cls("NSPasteboard").Send(sel("pasteboardWithUniqueName"))
			pb.Send(sel("clearContents"))
			items := cls("NSMutableArray").Send(sel("array"))
			for _, f := range files {
				items.Send(sel("addObject:"), cls("NSURL").Send(sel("fileURLWithPath:"), nsString(f)))
			}
			// A web URL on the pasteboard is not a file.
			items.Send(sel("addObject:"), cls("NSURL").Send(sel("URLWithString:"), nsString("https://example.com/x.png")))
			pb.Send(sel("writeObjects:"), items)
			guiApp.notifyDrop(win.pv.web, fakeDragInfo(t, pb))
		})
	})
	select {
	case d := <-got:
		if !reflect.DeepEqual(realPaths(t, d.Paths), realPaths(t, files)) {
			t.Errorf("paths = %v, want %v", d.Paths, files)
		}
		if d.View != win.pv.ref || d.Window != win.Ref() {
			t.Errorf("view %d window %d, want %d %d", d.View, d.Window, win.pv.ref, win.Ref())
		}
		if d.X != -1 || d.Y != -1 {
			t.Errorf("position %d,%d; the fake has none", d.X, d.Y)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no drop reported")
	}

	// A drag with no files (text, or web URLs only) reports nothing.
	runOnMain(func() {
		withPool(func() {
			pb := cls("NSPasteboard").Send(sel("pasteboardWithUniqueName"))
			pb.Send(sel("clearContents"))
			items := cls("NSMutableArray").Send(sel("array"))
			items.Send(sel("addObject:"), cls("NSURL").Send(sel("URLWithString:"), nsString("https://example.com/y.png")))
			pb.Send(sel("writeObjects:"), items)
			guiApp.notifyDrop(win.pv.web, fakeDragInfo(t, pb))
		})
	})
	select {
	case d := <-got:
		t.Fatalf("unexpected drop %+v", d)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestGUIDelegateOpensFilesAndURLs(t *testing.T) {
	requireGUI(t)
	files := writeFiles(t, "doc one.md", "doc two.md")
	gotFiles := make(chan []string, 4)
	gotURLs := make(chan []string, 4)
	guiApp.OnOpenFiles(func(p []string) { gotFiles <- p })
	guiApp.OnOpenURLs(func(u []string) { gotURLs <- u })
	defer guiApp.OnOpenFiles(nil)
	defer guiApp.OnOpenURLs(nil)

	var responds bool
	runOnMain(func() {
		withPool(func() {
			responds = respondsTo(guiApp.appDelegate, sel("application:openURLs:"))
			urls := cls("NSMutableArray").Send(sel("array"))
			for _, f := range files {
				urls.Send(sel("addObject:"), cls("NSURL").Send(sel("fileURLWithPath:"), nsString(f)))
			}
			urls.Send(sel("addObject:"), cls("NSURL").Send(sel("URLWithString:"), nsString("acme://open/42")))
			// What AppKit calls for a double click, "open -a" or a Dock drop.
			guiApp.appDelegate.Send(sel("application:openURLs:"), guiApp.nsApp, urls)
		})
	})
	if !responds {
		t.Fatal("the app delegate does not implement application:openURLs:")
	}
	select {
	case p := <-gotFiles:
		if !reflect.DeepEqual(realPaths(t, p), realPaths(t, files)) {
			t.Errorf("files = %v, want %v", p, files)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no open-file request")
	}
	select {
	case u := <-gotURLs:
		if !reflect.DeepEqual(u, []string{"acme://open/42"}) {
			t.Errorf("urls = %v", u)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no open-URL request")
	}
}
