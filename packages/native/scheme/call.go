package scheme

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

type call struct {
	id   int64
	fn   string
	args *wire.Decoder
}

type panicError struct {
	val   any
	stack []byte
}

func (p *panicError) Error() string { return fmt.Sprintf("panic: %v", p.val) }

// protect runs f and turns a panic into a *panicError.
func protect(f func() error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = &panicError{val: v, stack: debug.Stack()}
		}
	}()
	return f()
}

func parseCalls(d *wire.Decoder) ([]call, error) {
	if err := d.BeginObject(); err != nil {
		return nil, err
	}
	var calls []call
	seen := map[int64]bool{}
	for d.More() {
		k, err := d.Key()
		if err != nil {
			return nil, err
		}
		if k != "calls" {
			if err := d.Skip(); err != nil {
				return nil, err
			}
			continue
		}
		if err := d.BeginArray(); err != nil {
			return nil, err
		}
		for d.More() {
			c, err := parseCall(d)
			if err != nil {
				return nil, err
			}
			if seen[c.id] {
				return nil, fmt.Errorf("duplicate call id %d", c.id)
			}
			seen[c.id] = true
			calls = append(calls, c)
		}
		if err := d.EndArray(); err != nil {
			return nil, err
		}
	}
	if err := d.EndObject(); err != nil {
		return nil, err
	}
	if err := d.Finish(); err != nil {
		return nil, err
	}
	return calls, nil
}

func parseCall(d *wire.Decoder) (call, error) {
	var c call
	var hasID, hasFn bool
	if err := d.BeginObject(); err != nil {
		return c, err
	}
	for d.More() {
		k, err := d.Key()
		if err != nil {
			return c, err
		}
		switch k {
		case "id":
			c.id, err = d.Int()
			hasID = true
		case "fn":
			c.fn, err = d.String()
			hasFn = true
		case "args":
			c.args, err = d.SubDecoder()
		default:
			err = d.Skip()
		}
		if err != nil {
			return c, err
		}
	}
	if err := d.EndObject(); err != nil {
		return c, err
	}
	if !hasID || !hasFn {
		return c, errors.New("a call needs id and fn")
	}
	if c.args == nil {
		c.args, _ = wire.NewDecoder([]byte(emptyArgs))
	}
	return c, nil
}

func (s *server) badRequest(w http.ResponseWriter, err error) {
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("error")
	e.BeginObject()
	e.Key("code")
	e.String("bad_request")
	e.Key("message")
	e.String(err.Error())
	e.EndObject()
	e.EndObject()
	w.Header().Set("Content-Type", framesType)
	w.WriteHeader(http.StatusBadRequest)
	e.WriteTo(w)
}

