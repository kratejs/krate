package build

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
)

// pluginServe plugins map from the configured community plugins. It is resolved
// from cfg inside wirePluginServeHandlers.
type pluginServe struct {
	plugins []config.PluginConfig
	root    string
}

// wirePluginServeHandlers builds the top-level serving handler for a config:
// a ServeRequest interceptor in front, and a ServeResponse buffering wrapper
// around the actual page-serving chain. Streaming responses (those that flush
// before returning) pass through untouched; only fully-buffered non-streaming
// responses are sent through the ServeResponse hooks.
func wirePluginServeHandlers(root string, cfg *config.Config, next http.Handler) http.Handler {
	ps := &pluginServe{plugins: cfg.Plugins, root: root}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ServeRequest interceptors run first; they may rewrite or respond.
		if reqRes, err := plugin.RunServeRequest(ps.plugins, ps.root,
			"http://"+r.Host+r.URL.RequestURI(), r.Method, r.URL.Path, headerMap(r.Header)); err == nil && reqRes != nil {
			if reqRes.Action == "respond" && reqRes.Status > 0 {
				for k, v := range reqRes.Headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(reqRes.Status)
				if reqRes.Body != "" {
					_, _ = w.Write([]byte(reqRes.Body))
				}
				return
			}
			if reqRes.Action == "rewrite" && reqRes.NewURL != "" {
				r.URL.Path = reqRes.NewURL
				r.URL.RawPath = ""
			}
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "  %sServeRequest plugin error:%s %v\n", cYellow, cReset, err)
		}

		cap := &bufferedResponseWriter{header: make(http.Header)}
		next.ServeHTTP(cap, r)
		cap.done()

		// Streaming responses flushed during serving: replay buffered bytes
		// unchanged (ServeResponse only applies to non-streaming output).
		if cap.streamed {
			if cap.status > 0 {
				copyHeaders(w.Header(), cap.header)
				w.WriteHeader(cap.status)
			}
			_, _ = w.Write(cap.body.Bytes())
			return
		}

		body := cap.body.String()
		status := cap.status
		headers := headerToMap(cap.header)
		resp, err := plugin.RunServeResponse(ps.plugins, ps.root,
			"http://"+r.Host+r.URL.RequestURI(), r.Method, r.URL.Path, status, headers, body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %sServeResponse plugin error:%s %v\n", cYellow, cReset, err)
			resp = plugin.ServeResponseResult{Status: status, Headers: headers, Body: body}
		}
		if resp.Status > 0 {
			copyHeaders(w.Header(), cap.header)
			for k, v := range resp.Headers {
				w.Header().Set(k, v)
			}
			// The buffered response may carry a stale Content-Length from the
			// static-file chain; a plugin that rewrites the body changes its
			// length, so reconcile the header with what we actually write to
			// avoid ERR_CONTENT_LENGTH_MISMATCH.
			if resp.Body != "" {
				w.Header().Set("Content-Length", strconv.Itoa(len(resp.Body)))
			} else {
				w.Header().Del("Content-Length")
			}
			w.WriteHeader(resp.Status)
			if resp.Body != "" {
				_, _ = w.Write([]byte(resp.Body))
			}
		} else {
			copyHeaders(w.Header(), cap.header)
			w.WriteHeader(status)
			_, _ = w.Write(cap.body.Bytes())
		}
	})
}

// bufferedResponseWriter records the response body/status and whether the
// downstream handler streamed (flushed before completing).
type bufferedResponseWriter struct {
	header   http.Header
	body     bytesBuffer
	status   int
	streamed bool
	started  bool
}

func (b *bufferedResponseWriter) Header() http.Header { return b.header }

func (b *bufferedResponseWriter) WriteHeader(code int) {
	if b.started {
		return
	}
	b.started = true
	b.status = code
}

func (b *bufferedResponseWriter) Write(p []byte) (int, error) {
	if !b.started {
		b.started = true
		b.status = 200
	}
	return b.body.Write(p)
}

func (b *bufferedResponseWriter) Flush() {
	b.streamed = true
	// Flushing implies streaming output that cannot be safely rewritten.
}

func (b *bufferedResponseWriter) done() {
	if !b.started {
		b.status = 200
	}
}
