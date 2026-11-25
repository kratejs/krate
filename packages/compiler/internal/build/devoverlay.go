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
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
	"github.com/kratejs/krate/packages/compiler/internal/version"
)

// writeBuildError serialises a build-error event (structured diagnostics) as an
// SSE frame. The client overlay renders the diagnostics with source + caret.
func writeBuildError(w io.Writer, ev DevEvent) {
	payload := struct {
		Diagnostics []interface{}     `json:"diagnostics"`
		BuildOK     bool              `json:"buildOk"`
		Warnings    []diag.Diagnostic `json:"warnings,omitempty"`
	}{BuildOK: len(ev.Diagnostics) == 0, Warnings: ev.Warnings}
	for _, d := range ev.Diagnostics {
		payload.Diagnostics = append(payload.Diagnostics, d)
	}
	data, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: build-error\ndata: %s\n\n", data)
}

// writeBuildWarnings tells a tab that connects after a build which warnings
// that build left (build-end carries them for the tabs already connected).
func writeBuildWarnings(w io.Writer, warnings []diag.Diagnostic) {
	data, _ := json.Marshal(struct {
		Warnings []diag.Diagnostic `json:"warnings"`
	}{warnings})
	fmt.Fprintf(w, "event: build-warnings\ndata: %s\n\n", data)
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
	shell, _ := json.Marshal(map[string]any{
		"sse":     "/__krate/hotreload",
		"errors":  "/__krate/client-error",
		"open":    "/__krate/open",
		"api":     devAPIBase,
		"version": version.Value,
		"overlay": true,
		"toolbar": true,
	})
	bootstrap := string(shell)
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

// devBuildWait is how long a request waits for a running rebuild before it is
// answered from whatever the output holds.
var devBuildWait = 60 * time.Second

// devBuildGate holds the requests that arrive while a rebuild runs until it
// ends. A full rebuild empties the output directory first and a partial one
// rewrites the server bundles page by page, so a request in that window got a
// 404 or a page of the old build with the assets of the new one. The dev
// endpoints (the reload stream, the error reports) and the health checks pass.
func devBuildGate(hub *DevHub, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if idle := hub.buildIdle(); idle != nil && !strings.HasPrefix(p, "/__krate/") && p != "/healthz" && p != "/readyz" {
			timer := time.NewTimer(devBuildWait)
			select {
			case <-idle:
			case <-r.Context().Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			timer.Stop()
		}
		next.ServeHTTP(w, r)
	})
}

// DevOutDir is where `krate dev` writes and serves its output: a private
// directory under .krate, which the watcher already ignores. Keeping it apart
// from the production outDir lets `krate build` run while dev is up, without
// replacing the vendor and island chunks the dev pages import.
func DevOutDir(root string) string {
	return filepath.Join(root, ".krate", "dev-out")
}

// UseDevOutDir points cfg at the dev output directory. Call it once, before
// building the dev Builder and starting ServeDev, so everything downstream
// (builder, static serving, sidecars, manifests, the watcher's pruning)
// follows cfg.OutDir.
func UseDevOutDir(root string, cfg *config.Config) {
	cfg.OutDir = DevOutDir(root)
}

// devRetiredChunksDir holds the chunks a dev rebuild replaced, outside the
// output directory so a full rebuild does not empty it. A page served before
// the rebuild, or still open in a browser, names them.
func devRetiredChunksDir(root string) string {
	return filepath.Join(root, ".krate", "dev-retired-chunks")
}

// retireChunk moves a replaced chunk into the retired directory.
func retireChunk(retired, path string) {
	if retired == "" {
		_ = os.Remove(path)
		return
	}
	dst := filepath.Join(retired, filepath.Base(path))
	if err := os.MkdirAll(retired, 0755); err != nil || os.Rename(path, dst) != nil {
		_ = os.Remove(path)
		return
	}
	now := time.Now()
	_ = os.Chtimes(dst, now, now)
}

// devRetiredGrace is how long a retired chunk is kept.
const devRetiredGrace = 30 * time.Minute

// retireOutputChunks moves the chunks of the output directory about to be
// emptied into the retired directory, so the chunks of the builds before stay
// served. Those retired longer ago than devRetiredGrace are deleted.
func retireOutputChunks(root, outDir string) {
	retired := devRetiredChunksDir(root)
	chunks := filepath.Join(outDir, "chunks")
	entries, err := os.ReadDir(chunks)
	if err != nil {
		return
	}
	if old, err := os.ReadDir(retired); err == nil {
		for _, e := range old {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > devRetiredGrace {
				_ = os.Remove(filepath.Join(retired, e.Name()))
			}
		}
	}
	for _, e := range entries {
		if !e.IsDir() {
			retireChunk(retired, filepath.Join(chunks, e.Name()))
		}
	}
}

// serveRetiredChunk answers a request for a chunk the output no longer holds
// with its copy from the retired directory.
func serveRetiredChunk(w http.ResponseWriter, r *http.Request, root, outDir string) bool {
	name := strings.TrimPrefix(r.URL.Path, "/chunks/")
	if name == r.URL.Path || name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return false
	}
	if _, err := os.Stat(filepath.Join(outDir, "chunks", name)); err == nil {
		return false
	}
	file := filepath.Join(devRetiredChunksDir(root), name)
	if info, err := os.Stat(file); err != nil || info.IsDir() {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, file)
	return true
}
