// Package darwin implements the macOS window and webview layer with AppKit and
// WKWebView, reached through purego (no cgo). The OS-independent parts (header
// conversion, request building, the response writer state machine and the
// main-thread work queue) live in files without a build tag so they can be
// unit tested on any platform; everything that touches Objective-C is in
// *_darwin.go files.
//
// This package does not import the parent platform package (which imports it
// from new_darwin.go); platform.New adapts between the two option types.
package darwin
