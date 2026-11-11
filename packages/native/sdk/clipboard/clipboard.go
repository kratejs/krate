// Package clipboard reads and writes the system clipboard: text, an image as
// PNG bytes, and a list of files.
//
// Reads need the separate "clipboard:read" permission when called from the UI
// because a page that can read the clipboard can see what the user copied in
// other applications. Reads return an empty value when the clipboard holds
// nothing of that kind.
package clipboard

import (
	"context"

	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permissions the UI needs.
const (
	Permission     = "clipboard"
	ReadPermission = "clipboard:read"
)

// Host function ids.
const (
	IDReadText   = "krate/clipboard.readText"
	IDWriteText  = "krate/clipboard.writeText"
	IDReadImage  = "krate/clipboard.readImage"
	IDWriteImage = "krate/clipboard.writeImage"
	IDReadFiles  = "krate/clipboard.readFiles"
	IDWriteFiles = "krate/clipboard.writeFiles"
	IDClear      = "krate/clipboard.clear"
)

// ReadText returns the clipboard text.
func ReadText() (string, error) {
	d, err := bridge.Call(context.Background(), IDReadText, nil)
	if err != nil {
		return "", err
	}
	if d.IsNull() {
		return "", nil
	}
	return d.String()
}

// WriteText replaces the clipboard with text.
func WriteText(s string) error {
	return bridge.CallDo(context.Background(), IDWriteText, func(e *wire.Encoder) { e.String(s) })
}

// ReadImage returns the clipboard image as PNG bytes, or nil.
func ReadImage() ([]byte, error) {
	d, err := bridge.Call(context.Background(), IDReadImage, nil)
	if err != nil {
		return nil, err
	}
	if d.IsNull() {
		return nil, nil
	}
	return d.Bytes()
}

// WriteImage puts a PNG image on the clipboard.
func WriteImage(png []byte) error {
	return bridge.CallDo(context.Background(), IDWriteImage, func(e *wire.Encoder) { e.Bytes(png) })
}

// ReadFiles returns the absolute paths of files on the clipboard.
func ReadFiles() ([]string, error) {
	d, err := bridge.Call(context.Background(), IDReadFiles, nil)
	if err != nil {
		return nil, err
	}
	return codec.ReadStrings(d)
}

// WriteFiles puts a list of files on the clipboard, as a file manager's copy.
func WriteFiles(paths []string) error {
	return bridge.CallDo(context.Background(), IDWriteFiles, func(e *wire.Encoder) { codec.WriteStrings(e, paths) })
}

// Clear empties the clipboard.
func Clear() error { return bridge.CallDo(context.Background(), IDClear, nil) }
