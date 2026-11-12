//go:build linux

package linux

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var colorOnce sync.Once

func registerColorPage() {
	colorOnce.Do(registerColor)
}

func registerColor() {
	guiSrv.mux.HandleFunc("/anim", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>a</title><body style="margin:0"><canvas id=c width=800 height=600></canvas><script>
const g=document.getElementById("c").getContext("2d");let t=0;
(function f(){t++;for(let i=0;i<200;i++){g.fillStyle="hsl("+((t*3+i*7)%360)+",80%,50%)";g.fillRect((i*37+t*5)%760,(i*53+t*3)%560,40,40);}requestAnimationFrame(f)})();
</script></body>`))
	})
	guiSrv.mux.HandleFunc("/color", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		c := r.URL.Query().Get("c")
		_, _ = w.Write([]byte(`<!doctype html><title>c</title><body style="margin:0;background:#` + c + `"><div id=d style="height:100vh"></div></body>`))
	})
}

func pixelAt(s caps.Snapshot, x, y int) (r, g, b, a byte) {
	i := y*s.Stride + x*4
	return s.Pix[i+2], s.Pix[i+1], s.Pix[i], s.Pix[i+3]
}

func TestGUIOffscreenSnapshot(t *testing.T) {
	needGUI(t)
	registerColorPage()
	if !guiApp.OffscreenSupported() {
		t.Skip("this GTK has no GtkOffscreenWindow")
	}
	loaded := make(chan struct{}, 4)
	w, err := guiApp.NewWindowNotify(WindowOptions{Title: "off", Width: 320, Height: 200, Resizable: true, Path: "/color?c=ff0000", Offscreen: true}, func() { loaded <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	select {
	case <-loaded:
	case <-time.After(20 * time.Second):
		t.Fatal("the offscreen page did not load")
	}
	ref := w.PrimaryView()
	snap, err := guiApp.SnapshotView(ref, caps.SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Width <= 0 || snap.Height <= 0 || len(snap.Pix) < snap.Stride*snap.Height {
		t.Fatalf("snapshot %dx%d stride %d len %d", snap.Width, snap.Height, snap.Stride, len(snap.Pix))
	}
	r, g, b, a := pixelAt(snap, snap.Width/2, snap.Height/2)
	if r < 250 || g > 5 || b > 5 || a != 255 {
		t.Fatalf("the red page rendered as rgba(%d,%d,%d,%d) (%dx%d)", r, g, b, a, snap.Width, snap.Height)
	}
	// The page is live: change it and capture again.
	w.Eval(`document.body.style.background = "#0000ff"`)
	waitFor(t, "the page to turn blue", func() bool {
		s, err := guiApp.SnapshotView(ref, caps.SnapshotOptions{})
		if err != nil {
			return false
		}
		r, g, b, _ := pixelAt(s, s.Width/2, s.Height/2)
		return b > 250 && r < 5 && g < 5
	})
	// A frame rate measurement for the docs (no assertion on the number).
	start := time.Now()
	n := 0
	for time.Since(start) < 2*time.Second {
		if _, err := guiApp.SnapshotView(ref, caps.SnapshotOptions{}); err != nil {
			t.Fatal(err)
		}
		n++
	}
	t.Logf("offscreen snapshot of a %dx%d view: %.1f frames/s (%d in %s)", snap.Width, snap.Height, float64(n)/time.Since(start).Seconds(), n, time.Since(start).Round(time.Millisecond))
	if n < 2 {
		t.Fatalf("only %d snapshots in two seconds", n)
	}
}

func TestGUISnapshotOfAMappedWindowAndErrors(t *testing.T) {
	needGUI(t)
	registerColorPage()
	w := ext_newWin(t, "snapvisible")
	snap, err := guiApp.SnapshotView(w.PrimaryView(), caps.SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Width == 0 || snap.Height == 0 {
		t.Fatalf("empty snapshot %+v", snap.Width)
	}
	if _, err := guiApp.SnapshotView(caps.ViewRef(99999), caps.SnapshotOptions{}); err == nil {
		t.Fatal("a missing view must fail")
	}
}

func TestGUIOffscreenSnapshotRate(t *testing.T) {
	needGUI(t)
	registerColorPage()
	loaded := make(chan struct{}, 4)
	w, err := guiApp.NewWindowNotify(WindowOptions{Title: "rate", Width: 800, Height: 600, Resizable: true, Path: "/anim", Offscreen: true}, func() { loaded <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	select {
	case <-loaded:
	case <-time.After(20 * time.Second):
		t.Fatal("the animated page did not load")
	}
	time.Sleep(500 * time.Millisecond)
	start := time.Now()
	n := 0
	for time.Since(start) < 3*time.Second {
		if _, err := guiApp.SnapshotView(w.PrimaryView(), caps.SnapshotOptions{}); err != nil {
			t.Fatal(err)
		}
		n++
	}
	t.Logf("offscreen snapshot of an animated 800x600 canvas page: %.1f frames/s (%d in %s)", float64(n)/time.Since(start).Seconds(), n, time.Since(start).Round(time.Millisecond))
	if n < 2 {
		t.Fatalf("only %d snapshots in three seconds", n)
	}
}
