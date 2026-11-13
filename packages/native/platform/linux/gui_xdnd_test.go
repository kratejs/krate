//go:build linux

package linux

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

const dropZonePage = `<!doctype html><meta charset="utf-8"><title>zone</title>
<body style="margin:0"><div id="zone" style="position:absolute;inset:0;background:#cde">drop here</div>
<script>
const zone = document.getElementById("zone");
zone.addEventListener("dragover", (e) => e.preventDefault());
zone.addEventListener("drop", async (e) => {
  e.preventDefault();
  const files = [...e.dataTransfer.files].map((f) => f.name + ":" + f.size);
  await fetch("/droplog", {method: "POST", body: JSON.stringify({files, types: [...e.dataTransfer.types]})});
});
fetch("/droplog", {method: "POST", body: JSON.stringify({ready: true})});
</script>`

var dragSrcURIs struct {
	sync.Mutex
	body string
}

// dragSourceGet is the drag-data-get handler of the test source: it hands
// out the text/uri-list.
func dragSourceGet(_, _, sd, _, _, _ uintptr) uintptr {
	dragSrcURIs.Lock()
	body := []byte(dragSrcURIs.body)
	dragSrcURIs.Unlock()
	guiApp.x.gtkSelSet(sd, drops.atom, 8, unsafe.Pointer(&body[0]), int32(len(body)))
	return 0
}

