// Package chrome is the page side of window chrome: the script that lets a
// page mark drag regions and the HTTP endpoint the script talks to.
//
// The script is injected into the HTML pages of native windows. It finds the
// regions a page marks with the data-krate-drag attribute, the CSS properties
// -webkit-app-region and app-region (read from the page's style sheets,
// because most engines drop the property while parsing), or the custom
// property --krate-app-region, and turns a press in one into a request to
// start a native window move. Double presses toggle maximize. On frameless
// Linux windows it also draws thin resize edges.
package chrome

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

//go:embed chrome.js
var scriptSource string

// Path is the endpoint the script talks to.
const Path = "/__krate/chrome"

// Script is the element injected into a page.
var Script = "<script>" + scriptSource + "</script>"

// Wanted reports whether an HTML page uses drag regions, so that the script is
// worth injecting. A page that does not mention a marker cannot have regions
// (a page that gets its style from an external sheet needs always = true).
func Wanted(html []byte) bool {
	return bytes.Contains(html, []byte("data-krate-drag")) || bytes.Contains(html, []byte("app-region"))
}

// Resolver finds the window a request came from. It must not trust anything
// the page says about its identity; the platform tags each request with its
// webview.
type Resolver func(r *http.Request) (any, bool)

// Handler serves Path: GET describes the window (JSON), POST runs an
// operation {"op": "drag" | "toggleMaximize" | "resize", "edge": "..."}.
// handled is false for any other path.
func Handler(resolve Resolver) func(w http.ResponseWriter, r *http.Request) (handled bool) {
	return func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != Path {
			return false
		}
		win, ok := resolve(r)
		var c caps.WindowChrome
		if ok {
			c, ok = win.(caps.WindowChrome)
		}
		if !ok {
			http.Error(w, "no chrome for this window", http.StatusNotFound)
			return true
		}
		switch r.Method {
		case http.MethodGet:
			writeInfo(w, win, c)
		case http.MethodPost:
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1024))
			var req struct {
				Op   string `json:"op"`
				Edge string `json:"edge"`
			}
			if json.Unmarshal(body, &req) != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return true
			}
			var err error
			switch req.Op {
			case "drag":
				err = c.StartDrag()
			case "toggleMaximize":
				err = c.ToggleMaximize()
			case "resize":
				if caps.EdgeIndex(req.Edge) < 0 {
					http.Error(w, "unknown edge", http.StatusBadRequest)
					return true
				}
				err = c.StartResize(req.Edge)
			default:
				http.Error(w, "unknown op", http.StatusBadRequest)
				return true
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotImplemented)
				return true
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return true
	}
}

// Info is the JSON answer to GET.
type Info struct {
	Decorated     bool   `json:"decorated"`
	Resizable     bool   `json:"resizable"`
	Maximized     bool   `json:"maximized"`
	Fullscreen    bool   `json:"fullscreen"`
	TitleBar      string `json:"titleBar"`
	NativeRegions bool   `json:"nativeRegions"`
	// Edges asks the page to draw its own resize edges.
	Edges bool `json:"edges"`
}

// BuildInfo derives the answer from a window's chrome and state.
func BuildInfo(cs caps.ChromeState, ws caps.WindowState) Info {
	return Info{
		Decorated: ws.Decorated, Resizable: cs.Resizable, Maximized: ws.Maximized,
		Fullscreen: ws.Fullscreen, TitleBar: cs.TitleBar, NativeRegions: cs.NativeRegions,
		Edges: cs.ScriptResize && !ws.Decorated && cs.Resizable && !ws.Maximized && !ws.Fullscreen,
	}
}

func writeInfo(w http.ResponseWriter, win any, c caps.WindowChrome) {
	var ws caps.WindowState
	if wc, ok := win.(caps.WindowControls); ok {
		ws = wc.State()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(BuildInfo(c.ChromeState(), ws))
}
