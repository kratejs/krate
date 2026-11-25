package build

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fix"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
	"github.com/kratejs/krate/packages/compiler/internal/version"
)

// The dev overlay's API: JSON endpoints under devAPIBase, registered by the
// dev server next to /__krate/hotreload and guarded the same way.
const devAPIBase = "/__krate/dev"

// devDocsURL is the diagnostics reference; the overlay appends the
// lowercase code ("#kr5002").
const devDocsURL = "https://krate.js.org/docs/reference/diagnostics/#"

// devAPIBodyLimit caps the JSON body of a POST to the dev API.
const devAPIBodyLimit = 64 << 10

// Source frame limits of GET /__krate/dev/source.
const (
	devSourceContextDefault = 6
	devSourceContextMax     = 40
	devSourceLineMax        = 500
	devSourceFileMax        = 8 << 20
)

// Sidecar ids of restart-sidecar.
const (
	sidecarSSR = "ssr"
	sidecarAPI = "api"
	sidecarGo  = "go"
)

var (
	// errSidecarUnknown is a restart target that is not a sidecar id.
	errSidecarUnknown = errors.New("unknown sidecar")
	// errSidecarNotRunning is a sidecar this server does not run.
	errSidecarNotRunning = errors.New("that sidecar is not running")
)

// devAPI serves the overlay's endpoints for one dev server.
type devAPI struct {
	root      string
	cfg       *config.Config
	outDir    string // absolute dev output directory
	hub       *DevHub
	startedAt time.Time
	// restartSidecar stops a sidecar so its supervisor starts it again.
	restartSidecar func(target string) error
}

// register adds the endpoints through guard, which wraps each handler in
// devEndpointGuard.
func (d *devAPI) register(guard func(pattern string, h http.HandlerFunc)) {
	guard(devAPIBase+"/info", d.get(d.info))
	guard(devAPIBase+"/code", d.get(d.code))
	guard(devAPIBase+"/route", d.get(d.route))
	guard(devAPIBase+"/routes", d.get(d.routes))
	guard(devAPIBase+"/activity", d.get(d.activity))
	guard(devAPIBase+"/logs", d.get(d.logs))
	guard(devAPIBase+"/source", d.get(d.source))
	guard(devAPIBase+"/html", d.get(d.html))
	guard(devAPIBase+"/fix", d.fix)
	guard(devAPIBase+"/action", d.post(d.action))
}

// get wraps a handler that answers GET (and HEAD) only.
func (d *devAPI) get(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			devError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h(w, r)
	}
}

// post wraps a handler that answers POST with a JSON body only.
func (d *devAPI) post(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			devError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !jsonContentType(r) {
			devError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, devAPIBodyLimit)
		h(w, r)
	}
}

func jsonContentType(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

// decodeDevBody reads a POST body into v, answering 400 (413 when too large)
// itself when it cannot.
func decodeDevBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			devError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		devError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func devJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func devError(w http.ResponseWriter, status int, msg string) {
	devJSON(w, status, map[string]string{"error": msg})
}

// GET /__krate/dev/info
func (d *devAPI) info(w http.ResponseWriter, r *http.Request) {
	logFile, _ := devLogLocation.Load().(string)
	if logFile == "" {
		if _, err := os.Stat(DevLogPath(d.root)); err == nil {
			logFile = ".krate/dev.log"
		}
	}
	devJSON(w, http.StatusOK, map[string]any{
		"version":   version.Value,
		"startedAt": d.startedAt.UnixMilli(),
		"logFile":   logFile,
		"docsURL":   devDocsURL,
		"editor": map[string]any{
			"configured": strings.TrimSpace(d.cfg.DevServer.Editor),
			"available":  availableEditors(),
		},
	})
}

// GET /__krate/dev/code?code=KR5002: the registry's title and explanation
// of a diagnostic code, for the overlay's category chip and popover.
func (d *devAPI) code(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
	info, ok := diag.Lookup(diag.Code(code))
	if code == "" || !ok {
		devError(w, http.StatusNotFound, "unknown diagnostic code")
		return
	}
	devJSON(w, http.StatusOK, map[string]string{
		"code":        string(info.Code),
		"title":       info.Title,
		"explanation": info.Explanation,
		"docsURL":     devDocsURL + strings.ToLower(string(info.Code)),
	})
}

// GET /__krate/dev/route?path=/blog/hello
func (d *devAPI) route(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		p = "/"
	}
	info, ok := devRouteInfo(d.root, d.cfg, d.outDir, p)
	if !ok {
		devError(w, http.StatusNotFound, "no built page serves "+p)
		return
	}
	devJSON(w, http.StatusOK, info)
}

