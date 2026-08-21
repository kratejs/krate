package jsruntime

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// injectPolyfills injects Web API polyfills into the JS runtime
func (r *Runtime) injectPolyfills() error {
	// Inject console
	if err := r.injectConsole(); err != nil {
		return fmt.Errorf("console: %w", err)
	}

	// Inject setTimeout/setInterval
	if err := r.injectTimers(); err != nil {
		return fmt.Errorf("timers: %w", err)
	}

	// Inject TextEncoder/TextDecoder
	if err := r.injectTextEncoding(); err != nil {
		return fmt.Errorf("text encoding: %w", err)
	}

	// Inject fetch API
	if err := r.injectFetch(); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	// Inject URL and Response web APIs
	if err := r.injectWebAPIs(); err != nil {
		return fmt.Errorf("web APIs: %w", err)
	}

	// Inject process.env
	if err := r.injectProcessEnv(); err != nil {
		return fmt.Errorf("process.env: %w", err)
	}

	// Inject fs.readFile
	if err := r.injectFsReadFile(); err != nil {
		return fmt.Errorf("fs.readFile: %w", err)
	}

	return nil
}

// injectFsReadFile adds a fs.readFile polyfill that reads files from the filesystem.
func (r *Runtime) injectFsReadFile() error {
	if err := r.RegisterFunc("_go_readFile", func(args []any) (any, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("fs.readFile requires a path")
		}
		path, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("fs.readFile path must be a string")
		}
		encoding := "utf-8"
		if len(args) > 1 {
			if opts, ok := args[1].(map[string]any); ok {
				if enc, ok := opts["encoding"].(string); ok {
					encoding = enc
				}
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}
		if encoding == "utf-8" || encoding == "utf8" || encoding == "" {
			return string(data), nil
		}
		return nil, fmt.Errorf("unsupported encoding: %s", encoding)
	}); err != nil {
		return err
	}

	_, err := r.Execute(`
		function __require_fs() {
			return {
				readFileSync: function(p, opts) { return _go_readFile(p, typeof opts === 'object' ? opts : {encoding: 'utf-8'}); },
				readFile: function(p, opts, cb) {
					if (typeof opts === 'function') { cb = opts; opts = {encoding: 'utf-8'}; }
					try { var data = _go_readFile(p, typeof opts === 'object' ? opts : {encoding: 'utf-8'}); cb(null, data); }
					catch(e) { cb(e); }
				}
			};
		}
		function __require_path() {
			return {
				join: function() { var parts = []; for (var i = 0; i < arguments.length; i++) parts.push(arguments[i]); return parts.join('/').replace(/\\\\+/g, '/'); },
				resolve: function() { var parts = []; for (var i = 0; i < arguments.length; i++) parts.push(arguments[i]); return parts.join('/').replace(/\\\\+/g, '/'); },
				sep: '/',
				basename: function(p, ext) { var name = p.split('/').pop().split('\\\\').pop(); if (ext && name.endsWith(ext)) name = name.slice(0, -ext.length); return name; },
				dirname: function(p) { var parts = p.split('/'); parts.pop(); return parts.join('/') || '.'; },
				extname: function(p) { var idx = p.lastIndexOf('.'); return idx >= 0 ? p.slice(idx) : ''; }
			};
		}
		if (typeof require === 'undefined') {
			globalThis.require = function(m) {
				if (m === 'fs') return __require_fs();
				if (m === 'path') return __require_path();
				throw new Error('Module not found: ' + m);
			};
		}
	`)
	return err
}

// injectConsole adds console.log, console.error, console.warn
func (r *Runtime) injectConsole() error {
	// Create console object
	_, err := r.Execute(`
		var console = {
			log: function() {
				// Will be overridden by Go
			},
			error: function() {
				// Will be overridden by Go
			},
			warn: function() {
				// Will be overridden by Go
			}
		};
	`)
	if err != nil {
		return err
	}

	// Register Go implementations
	if err := r.RegisterFunc("console_log", func(args []any) (any, error) {
		fmt.Println(args...)
		return nil, nil
	}); err != nil {
		return err
	}

	if err := r.RegisterFunc("console_error", func(args []any) (any, error) {
		fmt.Println("ERROR:", args)
		return nil, nil
	}); err != nil {
		return err
	}

	if err := r.RegisterFunc("console_warn", func(args []any) (any, error) {
		fmt.Println("WARN:", args)
		return nil, nil
	}); err != nil {
		return err
	}

	// Override console methods with Go implementations
	_, err = r.Execute(`
		console.log = function() {
			var args = Array.prototype.slice.call(arguments);
			console_log(JSON.stringify(args));
		};
		console.error = function() {
			var args = Array.prototype.slice.call(arguments);
			console_error(JSON.stringify(args));
		};
		console.warn = function() {
			var args = Array.prototype.slice.call(arguments);
			console_warn(JSON.stringify(args));
		};
	`)
	return err
}