func (s *server) handleCall(w http.ResponseWriter, r *http.Request) {
	d, err := wire.ReadFrame(r.Body)
	if err != nil {
		s.badRequest(w, err)
		return
	}
	calls, err := parseCalls(d)
	if err != nil {
		s.badRequest(w, err)
		return
	}
	window := r.Header.Get(windowHdr)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	results := make(chan []byte, len(calls))

	for _, c := range calls {
		fn, sfn := s.reg.lookup(c.fn)
		switch {
		case fn != nil:
			cctx, ccancel := context.WithCancel(ctx)
			key := callKey{window, c.id}
			s.mu.Lock()
			s.calls[key] = ccancel
			s.mu.Unlock()
			go func() {
				defer func() {
					ccancel()
					s.mu.Lock()
					delete(s.calls, key)
					s.mu.Unlock()
				}()
				results <- s.runFunc(krate.WithWindow(cctx, window), c, fn)
			}()
		case sfn != nil:
			results <- s.startStream(window, c, sfn)
		default:
			nf := krate.Errorf("not_found", "no such function %q", c.fn)
			s.observe(Event{Kind: "call", ID: c.id, Fn: c.fn, Window: window, Code: "not_found", Message: nf.Error()})
			results <- s.errFrame(c.id, nf)
		}
	}

	w.Header().Set("Content-Type", framesType)
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	if fl != nil {
		fl.Flush()
	}
	for range calls {
		select {
		case fr := <-results:
			if _, err := w.Write(fr); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *server) runFunc(ctx krate.Ctx, c call, fn Func) []byte {
	start := time.Now()
	fr, err := s.runFuncErr(ctx, c, fn)
	ev := Event{Kind: "call", ID: c.id, Fn: c.fn, Window: ctx.Window(), Dur: time.Since(start)}
	if err != nil {
		ev.Code, ev.Message = errCodeMsg(err)
	}
	s.observe(ev)
	return fr
}

func (s *server) runFuncErr(ctx krate.Ctx, c call, fn Func) ([]byte, error) {
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		return s.errFrame(c.id, ctx.Err()), ctx.Err()
	}
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("id")
	e.Int(c.id)
	e.Key("ok")
	err := protect(func() error { return fn(ctx, c.args, e) })
	if err == nil {
		e.EndObject()
		fr, ferr := e.Finish()
		if ferr == nil {
			return fr, nil
		}
		err = krate.Errorf("internal", "%s: encoding the result: %w", c.fn, ferr)
	}
	return s.errFrame(c.id, err), err
}

// errFrame builds {"id":n,"err":{...}}.
func (s *server) errFrame(id int64, err error) []byte {
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("id")
	e.Int(id)
	e.Key("err")
	s.encodeError(e, err)
	e.EndObject()
	return e.Frame()
}

// encodeError writes the {"code","message","data"} object for err.
func (s *server) encodeError(e *wire.Encoder, err error) {
	code, msg, data := "internal", err.Error(), any(nil)
	var pe *panicError
	var ke *krate.Error
	switch {
	case errors.As(err, &pe):
		code, msg = "panic", "panic in host function"
		if s.o.Dev {
			msg = fmt.Sprintf("panic: %v\n\n%s", pe.val, pe.stack)
		}
	case errors.As(err, &ke):
		code, msg, data = ke.Code, ke.Message, ke.Data
	case errors.Is(err, context.Canceled):
		code = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		code = "deadline_exceeded"
	}
	e.BeginObject()
	e.Key("code")
	e.String(code)
	e.Key("message")
	e.String(msg)
	e.Key("data")
	if data == nil {
		e.Null()
	} else if raw, merr := json.Marshal(data); merr == nil {
		e.Raw(raw)
	} else {
		e.Null()
	}
	e.EndObject()
}

func (s *server) handleCancel(w http.ResponseWriter, r *http.Request) {
	d, err := wire.ReadFrame(r.Body)
	if err != nil {
		s.badRequest(w, err)
		return
	}
	var ids []int64
	var streams []string
	err = func() error {
		if err := d.BeginObject(); err != nil {
			return err
		}
		for d.More() {
			k, err := d.Key()
			if err != nil {
				return err
			}
			switch k {
			case "ids":
				if err := d.BeginArray(); err != nil {
					return err
				}
				for d.More() {
					id, err := d.Int()
					if err != nil {
						return err
					}
					ids = append(ids, id)
				}
				err = d.EndArray()
			case "streams":
				if err := d.BeginArray(); err != nil {
					return err
				}
				for d.More() {
					id, err := d.String()
					if err != nil {
						return err
					}
					streams = append(streams, id)
				}
				err = d.EndArray()
			default:
				err = d.Skip()
			}
			if err != nil {
				return err
			}
		}
		if err := d.EndObject(); err != nil {
			return err
		}
		return d.Finish()
	}()
	if err != nil {
		s.badRequest(w, err)
		return
	}
	window := r.Header.Get(windowHdr)
	s.mu.Lock()
	var cancels []context.CancelFunc
	for _, id := range ids {
		if c, ok := s.calls[callKey{window, id}]; ok {
			cancels = append(cancels, c)
		}
	}
	for _, id := range streams {
		if st, ok := s.streams[id]; ok && st.window == window {
			cancels = append(cancels, st.cancel)
		}
	}
	s.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	w.WriteHeader(http.StatusNoContent)
}