// GET /__krate/dev/routes
func (d *devAPI) routes(w http.ResponseWriter, r *http.Request) {
	goRoutes := map[string][]string{}
	if entries, _, err := loadGoAPIManifest(gohost.RoutesManifestPath(d.root)); err == nil {
		for _, e := range entries {
			goRoutes[e.Path] = append(goRoutes[e.Path], e.Method)
		}
	}
	devJSON(w, http.StatusOK, devRouteList(d.root, d.cfg, d.outDir, devAPIRoutes(d.root, goRoutes)))
}

// GET /__krate/dev/activity
func (d *devAPI) activity(w http.ResponseWriter, r *http.Request) {
	devJSON(w, http.StatusOK, d.hub.Activity())
}

// GET /__krate/dev/logs?since=<seq>
func (d *devAPI) logs(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	seq, lines := DevLogsSince(since)
	devJSON(w, http.StatusOK, map[string]any{"seq": seq, "lines": lines})
}

// DevSourceFrame is the payload of GET /__krate/dev/source.
type DevSourceFrame struct {
	File  string   `json:"file"`
	Lang  string   `json:"lang"`
	Start int      `json:"start"`
	Lines []string `json:"lines"`
}

// GET /__krate/dev/source?file=&line=&context=
func (d *devAPI) source(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	abs, ok := safeProjectPath(d.root, q.Get("file"))
	if !ok {
		devError(w, http.StatusNotFound, "no such project file")
		return
	}
	line, _ := strconv.Atoi(q.Get("line"))
	ctx := devSourceContextDefault
	if v := q.Get("context"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			ctx = n
		}
	}
	frame, ok := readSourceFrame(d.root, abs, line, ctx)
	if !ok {
		devError(w, http.StatusNotFound, "not a readable text file")
		return
	}
	devJSON(w, http.StatusOK, frame)
}

// readSourceFrame reads the lines around line (1-based; 0 means the top) of
// a text file. Binary and very large files are refused.
func readSourceFrame(root, abs string, line, ctx int) (DevSourceFrame, bool) {
	ctx = min(max(ctx, 0), devSourceContextMax)
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() || st.Size() > devSourceFileMax {
		return DevSourceFrame{}, false
	}
	f, err := os.Open(abs)
	if err != nil {
		return DevSourceFrame{}, false
	}
	defer f.Close()
	head := make([]byte, 8192)
	n, _ := io.ReadFull(f, head)
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return DevSourceFrame{}, false
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return DevSourceFrame{}, false
	}
	if line < 1 {
		line = 1
	}
	start := max(line-ctx, 1)
	end := line + ctx
	frame := DevSourceFrame{File: displayRel(root, abs), Lang: sourceLang(abs), Start: start, Lines: []string{}}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), devSourceFileMax)
	for i := 1; sc.Scan(); i++ {
		if i < start {
			continue
		}
		if i > end {
			break
		}
		text := strings.TrimRight(sc.Text(), "\r")
		if len(text) > devSourceLineMax {
			text = text[:devSourceLineMax] + "..."
		}
		frame.Lines = append(frame.Lines, text)
	}
	return frame, true
}

// sourceLang names the language of a file for the code frame's label and
// highlighting.
func sourceLang(p string) string {
	switch ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(p), ".")); ext {
	case "tsx", "ts", "jsx", "js", "css", "json", "md", "mdx", "go", "html", "svg", "yaml", "toml":
		return ext
	case "mjs", "cjs":
		return "js"
	case "mts", "cts":
		return "ts"
	case "yml":
		return "yaml"
	case "htm":
		return "html"
	}
	return "text"
}

