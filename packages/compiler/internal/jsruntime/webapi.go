package jsruntime

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"modernc.org/quickjs"
)

func strArg(args []any, i int) string {
	if i < len(args) {
		s, _ := args[i].(string)
		return s
	}
	return ""
}

// registerHost registers a host function once per VM; several features share
// some (__go_random).
func (r *Runtime) registerHost(name string, f func(args []any) (any, error)) error {
	if r.hosts[name] {
		return nil
	}
	if err := r.RegisterFunc(name, f); err != nil {
		return err
	}
	r.hosts[name] = true
	return nil
}

func bytesArg(args []any, i int) []byte {
	if i < len(args) {
		b, _ := args[i].([]byte)
		return b
	}
	return nil
}

func intArg(args []any, i int) (int, bool) {
	if i < len(args) {
		if n, ok := jsNumber(args[i]); ok {
			return int(n), true
		}
	}
	return 0, false
}

// registerBinaryHost registers a host function whose arguments or result may
// be bytes. kinds gives the kind of each argument: 'b' bytes (f receives a
// []byte), anything else a value converted as for registerHost. f returns
// []byte or a string.
//
// With binary transfer, byte arguments are typed arrays or ArrayBuffers and a
// []byte result becomes an ArrayBuffer; in the base64 fallback both are base64
// text. core.js (toHost, fromHost) accepts either. Failure is never a Go
// error, which aborts modernc QuickJS under checkptr: the function returns
// undefined and K.host throws the message __go_host_error returns.
func (r *Runtime) registerBinaryHost(name, kinds string, f func(args []any) (any, error)) error {
	if r.hosts[name] {
		return nil
	}
	if err := r.registerHost("__go_host_error", func(args []any) (any, error) {
		msg := r.hostErr
		r.hostErr = ""
		return msg, nil
	}); err != nil {
		return err
	}
	call := func(args []any) (res any, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				res, err = nil, fmt.Errorf("host function %s panicked: %v", name, rec)
			}
		}()
		return f(args)
	}
	var err error
	if r.bin {
		err = r.vm.RegisterFunc(name, func(vals ...quickjs.Value) quickjs.Value {
			args := make([]any, len(vals))
			for i, v := range vals {
				if i < len(kinds) && kinds[i] == 'b' {
					b, ok := valueBytes(r.vm, v)
					if !ok {
						return r.hostFail(fmt.Errorf("%s: argument %d is not binary data", name, i+1))
					}
					args[i] = b
					continue
				}
				a, err := v.Any()
				if err != nil {
					return r.hostFail(err)
				}
				args[i] = a
			}
			out, err := call(args)
			if err != nil {
				return r.hostFail(err)
			}
			switch x := out.(type) {
			case []byte:
				if v, ok := newArrayBuffer(r.vm, x); ok {
					return v
				}
				return r.hostFail(fmt.Errorf("%s: out of memory", name))
			case string:
				v, err := r.vm.NewString(x)
				if err != nil {
					return r.hostFail(err)
				}
				return v
			}
			return r.hostFail(fmt.Errorf("%s: unexpected result %T", name, out))
		}, false)
	} else {
		err = r.vm.RegisterHostFunc(name, func(args []any) (any, error) {
			for i := range args {
				if i < len(kinds) && kinds[i] == 'b' {
					b, derr := base64.StdEncoding.DecodeString(strArg(args, i))
					if derr != nil {
						return r.hostFailBase64(fmt.Errorf("%s: argument %d is not base64", name, i+1)), nil
					}
					args[i] = b
				}
			}
			out, err := call(args)
			if err != nil {
				return r.hostFailBase64(err), nil
			}
			if b, ok := out.([]byte); ok {
				return base64.StdEncoding.EncodeToString(b), nil
			}
			return out, nil
		})
	}
	if err != nil {
		return err
	}
	r.hosts[name] = true
	return nil
}

func (r *Runtime) hostFail(err error) quickjs.Value {
	r.hostErr = err.Error()
	return quickjs.UndefinedValue
}

func (r *Runtime) hostFailBase64(err error) any {
	r.hostErr = err.Error()
	return quickjs.Undefined{}
}

// registerTakeHost registers __go_take, which hands JS the bytes the previous
// host call left behind (a multipart body's parts, a fetch response body).
func (r *Runtime) registerTakeHost() error {
	return r.registerBinaryHost("__go_take", "", func(args []any) (any, error) {
		b := r.take
		r.take = nil
		if b == nil {
			b = []byte{}
		}
		return b, nil
	})
}