// injectTimers adds setTimeout and setInterval (simplified, non-async)
func (r *Runtime) injectTimers() error {
	// For middleware/API, timers are synchronous
	// setTimeout with 0 delay executes immediately
	if err := r.RegisterFunc("setTimeout", func(args []any) (any, error) {
		if len(args) < 1 {
			return nil, nil
		}
		// Just return 0 (timer ID) - we don't actually wait
		return 0, nil
	}); err != nil {
		return err
	}

	if err := r.RegisterFunc("setInterval", func(args []any) (any, error) {
		// Not supported in middleware context
		return 0, nil
	}); err != nil {
		return err
	}

	if err := r.RegisterFunc("clearTimeout", func(args []any) (any, error) {
		return nil, nil
	}); err != nil {
		return err
	}

	return r.RegisterFunc("clearInterval", func(args []any) (any, error) {
		return nil, nil
	})
}

// injectTextEncoding adds TextEncoder and TextDecoder
func (r *Runtime) injectTextEncoding() error {
	_, err := r.Execute(`
		class TextEncoder {
			encode(str) {
				if (typeof str !== 'string') str = String(str);
				var bytes = [];
				for (var i = 0; i < str.length; i++) {
					var c = str.charCodeAt(i);
					if (c < 128) {
						bytes.push(c);
					} else if (c < 2048) {
						bytes.push((c >> 6) | 192);
						bytes.push((c & 63) | 128);
					} else {
						bytes.push((c >> 12) | 224);
						bytes.push(((c >> 6) & 63) | 128);
						bytes.push((c & 63) | 128);
					}
				}
				return new Uint8Array(bytes);
			}
		}

		class TextDecoder {
			decode(buffer) {
				var bytes = new Uint8Array(buffer);
				var str = '';
				for (var i = 0; i < bytes.length; i++) {
					var byte = bytes[i];
					if (byte < 128) {
						str += String.fromCharCode(byte);
					} else if (byte < 224) {
						str += String.fromCharCode(((byte & 31) << 6) | (bytes[++i] & 63));
					} else {
						str += String.fromCharCode(((byte & 15) << 12) | ((bytes[++i] & 63) << 6) | (bytes[++i] & 63));
					}
				}
				return str;
			}
		}

		class Uint8Array {
			constructor(arr) {
				this._data = arr || [];
				this.length = this._data.length;
			}
			get(index) { return this._data[index]; }
			set(index, value) { this._data[index] = value; }
			slice(start, end) { return new Uint8Array(this._data.slice(start, end)); }
		}
	`)
	return err
}

// fetchResponse holds the result of a fetch call
type fetchResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       string
}

// injectFetch adds the fetch API
func (r *Runtime) injectFetch() error {
	// Register Go fetch implementation
	if err := r.RegisterFunc("_go_fetch", func(args []any) (any, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("fetch requires a URL")
		}

		url, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("fetch URL must be a string")
		}

		// Parse options
		method := "GET"
		var body []byte
		headers := make(map[string]string)

		if len(args) > 1 {
			if opts, ok := args[1].(map[string]any); ok {
				if m, ok := opts["method"].(string); ok {
					method = m
				}
				if b, ok := opts["body"].(string); ok {
					body = []byte(b)
				}
				if h, ok := opts["headers"].(map[string]any); ok {
					for k, v := range h {
						if s, ok := v.(string); ok {
							headers[k] = s
						}
					}
				}
			}
		}

		req, err := http.NewRequest(method, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch failed: %w", err)
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		respHeaders := make(map[string]string)
		for k := range resp.Header {
			respHeaders[k] = resp.Header.Get(k)
		}

		return map[string]any{
			"status":     resp.StatusCode,
			"headers":    respHeaders,
			"body":       string(respBody),
			"statusText": resp.Status,
		}, nil
	}); err != nil {
		return err
	}

	// Inject fetch JavaScript wrapper
	_, err := r.Execute(`
		async function fetch(url, options) {
			var result = _go_fetch(url, options || {});
			return {
				status: result.status,
				statusText: result.statusText,
				headers: result.headers,
				body: result.body,
				json: function() { return JSON.parse(result.body); },
				text: function() { return result.body; },
				ok: result.status >= 200 && result.status < 300
			};
		}
	`)
	return err
}

// injectProcessEnv adds process.env support
func (r *Runtime) injectProcessEnv() error {
	// This will be overridden by the caller with actual env vars
	_, err := r.Execute(`
		var process = {
			env: {}
		};
	`)
	return err
}

// SetEnv sets environment variables for the runtime
func (r *Runtime) SetEnv(env map[string]string) error {
	// Convert to JSON and inject
	jsonEnv := "{"
	first := true
	for k, v := range env {
		if !first {
			jsonEnv += ","
		}
		jsonEnv += fmt.Sprintf(`"%s":"%s"`, k, v)
		first = false
	}
	jsonEnv += "}"

	_, err := r.Execute(fmt.Sprintf("process.env = %s;", jsonEnv))
	return err
}

