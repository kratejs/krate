package darwin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type event struct {
	kind   string
	status int
	header http.Header
	data   string
	err    error
}

type fakeSink struct {
	mu sync.Mutex
	ev []event
}

func (f *fakeSink) add(e event) {
	f.mu.Lock()
	f.ev = append(f.ev, e)
	f.mu.Unlock()
}
func (f *fakeSink) receiveResponse(status int, h http.Header) {
	f.add(event{kind: "response", status: status, header: h})
}
func (f *fakeSink) receiveData(p []byte) { f.add(event{kind: "data", data: string(p)}) }
func (f *fakeSink) finish()              { f.add(event{kind: "finish"}) }
func (f *fakeSink) fail(err error)       { f.add(event{kind: "fail", err: err}) }

func (f *fakeSink) kinds() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var parts []string
	for _, e := range f.ev {
		parts = append(parts, e.kind)
	}
	return strings.Join(parts, ",")
}

func newTestWriter() (*taskWriter, *fakeSink, context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &fakeSink{}
	return newTaskWriter(s, cancel), s, ctx
}

func TestFlushSendsHeadOnly(t *testing.T) {
	w, s, _ := newTestWriter()
	w.Flush()
	w.Flush()
	if got := s.kinds(); got != "response" {
		t.Fatalf("events = %s", got)
	}
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	w.finish()
	if got := s.kinds(); got != "response,data,finish" {
		t.Fatalf("events = %s", got)
	}
}

func TestStopBeforeFinish(t *testing.T) {
	w, s, ctx := newTestWriter()
	if _, err := w.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	w.stop()
	if ctx.Err() == nil {
		t.Fatal("context not cancelled")
	}
	if _, err := w.Write([]byte("b")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	w.Flush()
	w.finish()
	w.failWith(errors.New("x"))
	if got := s.kinds(); got != "response,data" {
		t.Fatalf("events = %s", got)
	}
}
