package darwin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestHeadersSentOnce(t *testing.T) {
	w, s, _ := newTestWriter()
	w.Header().Set("X-A", "1")
	w.WriteHeader(201)
	w.WriteHeader(404)
	if _, err := w.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	w.Header().Set("X-Late", "1")
	w.WriteHeader(500)
	if _, err := w.Write([]byte("b")); err != nil {
		t.Fatal(err)
	}
	w.finish()
	if got := s.kinds(); got != "response,data,data,finish" {
		t.Fatalf("events = %s", got)
	}
	r := s.ev[0]
	if r.status != 201 || r.header.Get("X-A") != "1" || r.header.Get("X-Late") != "" {
		t.Fatalf("response = %+v", r)
	}
}

func TestDefaultStatusAndSniff(t *testing.T) {
	w, s, _ := newTestWriter()
	if _, err := w.Write([]byte("<!doctype html><html></html>")); err != nil {
		t.Fatal(err)
	}
	w.finish()
	r := s.ev[0]
	if r.status != 200 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") {
		t.Fatalf("response = %+v", r)
	}
}

func TestExplicitContentTypeKept(t *testing.T) {
	w, s, _ := newTestWriter()
	w.Header().Set("Content-Type", "application/x-krate-frames")
	if _, err := w.Write([]byte("<html>")); err != nil {
		t.Fatal(err)
	}
	if got := s.ev[0].header.Get("Content-Type"); got != "application/x-krate-frames" {
		t.Fatalf("content type = %q", got)
	}
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

func TestFinishWithoutWriteSendsResponse(t *testing.T) {
	w, s, _ := newTestWriter()
	w.WriteHeader(204)
	w.finish()
	w.finish()
	if got := s.kinds(); got != "response,finish" || s.ev[0].status != 204 {
		t.Fatalf("events = %s %+v", got, s.ev)
	}
}

func TestInformationalIgnored(t *testing.T) {
	w, s, _ := newTestWriter()
	w.WriteHeader(103)
	w.WriteHeader(418)
	w.finish()
	if s.ev[0].status != 418 {
		t.Fatalf("status = %d", s.ev[0].status)
	}
}

func TestInvalidStatusBecomes500(t *testing.T) {
	w, s, _ := newTestWriter()
	w.WriteHeader(42)
	w.finish()
	if s.ev[0].status != 500 {
		t.Fatalf("status = %d", s.ev[0].status)
	}
}

func TestWriteAfterFinish(t *testing.T) {
	w, s, _ := newTestWriter()
	w.finish()
	if _, err := w.Write([]byte("x")); !errors.Is(err, errWriteAfterFinish) {
		t.Fatalf("err = %v", err)
	}
	w.Flush()
	w.failWith(errors.New("late"))
	if got := s.kinds(); got != "response,finish" {
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

func TestStopBeforeAnyWrite(t *testing.T) {
	w, s, _ := newTestWriter()
	w.stop()
	w.finish()
	if got := s.kinds(); got != "" {
		t.Fatalf("events = %s", got)
	}
}

func TestDataIsCopied(t *testing.T) {
	w, s, _ := newTestWriter()
	buf := []byte("abc")
	if _, err := w.Write(buf); err != nil {
		t.Fatal(err)
	}
	buf[0] = 'z'
	if s.ev[1].data != "abc" {
		t.Fatalf("data = %q", s.ev[1].data)
	}
}

func req(t *testing.T) *http.Request {
	t.Helper()
	r, err := newHTTPRequest(context.Background(), "GET", "krate://app/x", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestServeHandlerPanicBeforeHeader(t *testing.T) {
	w, s, _ := newTestWriter()
	serve(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }), req(t), w)
	if got := s.kinds(); got != "response,data,finish" || s.ev[0].status != 500 {
		t.Fatalf("events = %s %+v", got, s.ev)
	}
}

func TestServeHandlerPanicAfterHeader(t *testing.T) {
	w, s, _ := newTestWriter()
	serve(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("part")); err != nil {
			t.Error(err)
		}
		panic("boom")
	}), req(t), w)
	if got := s.kinds(); got != "response,data,fail" {
		t.Fatalf("events = %s", got)
	}
}

func TestServeAbortHandler(t *testing.T) {
	w, s, _ := newTestWriter()
	serve(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }), req(t), w)
	if got := s.kinds(); got != "fail" || !errors.Is(s.ev[0].err, errAborted) {
		t.Fatalf("events = %s", got)
	}
}

func TestServeCancelsContextWhenDone(t *testing.T) {
	w, _, ctx := newTestWriter()
	serve(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), req(t), w)
	if ctx.Err() == nil {
		t.Fatal("context still live")
	}
}

func TestServeStoppedHandlerSeesCancel(t *testing.T) {
	w, s, ctx := newTestWriter()
	r := req(t).WithContext(ctx)
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		serve(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			if _, err := rw.Write([]byte("a")); err != nil {
				t.Error(err)
			}
			close(started)
			<-r.Context().Done()
			if _, err := rw.Write([]byte("b")); err == nil {
				t.Error("write after stop succeeded")
			}
		}), r, w)
		close(done)
	}()
	<-started
	w.stop()
	<-done
	if got := s.kinds(); got != "response,data" {
		t.Fatalf("events = %s", got)
	}
}

func TestWorksWithHTTPTestHandlerStyle(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/echo" || r.Host != "app" || r.Body == nil {
			t.Errorf("bad request: %+v", r)
		}
		buf := make([]byte, 16)
		n, _ := r.Body.Read(buf)
		if _, err := w.Write(buf[:n]); err != nil {
			t.Error(err)
		}
	})
	r, err := newHTTPRequest(context.Background(), "POST", "krate://app/echo?q=1", map[string]string{"x-test": "y"}, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Header.Get("X-Test") != "y" || r.ContentLength != 5 || r.URL.RawQuery != "q=1" || r.RequestURI != "/echo?q=1" {
		t.Fatalf("request = %+v", r)
	}
	w, s, _ := newTestWriter()
	serve(mux, r, w)
	if got := s.kinds(); got != "response,data,finish" || s.ev[1].data != "hello" {
		t.Fatalf("events = %s %+v", got, s.ev)
	}
	_ = httptest.NewRecorder
}
