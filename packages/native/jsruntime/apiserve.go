package jsruntime

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

//go:embed apiruntime.js
var apiRunnerBody string

//go:embed matcher.js
var matcherSource string

// MatcherJS is the Next-compatible middleware matcher compiler, shared with
// the Node/Bun sidecar scripts so every runtime scopes middleware identically.
var MatcherJS = matcherSource

// apiRunnerSource is the matcher library followed by the route runner.
var apiRunnerSource = matcherSource + "\n" + apiRunnerBody

var (
	errStalled = errors.New("handler is pending but nothing can resolve it (no timers or I/O outstanding)")
	// ErrStopStream is returned by an APISink to end a response early without
	// it being treated as a failure (for example a HEAD request).
	ErrStopStream = errors.New("stop streaming")
	errBudget     = errors.New("handler exceeded the request time budget (KRATE_API_TIMEOUT)")
)

// jsNumber converts a numeric result from the VM.
func jsNumber(v any) (float64, bool) {
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

// waitFor drives the microtask queue and the JS timer queue until ready()
// reports true, the budget elapses, or ctx is cancelled.
func (r *Runtime) waitFor(ctx context.Context, budget time.Duration, ready func() bool) error {
	deadline := time.Now().Add(budget)
	for {
		r.DrainJobs()
		if ready() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		v, err := r.Execute("__krate_timers_next()")
		if err != nil {
			return err
		}
		d, _ := jsNumber(v)
		if d < 0 {
			return errStalled
		}
		wait := time.Duration(d * float64(time.Millisecond))
		if time.Now().Add(wait).After(deadline) {
			return errBudget
		}
		if wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			}
		}
		if _, err := r.Execute("__krate_timers_run()"); err != nil {
			return err
		}
	}
}

type jsEnvelope struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Headers    [][2]string       `json:"headers"`
	Path       string            `json:"path,omitempty"`
	Params     map[string]string `json:"params,omitempty"`
	RemoteAddr string            `json:"remoteAddr,omitempty"`
	HasBody    bool              `json:"hasBody,omitempty"`
}

func envelopeHeaders(h map[string][]string) [][2]string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := [][2]string{}
	for _, k := range keys {
		for _, v := range h[k] {
			out = append(out, [2]string{strings.ToLower(k), v})
		}
	}
	return out
}

type jsHead struct {
	Status     int         `json:"status"`
	StatusText string      `json:"statusText"`
	Headers    [][2]string `json:"headers"`
	Body       string      `json:"body"`
	Text       *string     `json:"text"`
	Bin        bool        `json:"bin"`
	Stream     bool        `json:"stream"`
	Error      string      `json:"error"`
	Stack      string      `json:"stack"`
	Allow      []string    `json:"allow"`
}

// outBytes returns the response bytes the runner left for Go: through
// __krate_out when bin is set (binary transfer), else decoded from b64.
func (r *Runtime) outBytes(bin bool, b64 string) ([]byte, bool) {
	if !bin {
		d, err := base64.StdEncoding.DecodeString(b64)
		return d, err == nil
	}
	v, err := r.CallValue("__krate_out")
	if err != nil {
		return nil, false
	}
	defer v.Free()
	return valueBytes(r.vm, v)
}

func (r *Runtime) evalString(code string) (string, error) {
	v, err := r.Execute(code)
	if err != nil {
		return "", err
	}
	s, _ := v.(string)
	return s, nil
}

