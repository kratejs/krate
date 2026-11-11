// Package dialog shows native file and message dialogs.
//
// The functions work from Go host code in every process: in a dev run the
// call is forwarded to the shell, which owns the windows. They block until
// the user answers. A cancelled file dialog returns no paths and no error.
package dialog

import (
	"context"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permission is the app permission the UI needs to call these functions.
const Permission = "dialog"

// Host function ids.
const (
	IDOpenFile   = "krate/dialog.OpenFile"
	IDOpenFolder = "krate/dialog.OpenFolder"
	IDSaveFile   = "krate/dialog.SaveFile"
	IDMessage    = "krate/dialog.Message"
)

// Filter limits a file dialog to files matching glob patterns such as "*.png".
type Filter struct {
	Name     string
	Patterns []string
}

// OpenOptions configures OpenFile and OpenFolder.
type OpenOptions struct {
	Title      string
	Dir        string // initial folder
	Filters    []Filter
	Multiple   bool
	ShowHidden bool
	// Window is the SDK id of the parent window ("" = the main window).
	Window string
}

// SaveOptions configures SaveFile.
type SaveOptions struct {
	Title   string
	Dir     string
	Name    string // suggested file name
	Filters []Filter
	// Overwrite asks before replacing an existing file.
	Overwrite bool
	Window    string
}

// Kind is the icon of a message dialog.
type Kind int

// Message dialog kinds.
const (
	Info Kind = iota
	Warning
	Error
	Question
)

// MessageOptions configures Message.
type MessageOptions struct {
	Kind    Kind
	Title   string
	Message string
	Detail  string
	// Buttons are the labels, left to right; empty means a single "OK".
	Buttons []string
	// Default is the index of the default button.
	Default int
	Window  string
}

// OpenFile asks the user to choose existing files.
func OpenFile(ctx krate.Ctx, o OpenOptions) ([]string, error) {
	return openCall(ctx, IDOpenFile, o)
}

// OpenFolder asks the user to choose folders.
func OpenFolder(ctx krate.Ctx, o OpenOptions) ([]string, error) {
	return openCall(ctx, IDOpenFolder, o)
}

func openCall(ctx context.Context, id string, o OpenOptions) ([]string, error) {
	d, err := bridge.Call(ctx, id, func(e *wire.Encoder) { encodeOpen(e, o) })
	if err != nil {
		return nil, err
	}
	return codec.ReadStrings(d)
}

// SaveFile asks the user for a file name to write. It returns "" when the
// user cancels.
func SaveFile(ctx krate.Ctx, o SaveOptions) (string, error) {
	d, err := bridge.Call(ctx, IDSaveFile, func(e *wire.Encoder) { encodeSave(e, o) })
	if err != nil {
		return "", err
	}
	if d.IsNull() {
		return "", nil
	}
	return d.String()
}

// Message shows a message dialog and returns the index of the pressed button,
// or -1 when it was closed without one.
func Message(ctx krate.Ctx, o MessageOptions) (int, error) {
	d, err := bridge.Call(ctx, IDMessage, func(e *wire.Encoder) { encodeMessage(e, o) })
	if err != nil {
		return -1, err
	}
	n, err := d.Int()
	return int(n), err
}

// Confirm asks a yes/no question and reports whether the user chose Yes.
func Confirm(ctx krate.Ctx, title, message string) (bool, error) {
	n, err := Message(ctx, MessageOptions{Kind: Question, Title: title, Message: message, Buttons: []string{"No", "Yes"}, Default: 1})
	return n == 1, err
}

func encodeFilters(e *wire.Encoder, fs []Filter) {
	e.BeginArray()
	for _, f := range fs {
		e.BeginObject()
		e.Key("name")
		e.String(f.Name)
		e.Key("patterns")
		codec.WriteStrings(e, f.Patterns)
		e.EndObject()
	}
	e.EndArray()
}

func decodeFilters(r *codec.R) []Filter {
	var out []Filter
	r.Array(func() {
		var f Filter
		r.Object(func(k string) bool {
			switch k {
			case "name":
				f.Name = r.String()
			case "patterns":
				f.Patterns = r.Strings()
			default:
				return false
			}
			return true
		})
		out = append(out, f)
	})
	return out
}

func encodeOpen(e *wire.Encoder, o OpenOptions) {
	e.BeginObject()
	e.Key("title")
	e.String(o.Title)
	e.Key("dir")
	e.String(o.Dir)
	e.Key("filters")
	encodeFilters(e, o.Filters)
	e.Key("multiple")
	e.Bool(o.Multiple)
	e.Key("showHidden")
	e.Bool(o.ShowHidden)
	e.Key("window")
	e.String(o.Window)
	e.EndObject()
}

func decodeOpen(r *codec.R) (o OpenOptions) {
	r.Object(func(k string) bool {
		switch k {
		case "title":
			o.Title = r.String()
		case "dir":
			o.Dir = r.String()
		case "filters":
			o.Filters = decodeFilters(r)
		case "multiple":
			o.Multiple = r.Bool()
		case "showHidden":
			o.ShowHidden = r.Bool()
		case "window":
			o.Window = r.String()
		default:
			return false
		}
		return true
	})
	return
}

func encodeSave(e *wire.Encoder, o SaveOptions) {
	e.BeginObject()
	e.Key("title")
	e.String(o.Title)
	e.Key("dir")
	e.String(o.Dir)
	e.Key("name")
	e.String(o.Name)
	e.Key("filters")
	encodeFilters(e, o.Filters)
	e.Key("overwrite")
	e.Bool(o.Overwrite)
	e.Key("window")
	e.String(o.Window)
	e.EndObject()
}

func decodeSave(r *codec.R) (o SaveOptions) {
	r.Object(func(k string) bool {
		switch k {
		case "title":
			o.Title = r.String()
		case "dir":
			o.Dir = r.String()
		case "name":
			o.Name = r.String()
		case "filters":
			o.Filters = decodeFilters(r)
		case "overwrite":
			o.Overwrite = r.Bool()
		case "window":
			o.Window = r.String()
		default:
			return false
		}
		return true
	})
	return
}

func encodeMessage(e *wire.Encoder, o MessageOptions) {
	e.BeginObject()
	e.Key("kind")
	e.Int(int64(o.Kind))
	e.Key("title")
	e.String(o.Title)
	e.Key("message")
	e.String(o.Message)
	e.Key("detail")
	e.String(o.Detail)
	e.Key("buttons")
	codec.WriteStrings(e, o.Buttons)
	e.Key("default")
	e.Int(int64(o.Default))
	e.Key("window")
	e.String(o.Window)
	e.EndObject()
}

func decodeMessage(r *codec.R) (o MessageOptions) {
	r.Object(func(k string) bool {
		switch k {
		case "kind":
			o.Kind = Kind(r.Int())
		case "title":
			o.Title = r.String()
		case "message":
			o.Message = r.String()
		case "detail":
			o.Detail = r.String()
		case "buttons":
			o.Buttons = r.Strings()
		case "default":
			o.Default = int(r.Int())
		case "window":
			o.Window = r.String()
		default:
			return false
		}
		return true
	})
	return
}
