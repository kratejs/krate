package hostjs

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
)

// Bridge operations, matching the constants in host.js.
const (
	opCall      = 0
	opStream    = 1
	opSignal    = 2
	opCancel    = 3
	bridgeValue = 0
	bridgeFail  = 1
	bridgeEnd   = 2
)

// bridgeRegistry is the Registry that __krateHost.call/stream/signal invoke:
// the last one passed to Register. Calls through it are in-process and not
// permission gated, because host code is trusted.
func (h *Host) bridgeRegistry() *scheme.Registry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bridge
}

func (h *Host) registerBridge() error {
	rt := h.rt
	if err := rt.RegisterFunc("__kh_bnew", func(args []any) (any, error) {
		id := h.nextID.Add(1)
		h.sinks[id] = &sink{}
		return id, nil
	}); err != nil {
		return err
	}
	if err := rt.RegisterFunc("__kh_bfree", func(args []any) (any, error) {
		id := argInt(args, 0)
		if s := h.sinks[id]; s != nil {
			if s.cancel != nil {
				s.cancel()
			}
			delete(h.sinks, id)
		}
		return nil, nil
	}); err != nil {
		return err
	}
	return rt.RegisterFunc("__kh_bcall", func(args []any) (any, error) {
		rid, op := argInt(args, 0), int(argInt(args, 1))
		var id, argsJSON string
		if len(args) > 2 {
			id, _ = args[2].(string)
		}
		if len(args) > 3 {
			argsJSON, _ = args[3].(string)
		}
		h.bridgeStart(rid, op, id, argsJSON)
		return nil, nil
	})
}

// deliver hands one outcome to the JS side of bridge request rid. It runs on
// the VM goroutine.
func (h *Host) deliver(rid int64, kind int, text string, blobs [][]byte) {
	s := h.sinks[rid]
	if s == nil {
		return
	}
	s.in = blobs
	if _, err := h.rt.Call("__kh_bdone", rid, kind, text); err != nil {
		h.logf("hostjs: delivering a bridge result: %v", err)
	}
	s.in = nil
}

func (h *Host) deliverFail(rid int64, err error) {
	h.post(func() { h.deliver(rid, bridgeFail, bridgeErrJSON(err), nil) })
}

func bridgeErrJSON(err error) string {
	var ke *krate.Error
	info := map[string]any{"code": "internal", "message": err.Error()}
	switch {
	case errors.As(err, &ke):
		info["code"], info["message"] = ke.Code, ke.Message
		if ke.Data != nil {
			info["data"] = ke.Data
		}
	case errors.Is(err, context.Canceled):
		info["code"] = "canceled"
	}
	b, merr := json.Marshal(info)
	if merr != nil {
		return errInfoJSON("internal", err.Error())
	}
	return string(b)
}

// bridgeStart runs on the VM goroutine and starts the work on another one.
func (h *Host) bridgeStart(rid int64, op int, id, argsJSON string) {
	s := h.sinks[rid]
	if s == nil {
		return
	}
	if op == opCancel {
		if s.cancel != nil {
			s.cancel()
		}
		delete(h.sinks, rid)
		return
	}
	reg := h.bridgeRegistry()
	if reg == nil {
		h.deliverFailLater(rid, krate.Errorf("unavailable", "no host registry is attached; __krateHost.call needs the app shell"))
		return
	}
	frame := buildFrame([]byte(argsJSON), s.out)
	s.out = nil
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	kctx := krate.WithWindow(ctx, "")
	switch op {
	case opCall:
		go func() {
			res, err := reg.Invoke(kctx, id, frame)
			if err != nil {
				h.deliverFail(rid, err)
				return
			}
			js, blobs, err := splitFrame(res)
			if err != nil {
				h.deliverFail(rid, err)
				return
			}
			h.post(func() { h.deliver(rid, bridgeValue, string(js), blobs) })
		}()
	case opStream:
		go func() {
			err := reg.InvokeStream(kctx, id, frame, func(fr []byte) error {
				js, blobs, err := splitFrame(fr)
				if err != nil {
					return err
				}
				if !h.post(func() { h.deliver(rid, bridgeValue, string(js), blobs) }) {
					return ErrClosed
				}
				return nil
			})
			if err != nil {
				if ctx.Err() == nil {
					h.deliverFail(rid, err)
				}
				return
			}
			h.post(func() { h.deliver(rid, bridgeEnd, "", nil) })
		}()
	case opSignal:
		stop, err := reg.Subscribe(id, func(fr []byte) {
			js, blobs, err := splitFrame(fr)
			if err != nil {
				return
			}
			h.post(func() { h.deliver(rid, bridgeValue, string(js), blobs) })
		})
		if err != nil {
			cancel()
			h.deliverFailLater(rid, err)
			return
		}
		s.cancel = func() { stop(); cancel() }
	default:
		cancel()
		h.deliverFailLater(rid, krate.Errorf("internal", "unknown bridge operation %d", op))
	}
}

// deliverFailLater delivers a failure after the current VM job, so the JS
// caller has registered its handlers by then.
func (h *Host) deliverFailLater(rid int64, err error) { h.deliverFail(rid, err) }
