// Package crash reports crashes of the app: Go panics and fatal errors in the
// shell or the Go host, and crashes of the webview's render process.
//
// A Go crash that ends a process is written to a JSON report in
// <logDir>/crashes by the next process to list the reports (see package
// crashreport). A webview process crash (WebKitGTK web-process-terminated,
// WKWebView webViewWebContentProcessDidTerminate:, WebView2 ProcessFailed) is
// reported at once as an event and a report, and the shell can reload the view
// by itself (SetAutoReload). The app lists, reads and deletes reports on its
// next launch and uploads them wherever it likes; nothing leaves the machine
// by itself.
package crash

import (
	"github.com/kratejs/krate/packages/native/crashreport"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
)

// Permission is the app permission the UI needs.
const Permission = "crash"

// Host function ids.
const (
	IDList          = "krate/crash.list"
	IDRead          = "krate/crash.read"
	IDDelete        = "krate/crash.delete"
	IDSetAutoReload = "krate/crash.setAutoReload"
	IDEvents        = "krate/crash.events"
)

// Summary describes a report in a listing.
type Summary = crashreport.Summary

// Report is a full crash report.
type Report = crashreport.Report

// Event reports a crash that happened while the app ran.
type Event struct {
	// Kind is "webview" (a render process crash) or "panic"/"fatal" (the Go
	// host child ended).
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
	Window string `json:"window"`
	View   string `json:"view"`
	// ReportID is the report written for it, "" if writing failed.
	ReportID string `json:"reportId"`
	// Reloaded is true when the shell reloaded the view.
	Reloaded bool `json:"reloaded"`
}

// List returns the stored reports, newest first.
func List() ([]Summary, error) { return jsonwire.Query[[]Summary](IDList) }

// Read returns one report.
func Read(id string) (Report, error) { return jsonwire.Query[Report](IDRead, id) }

// Delete removes a report.
func Delete(id string) error { return jsonwire.Do(IDDelete, id) }

// SetAutoReload makes the shell reload a webview whose process crashed.
func SetAutoReload(on bool) error { return jsonwire.Do(IDSetAutoReload, on) }

// OnCrash calls fn for crashes until cancel is called.
func OnCrash(fn func(Event)) (cancel func(), err error) { return jsonwire.Events(IDEvents, fn) }
