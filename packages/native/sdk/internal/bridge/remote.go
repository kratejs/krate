package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

// remote speaks the host call protocol (PROTOCOL.md) to the shell.
type remote struct {
	client *http.Client
	base   string
	tr     *http.Transport
	next   atomic.Int64
}

func newRemote(addr string) (*remote, error) {
	r := &remote{}
	switch {
	case strings.HasPrefix(addr, "unix:"):
		sock := strings.TrimPrefix(addr, "unix:")
		r.tr = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     30 * time.Second,
		}
		r.base = "http://krate-parent"
	case strings.HasPrefix(addr, "http://"):
		r.tr = &http.Transport{MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second}
		r.base = strings.TrimRight(addr, "/")
	default:
		return nil, fmt.Errorf("bridge: %s=%q must be unix:<path> or http://host:port", ParentEnv, addr)
	}
	r.client = &http.Client{Transport: r.tr}
	return r, nil
}

func (r *remote) close() { r.tr.CloseIdleConnections() }

func (r *remote) post(ctx krate.Ctx, id string, args func(*wire.Encoder)) (*http.Response, error) {
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(r.next.Add(1))
	e.Key("fn")
	e.String(id)
	e.Key("args")
	e.BeginArray()
	if args != nil {
		args(e)
	}
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	body, err := e.Finish()
	if err != nil {
		return nil, krate.Errorf("bad_args", "%s: %w", id, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+"/__krate/call", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-krate-frames")
	if w := ctx.Window(); w != "" {
		req.Header.Set("x-krate-window", w)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, krate.Errorf("unavailable", "the Krate shell is not reachable: %v", err)
	}
	return resp, nil
}

type result struct {
	ok     *wire.Decoder
	stream string
	err    error
}

func readResult(d *wire.Decoder) (res result, err error) {
	if err = d.BeginObject(); err != nil {
		return
	}
	for d.More() {
		var k string
		if k, err = d.Key(); err != nil {
			return
		}
		switch k {
		case "ok":
			res.ok, err = d.SubDecoder()
		case "stream":
			res.stream, err = d.String()
		case "err", "error":
			res.err, err = readError(d)
		default:
			err = d.Skip()
		}
		if err != nil {
			return
		}
	}
	err = d.EndObject()
	return
}

func readError(d *wire.Decoder) (error, error) {
	e := &krate.Error{}
	if err := d.BeginObject(); err != nil {
		return nil, err
	}
	for d.More() {
		k, err := d.Key()
		if err != nil {
			return nil, err
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
			return nil, err
		}
	}
	if e.Code == "" {
		e.Code = "internal"
	}
	return e, d.EndObject()
}

func (r *remote) call(ctx krate.Ctx, id string, args func(*wire.Encoder)) (*wire.Decoder, error) {
	resp, err := r.post(ctx, id, args)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	d, err := wire.ReadFrame(resp.Body)
	if err != nil {
		return nil, krate.Errorf("unavailable", "%s: reading the reply: %v", id, err)
	}
	res, err := readResult(d)
	if err != nil {
		return nil, krate.Errorf("bad_result", "%s: %w", id, err)
	}
	if res.err != nil {
		return nil, res.err
	}
	if res.ok == nil {
		return nil, krate.Errorf("bad_result", "%s: no result", id)
	}
	// The body is closed on return; copy the value out first.
	e := wire.NewEncoder()
	if err := wire.Copy(e, res.ok); err != nil {
		return nil, krate.Errorf("bad_result", "%s: %w", id, err)
	}
	b, err := e.Finish()
	if err != nil {
		return nil, err
	}
	return wire.NewDecoder(b)
}

// stream opens id and forwards values until ctx ends, reconnecting after a
// drop.
func (r *remote) stream(ctx krate.Ctx, id string, onValue func(*wire.Decoder)) {
	backoff := 100 * time.Millisecond
	for ctx.Err() == nil {
		if r.streamOnce(ctx, id, onValue) {
			backoff = 100 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > 2*time.Second {
			backoff = 2 * time.Second
		}
	}
}

func (r *remote) streamOnce(ctx krate.Ctx, id string, onValue func(*wire.Decoder)) (got bool) {
	resp, err := r.post(ctx, id, nil)
	if err != nil {
		return false
	}
	d, err := wire.ReadFrame(resp.Body)
	resp.Body.Close()
	if err != nil {
		return false
	}
	res, err := readResult(d)
	if err != nil || res.err != nil || res.stream == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+"/__krate/stream/"+url.PathEscape(res.stream), nil)
	if err != nil {
		return false
	}
	sresp, err := r.client.Do(req)
	if err != nil {
		return false
	}
	defer sresp.Body.Close()
	for {
		fd, err := wire.ReadFrame(sresp.Body)
		if err != nil {
			if err == io.EOF {
				return got
			}
			return got
		}
		if v, done := streamValue(fd); done {
			return got
		} else if v != nil {
			got = true
			onValue(v)
		}
	}
}

// streamValue decodes a {"v":..} | {"end":true} | {"err":..} frame.
func streamValue(d *wire.Decoder) (v *wire.Decoder, done bool) {
	if d.BeginObject() != nil {
		return nil, true
	}
	for d.More() {
		k, err := d.Key()
		if err != nil {
			return nil, true
		}
		switch k {
		case "v":
			sub, err := d.SubDecoder()
			if err != nil {
				return nil, true
			}
			e := wire.NewEncoder()
			if wire.Copy(e, sub) != nil {
				return nil, true
			}
			b, err := e.Finish()
			if err != nil {
				return nil, true
			}
			out, err := wire.NewDecoder(b)
			if err != nil {
				return nil, true
			}
			return out, false
		case "end", "err":
			return nil, true
		default:
			if d.Skip() != nil {
				return nil, true
			}
		}
	}
	return nil, false
}
