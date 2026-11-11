// Package windows is the Windows implementation of the Krate desktop platform
// layer: Win32 windows hosting WebView2 through hand-written COM bindings over
// syscall, with no cgo and no WebView2Loader.dll requirement.
//
// The package does not import package platform (platform wires it in
// new_windows.go), so it defines its own Config and WindowConfig types.
//
// Files without a build tag hold the OS-independent logic (stream buffer,
// response writer, dispatch queue, vtable tables, URL helpers) so it can be
// unit tested on any OS. Files ending in _windows.go hold the Win32 and COM
// glue, which can only be exercised on Windows (see MANUAL_TEST.md).
package windows
