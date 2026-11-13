// Package worker starts background worker processes from host code or a page:
// the counterpart of Electron's utilityProcess.fork.
//
// A worker is declared in app.json under "workers" (the build writes it from
// native.workers) as a TypeScript module on Node, Bun or Deno, a Go program, or
// any executable that serves the host child protocol. Pages and host code
// start it by name; the app never runs a command a page names. Each instance
// is a separate process with
//
//   - its own SDK endpoint, whose permissions are the declared list narrowed
//     from the app's (a worker that lists nothing can call nothing but
//     worker.emit);
//   - its stdout and stderr in the app log, prefixed with the worker name;
//   - a restart policy (never, onFailure, always) with backoff, and crash,
//     restart and exit events;
//   - message passing: Post sends a value to its message handler, the worker
//     sends back with Emit, and both ride the wire codec, so binary data,
//     Dates, Maps and Sets cross unchanged. Ports (package port) can also be
//     handed to a worker for direct page-to-worker traffic.
//
// In a worker written in Go, call Serve with the message handler. In
// TypeScript, import onMessage and postMessage from "@krate/app/worker-host".
package worker

import (
	"context"
	"encoding/json"

	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permission is the app permission the UI needs.
const Permission = "worker"

// Host function ids.
const (
	IDStart  = "krate/worker.start"
	IDStop   = "krate/worker.stop"
	IDList   = "krate/worker.list"
	IDPost   = "krate/worker.post"
	IDEvents = "krate/worker.events"
	IDEmit   = "krate/worker.emit"
	IDSpecs  = "krate/worker.specs"
)

// MessageFn is the function id a worker serves for messages posted to it. It
// is shared by the Go and TypeScript worker libraries.
const MessageFn = "__worker.message"

// StartOptions are the optional arguments of Start.
type StartOptions struct {
	// Args are appended to the worker's declared arguments.
	Args []string `json:"args,omitempty"`
	// Env adds environment variables for this instance.
	Env map[string]string `json:"env,omitempty"`
}

// Start starts an instance of a declared worker and returns its id.
func Start(name string, opts *StartOptions) (string, error) {
	return queryJSON[string](IDStart, name, opts)
}

// Stop stops an instance; it does not restart.
func Stop(id string) error { return doJSON(IDStop, id) }

// List describes the running and finished instances.
func List() ([]Info, error) { return queryJSON[[]Info](IDList) }

// Specs lists the declared workers.
func Specs() ([]Spec, error) { return queryJSON[[]Spec](IDSpecs) }

// Post sends a JSON-encodable value to the worker's message handler.
func Post(id string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return bridge.CallDo(context.Background(), IDPost, func(e *wire.Encoder) { e.String(id); e.Raw(b) })
}

// PostBytes sends binary data to the worker.
func PostBytes(id string, data []byte) error {
	return bridge.CallDo(context.Background(), IDPost, func(e *wire.Encoder) { e.String(id); e.Bytes(data) })
}

// Message is a value a worker sent to the host: its JSON, or Bytes for binary.
type Message struct {
	Data  json.RawMessage
	Bytes []byte
}

// Notice is one worker event as host code sees it.
type Notice struct {
	Event
	Message *Message
}

// OnEvent calls fn for the events of one instance ("" for all) until cancel is
// called.
func OnEvent(id string, fn func(Notice)) (cancel func(), err error) {
	return bridge.SubscribeArgs(context.Background(), IDEvents, func(e *wire.Encoder) { e.String(id) }, func(d *wire.Decoder) {
		if n, ok := decodeNotice(d); ok {
			fn(n)
		}
	})
}

func decodeNotice(d *wire.Decoder) (Notice, bool) {
	var n Notice
	if d.BeginObject() != nil {
		return n, false
	}
	for d.More() {
		k, err := d.Key()
		if err != nil {
			return n, false
		}
		if k == "data" {
			m := &Message{}
			if d.Kind() == wire.KindBytes {
				m.Bytes, _ = d.Bytes()
			} else if raw, err := d.RawValue(); err == nil {
				m.Data = append(json.RawMessage(nil), raw...)
			}
			n.Message = m
			continue
		}
		raw, err := d.RawValue()
		if err != nil {
			return n, false
		}
		switch k {
		case "kind":
			_ = json.Unmarshal(raw, &n.Kind)
		case "id":
			_ = json.Unmarshal(raw, &n.ID)
		case "name":
			_ = json.Unmarshal(raw, &n.Name)
		case "pid":
			_ = json.Unmarshal(raw, &n.Pid)
		case "exitCode":
			_ = json.Unmarshal(raw, &n.ExitCode)
		case "error":
			_ = json.Unmarshal(raw, &n.Error)
		case "attempt":
			_ = json.Unmarshal(raw, &n.Attempt)
		case "delayMs":
			_ = json.Unmarshal(raw, &n.DelayMs)
		}
	}
	return n, true
}
