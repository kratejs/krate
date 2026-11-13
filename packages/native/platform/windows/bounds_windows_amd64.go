//go:build windows && amd64

package windows

import "unsafe"

// putBounds calls ICoreWebView2Controller::put_Bounds. On x64 a RECT (16
// bytes) is passed by reference to a caller-made copy.
func putBounds(controller unsafe.Pointer, r *rect) {
	vcall(controller, ctlPutBounds, uintptr(unsafe.Pointer(r)))
}