// injectWebAPIs adds URL, Headers, and Response web standard APIs
func (r *Runtime) injectWebAPIs() error {
	_, err := r.Execute(`
		class URLSearchParams {
			constructor(init) {
				this._params = [];
				if (init) {
					if (typeof init === 'string') {
						var str = init.replace(/^\?/, '');
						if (str) {
							var pairs = str.split('&');
							for (var i = 0; i < pairs.length; i++) {
								var kv = pairs[i].split('=');
								this._params.push([decodeURIComponent(kv[0] || ''), decodeURIComponent(kv.slice(1).join('') || '')]);
							}
						}
					}
				}
			}
			get(name) {
				for (var i = 0; i < this._params.length; i++) {
					if (this._params[i][0] === name) return this._params[i][1];
				}
				return null;
			}
			getAll(name) {
				var result = [];
				for (var i = 0; i < this._params.length; i++) {
					if (this._params[i][0] === name) result.push(this._params[i][1]);
				}
				return result;
			}
			has(name) {
				for (var i = 0; i < this._params.length; i++) {
					if (this._params[i][0] === name) return true;
				}
				return false;
			}
			set(name, value) {
				for (var i = 0; i < this._params.length; i++) {
					if (this._params[i][0] === name) { this._params[i][1] = String(value); return; }
				}
				this._params.push([name, String(value)]);
			}
			append(name, value) { this._params.push([name, String(value)]); }
			delete(name) { this._params = this._params.filter(function(p) { return p[0] !== name; }); }
			toString() {
				var parts = [];
				for (var i = 0; i < this._params.length; i++) {
					parts.push(encodeURIComponent(this._params[i][0]) + '=' + encodeURIComponent(this._params[i][1]));
				}
				return parts.join('&');
			}
		}

		class URL {
			constructor(url, base) {
				if (base) {
					this._href = new URL(url, base).href;
				} else {
					this._href = url;
				}
				var match = this._href.match(/^(https?:)\/\/([^\/:]+)(:\d+)?(\/[^\?]*)?(\?.*)?(#.*)?$/);
				if (match) {
					this.protocol = match[1];
					this.hostname = match[2];
					this.port = (match[3] || '').replace(/^:/, '');
					this.pathname = match[4] || '/';
					this.search = match[5] || '';
					this.hash = match[6] || '';
				} else {
					this.protocol = '';
					this.hostname = '';
					this.pathname = this._href;
					this.search = '';
					this.hash = '';
					this.port = '';
				}
				this.searchParams = new URLSearchParams(this.search);
			}
			get href() { return this._href; }
			toString() { return this._href; }
		}

		class Headers {
			constructor(init) {
				this._keys = [];
				this._vals = {};
				if (init) {
					if (typeof init === 'object') {
						if (Array.isArray(init)) {
							for (var i = 0; i < init.length; i++) {
								this.set(init[i][0], init[i][1]);
							}
						} else {
							var keys = Object.keys(init);
							for (var i = 0; i < keys.length; i++) {
								this.set(keys[i], init[keys[i]]);
							}
						}
					}
				}
			}
			get(name) { return this._vals[name.toLowerCase()] || null; }
			set(name, value) {
				var key = name.toLowerCase();
				if (!(key in this._vals)) {
					this._keys.push(key);
				}
				this._vals[key] = String(value);
			}
			append(name, value) {
				var key = name.toLowerCase();
				if (key in this._vals) {
					this._vals[key] += ', ' + value;
				} else {
					this._keys.push(key);
					this._vals[key] = String(value);
				}
			}
			has(name) { return name.toLowerCase() in this._vals; }
			delete(name) {
				var key = name.toLowerCase();
				if (key in this._vals) {
					delete this._vals[key];
					var idx = this._keys.indexOf(key);
					if (idx >= 0) this._keys.splice(idx, 1);
				}
			}
			entries() {
				var result = [];
				for (var i = 0; i < this._keys.length; i++) {
					result.push([this._keys[i], this._vals[this._keys[i]]]);
				}
				return result;
			}
			forEach(cb) {
				for (var i = 0; i < this._keys.length; i++) {
					var k = this._keys[i];
					cb(this._vals[k], k, this);
				}
			}
		}

		class Response {
			constructor(body, init) {
				this._body = body || '';
				this.status = (init && init.status) || 200;
				this.statusText = (init && init.statusText) || 'OK';
				this.headers = new Headers(init && init.headers);
				this.ok = this.status >= 200 && this.status < 300;
			}
			text() { return typeof this._body === 'string' ? this._body : ''; }
			json() {
				var t = this.text();
				return t ? JSON.parse(t) : null;
			}
			arrayBuffer() { return new ArrayBuffer(0); }
		}
		Response.json = function(data, init) {
			var body = JSON.stringify(data);
			var h = new Headers(init && init.headers);
			if (!h.has('content-type')) h.set('content-type', 'application/json');
			return new Response(body, { status: (init && init.status) || 200, headers: h });
		};
	`)
	return err
}
