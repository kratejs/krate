package build

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
)

// writeBuildError serialises a build-error event (structured diagnostics) as an
// SSE frame. The client overlay renders the diagnostics with source + caret.
func writeBuildError(w io.Writer, ev DevEvent) {
	payload := struct {
		Diagnostics []interface{} `json:"diagnostics"`
		BuildOK     bool          `json:"buildOk"`
	}{BuildOK: len(ev.Diagnostics) == 0}
	for _, d := range ev.Diagnostics {
		payload.Diagnostics = append(payload.Diagnostics, d)
	}
	data, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: build-error\ndata: %s\n\n", data)
}

// safeProjectPath resolves a client-supplied file path (absolute or relative)
// against the project root and rejects anything that escapes it or is not an
// existing regular file. Diagnostics carry absolute page paths, so absolute
// paths inside the root must be accepted.
func safeProjectPath(root, file string) (string, bool) {
	if file == "" {
		return "", false
	}
	p := filepath.Clean(filepath.FromSlash(file))
	var abs string
	if filepath.IsAbs(p) {
		abs = p
	} else {
		abs = filepath.Join(root, p)
	}
	if !pluginapi.WithinRoot(root, abs) {
		return "", false
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return "", false
	}
	return abs, true
}

// editorArgs builds the argument list that opens file at line for the given
// editor executable (flags already given by the user come first). Editors
// disagree on the syntax, so the common ones are mapped explicitly; an unknown
// editor gets the plain file path.
func editorArgs(name string, userArgs []string, file, line string) []string {
	args := append([]string{}, userArgs...)
	hasLine := line != "" && line != "0"
	if _, err := strconv.Atoi(line); err != nil {
		hasLine = false
	}
	if !hasLine {
		return append(args, file)
	}
	exe := strings.ToLower(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	gotoGiven := false
	for _, a := range userArgs {
		if a == "-g" || a == "--goto" {
			gotoGiven = true
		}
	}
	switch exe {
	case "code", "code-insiders", "codium", "vscodium", "cursor", "windsurf", "trae":
		if gotoGiven {
			return append(args, file+":"+line)
		}
		return append(args, "-g", file+":"+line)
	case "idea", "webstorm", "phpstorm", "pycharm", "goland", "clion", "rider", "rubymine", "datagrip", "studio":
		return append(args, "--line", line, file)
	case "subl", "sublime_text", "zed":
		return append(args, file+":"+line)
	case "mate":
		return append(args, "-l", line, file)
	case "vim", "nvim", "vi", "nano", "emacs", "emacsclient", "hx", "helix", "micro":
		if exe == "hx" || exe == "helix" {
			return append(args, file+":"+line)
		}
		return append(args, "+"+line, file)
	}
	return append(args, file)
}

// openInEditor launches the configured editor at file:line. The editor command
// may include flags (e.g. "code -r"); when empty, VS Code's `code` is assumed.
// Launch is non-blocking so the request returns immediately.
func openInEditor(editor, file, line string) error {
	if editor == "" {
		editor = "code"
	}
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		fields = []string{"code"}
	}
	name := fields[0]
	cmd := exec.Command(name, editorArgs(name, fields[1:], file, line)...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// devErrorShell serves an HTML page carrying the dev overlay and SSE client in
// place of a missing document while the build is failing, so a failed initial
// build shows the diagnostics (and recovers on the next good build) instead of
// a directory listing or a bare 404.
func devErrorShell(hub *DevHub, root, outDir string, next http.Handler) http.Handler {
	bootstrap := `{"sse":"/__krate/hotreload","errors":"/__krate/client-error","open":"/__krate/open","overlay":true,"toolbar":true}`
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__krate/dev.js" {
			js := loadDevBundleFromDisk(root)
			if js == "" {
				js = devFallbackJS
			}
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, js)
			return
		}
		cur, ok := hub.Current()
		if !ok || cur.BuildOK || (r.Method != http.MethodGet && r.Method != http.MethodHead) ||
			strings.HasPrefix(r.URL.Path, "/__krate/") || strings.HasPrefix(r.URL.Path, "/api/") ||
			!strings.Contains(r.Header.Get("Accept"), "text/html") || devDocumentExists(outDir, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		var sb strings.Builder
		sb.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Build failed</title></head><body style=\"font-family:system-ui,sans-serif;padding:2rem;color:#222\"><h1>Build failed</h1><p>Fix the errors below; this page reloads when the build succeeds.</p><pre style=\"white-space:pre-wrap\">")
		for _, d := range cur.Diagnostics {
			sb.WriteString(html.EscapeString(d.Error()))
			if d.Hint != "" {
				sb.WriteString("\n  hint: " + html.EscapeString(d.Hint))
			}
			sb.WriteString("\n\n")
		}
		sb.WriteString("</pre><script>window.__KRATE_DEV__=" + bootstrap + ";</script><script src=\"/__krate/dev.js\"></script></body></html>")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusInternalServerError)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, sb.String())
		}
	})
}

// devDocumentExists reports whether the built output has an HTML document for
// urlPath (exact file, directory index or .html sibling).
func devDocumentExists(outDir, urlPath string) bool {
	rel := filepath.FromSlash(strings.TrimPrefix(filepath.Clean("/"+urlPath), "/"))
	for _, c := range []string{rel, filepath.Join(rel, "index.html"), rel + ".html"} {
		if info, err := os.Stat(filepath.Join(outDir, c)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}
