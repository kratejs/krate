package hostjs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

// listing is what a loaded bundle registered.
type listing struct {
	Funcs   []string `json:"funcs"`
	Streams []string `json:"streams"`
	Signals []string `json:"signals"`
}

func parseListing(s string) (listing, error) {
	var l listing
	if err := json.Unmarshal([]byte(s), &l); err != nil {
		return l, fmt.Errorf("hostjs: reading the registry: %w", err)
	}
	return l, nil
}

// Register adds every function, stream and signal the loaded bundle registered
// to r. Call it after Load, and again after Reload to add ids the new bundle
// introduced; ids already registered with r are skipped, because they resolve
// the JS function by id on every call.
func (h *Host) Register(r *scheme.Registry) {
	h.mu.Lock()
	l := h.listed
	seen := h.regd[r]
	if seen == nil {
		seen = map[string]bool{}
		h.regd[r] = seen
	}
	h.mu.Unlock()
	add := func(ids []string, kind string, reg func(id string)) {
		for _, id := range ids {
			if seen[kind+"/"+id] {
				continue
			}
			seen[kind+"/"+id] = true
			reg(id)
		}
	}
	add(l.Funcs, "func", func(id string) { r.Func(id, h.funcOf(id)) })
	add(l.Streams, "stream", func(id string) { r.Stream(id, h.streamOf(id)) })
	add(l.Signals, "signal", func(id string) { r.Signal(id, h.signalOf(id)) })
}

// start posts the invocation of fn id for sink s and returns the call id.
func (h *Host) start(ctx krate.Ctx, id string, s *sink, argsJSON string) (int64, error) {
	cid := h.nextID.Add(1)
	window := ctx.Window()
	ok := h.post(func() {
		h.sinks[cid] = s
		if _, err := h.rt.Call("__kh_invoke", cid, id, argsJSON, window); err != nil {
			h.settle(cid, kindFail, errInfoJSON("internal", err.Error()))
		}
	})
	if !ok {
		return 0, ErrClosed
	}
	return cid, nil
}

func errInfoJSON(code, msg string) string {
	b, _ := json.Marshal(map[string]string{"code": code, "message": msg})
	return string(b)
}

// cancel tells the JS side to abort call cid and forgets its sink.
func (h *Host) cancel(cid int64) {
	h.post(func() {
		delete(h.sinks, cid)
		if _, err := h.rt.Call("__kh_cancel", cid); err != nil {
			h.logf("hostjs: cancelling call %d: %v", cid, err)
		}
	})
}

func (h *Host) wait(ctx krate.Ctx, cid int64, s *sink) (outcome, error) {
	select {
	case oc := <-s.ch:
		return oc, oc.err
	case <-ctx.Done():
		h.cancel(cid)
		return outcome{}, ctx.Err()
	case <-h.done:
		return outcome{}, ErrClosed
	}
}

func decodeResult(oc outcome) (*wire.Decoder, error) {
	return wire.NewDecoder(buildFrame(oc.json, oc.blobs))
}

func (h *Host) funcOf(id string) scheme.Func {
	return func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		argsJSON, blobs, err := packArgs(args)
		if err != nil {
			return krate.Errorf("bad_request", "%s: arguments: %w", id, err)
		}
		s := &sink{in: blobs, ch: make(chan outcome, 1)}
		cid, err := h.start(ctx, id, s, argsJSON)
		if err != nil {
			return err
		}
		oc, err := h.wait(ctx, cid, s)
		if err != nil {
			return err
		}
		d, err := decodeResult(oc)
		if err != nil {
			return krate.Errorf("bad_result", "%s: %w", id, err)
		}
		if err := copyValue(d, out); err != nil {
			return krate.Errorf("bad_result", "%s: %w", id, err)
		}
		return nil
	}
}

func (h *Host) streamOf(id string) scheme.StreamFunc {
	return func(ctx krate.Ctx, args *wire.Decoder, emit func(write func(*wire.Encoder)) error) error {
		argsJSON, blobs, err := packArgs(args)
		if err != nil {
			return krate.Errorf("bad_request", "%s: arguments: %w", id, err)
		}
		s := &sink{in: blobs, ch: make(chan outcome, 1), stream: true}
		cid, err := h.start(ctx, id, s, argsJSON)
		if err != nil {
			return err
		}
		// Closing the stream, however it ends, aborts the JS call and calls the
		// iterator's return().
		defer h.cancel(cid)
		for {
			if !h.post(func() {
				if _, err := h.rt.Call("__kh_pull", cid); err != nil {
					h.settle(cid, kindFail, errInfoJSON("internal", err.Error()))
				}
			}) {
				return ErrClosed
			}
			oc, err := h.wait(ctx, cid, s)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return ctx.Err()
				}
				return err
			}
			if oc.kind == kindEnd {
				return nil
			}
			d, err := decodeResult(oc)
			if err != nil {
				return krate.Errorf("bad_result", "%s: %w", id, err)
			}
			// Validate now so the write callback cannot fail.
			if err := copyValue(d, wire.NewEncoder()); err != nil {
				return krate.Errorf("bad_result", "%s: %w", id, err)
			}
			if err := emit(func(e *wire.Encoder) {
				d, _ := decodeResult(oc)
				_ = copyValue(d, e)
			}); err != nil {
				return err
			}
		}
	}
}

func (h *Host) signalOf(id string) scheme.SignalFunc {
	return func(onChange func(write func(*wire.Encoder))) (cancel func()) {
		sid := h.nextID.Add(1)
		s := &sink{onValue: func(oc outcome) {
			onChange(func(e *wire.Encoder) {
				d, err := decodeResult(oc)
				if err == nil {
					err = copyValue(d, e)
				}
				if err != nil {
					e.Null()
				}
			})
		}}
		if !h.post(func() {
			h.sinks[sid] = s
			v, err := h.rt.Call("__kh_subscribe", sid, id)
			if msg, _ := v.(string); err == nil && msg != "" {
				err = errors.New(msg)
			}
			if err != nil {
				h.logf("hostjs: subscribing to signal %s: %v", id, err)
				delete(h.sinks, sid)
			}
		}) {
			return func() {}
		}
		return func() {
			h.post(func() {
				delete(h.sinks, sid)
				if _, err := h.rt.Call("__kh_unsubscribe", sid); err != nil {
					h.logf("hostjs: unsubscribing from signal %s: %v", id, err)
				}
			})
		}
	}
}
