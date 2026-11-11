package scheme

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

type outFrame struct {
	data  []byte
	final bool
	done  chan struct{}
}

type stream struct {
	id       string
	window   string
	ctx      context.Context
	cancel   context.CancelFunc
	opened   bool          // guarded by server.mu
	open     chan struct{} // closed when the reader opens the stream
	out      chan outFrame
	finished chan struct{} // closed when the producer goroutine exits
}

// send hands a frame to the reader and waits until it is written.
func (st *stream) send(data []byte, final bool) error {
	of := outFrame{data: data, final: final, done: make(chan struct{})}
	select {
	case st.out <- of:
	case <-st.ctx.Done():
		return st.ctx.Err()
	}
	select {
	case <-of.done:
		return nil
	case <-st.ctx.Done():
		return st.ctx.Err()
	}
}

// startStream registers a stream and parks its producer until the stream is
// opened. It returns the call's result frame.
func (s *server) startStream(window string, c call, f StreamFunc) []byte {
	id := "s" + strconv.FormatInt(s.nextID.Add(1), 10)
	ctx, cancel := context.WithCancel(context.Background())
	st := &stream{
		id: id, window: window, ctx: ctx, cancel: cancel,
		open: make(chan struct{}), out: make(chan outFrame), finished: make(chan struct{}),
	}
	s.mu.Lock()
	s.streams[id] = st
	s.mu.Unlock()

	timer := time.AfterFunc(s.expiry, func() {
		s.mu.Lock()
		expired := !st.opened
		s.mu.Unlock()
		if expired {
			cancel()
		}
	})

	go func() {
		defer func() {
			timer.Stop()
			cancel()
			s.mu.Lock()
			delete(s.streams, id)
			s.mu.Unlock()
			close(st.finished)
		}()
		select {
		case <-st.open:
		case <-ctx.Done():
			return
		}
		kctx := krate.WithWindow(ctx, window)
		emit := func(write func(*wire.Encoder)) error {
			e := wire.NewEncoder()
			e.BeginObject()
			e.Key("v")
			if err := protect(func() error { write(e); return nil }); err != nil {
				return err
			}
			e.EndObject()
			fr, err := e.Finish()
			if err != nil {
				return err
			}
			return st.send(fr, false)
		}
		err := protect(func() error { return f(kctx, c.args, emit) })
		if ctx.Err() != nil {
			return
		}
		e := wire.NewEncoder()
		e.BeginObject()
		if err == nil {
			e.Key("end")
			e.Bool(true)
		} else {
			e.Key("err")
			s.encodeError(e, err)
		}
		e.EndObject()
		st.send(e.Frame(), true)
	}()

	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("id")
	e.Int(c.id)
	e.Key("stream")
	e.String(id)
	e.EndObject()
	return e.Frame()
}

func (s *server) handleStream(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, streamPath)
	s.mu.Lock()
	st := s.streams[id]
	if st != nil && !st.opened {
		st.opened = true
	} else {
		st = nil
	}
	s.mu.Unlock()
	if st == nil {
		s.badStream(w)
		return
	}
	defer st.cancel()
	close(st.open)

	w.Header().Set("Content-Type", framesType)
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	if fl != nil {
		fl.Flush()
	}
	for {
		select {
		case of := <-st.out:
			_, err := w.Write(of.data)
			if fl != nil {
				fl.Flush()
			}
			close(of.done)
			if err != nil || of.final {
				return
			}
		case <-st.finished:
			// The producer stopped without a terminal frame: it was cancelled.
			e := wire.NewEncoder()
			e.BeginObject()
			e.Key("err")
			s.encodeError(e, context.Canceled)
			e.EndObject()
			e.WriteTo(w)
			return
		case <-r.Context().Done():
			return
		}
	}
}

func (s *server) badStream(w http.ResponseWriter) {
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("error")
	s.encodeError(e, krate.Errorf("not_found", "unknown or already opened stream"))
	e.EndObject()
	w.Header().Set("Content-Type", framesType)
	w.WriteHeader(http.StatusNotFound)
	e.WriteTo(w)
}
