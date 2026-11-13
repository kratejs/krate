// Package downloads controls the downloads a page starts: a link with the
// download attribute, a blob URL, a navigation to a file response. By default
// each download asks for a place to save with the native save dialog; a
// configured folder saves silently under a free file name. Events report the
// progress, the final path and the end of each download, and a running one can
// be cancelled.
//
// Linux uses WebKitGTK's download signals, macOS WKDownload and Windows the
// WebView2 DownloadStarting event. The engines cannot download a page of the
// app's own origin; those downloads are fetched from the app's handler by the
// shell and report the same events.
package downloads

import (
	"context"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permission is the app permission the UI needs for these operations.
const Permission = "downloads"

// Host function ids.
const (
	IDConfigure = "krate/downloads.configure"
	IDList      = "krate/downloads.list"
	IDCancel    = "krate/downloads.cancel"
	IDEvents    = "krate/downloads.events"
)

// State is the stage of a download.
type State string

// Download states.
const (
	Started   State = "started"
	Progress  State = "progress"
	Completed State = "completed"
	Cancelled State = "cancelled"
	Failed    State = "failed"
)

// Options configures where downloads go.
type Options struct {
	// Dir is the folder downloads are saved to without asking. Empty means
	// every download asks with a save dialog.
	Dir string
	// Ask makes the save dialog start in Dir instead of saving silently.
	Ask bool
}

// Info describes one download.
type Info struct {
	ID int64
	// View is the id of the view that started it.
	View     string
	URL      string
	Path     string
	State    State
	Received int64
	// Total is the size in bytes, -1 when unknown.
	Total int64
	// Error is set for Failed.
	Error string
}

// Event reports a change of a download; it carries the same fields as Info.
type Event = Info

// Configure sets where downloads are saved.
func Configure(o Options) error {
	return bridge.CallDo(context.Background(), IDConfigure, func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("dir")
		e.String(o.Dir)
		e.Key("ask")
		e.Bool(o.Ask)
		e.EndObject()
	})
}

// List returns the running downloads and the most recent finished ones.
func List() ([]Info, error) {
	d, err := bridge.Call(context.Background(), IDList, nil)
	if err != nil {
		return nil, err
	}
	r := &codec.R{D: d, Name: "download list"}
	var out []Info
	r.Array(func() { out = append(out, decodeInfo(r)) })
	return out, r.Err()
}

// Cancel stops a running download.
func Cancel(id int64) error {
	return bridge.CallDo(context.Background(), IDCancel, func(e *wire.Encoder) { e.Int(id) })
}

// OnEvent calls fn for every download event until cancel is called.
func OnEvent(fn func(Event)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDEvents, func(d *wire.Decoder) {
		r := &codec.R{D: d, Name: "download event"}
		ev := decodeInfo(r)
		if r.Err() == nil {
			fn(ev)
		}
	})
}

func encodeInfo(e *wire.Encoder, in Info) {
	e.BeginObject()
	e.Key("id")
	e.Int(in.ID)
	e.Key("view")
	e.String(in.View)
	e.Key("url")
	e.String(in.URL)
	e.Key("path")
	e.String(in.Path)
	e.Key("state")
	e.String(string(in.State))
	e.Key("received")
	e.Int(in.Received)
	e.Key("total")
	e.Int(in.Total)
	e.Key("error")
	e.String(in.Error)
	e.EndObject()
}

func decodeInfo(r *codec.R) (in Info) {
	r.Object(func(k string) bool {
		switch k {
		case "id":
			in.ID = r.Int()
		case "view":
			in.View = r.String()
		case "url":
			in.URL = r.String()
		case "path":
			in.Path = r.String()
		case "state":
			in.State = State(r.String())
		case "received":
			in.Received = r.Int()
		case "total":
			in.Total = r.Int()
		case "error":
			in.Error = r.String()
		default:
			return false
		}
		return true
	})
	return in
}

func fromCaps(env *bridge.Env, e caps.DownloadEvent) Info {
	in := Info{ID: int64(e.ID), URL: e.URL, Path: e.Path, State: State(e.State), Received: e.Received, Total: e.Total, Error: e.Error}
	if rec, ok := env.Windows.ViewByRef(e.View); ok {
		in.View = rec.ID
	}
	return in
}
