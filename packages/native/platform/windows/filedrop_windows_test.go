//go:build windows

package windows

import (
	"encoding/json"
	"reflect"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// The COM objects below are Go implementations of what WebView2 hands the
// host with a drop: a message-received args object that also supports
// ICoreWebView2WebMessageReceivedEventArgs2, a collection of additional
// objects and ICoreWebView2File values. Their method order is the vtable
// order of WebView2.h (checked by TestVtableLayout), so droppedFiles walks
// them exactly as it walks the real ones.

type fakeCOM struct {
	vtbl unsafe.Pointer
	refs atomic.Int32
	ids  []guid

	// by kind
	coll  *fakeCOM
	items []*fakeCOM
	path  string
}

var (
	fakeMu    sync.Mutex
	fakeObjs  = map[uintptr]*fakeCOM{}
	fakeOnce  sync.Once
	vtArgs2   unsafe.Pointer
	vtColl    unsafe.Pointer
	vtFile    unsafe.Pointer
	cotaskMem = syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemAlloc")
)

func fakeOf(this uintptr) *fakeCOM {
	fakeMu.Lock()
	defer fakeMu.Unlock()
	return fakeObjs[this]
}

func newFake(vt unsafe.Pointer, ids ...string) *fakeCOM {
	f := &fakeCOM{vtbl: vt, ids: []guid{mustGUID(iidIUnknown)}}
	for _, id := range ids {
		f.ids = append(f.ids, mustGUID(id))
	}
	f.refs.Store(1)
	fakeMu.Lock()
	fakeObjs[uintptr(unsafe.Pointer(f))] = f
	fakeMu.Unlock()
	return f
}

func initFakeVtables() {
	fakeOnce.Do(func() {
		qi := syscall.NewCallback(func(this, riid, out uintptr) uintptr {
			*(*uintptr)(ptr(out)) = 0
			f := fakeOf(this)
			if f == nil {
				return eNoInterface
			}
			g := *(*guid)(ptr(riid))
			for _, id := range f.ids {
				if id == g {
					f.refs.Add(1)
					*(*uintptr)(ptr(out)) = this
					return sOK
				}
			}
			return eNoInterface
		})
		addRef := syscall.NewCallback(func(this uintptr) uintptr { return uintptr(fakeOf(this).refs.Add(1)) })
		release := syscall.NewCallback(func(this uintptr) uintptr { return uintptr(fakeOf(this).refs.Add(-1)) })
		nop := syscall.NewCallback(func(this, a uintptr) uintptr { return eNotImpl })

		getAdditional := syscall.NewCallback(func(this, out uintptr) uintptr {
			f := fakeOf(this)
			f.coll.refs.Add(1)
			*(*uintptr)(ptr(out)) = uintptr(unsafe.Pointer(f.coll))
			return sOK
		})
		getCount := syscall.NewCallback(func(this, out uintptr) uintptr {
			*(*uint32)(ptr(out)) = uint32(len(fakeOf(this).items))
			return sOK
		})
		getAt := syscall.NewCallback(func(this, i, out uintptr) uintptr {
			f := fakeOf(this)
			if int(i) >= len(f.items) {
				return eInvalidArg
			}
			it := f.items[i]
			it.refs.Add(1)
			*(*uintptr)(ptr(out)) = uintptr(unsafe.Pointer(it))
			return sOK
		})
		getPath := syscall.NewCallback(func(this, out uintptr) uintptr {
			u, _ := syscall.UTF16FromString(fakeOf(this).path)
			p, _, _ := cotaskMem.Call(uintptr(len(u) * 2))
			copy(unsafe.Slice((*uint16)(ptr(p)), len(u)), u)
			*(*uintptr)(ptr(out)) = p
			return sOK
		})
		mk := func(n int, set map[int]uintptr) unsafe.Pointer {
			vt := make([]uintptr, n)
			for i := range vt {
				vt[i] = nop
			}
			vt[iunknownSlotQI], vt[iunknownSlotAddRef], vt[iunknownSlotRelease] = qi, addRef, release
			for i, f := range set {
				vt[i] = f
			}
			return unsafe.Pointer(&vt[0])
		}
		vtArgs2 = mk(len(ifaceMethods["ICoreWebView2WebMessageReceivedEventArgs2"]), map[int]uintptr{msgArgs2GetAdditionalObjects: getAdditional})
		vtColl = mk(len(ifaceMethods["ICoreWebView2ObjectCollectionView"]), map[int]uintptr{objViewGetCount: getCount, objViewGetValueAtIndex: getAt})
		vtFile = mk(len(ifaceMethods["ICoreWebView2File"]), map[int]uintptr{fileGetPath: getPath})
	})
}

// fakeDropArgs builds the args of a message with the given additional
// objects: a path makes an ICoreWebView2File, "!" an object that is not a file.
func fakeDropArgs(paths ...string) (args *fakeCOM, all []*fakeCOM) {
	initFakeVtables()
	coll := newFake(vtColl, iidObjectCollectionView)
	for _, p := range paths {
		var it *fakeCOM
		if p == "!" {
			it = newFake(vtFile) // supports IUnknown only
		} else {
			it = newFake(vtFile, iidWebView2File)
			it.path = p
		}
		coll.items = append(coll.items, it)
	}
	args = newFake(vtArgs2, iidWebMessageArgs2)
	args.coll = coll
	return args, append([]*fakeCOM{args, coll}, coll.items...)
}

func TestDroppedFilesReadsPathsFromAdditionalObjects(t *testing.T) {
	args, all := fakeDropArgs(`C:\Users\me\a file.txt`, "!", "", `D:\data\b.md`)
	paths, n := droppedFiles(unsafe.Pointer(args))
	if want := []string{`C:\Users\me\a file.txt`, `D:\data\b.md`}; !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %q, want %q", paths, want)
	}
	if n != 4 {
		t.Errorf("objects = %d, want 4", n)
	}
	for i, f := range all {
		if got := f.refs.Load(); got != 1 {
			t.Errorf("object %d holds %d references after the call, want the creator's 1 (a leak or a double release)", i, got)
		}
	}
}

