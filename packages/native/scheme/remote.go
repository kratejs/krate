package scheme

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

// Backoff bounds for reconnecting a forwarded signal.
const (
	remoteBackoffMin = 100 * time.Millisecond
	remoteBackoffMax = 2 * time.Second
)

// Remote forwards functions, streams and signals to another handler made by
// New (the "child"), reachable over HTTP. The child can be replaced while the
// app runs: SetBase, SetClient and Swap point the Remote at the new one, and
// forwarded signals reconnect on their own, so readers see the new child's
// current values without noticing the restart.
//
// Windows are preserved: the x-krate-window header of the calling window is
// sent on every forwarded request, and cancelling a call's context cancels
// the HTTP request, which makes the child cancel the call.
type Remote struct {
	mu     sync.RWMutex
	client *http.Client
	base   string
	epoch  chan struct{} // closed when the target changes

	regMu sync.Mutex
	regd  map[*Registry]map[string]bool
}

// NewRemote returns a Remote that talks to the handler at base (for example
// "http://127.0.0.1:4000", no trailing slash needed) through client. A nil
// client means calls fail with code "unavailable" until one is set.
func NewRemote(client *http.Client, base string) *Remote {
	return &Remote{
		client: client,
		base:   trimBase(base),
		epoch:  make(chan struct{}),
		regd:   map[*Registry]map[string]bool{},
	}
}

func trimBase(b string) string {
	for len(b) > 0 && b[len(b)-1] == '/' {
		b = b[:len(b)-1]
	}
	return b
}

// SetBase points the Remote at a new base URL. Open signal connections drop
// and reconnect at once.
func (r *Remote) SetBase(base string) {
	r.set(func() { r.base = trimBase(base) })
}

// SetClient replaces the HTTP client (for example one that dials a new unix
// socket). Open signal connections drop and reconnect at once.
func (r *Remote) SetClient(c *http.Client) {
	r.set(func() { r.client = c })
}

// SetTarget replaces both the client and the base URL in one step.
func (r *Remote) SetTarget(c *http.Client, base string) {
	r.set(func() { r.client, r.base = c, trimBase(base) })
}

func (r *Remote) set(update func()) {
	r.mu.Lock()
	update()
	old := r.epoch
	r.epoch = make(chan struct{})
	r.mu.Unlock()
	close(old)
}

func (r *Remote) target() (*http.Client, string, <-chan struct{}) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.client, r.base, r.epoch
}

// RegisterInto registers a forwarding Func, StreamFunc or SignalFunc on reg
// for each id. Ids this Remote already registered on reg are skipped, so it
// can be called again after the child gained new ids.
func (r *Remote) RegisterInto(reg *Registry, funcs, streams, signals []string) {
	r.regMu.Lock()
	seen := r.regd[reg]
	if seen == nil {
		seen = map[string]bool{}
		r.regd[reg] = seen
	}
	r.regMu.Unlock()
	add := func(kind, id string, f func()) {
		r.regMu.Lock()
		dup := seen[kind+"/"+id]
		seen[kind+"/"+id] = true
		r.regMu.Unlock()
		if !dup {
			f()
		}
	}
	for _, id := range funcs {
		add("func", id, func() { reg.Func(id, r.funcOf(id)) })
	}
	for _, id := range streams {
		add("stream", id, func() { reg.Stream(id, r.streamOf(id)) })
	}
	for _, id := range signals {
		add("signal", id, func() { reg.Signal(id, r.signalOf(id)) })
	}
}

func unavailable(err error) error {
	return krate.Errorf("unavailable", "go host is not reachable: %v", err)
}

// callFrame builds {"calls":[{"id":1,"fn":id,"args":<args>}]}.
func callFrame(id string, args *wire.Decoder) ([]byte, error) {
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String(id)
	e.Key("args")
	if err := wire.Copy(e, args); err != nil {
		return nil, err
	}
	if err := args.Finish(); err != nil {
		return nil, err
	}
	e.EndObject()
	e.EndArray()
	e.EndObject()
	return e.Finish()
}

func (r *Remote) newRequest(ctx krate.Ctx, method, path string, body []byte) (*http.Client, *http.Request, error) {
	client, base, _ := r.target()
	if client == nil || base == "" {
		return nil, nil, unavailable(errors.New("no go host is attached"))
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return nil, nil, krate.Errorf("internal", "forwarding: %w", err)
	}
	if w := ctx.Window(); w != "" {
		req.Header.Set(windowHdr, w)
	}
	return client, req, nil
}