// run executes one request on this worker and streams the result into sink.
func (w *apiWorker) run(ctx context.Context, routeID string, req APIRequest, sink APISink) *APIFailure {
	rt := w.rt
	rt.curBody = req.Body
	defer func() {
		_, _ = rt.Execute("__krate_finish()")
		rt.curBody, rt.take = nil, nil
		if rt.headroom > 0 {
			// The body's garbage can sit in reference cycles (promise
			// chains) that QuickJS only frees in a cycle collection, which
			// may not come before the limit drops back to the base and the
			// next request fails out of memory.
			runGC(rt.vm)
			rt.setMemoryHeadroom(0)
		}
	}()

	if req.URL == "" {
		req.URL = "http://localhost" + req.Path
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	env := jsEnvelope{URL: req.URL, Method: req.Method, Headers: envelopeHeaders(req.Headers), Params: req.Params, RemoteAddr: req.RemoteAddr, HasBody: len(req.Body) > 0}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return &APIFailure{Status: 500, Message: err.Error()}
	}
	for name, val := range map[string]string{
		"__krate_route_id": routeID, "__krate_method": strings.ToUpper(req.Method), "__krate_req": string(envJSON),
	} {
		if err := rt.SetGlobal(name, val); err != nil {
			return &APIFailure{Status: 500, Message: err.Error()}
		}
	}

	v, err := rt.EvalBytecode(w.begin)
	if err != nil {
		return &APIFailure{Status: 500, Message: err.Error()}
	}
	if imm, _ := v.(string); imm != "" {
		var h jsHead
		if err := json.Unmarshal([]byte(imm), &h); err != nil {
			return &APIFailure{Status: 500, Message: "invalid handler response"}
		}
		if h.Status == 204 && len(h.Allow) > 0 {
			if err := sink.Start(204, [][2]string{{"allow", strings.Join(h.Allow, ", ")}}); err != nil {
				return &APIFailure{Status: 500, Message: err.Error(), Started: true}
			}
			return nil
		}
		return &APIFailure{Status: h.Status, Message: h.Error, Allow: h.Allow}
	}

	// Decoding a large base64 body inside the VM is slow; without binary
	// transfer give such requests proportionally more time.
	budget := req.timeout
	if budget <= 0 {
		budget = apiTimeout()
	}
	if !rt.bin {
		budget += time.Duration(len(req.Body)>>20) * 4 * time.Second
	}
	phase := func() bool {
		s, _ := rt.evalString("__krate_phase()")
		return s != "pending"
	}
	if err := rt.waitFor(ctx, budget, phase); err != nil {
		_, _ = rt.Execute("__krate_abort()")
		if errors.Is(err, context.Canceled) {
			return &APIFailure{Status: 499, Message: "client closed request"}
		}
		if errors.Is(err, errBudget) {
			return &APIFailure{Status: 504, Message: err.Error()}
		}
		return &APIFailure{Status: 500, Message: err.Error()}
	}
	raw, err := rt.evalString("__krate_head()")
	if err != nil {
		return &APIFailure{Status: 500, Message: err.Error()}
	}
	var head jsHead
	if err := json.Unmarshal([]byte(raw), &head); err != nil {
		return &APIFailure{Status: 500, Message: "invalid handler response"}
	}
	if head.Error != "" {
		return &APIFailure{Status: 500, Message: head.Error, Stack: head.Stack}
	}
	if err := sink.Start(head.Status, head.Headers); err != nil {
		return &APIFailure{Status: 500, Message: err.Error(), Started: true}
	}
	if head.Stream {
		return w.pumpStream(ctx, budget, sink)
	}
	if head.Text != nil || head.Body != "" || head.Bin {
		var data []byte
		if head.Text != nil {
			data = []byte(*head.Text)
		} else {
			d, ok := rt.outBytes(head.Bin, head.Body)
			if !ok {
				return &APIFailure{Status: 500, Message: "invalid response body", Started: true}
			}
			data = d
		}
		if len(data) == 0 {
			return nil
		}
		if err := sink.Write(data); err != nil && !errors.Is(err, ErrStopStream) {
			return &APIFailure{Status: 500, Message: err.Error(), Started: true}
		}
	}
	return nil
}

func (w *apiWorker) pumpStream(ctx context.Context, budget time.Duration, sink APISink) *APIFailure {
	rt := w.rt
	for {
		if _, err := rt.Execute("__krate_chunk_start()"); err != nil {
			return &APIFailure{Status: 500, Message: err.Error(), Started: true}
		}
		ready := func() bool {
			s, _ := rt.evalString("__krate_chunk()")
			return s != ""
		}
		if err := rt.waitFor(ctx, budget, ready); err != nil {
			_, _ = rt.Execute("__krate_abort()")
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return &APIFailure{Status: 500, Message: "response stream: " + err.Error(), Started: true}
		}
		raw, _ := rt.evalString("__krate_chunk()")
		var c struct {
			Done  bool   `json:"done"`
			Body  string `json:"body"`
			Bin   bool   `json:"bin"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			return &APIFailure{Status: 500, Message: "invalid stream chunk", Started: true}
		}
		if c.Error != "" {
			return &APIFailure{Status: 500, Message: "response stream: " + c.Error, Started: true}
		}
		if c.Done {
			return nil
		}
		data, ok := rt.outBytes(c.Bin, c.Body)
		if !ok {
			return &APIFailure{Status: 500, Message: "invalid stream chunk", Started: true}
		}
		if len(data) == 0 {
			continue
		}
		if err := sink.Write(data); err != nil {
			_, _ = rt.Execute("__krate_abort()")
			return nil
		}
	}
}