func TestDroppedFilesWithoutArgs2(t *testing.T) {
	initFakeVtables()
	old := newFake(vtArgs2) // an args object without the version 2 interface
	paths, n := droppedFiles(unsafe.Pointer(old))
	if paths != nil || n != 0 {
		t.Errorf("paths %q objects %d from a runtime without args2", paths, n)
	}
	if got := old.refs.Load(); got != 1 {
		t.Errorf("references = %d, want 1", got)
	}
}

// TestGUISyntheticDropReachesTheHost dispatches a drop event whose
// DataTransfer holds a script-made File. The injected shim must post it with
// postMessageWithAdditionalObjects, the host must read one additional object
// (a script-made File has no path, so there is nothing to report), and the
// page's own drop listener must still run.
func TestGUISyntheticDropReachesTheHost(t *testing.T) {
	needGUI(t)
	w, err := guiApp.NewWindow(WindowConfig{Title: "drop", Width: 480, Height: 360, Resizable: true, Path: "/?w=drop"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitReport(t, "drop")
	drops := make(chan caps.FileDrop, 2)
	guiApp.OnFileDrop(func(d caps.FileDrop) { drops <- d })
	defer guiApp.OnFileDrop(nil)
	m0, o0 := dropSeen.messages.Load(), dropSeen.objects.Load()

	w.Eval(`document.addEventListener("drop", () => fetch("/report", {method: "POST", body: JSON.stringify({window: "dropcheck", origin: location.origin})}));
		const dt = new DataTransfer();
		dt.items.add(new File(["hello"], "synthetic.txt"));
		document.body.dispatchEvent(new DragEvent("drop", {dataTransfer: dt, clientX: 12, clientY: 34, bubbles: true, cancelable: true}));`)

	waitReport(t, "dropcheck") // the page's own listener ran
	deadline := time.Now().Add(15 * time.Second)
	for dropSeen.messages.Load() == m0 {
		if time.Now().After(deadline) {
			dropNoMessage(t, w)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := dropSeen.objects.Load() - o0; got != 1 {
		t.Errorf("additional objects = %d, want 1", got)
	}
	select {
	case d := <-drops:
		t.Errorf("a File with no path was reported as a drop: %+v", d)
	case <-time.After(200 * time.Millisecond):
	}
}

// dropNoMessage tells why the host got no drop message: it asks the page
// whether the shim is installed and whether the runtime accepts a File made by
// script as an additional object. The first is a failure; the second is a
// property of the runtime that this test cannot change.
func dropNoMessage(t *testing.T, w *Window) {
	t.Helper()
	w.Eval(`(function(){
		var wv = window.chrome && window.chrome.webview, err = "";
		try { wv.postMessageWithAdditionalObjects("{}", [new File(["x"], "p.txt")]); } catch (e) { err = String(e); }
		fetch("/report", {method: "POST", body: JSON.stringify({window: "dropprobe", error: JSON.stringify({shim: !!window.__krateDrop, api: !!(wv && wv.postMessageWithAdditionalObjects), err: err})})});
	})();`)
	r := waitReport(t, "dropprobe")
	t.Logf("page probe: %s", r.Error)
	var p struct {
		Shim, API bool
		Err       string
	}
	if err := json.Unmarshal([]byte(r.Error), &p); err != nil {
		t.Fatalf("the host got no drop message (unreadable probe %q)", r.Error)
	}
	switch {
	case !p.API:
		t.Skip("this WebView2 runtime has no postMessageWithAdditionalObjects")
	case !p.Shim:
		t.Fatal("the drop shim is not installed in the page")
	case p.Err != "":
		t.Skipf("the runtime refuses a script-made File as an additional object: %s", p.Err)
	}
	t.Fatal("the shim is installed and the runtime accepts the call, yet the host got no drop message")
}