func (r *Runtime) registerConsoleHost() error {
	if err := r.registerHost("console_log", func(args []any) (any, error) { r.Log(args); return nil, nil }); err != nil {
		return err
	}
	if err := r.registerHost("console_error", func(args []any) (any, error) { r.Error(args); return nil, nil }); err != nil {
		return err
	}
	return r.registerHost("console_warn", func(args []any) (any, error) { r.Warn(args); return nil, nil })
}

func (r *Runtime) registerURLHost() error {
	return r.registerHost("__go_url_parse", func(args []any) (any, error) { return urlParseHost(args) })
}

func (r *Runtime) registerRandomHost() error {
	return r.registerBinaryHost("__go_random", "n", func(args []any) (any, error) {
		n, ok := intArg(args, 0)
		if !ok || n < 0 || n > 1<<16 {
			return nil, fmt.Errorf("invalid random length")
		}
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		return b, nil
	})
}

func (r *Runtime) registerCryptoHost() error {
	if err := r.registerRandomHost(); err != nil {
		return err
	}
	if err := r.registerBinaryHost("__go_digest", "sb", func(args []any) (any, error) {
		h, err := newHash(strArg(args, 0))
		if err != nil {
			return nil, err
		}
		hh := h()
		hh.Write(bytesArg(args, 1))
		return hh.Sum(nil), nil
	}); err != nil {
		return err
	}
	return r.registerBinaryHost("__go_hmac", "sbb", func(args []any) (any, error) {
		h, err := newHash(strArg(args, 0))
		if err != nil {
			return nil, err
		}
		m := hmac.New(h, bytesArg(args, 1))
		m.Write(bytesArg(args, 2))
		return m.Sum(nil), nil
	})
}

// registerFormDataHost registers multipart parsing, plus entropy for the
// boundaries of serialized multipart bodies.
func (r *Runtime) registerFormDataHost() error {
	if err := r.registerRandomHost(); err != nil {
		return err
	}
	if err := r.registerTakeHost(); err != nil {
		return err
	}
	return r.registerBinaryHost("__go_parse_multipart", "sb", func(args []any) (any, error) {
		return r.parseMultipart(strArg(args, 0), bytesArg(args, 1)), nil
	})
}

func (r *Runtime) registerFetchHost() error {
	if err := r.registerTakeHost(); err != nil {
		return err
	}
	return r.registerBinaryHost("__go_fetch", "sb", func(args []any) (any, error) {
		return r.goFetch(strArg(args, 0), bytesArg(args, 1))
	})
}

// registerBodyHost registers the functions the API and middleware runner
// (apiruntime.js) reads the in-flight request body through.
func (r *Runtime) registerBodyHost() error {
	if err := r.registerHost("__go_body_text", func(args []any) (any, error) {
		r.growBodyHeadroom(headroomText)
		b := bytes.TrimPrefix(r.curBody, []byte{0xEF, 0xBB, 0xBF})
		return strings.ToValidUTF8(string(b), "\uFFFD"), nil
	}); err != nil {
		return err
	}
	if err := r.registerBinaryHost("__go_body_bytes", "", func(args []any) (any, error) {
		r.growBodyHeadroom(headroomBytes)
		return r.curBody, nil
	}); err != nil {
		return err
	}
	if err := r.registerTakeHost(); err != nil {
		return err
	}
	return r.registerBinaryHost("__go_body_multipart", "s", func(args []any) (any, error) {
		r.growBodyHeadroom(headroomBytes)
		return r.parseMultipart(strArg(args, 0), r.curBody), nil
	})
}

func newHash(name string) (func() hash.Hash, error) {
	switch name {
	case "sha1":
		return sha1.New, nil
	case "sha256":
		return sha256.New, nil
	case "sha384":
		return sha512.New384, nil
	case "sha512":
		return sha512.New, nil
	}
	return nil, fmt.Errorf("unsupported hash %q", name)
}

type multipartPart struct {
	Name     string  `json:"name"`
	Filename *string `json:"filename,omitempty"`
	Type     string  `json:"type,omitempty"`
	// Off and Len locate the part's bytes in the data __go_take returns.
	Off int `json:"off"`
	Len int `json:"len"`
}

// parseMultipart parses a multipart/form-data body into a JSON description the
// JS FormData builder consumes; the part bytes, concatenated, wait for
// __go_take.
func (r *Runtime) parseMultipart(contentType string, body []byte) string {
	desc, data := parseMultipartBytes(contentType, body)
	r.take = data
	return desc
}

