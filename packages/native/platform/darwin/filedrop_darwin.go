//go:build darwin

package darwin

import (
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.FileDrops = (*App)(nil)
	_ caps.OpenFiles = (*App)(nil)
	_ caps.OpenURLs  = (*App)(nil)
)

// webViewClass is the name of the WKWebView subclass that watches drops.
const webViewClass = "KrateWebView"

// fileState is the process-wide state of file drops and open requests; there
// is one App per process.
var fileState struct {
	mu    sync.Mutex
	drop  func(caps.FileDrop)
	files pathQueue
	urls  pathQueue
	// dropClass is false when the WKWebView subclass could not be registered.
	dropClass bool
	// superDrag is WKWebView's own performDragOperation: implementation. The
	// override calls it directly instead of objc_msgSendSuper: a KVO observer
	// on the view swaps in a subclass of ours, and a super send from there
	// would find our override again.
	superDrag uintptr
}

// OnFileDrop sets the handler for files dropped onto a view. The page keeps
// its own HTML5 drop events: the WKWebView subclass reads the dragging
// pasteboard and then lets WebKit handle the drop as usual.
func (a *App) OnFileDrop(fn func(caps.FileDrop)) {
	fileState.mu.Lock()
	fileState.drop = fn
	fileState.mu.Unlock()
}

// OnOpenFiles sets the handler for open-file requests (Finder double click,
// "open -a", a drop on the Dock icon). Requests that arrived earlier are
// delivered to it at once.
func (a *App) OnOpenFiles(fn func(paths []string)) { fileState.files.setHandler(fn) }

// OnOpenURLs sets the handler for URLs of the app's schemes.
func (a *App) OnOpenURLs(fn func(urls []string)) { fileState.urls.setHandler(fn) }

// classMethodImp is class_getMethodImplementation, bound by registerFileClasses.
var classMethodImp func(class objc.Class, name objc.SEL) uintptr

// registerFileClasses registers KrateWebView, the WKWebView subclass that
// watches drops (performDragOperation:) and reports context menus before they
// open (willOpenMenu:withEvent:, see contextmenu_darwin.go). Both overrides
// call WebKit's own implementation directly instead of objc_msgSendSuper: a
// KVO observer on the view swaps in a subclass of ours, and a super send from
// there would find our override again. A failure leaves plain WKWebViews,
// and with them no file drops and the engine's own context menu.
func registerFileClasses(a *App) {
	wk := objc.GetClass("WKWebView")
	if wk == 0 {
		return
	}
	libobjc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return
	}
	purego.RegisterLibFunc(&classMethodImp, libobjc, "class_getMethodImplementation")
	var methods []objc.MethodDef
	fileState.superDrag = classMethodImp(wk, sel("performDragOperation:"))
	if fileState.superDrag != 0 {
		methods = append(methods, objc.MethodDef{Cmd: sel("performDragOperation:"), Fn: func(self objc.ID, cmd objc.SEL, sender objc.ID) bool {
			// Read the pasteboard first: WebKit's handling may consume it.
			a.notifyDrop(self, sender)
			r, _, _ := purego.SyscallN(fileState.superDrag, uintptr(self), uintptr(cmd), uintptr(sender))
			return byte(r) != 0 // BOOL is a signed char
		}})
	}
	if superMenu := classMethodImp(wk, sel("willOpenMenu:withEvent:")); superMenu != 0 {
		methods = append(methods, objc.MethodDef{Cmd: sel("willOpenMenu:withEvent:"), Fn: func(self objc.ID, cmd objc.SEL, menu, event objc.ID) {
			purego.SyscallN(superMenu, uintptr(self), uintptr(cmd), uintptr(menu), uintptr(event))
			a.webViewWillOpenMenu(self, menu, event)
		}})
	}
	if len(methods) == 0 {
		return
	}
	_, err = objc.RegisterClass(webViewClass, wk, nil, nil, methods)
	fileState.dropClass = err == nil
}

// webViewClassName is the class newWebView instantiates.
func webViewClassName() string {
	if fileState.dropClass {
		return webViewClass
	}
	return "WKWebView"
}

// notifyDrop reports the files of a drop onto web. Main thread.
func (a *App) notifyDrop(web, sender objc.ID) {
	v := a.byWeb[web]
	if v == nil {
		return
	}
	paths := draggedFilePaths(sender)
	if len(paths) == 0 {
		return
	}
	x, y := -1, -1
	if respondsTo(sender, sel("draggingLocation")) {
		// The location is in window coordinates, origin bottom left.
		p := objc.Send[nsPoint](sender, sel("draggingLocation"))
		p = objc.Send[nsPoint](web, sel("convertPoint:fromView:"), p, objc.ID(0))
		x, y = int(p.X), int(p.Y)
		if !objc.Send[bool](web, sel("isFlipped")) {
			y = int(objc.Send[nsRect](web, sel("bounds")).Size.Height - p.Y)
		}
	}
	fileState.mu.Lock()
	fn := fileState.drop
	fileState.mu.Unlock()
	if fn != nil {
		fn(caps.FileDrop{View: v.ref, Window: v.w.Ref(), Paths: paths, X: x, Y: y})
	}
}

// draggedFilePaths returns the local files on the dragging pasteboard of an
// NSDraggingInfo.
func draggedFilePaths(sender objc.ID) (paths []string) {
	pb := sender.Send(sel("draggingPasteboard"))
	if pb == 0 {
		return nil
	}
	classes := cls("NSArray").Send(sel("arrayWithObject:"), cls("NSURL"))
	opts := cls("NSDictionary").Send(sel("dictionaryWithObject:forKey:"),
		cls("NSNumber").Send(sel("numberWithBool:"), true), nsString("NSPasteboardURLReadingFileURLsOnlyKey"))
	urls := pb.Send(sel("readObjectsForClasses:options:"), classes, opts)
	if urls == 0 {
		return nil
	}
	n := objc.Send[uint](urls, sel("count"))
	for i := uint(0); i < n; i++ {
		u := urls.Send(sel("objectAtIndex:"), i)
		if !objc.Send[bool](u, sel("isFileURL")) {
			continue
		}
		if p := goString(u.Send(sel("path"))); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// openURLs is application:openURLs:. File URLs are open-file requests; the
// rest are URLs of the schemes in CFBundleURLTypes.
func (a *App) openURLs(urls objc.ID) {
	if urls == 0 {
		return
	}
	var files, other []string
	n := objc.Send[uint](urls, sel("count"))
	for i := uint(0); i < n; i++ {
		u := urls.Send(sel("objectAtIndex:"), i)
		if objc.Send[bool](u, sel("isFileURL")) {
			if p := goString(u.Send(sel("path"))); p != "" {
				files = append(files, p)
			}
			continue
		}
		if s := goString(u.Send(sel("absoluteString"))); s != "" {
			other = append(other, s)
		}
	}
	fileState.files.push(files)
	fileState.urls.push(other)
}
