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
	return r.registerHost("__go_random", func(args []any) (any, error) {
		n := 0
		if len(args) > 0 {
			switch v := args[0].(type) {
			case int:
				n = v
			case int64:
				n = int(v)
			case float64:
				n = int(v)
			}
		}
		if n < 0 || n > 1<<16 {
			return nil, fmt.Errorf("invalid random length")
		}
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		return base64.StdEncoding.EncodeToString(b), nil
	})
}

func (r *Runtime) registerCryptoHost() error {
	if err := r.registerRandomHost(); err != nil {
		return err
	}
	if err := r.registerHost("__go_digest", func(args []any) (any, error) {
		h, err := newHash(strArg(args, 0))
		if err != nil {
			return nil, err
		}
		data, err := base64.StdEncoding.DecodeString(strArg(args, 1))
		if err != nil {
			return nil, err
		}
		hh := h()
		hh.Write(data)
		return base64.StdEncoding.EncodeToString(hh.Sum(nil)), nil
	}); err != nil {
		return err
	}
	return r.registerHost("__go_hmac", func(args []any) (any, error) {
		h, err := newHash(strArg(args, 0))
		if err != nil {
			return nil, err
		}
		key, err := base64.StdEncoding.DecodeString(strArg(args, 1))
		if err != nil {
			return nil, err
		}
		data, err := base64.StdEncoding.DecodeString(strArg(args, 2))
		if err != nil {
			return nil, err
		}
		m := hmac.New(h, key)
		m.Write(data)
		return base64.StdEncoding.EncodeToString(m.Sum(nil)), nil
	})
}

// registerFormDataHost registers multipart parsing, plus entropy for the
// boundaries of serialized multipart bodies.
func (r *Runtime) registerFormDataHost() error {
	if err := r.registerRandomHost(); err != nil {
		return err
	}
	return r.registerHost("__go_parse_multipart", func(args []any) (any, error) {
		body, err := base64.StdEncoding.DecodeString(strArg(args, 1))
		if err != nil {
			return parseMultipartBytes(strArg(args, 0), nil), nil
		}
		return parseMultipartBytes(strArg(args, 0), body), nil
	})
}

func (r *Runtime) registerFetchHost() error {
	return r.registerHost("__go_fetch", func(args []any) (any, error) { return r.goFetch(strArg(args, 0)) })
}

// registerBodyHost registers the functions the API and middleware runner
// (apiruntime.js) reads the in-flight request body through.
func (r *Runtime) registerBodyHost() error {
	if err := r.registerHost("__go_body_text", func(args []any) (any, error) {
		b := bytes.TrimPrefix(r.curBody, []byte{0xEF, 0xBB, 0xBF})
		return strings.ToValidUTF8(string(b), "\uFFFD"), nil
	}); err != nil {
		return err
	}
	if err := r.registerHost("__go_body_b64", func(args []any) (any, error) {
		return base64.StdEncoding.EncodeToString(r.curBody), nil
	}); err != nil {
		return err
	}
	return r.registerHost("__go_body_multipart", func(args []any) (any, error) {
		return parseMultipartBytes(strArg(args, 0), r.curBody), nil
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
	Data     string  `json:"data"`
}

// parseMultipartBytes parses a multipart/form-data body into a JSON description the
// JS FormData builder consumes.
func parseMultipartBytes(contentType string, body []byte) string {
	fail := func(msg string) string {
		b, _ := json.Marshal(map[string]string{"error": msg})
		return string(b)
	}
	mt, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mt, "multipart/") || params["boundary"] == "" {
		return fail("missing multipart boundary")
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var parts []multipartPart
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
		mp := multipartPart{Name: dp["name"], Data: base64.StdEncoding.EncodeToString(data)}
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
	return string(out)
}

type fetchRequest struct {
	URL      string      `json:"url"`
	Method   string      `json:"method"`
	Headers  [][2]string `json:"headers"`
	Body     string      `json:"body"`
	Cache    string      `json:"cache"`
	Next     any         `json:"next"`
	Redirect string      `json:"redirect"`
}

// fetchResult is a fetch response as handed to JS (and stored in the data
// cache).
type fetchResult struct {
	Status     int         `json:"status"`
	StatusText string      `json:"statusText"`
	Headers    [][2]string `json:"headers"`
	Body       string      `json:"body"`
	URL        string      `json:"url"`
	Redirected bool        `json:"redirected"`
}

func (r *Runtime) goFetch(payload string) (any, error) {
	var fr fetchRequest
	if err := json.Unmarshal([]byte(payload), &fr); err != nil {
		return nil, fmt.Errorf("invalid fetch request: %w", err)
	}
	body, err := base64.StdEncoding.DecodeString(fr.Body)
	if err != nil {
		return nil, fmt.Errorf("invalid fetch body: %w", err)
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
			return hit, nil
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
	data, err := json.Marshal(fetchResult{
		Status: resp.StatusCode, StatusText: text, Headers: respHeaders,
		Body: base64.StdEncoding.EncodeToString(respBody), URL: resp.Request.URL.String(), Redirected: redirected,
	})
	if err != nil {
		return nil, err
	}
	if cacheKey != "" && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		r.dataCache.Set(cacheKey, string(data), ttl, tags)
	}
	return string(data), nil
}
