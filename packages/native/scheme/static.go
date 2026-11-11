package scheme

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// mimeTypes covers what a built site contains; the platform mime table is
// only a fallback because it varies by OS (the Windows registry in
// particular).
var mimeTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".htm":         "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".map":         "application/json",
	".webmanifest": "application/manifest+json",
	".txt":         "text/plain; charset=utf-8",
	".xml":         "application/xml",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".gif":         "image/gif",
	".webp":        "image/webp",
	".avif":        "image/avif",
	".ico":         "image/x-icon",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".ttf":         "font/ttf",
	".otf":         "font/otf",
	".wasm":        "application/wasm",
	".mp3":         "audio/mpeg",
	".mp4":         "video/mp4",
	".webm":        "video/webm",
	".pdf":         "application/pdf",
}

func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if t, ok := mimeTypes[ext]; ok {
		return t
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

func (s *server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	urlPath := r.URL.Path
	if urlPath == "" || urlPath[0] != '/' || strings.ContainsAny(urlPath, "\\\x00") {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	for _, seg := range strings.Split(urlPath, "/") {
		if seg == ".." {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
	}
	if s.o.Assets == nil {
		http.NotFound(w, r)
		return
	}

	name := strings.Trim(path.Clean(urlPath), "/")
	var candidates []string
	if name == "" {
		candidates = []string{"index.html"}
	} else {
		candidates = []string{name, name + "/index.html", name + ".html"}
	}
	for _, c := range candidates {
		if s.serveFile(w, r, c, http.StatusOK) {
			return
		}
	}
	if s.o.Fallback != nil {
		if file, ok := s.o.Fallback(path.Clean(urlPath)); ok && s.serveFile(w, r, file, http.StatusOK) {
			return
		}
	}
	if s.serveFile(w, r, "404.html", http.StatusNotFound) {
		return
	}
	http.NotFound(w, r)
}

// serveFile writes name from the assets if it is a regular file.
func (s *server) serveFile(w http.ResponseWriter, r *http.Request, name string, status int) bool {
	name = strings.TrimPrefix(name, "/")
	if !fs.ValidPath(name) {
		return false
	}
	f, err := s.o.Assets.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return false
	}
	h := w.Header()
	h.Set("Content-Type", contentType(name))
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		io.Copy(w, f)
	}
	return true
}
