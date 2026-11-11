//go:build windows && arm64

package windows

import "unsafe"

// putBounds calls ICoreWebView2Controller::put_Bounds. On ARM64 a 16-byte
// struct is passed by value in two integer registers: (left, top) packed in
// the first, (right, bottom) in the second. This matches how go-webview2
// does it.
func putBounds(controller unsafe.Pointer, r *rect) {
	words := (*[2]uintptr)(unsafe.Pointer(r))
	vcall(controller, ctlPutBounds, words[0], words[1])
}