// TestGUIFileDropFromARealDrag drags a file from a source widget onto the
// real WebKitWebView with synthesized X pointer events (xdotool), so GTK runs
// the whole XDND exchange. The platform must report the path and the page
// must still see its own drop event.
func TestGUIFileDropFromARealDrag(t *testing.T) {
	needGUI(t)
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Skip("xdotool is not installed (see ~/wt/kn-sysroot/env-svc.sh)")
	}
	if os.Getenv("KRATE_NATIVE_WM") != "1" {
		t.Skip("needs a window manager so that windows land where they are placed (KRATE_NATIVE_WM=1)")
	}
	x := guiApp.x
	if x.gtkSelSet == nil || x.gtkWidgetGetWindow == nil || x.gdkWindowGetOrigin == nil {
		t.Skip("missing GTK functions")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "dragged file.txt")
	if err := os.WriteFile(file, []byte("twelve bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	dragSrcURIs.Lock()
	dragSrcURIs.body = "file://" + (&urlPath{file}).escaped() + "\r\n"
	dragSrcURIs.Unlock()

	logCh := make(chan string, 8)
	xdndLog.Lock()
	xdndLog.ch = logCh
	xdndLog.Unlock()
	// The mux is shared by the whole package and a pattern can be registered
	// once, so the routes outlive a run (go test -count=N) and send to the
	// channel of the current one.
	xdndRoutes.Do(func() {
		guiSrv.mux.HandleFunc("/dropzone", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, dropZonePage)
		})
		guiSrv.mux.HandleFunc("/droplog", func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			xdndLog.Lock()
			ch := xdndLog.ch
			xdndLog.Unlock()
			select {
			case ch <- string(b):
			default:
			}
			w.WriteHeader(http.StatusNoContent)
		})
	})

	drops := make(chan caps.FileDrop, 4)
	guiApp.OnFileDrop(func(d caps.FileDrop) { drops <- d })
	t.Cleanup(func() { guiApp.OnFileDrop(nil) })

	loaded := make(chan struct{}, 2)
	target, err := guiApp.NewWindowNotify(WindowOptions{Title: "xdnd-target", Width: 500, Height: 400, Resizable: true, Path: "/dropzone"}, func() { loaded <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(target.Close)
	select {
	case <-loaded:
	case <-time.After(15 * time.Second):
		t.Fatal("target window did not load")
	}
	waitLog := func(what string) string {
		t.Helper()
		select {
		case s := <-logCh:
			return s
		case <-time.After(10 * time.Second):
			t.Fatalf("the page did not report %s", what)
			return ""
		}
	}
	if s := waitLog("being ready"); !strings.Contains(s, "ready") {
		t.Fatalf("first page report = %s", s)
	}

	// The drag source: a window with an event box.
	var srcWin, srcBox uintptr
	var srcX, srcY int32
	guiApp.callMain(func() {
		gtk, _ := purego.Dlopen("libgtk-3.so.0", rtldNow|rtldGlobal)
		var eventBoxNew func() uintptr
		var dragSourceSet func(w uintptr, mask uint32, targets unsafe.Pointer, n int32, actions uint32)
		purego.RegisterLibFunc(&eventBoxNew, gtk, "gtk_event_box_new")
		purego.RegisterLibFunc(&dragSourceSet, gtk, "gtk_drag_source_set")
		a := guiApp.api
		srcWin = a.gtkWindowNew(0)
		a.gtkWindowSetTitle(srcWin, "xdnd-source")
		a.gtkWindowSetDefSize(srcWin, 200, 120)
		srcBox = eventBoxNew()
		a.gtkContainerAdd(srcWin, srcBox)
		name := cstr("text/uri-list")
		entry := make([]byte, 16) // GtkTargetEntry {char *target; guint flags; guint info}
		*(*uintptr)(unsafe.Pointer(&entry[0])) = uintptr(unsafe.Pointer(&name[0]))
		dragSourceSet(srcBox, 256 /* GDK_BUTTON1_MASK */, unsafe.Pointer(&entry[0]), 1, 2 /* GDK_ACTION_COPY */)
		a.gSignalConnectData(srcBox, "drag-data-get", purego.NewCallback(dragSourceGet), 0, 0, 0)
		a.gtkWidgetShowAll(srcWin)
		x.gtkWindowMove(srcWin, 20, 20)
	})
	t.Cleanup(func() { guiApp.callMain(func() { guiApp.api.gtkWidgetDestroy(srcWin) }) })
	target.SetPosition(300, 100)

	origin := func(w uintptr) (ox, oy int32, ok bool) {
		guiApp.callMain(func() {
			if gw := x.gtkWidgetGetWindow(w); gw != 0 {
				ok = x.gdkWindowGetOrigin(gw, &ox, &oy) != 0
			}
		})
		return
	}
	var tx, ty int32
	waitFor(t, "the windows to be placed", func() bool {
		var ok1, ok2 bool
		srcX, srcY, ok1 = origin(srcBox)
		tx, ty, ok2 = origin(target.win)
		return ok1 && ok2 && srcX >= 0 && tx >= 300
	})
	time.Sleep(500 * time.Millisecond) // let the compositor and the WM settle

	sx, sy := srcX+60, srcY+40
	dx, dy := tx+150, ty+120
	args := []string{"mousemove", fmt.Sprint(sx), fmt.Sprint(sy), "sleep", "0.3", "mousedown", "1", "sleep", "0.2"}
	// Several small steps so that GTK passes the drag threshold and sends
	// XdndEnter, XdndPosition and XdndDrop.
	for i := 1; i <= 12; i++ {
		args = append(args, "mousemove", fmt.Sprint(sx+(dx-sx)*int32(i)/12), fmt.Sprint(sy+(dy-sy)*int32(i)/12), "sleep", "0.08")
	}
	args = append(args, "sleep", "0.4", "mouseup", "1")
	if out, err := exec.Command("xdotool", args...).CombinedOutput(); err != nil {
		t.Skipf("xdotool failed (no XTEST pointer?): %v\n%s", err, out)
	}

	select {
	case d := <-drops:
		if !reflect.DeepEqual(d.Paths, []string{file}) {
			t.Fatalf("platform drop paths = %v, want [%s]", d.Paths, file)
		}
		if v := guiApp.viewByRef(uintptr(d.View)); v == nil || v.w != target {
			t.Fatalf("drop reported for view %v, not the target window", d.View)
		}
		if d.X < 0 || d.Y < 0 {
			t.Errorf("drop position %d,%d", d.X, d.Y)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the platform reported no drop")
	}
	// The page must still get its own drop event. (This WebKitGTK build offers
	// no File objects for a uri-list drag from another widget, with or without
	// the watcher, so the check is on the event and its data types.)
	if s := waitLog("the HTML5 drop event"); !strings.Contains(s, `"types":["text/uri-list"`) {
		t.Fatalf("page drop report = %s", s)
	}
}

// urlPath escapes a path for a file URI.
type urlPath struct{ p string }

func (u *urlPath) escaped() string {
	var b strings.Builder
	for _, c := range []byte(u.p) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte("/-_.~", c) >= 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

var (
	xdndRoutes sync.Once
	xdndLog    struct {
		sync.Mutex
		ch chan string
	}
)
