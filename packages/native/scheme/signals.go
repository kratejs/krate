package scheme

import (
	"net/http"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

// sigQueue holds, per signal name, the newest pending write function. A
// reader slower than the signal sees the latest value, not every value.
type sigQueue struct {
	mu      sync.Mutex
	pending map[string]func(*wire.Encoder)
	order   []string
	wake    chan struct{}
}

func (q *sigQueue) push(name string, write func(*wire.Encoder)) {
	q.mu.Lock()
	if _, queued := q.pending[name]; !queued {
		q.order = append(q.order, name)
	}
	q.pending[name] = write
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *sigQueue) take() (names []string, writes []func(*wire.Encoder)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, n := range q.order {
		names = append(names, n)
		writes = append(writes, q.pending[n])
	}
	q.order = q.order[:0]
	clear(q.pending)
	return
}

func (s *server) sigErrFrame(name string, err error) []byte {
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("sig")
	e.String(name)
	e.Key("err")
	s.encodeError(e, err)
	e.EndObject()
	return e.Frame()
}

func (s *server) handleSignals(w http.ResponseWriter, r *http.Request) {
	var names []string
	seen := map[string]bool{}
	for _, n := range strings.Split(r.URL.Query().Get("names"), ",") {
		if n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	if len(names) == 0 || len(names) > maxSigNames {
		s.badRequest(w, krate.Errorf("bad_request", "names must list 1 to %d signals", maxSigNames))
		return
	}

	w.Header().Set("Content-Type", framesType)
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	flush := func() {
		if fl != nil {
			fl.Flush()
		}
	}
	flush()

	q := &sigQueue{pending: map[string]func(*wire.Encoder){}, wake: make(chan struct{}, 1)}
	var cancels []func()
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()
	for _, name := range names {
		sf := s.reg.signal(name)
		if sf == nil {
			w.Write(s.sigErrFrame(name, krate.Errorf("not_found", "no such signal %q", name)))
			flush()
			continue
		}
		s.observe(Event{Kind: "signal-open", Fn: name})
		var cancel func()
		err := protect(func() error {
			cancel = sf(func(write func(*wire.Encoder)) { q.push(name, write) })
			return nil
		})
		if err != nil {
			w.Write(s.sigErrFrame(name, err))
			flush()
			continue
		}
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
	}
	if len(cancels) == 0 {
		return
	}

	for {
		select {
		case <-q.wake:
		case <-r.Context().Done():
			return
		}
		ns, ws := q.take()
		for i, name := range ns {
			e := wire.NewEncoder()
			e.BeginObject()
			e.Key("sig")
			e.String(name)
			e.Key("v")
			err := protect(func() error { ws[i](e); return nil })
			e.EndObject()
			fr, ferr := e.Finish()
			if err == nil {
				err = ferr
			}
			if err != nil {
				fr = s.sigErrFrame(name, err)
			}
			if err == nil {
				s.observe(Event{Kind: "signal", Fn: name})
			}
			if _, werr := w.Write(fr); werr != nil {
				return
			}
		}
		flush()
	}
}