func parseMultipartBytes(contentType string, body []byte) (string, []byte) {
	fail := func(msg string) (string, []byte) {
		b, _ := json.Marshal(map[string]string{"error": msg})
		return string(b), nil
	}
	mt, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mt, "multipart/") || params["boundary"] == "" {
		return fail("missing multipart boundary")
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	parts := []multipartPart{}
	var all []byte
	for {
		p, err := mr.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(err.Error())
		}
		data, err := io.ReadAll(p)
		if err != nil {
			return fail(err.Error())
		}
		_, dp, derr := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
		if derr != nil {
			return fail("invalid content-disposition")
		}
		mp := multipartPart{Name: dp["name"], Off: len(all), Len: len(data)}
		all = append(all, data...)
		if fn, ok := dp["filename"]; ok {
			mp.Filename = &fn
			mp.Type = p.Header.Get("Content-Type")
			if mp.Type == "" {
				mp.Type = "application/octet-stream"
			}
		}
		parts = append(parts, mp)
	}
	out, _ := json.Marshal(map[string]any{"parts": parts})
	return string(out), all
}

type fetchRequest struct {
	URL      string      `json:"url"`
	Method   string      `json:"method"`
	Headers  [][2]string `json:"headers"`
	Cache    string      `json:"cache"`
	Next     any         `json:"next"`
	Redirect string      `json:"redirect"`
}

// fetchResult is the head of a fetch response as handed to JS; the body
// waits for __go_take.
type fetchResult struct {
	Status     int         `json:"status"`
	StatusText string      `json:"statusText"`
	Headers    [][2]string `json:"headers"`
	URL        string      `json:"url"`
	Redirected bool        `json:"redirected"`
}

// cachedFetch is a fetch response as stored in the data cache.
type cachedFetch struct {
	Head string `json:"head"`
	Body []byte `json:"body"`
}

func (r *Runtime) goFetch(payload string, body []byte) (any, error) {
	r.take = nil
	var fr fetchRequest
	if err := json.Unmarshal([]byte(payload), &fr); err != nil {
		return nil, fmt.Errorf("invalid fetch request: %w", err)
	}
	headers := make(map[string]string, len(fr.Headers))
	for _, h := range fr.Headers {
		headers[h[0]] = h[1]
	}
	opts := map[string]any{}
	if fr.Cache != "" {
		opts["cache"] = fr.Cache
	}
	if fr.Next != nil {
		opts["next"] = fr.Next
	}
	// Next-style data cache: { next: { revalidate, tags } } or
	// cache: "force-cache" caches successful GET/HEAD responses.
	ttl, tags := fetchCachePolicy(fr.Method, opts)
	cacheKey := ""
	if ttl != 0 && r.dataCache != nil {
		cacheKey = fetchCacheKey(fr.Method, fr.URL, headers)
		if hit, ok := r.dataCache.Get(cacheKey); ok {
			var c cachedFetch
			if json.Unmarshal([]byte(hit), &c) == nil {
				r.take = c.Body
				return c.Head, nil
			}
		}
	}
	req, err := http.NewRequest(fr.Method, fr.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for _, h := range fr.Headers {
		req.Header.Add(h[0], h[1])
	}
	client := &http.Client{Timeout: 30 * time.Second}
	redirected := false
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		switch fr.Redirect {
		case "manual":
			return http.ErrUseLastResponse
		case "error":
			return fmt.Errorf("unexpected redirect")
		}
		redirected = true
		if len(via) >= 20 {
			return fmt.Errorf("stopped after 20 redirects")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	var respHeaders [][2]string
	for k, vs := range resp.Header {
		for _, v := range vs {
			respHeaders = append(respHeaders, [2]string{k, v})
		}
	}
	text := strings.TrimSpace(strings.TrimPrefix(resp.Status, fmt.Sprint(resp.StatusCode)))
	head, err := json.Marshal(fetchResult{
		Status: resp.StatusCode, StatusText: text, Headers: respHeaders,
		URL: resp.Request.URL.String(), Redirected: redirected,
	})
	if err != nil {
		return nil, err
	}
	if cacheKey != "" && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if c, err := json.Marshal(cachedFetch{Head: string(head), Body: respBody}); err == nil {
			r.dataCache.Set(cacheKey, string(c), ttl, tags)
		}
	}
	r.take = respBody
	return string(head), nil
}
