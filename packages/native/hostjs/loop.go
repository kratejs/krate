package hostjs

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
)

// Outcome kinds, matching the constants in host.js.
const (
	kindValue = 0
	kindFail  = 1
	kindEnd   = 2
)

// outcome is one result delivered from JS to a waiting Go caller.
type outcome struct {
	kind  int
	json  []byte
	blobs [][]byte
	err   error
}

// sink receives the outcomes of one call, stream or signal subscription.
type sink struct {
	// in holds the argument blobs the JS side fetches by index.
	in [][]byte
	// out collects the blobs of the value being delivered.
	out [][]byte
	// ch receives outcomes for calls and streams.
	ch chan outcome
	// onValue, when set, receives signal values instead of ch.
	onValue func(outcome)
	// stream sinks stay registered after a value.
	stream bool
	// cancel stops an in-process bridge request (see bridge.go).
	cancel func()
}

func (h *Host) registerHosts() error {
	rt := h.rt
	if err := rt.RegisterBinaryFunc("__kh_blob", "nn", func(args []any) (any, error) {
		id, i := argInt(args, 0), argInt(args, 1)
		s := h.sinks[id]
		if s == nil || i < 0 || int(i) >= len(s.in) {
			return nil, fmt.Errorf("no argument blob %d for call %d", i, id)
		}
		return s.in[i], nil
	}); err != nil {
		return err
	}
	if err := rt.RegisterBinaryFunc("__kh_out_blob", "nb", func(args []any) (any, error) {
		s := h.sinks[argInt(args, 0)]
		if s != nil && len(args) > 1 {
			if b, _ := args[1].([]byte); b != nil {
				s.out = append(s.out, b)
			} else {
				s.out = append(s.out, []byte{})
			}
		}
		return "", nil
	}); err != nil {
		return err
	}
	// Host functions never return a Go error: the engine cannot take one
	// under the race detector's pointer checks.
	return rt.RegisterFunc("__kh_settle", func(args []any) (any, error) {
		id, kind := argInt(args, 0), int(argInt(args, 1))
		text := ""
		if len(args) > 2 {
			text, _ = args[2].(string)
		}
		h.settle(id, kind, text)
		return nil, nil
	})
}

func argInt(args []any, i int) int64 {
	if i >= len(args) {
		return -1
	}
	switch n := args[i].(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint32:
		return int64(n)
	case float64:
		return int64(n)
	}
	return -1
}

// settle delivers an outcome from JS. It runs on the VM goroutine.
func (h *Host) settle(id int64, kind int, text string) {
	s := h.sinks[id]
	if s == nil {
		return
	}
	oc := outcome{kind: kind}
	switch kind {
	case kindValue:
		oc.json, oc.blobs = []byte(text), s.out
	case kindFail:
		oc.err = h.errorFromInfo(text)
	}
	s.out = nil
	if s.onValue == nil && (kind != kindValue || !s.stream) {
		delete(h.sinks, id)
	}
	if s.onValue != nil {
		if kind == kindValue {
			s.onValue(oc)
		} else if oc.err != nil {
			h.logf("hostjs: signal value dropped: %v", oc.err)
		}
		return
	}
	select {
	case s.ch <- oc:
	default:
	}
}

// loop is the VM goroutine.
func (h *Host) loop() {
	defer close(h.done)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	var delay time.Duration = -1
	for {
		h.mu.Lock()
		jobs := h.jobs
		h.jobs = nil
		closed := h.closed
		h.mu.Unlock()
		if closed {
			h.shutdown(jobs)
			return
		}
		for _, j := range jobs {
			h.runJob(j)
		}
		delay = h.settleLoop()
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		h.mu.Lock()
		pending := len(h.jobs) > 0
		h.mu.Unlock()
		if pending {
			continue
		}
		if delay >= 0 {
			timer.Reset(delay)
		}
		select {
		case <-h.wake:
		case <-timer.C:
		}
	}
}

func (h *Host) runJob(j func()) {
	defer func() {
		if r := recover(); r != nil {
			h.logf("hostjs: job panicked: %v", r)
		}
	}()
	j()
	h.rt.DrainJobs()
}

// settleLoop drains microtasks, runs the timers that are due and returns how
// long until the next one, or -1 if none is scheduled.
func (h *Host) settleLoop() time.Duration {
	h.rt.DrainJobs()
	for range 1 << 16 {
		v, err := h.rt.EvalBytecode(h.nextBC)
		if err != nil {
			h.logf("hostjs: timers: %v", err)
			return -1
		}
		ms, ok := number(v)
		if !ok || ms < 0 {
			return -1
		}
		if ms > 0 {
			return time.Duration(ms * float64(time.Millisecond))
		}
		if _, err := h.rt.EvalBytecode(h.runBC); err != nil {
			h.logf("hostjs: timers: %v", err)
			return -1
		}
		h.rt.DrainJobs()
	}
	// A timer storm: yield to queued jobs and come back right away.
	return 0
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// shutdown fails everything pending and releases the VM.
func (h *Host) shutdown(jobs []func()) {
	// Jobs queued before Close still run, so a Load or call that raced with
	// Close gets an answer; their results are delivered below.
	for _, j := range jobs {
		h.runJob(j)
	}
	for id, s := range h.sinks {
		delete(h.sinks, id)
		if s.cancel != nil {
			s.cancel()
		}
		if s.onValue == nil {
			select {
			case s.ch <- outcome{kind: kindFail, err: ErrClosed}:
			default:
			}
		}
	}
	_ = h.rt.Close()
}

// errorFromInfo builds the *krate.Error for a JS failure described by the
// errInfo JSON in host.js, mapping the stack through the source map.
func (h *Host) errorFromInfo(text string) error {
	var info struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Stack   string          `json:"stack"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		return krate.Errorf("internal", "hostjs: undecodable error from host code: %s", text)
	}
	stack := h.mapStack(info.Stack)
	e := &krate.Error{Code: info.Code, Message: info.Message}
	unexpected := info.Code == "internal"
	if unexpected {
		if at := firstFrame(stack); at != "" {
			e.Message += " (at " + at + ")"
		}
	}
	switch {
	case len(info.Data) > 0 && string(info.Data) != "null":
		var data any
		if json.Unmarshal(info.Data, &data) == nil {
			e.Data = data
		}
	case unexpected || h.o.Dev:
		if stack != "" {
			e.Data = map[string]any{"stack": stack}
		}
	}
	if unexpected {
		h.logf("krate host: %s\n%s", e.Message, stack)
	}
	return e
}
