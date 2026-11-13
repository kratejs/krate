package scheme_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

// Task and Add stand for the user's Go code in src/lib/tasks.go.
type Task struct {
	ID      int64
	Title   string
	Done    bool
	Created time.Time
}

func Add(ctx krate.Ctx, title string) (Task, error) {
	if title == "" {
		return Task{}, krate.Errorf("invalid", "title is empty")
	}
	return Task{ID: 1, Title: title, Created: time.Date(2026, 11, 11, 7, 23, 15, 0, time.UTC)}, nil
}

// What follows is what the compiler generates for Add: a per-type encoder and
// a wrapper that decodes arguments, calls the function and encodes the result.
// There is no reflection and no generic JSON walking.

func encodeTask(e *wire.Encoder, t Task) {
	e.BeginObject()
	e.Key("ID")
	e.Int(t.ID)
	e.Key("Title")
	e.String(t.Title)
	e.Key("Done")
	e.Bool(t.Done)
	e.Key("Created")
	e.Time(t.Created)
	e.EndObject()
}

func wrapAdd(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
	if err := args.BeginArray(); err != nil {
		return krate.Errorf("bad_args", "tasks.Add: %w", err)
	}
	if !args.More() {
		return krate.Errorf("bad_args", "tasks.Add: missing argument 0 (title)")
	}
	title, err := args.String()
	if err != nil {
		return krate.Errorf("bad_args", "tasks.Add: argument 0 (title): %w", err)
	}
	if err := args.EndArray(); err != nil { // rejects extra arguments
		return krate.Errorf("bad_args", "tasks.Add: %w", err)
	}
	res, err := Add(ctx, title)
	if err != nil {
		return err // *krate.Error keeps its code; anything else becomes "internal"
	}
	encodeTask(out, res)
	return nil
}

func Example() {
	reg := scheme.NewRegistry()
	reg.Func("src/lib/tasks.Add", wrapAdd)

	// A host signal is registered the same way:
	//   reg.Signal("src/lib/tasks.Count", scheme.SignalOf(count, func(e *wire.Encoder, v int64) { e.Int(v) }))

	h := scheme.New(scheme.Options{Registry: reg})

	// The webview sends {"calls":[...]} as one frame.
	req := wire.NewEncoder()
	req.BeginObject()
	req.Key("calls")
	req.BeginArray()
	req.BeginObject()
	req.Key("id")
	req.Int(1)
	req.Key("fn")
	req.String("src/lib/tasks.Add")
	req.Key("args")
	req.BeginArray()
	req.String("Buy milk")
	req.EndArray()
	req.EndObject()
	req.EndArray()
	req.EndObject()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/__krate/call", bytes.NewReader(req.Frame()))
	r.Header.Set("x-krate-window", "main")
	h.ServeHTTP(rec, r)

	// The reply is one frame per call; here the JSON section of the first.
	d, _ := wire.ReadFrame(rec.Body)
	raw, _ := d.RawValue()
	fmt.Println(string(raw))
	// Output: {"id":1,"ok":{"ID":1,"Title":"Buy milk","Done":false,"Created":{"$d":"2026-11-11T07:23:15.000Z"}}}
}