// doErr turns a transport error into the error the caller returns.
func doErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return unavailable(err)
}

// openCall posts a one-call frame and returns the response with the single
// result frame decoded. The caller closes resp.Body.
func (r *Remote) openCall(ctx krate.Ctx, id string, args *wire.Decoder) (*http.Response, *wire.Decoder, error) {
	body, err := callFrame(id, args)
	if err != nil {
		return nil, nil, krate.Errorf("bad_request", "%s: arguments: %w", id, err)
	}
	client, req, err := r.newRequest(ctx, http.MethodPost, pathPrefix+"call", body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", framesType)
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, doErr(ctx, err)
	}
	d, err := wire.ReadFrame(resp.Body)
	if err != nil {
		resp.Body.Close()
		return nil, nil, doErr(ctx, err)
	}
	return resp, d, nil
}

// remoteError reads an {"code","message","data"} object.
func remoteError(d *wire.Decoder) error {
	var e krate.Error
	if err := d.BeginObject(); err != nil {
		return err
	}
	for d.More() {
		k, err := d.Key()
		if err != nil {
			return err
		}
		switch k {
		case "code":
			e.Code, err = d.String()
		case "message":
			e.Message, err = d.String()
		case "data":
			var raw []byte
			if raw, err = d.RawValue(); err == nil && string(raw) != "null" {
				var v any
				if json.Unmarshal(raw, &v) == nil {
					e.Data = v
				}
			}
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
	if e.Code == "" {
		e.Code = "internal"
	}
	return &e
}

// result is what a call's result frame held.
type callResult struct {
	ok     *wire.Decoder // positioned at the value
	stream string
	err    error
}

// readResult parses {"id":..,"ok":..|"err":..|"stream":..} or a top-level
// {"error":..} frame.
func readResult(d *wire.Decoder) (callResult, error) {
	var res callResult
	if err := d.BeginObject(); err != nil {
		return res, err
	}
	for d.More() {
		k, err := d.Key()
		if err != nil {
			return res, err
		}
		switch k {
		case "ok":
			res.ok, err = d.SubDecoder()
		case "stream":
			res.stream, err = d.String()
		case "err", "error":
			res.err = remoteError(d)
			if _, isK := res.err.(*krate.Error); !isK && res.err != nil {
				return res, res.err
			}
		default:
			err = d.Skip()
		}
		if err != nil {
			return res, err
		}
	}
	return res, d.EndObject()
}

func (r *Remote) funcOf(id string) Func {
	return func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		resp, d, err := r.openCall(ctx, id, args)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		res, err := readResult(d)
		if err != nil {
			return krate.Errorf("bad_result", "%s: %w", id, err)
		}
		if res.err != nil {
			return res.err
		}
		if res.ok == nil {
			return krate.Errorf("bad_result", "%s: the go host answered without a result", id)
		}
		if err := wire.Copy(out, res.ok); err != nil {
			return krate.Errorf("bad_result", "%s: %w", id, err)
		}
		return nil
	}
}

func (r *Remote) streamOf(id string) StreamFunc {
	return func(ctx krate.Ctx, args *wire.Decoder, emit func(write func(*wire.Encoder)) error) error {
		resp, d, err := r.openCall(ctx, id, args)
		if err != nil {
			return err
		}
		res, err := readResult(d)
		resp.Body.Close()
		if err != nil {
			return krate.Errorf("bad_result", "%s: %w", id, err)
		}
		if res.err != nil {
			return res.err
		}
		if res.stream == "" {
			return krate.Errorf("bad_result", "%s: the go host did not open a stream", id)
		}
		client, req, err := r.newRequest(ctx, http.MethodGet, streamPath+url.PathEscape(res.stream), nil)
		if err != nil {
			return err
		}
		sresp, err := client.Do(req)
		if err != nil {
			return doErr(ctx, err)
		}
		// Closing the body cancels the producer in the child.
		defer sresp.Body.Close()
		for {
			fd, err := wire.ReadFrame(sresp.Body)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return unavailable(fmt.Errorf("stream %s: %w", id, err))
			}
			done, err := forwardStreamFrame(fd, emit)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

// forwardStreamFrame handles one {"v":..} | {"end":true} | {"err":..} frame.
func forwardStreamFrame(d *wire.Decoder, emit func(write func(*wire.Encoder)) error) (done bool, err error) {
	if err := d.BeginObject(); err != nil {
		return false, krate.Errorf("bad_result", "stream frame: %w", err)
	}
	for d.More() {
		k, err := d.Key()
		if err != nil {
			return false, krate.Errorf("bad_result", "stream frame: %w", err)
		}
		switch k {
		case "v":
			v, err := d.SubDecoder()
			if err != nil {
				return false, krate.Errorf("bad_result", "stream frame: %w", err)
			}
			// Re-encoding validates the value before emit, so the write
			// callback cannot fail.
			frame, err := encodeOne(v)
			if err != nil {
				return false, err
			}
			if err := emit(func(e *wire.Encoder) { replay(e, frame) }); err != nil {
				return false, err
			}
			return false, nil
		case "end":
			return true, nil
		case "err":
			if err := remoteError(d); err != nil {
				return false, err
			}
			return true, nil
		default:
			if err := d.Skip(); err != nil {
				return false, krate.Errorf("bad_result", "stream frame: %w", err)
			}
		}
	}
	return false, nil
}

// encodeOne copies the next value of d into a fresh frame, so that it can be
// replayed any number of times.
func encodeOne(d *wire.Decoder) ([]byte, error) {
	e := wire.NewEncoder()
	if err := wire.Copy(e, d); err != nil {
		return nil, krate.Errorf("bad_result", "%w", err)
	}
	return e.Finish()
}

// replay writes the single value of a frame made by encodeOne to e.
func replay(e *wire.Encoder, frame []byte) {
	d, err := wire.NewDecoder(frame)
	if err == nil {
		err = wire.Copy(e, d)
	}
	if err != nil {
		e.Null()
	}
}

func (r *Remote) signalOf(id string) SignalFunc {
	return func(onChange func(write func(*wire.Encoder))) (cancel func()) {
		ctx, stop := context.WithCancel(context.Background())
		go r.watchSignal(ctx, id, onChange)
		return stop
	}
}

// watchSignal keeps one connection to the child's signal stream, reconnecting
// with backoff until ctx ends. A reconnect delivers the current value again.
func (r *Remote) watchSignal(ctx context.Context, id string, onChange func(write func(*wire.Encoder))) {
	backoff := remoteBackoffMin
	for ctx.Err() == nil {
		client, base, epoch := r.target()
		if r.readSignal(ctx, client, base, epoch, id, onChange) {
			backoff = remoteBackoffMin
		}
		// A changed target is retried at once; a lost connection backs off.
		wait := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			wait.Stop()
			return
		case <-epoch:
			wait.Stop()
			continue
		case <-wait.C:
		}
		if backoff *= 2; backoff > remoteBackoffMax {
			backoff = remoteBackoffMax
		}
	}
}

// readSignal runs one connection until it ends. It reports whether at least
// one value arrived.
func (r *Remote) readSignal(parent context.Context, client *http.Client, base string, epoch <-chan struct{}, id string, onChange func(write func(*wire.Encoder))) (gotValue bool) {
	if client == nil || base == "" {
		return false
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	go func() {
		select {
		case <-epoch:
			cancel()
		case <-ctx.Done():
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+pathPrefix+"signals?names="+url.QueryEscape(id), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	for {
		d, err := wire.ReadFrame(resp.Body)
		if err != nil {
			return gotValue
		}
		frame, ok := signalValue(d)
		if !ok {
			continue
		}
		gotValue = true
		onChange(func(e *wire.Encoder) { replay(e, frame) })
	}
}

// signalValue extracts v from {"sig":..,"v":..} as a replayable frame.
func signalValue(d *wire.Decoder) ([]byte, bool) {
	if d.BeginObject() != nil {
		return nil, false
	}
	var frame []byte
	for d.More() {
		k, err := d.Key()
		if err != nil {
			return nil, false
		}
		if k == "v" {
			v, err := d.SubDecoder()
			if err != nil {
				return nil, false
			}
			if frame, err = encodeOne(v); err != nil {
				return nil, false
			}
			continue
		}
		if d.Skip() != nil {
			return nil, false
		}
	}
	return frame, frame != nil
}