// GET /__krate/dev/html?path=
func (d *devAPI) html(w http.ResponseWriter, r *http.Request) {
	route := devURLRoute(r.URL.Query().Get("path"), d.cfg.BaseURLPath())
	doc := builtDocPath(d.outDir, route)
	if doc == "" {
		if m, ok := readDevManifest(d.outDir); ok {
			if _, pattern, _, ok := matchDevPage(d.root, d.cfg, m, route); ok {
				doc = builtDocPath(d.outDir, pattern)
			}
		}
	}
	if doc == "" || !pluginapi.WithinRoot(d.outDir, doc) {
		devError(w, http.StatusNotFound, "no built document for "+route)
		return
	}
	data, err := os.ReadFile(doc)
	if err != nil {
		devError(w, http.StatusNotFound, "no built document for "+route)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

// DevFixChange is one safe rewrite in the fix endpoints.
type DevFixChange struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Code string `json:"code"`
	From string `json:"from"`
	To   string `json:"to"`
}

func devFixChanges(changes []fix.Change) []DevFixChange {
	out := make([]DevFixChange, 0, len(changes))
	for _, c := range changes {
		out = append(out, DevFixChange{File: filepath.ToSlash(c.File), Line: c.Line, Col: c.Col, Code: string(c.Code), From: c.From, To: c.To})
	}
	return out
}

// fixFile resolves the file a fix request names to its project-relative
// path.
func (d *devAPI) fixFile(file string) (string, bool) {
	abs, ok := safeProjectPath(d.root, file)
	if !ok {
		return "", false
	}
	rel, err := filepath.Rel(d.root, abs)
	if err != nil {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func fixCodes(code string) []string {
	if code = strings.TrimSpace(code); code == "" {
		return nil
	}
	return strings.Split(code, ",")
}

// GET /__krate/dev/fix?file=&code= previews; POST {file, code} or {all: true}
// applies.
func (d *devAPI) fix(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		q := r.URL.Query()
		rel, ok := d.fixFile(q.Get("file"))
		if !ok {
			devError(w, http.StatusNotFound, "no such project file")
			return
		}
		_, changes, err := fix.PlanFile(d.root, rel, fixCodes(q.Get("code")))
		if err != nil {
			devJSON(w, http.StatusOK, map[string]any{"changes": []DevFixChange{}, "skipped": err.Error()})
			return
		}
		devJSON(w, http.StatusOK, map[string]any{"changes": devFixChanges(changes)})
	case http.MethodPost:
		d.post(d.applyFix)(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		devError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (d *devAPI) applyFix(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File string `json:"file"`
		Code string `json:"code"`
		All  bool   `json:"all"`
	}
	if !decodeDevBody(w, r, &req) {
		return
	}
	if req.All {
		res, err := fix.Run(d.root, true)
		if err != nil {
			devError(w, http.StatusInternalServerError, err.Error())
			return
		}
		devJSON(w, http.StatusOK, map[string]any{"applied": true, "changes": devFixChanges(res.Changes)})
		return
	}
	rel, ok := d.fixFile(req.File)
	if !ok {
		devError(w, http.StatusNotFound, "no such project file")
		return
	}
	changes, err := fix.ApplyFile(d.root, rel, fixCodes(req.Code))
	if err != nil {
		devError(w, http.StatusConflict, err.Error())
		return
	}
	devJSON(w, http.StatusOK, map[string]any{"applied": len(changes) > 0, "changes": devFixChanges(changes)})
}

// POST /__krate/dev/action
func (d *devAPI) action(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		Route  string `json:"route"`
		Target string `json:"target"`
	}
	if !decodeDevBody(w, r, &req) {
		return
	}
	accepted := func() { devJSON(w, http.StatusAccepted, map[string]any{"ok": true, "action": req.Action}) }
	queue := func(a DevAction) {
		switch err := d.hub.RequestAction(a); {
		case err == nil:
			accepted()
		default:
			devError(w, http.StatusConflict, err.Error())
		}
	}
	switch req.Action {
	case "rebuild-page":
		m, ok := readDevManifest(d.outDir)
		if !ok {
			devError(w, http.StatusNotFound, "nothing is built yet")
			return
		}
		page, _, _, ok := matchDevPage(d.root, d.cfg, m, devURLRoute(req.Route, d.cfg.BaseURLPath()))
		if !ok || page.Source == "" {
			devError(w, http.StatusNotFound, "no page serves "+req.Route)
			return
		}
		abs, ok := safeProjectPath(d.root, page.Source)
		if !ok {
			devError(w, http.StatusNotFound, "the page has no source file in the project")
			return
		}
		queue(DevAction{Kind: "rebuild-page", File: abs})
	case "rebuild-all", "clear-cache":
		queue(DevAction{Kind: req.Action})
	case "restart-server":
		printf("  %srestarting (requested by the dev overlay)...%s\n", cYellow, cReset)
		accepted()
		d.hub.RequestRestart()
	case "restart-sidecar":
		if d.restartSidecar == nil {
			devError(w, http.StatusConflict, errSidecarNotRunning.Error())
			return
		}
		switch err := d.restartSidecar(req.Target); {
		case err == nil:
			accepted()
		case errors.Is(err, errSidecarUnknown):
			devError(w, http.StatusBadRequest, "target must be ssr, api or go")
		default:
			devError(w, http.StatusConflict, err.Error())
		}
	default:
		devError(w, http.StatusBadRequest, "unknown action")
	}
}
