package build

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// requestRoute maps a URL path to the route form used by the manifest: no
// trailing slash, "/" for the root.
func requestRoute(urlPath string) string {
	route := strings.TrimRight(urlPath, "/")
	if route == "" {
		return "/"
	}
	return route
}

// isFileLikePath reports whether the last segment of a URL path has a file
// extension (/feed.xml, /sitemap.xml); such routes have no trailing slash form.
func isFileLikePath(p string) bool {
	return strings.Contains(path.Base(p), ".")
}

// CanonicalPagePath returns the single canonical URL path for a page route:
// the server redirects every other spelling to it. Directory-style pages end
// in "/" (the root is "/"); file-like routes (/feed.xml) are unchanged. The
// configured base path is prefixed when cfg is non-nil. Canonical link tags,
// sitemaps and redirects should all go through this.
func CanonicalPagePath(route string, cfg *config.Config) string {
	p := "/" + strings.Trim(route, "/")
	if p != "/" && !isFileLikePath(p) {
		p += "/"
	}
	if cfg != nil {
		if base := cfg.BaseURLPath(); base != "" {
			if p == "/" {
				return base + "/"
			}
			return base + p
		}
	}
	return p
}

// rewriteTargetPath is the internal path a rewrite (middleware
// x-middleware-rewrite or config rewrites) serves. Page URLs are canonical with
// a trailing slash and the server redirects the slashless spelling, so a rewrite
// to "/feedback" would answer 301 and change the visible URL. Serving the
// canonical form directly keeps the rewrite invisible. API routes, internal
// paths and file-like paths are left alone.
func rewriteTargetPath(p string) string {
	if p == "" || p == "/" || strings.HasSuffix(p, "/") || isFileLikePath(p) {
		return p
	}
	if p == "/api" || strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/__krate/") {
		return p
	}
	return p + "/"
}

// redirectToCanonicalSlash answers a GET/HEAD for a directory-style page URL
// without its trailing slash with a 301 to the slash form (query preserved),
// matching what the static file server does for SSG pages. It reports whether
// it wrote the response.
//
// Only trailingSlash "always" (the default) redirects this way; "never" and
// "ignore" serve the slashless URL in place.
func redirectToCanonicalSlash(mode string, w http.ResponseWriter, r *http.Request) bool {
	if mode != "always" {
		return false
	}
	p := r.URL.Path
	if p == "" || strings.HasSuffix(p, "/") || isFileLikePath(p) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	// A relative Location stays correct under a base path. The "./" matters: a
	// last segment like "ftp:3232235777" or "\\host" would otherwise parse as
	// an absolute or scheme-relative URL and turn the redirect off-site.
	target := "./" + path.Base(p) + "/"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusMovedPermanently)
	return true
}

type requestedPathKey struct{}

// requestedPath is the path the client asked for: a middleware rewrite
// changes r.URL.Path but the slash rule judges the original URL.
func requestedPath(r *http.Request) string {
	if p, ok := r.Context().Value(requestedPathKey{}).(string); ok {
		return p
	}
	return r.URL.Path
}

// slashlessTarget returns where trailingSlash "never" sends a request, or ""
// when the URL has no trailing slash. Like Next, the rule is judged on the URL
// alone, before middleware, config redirects and routing: any path ending in
// "/" (pages, files, API routes, any method) goes to the slashless path. The
// not-found page's own URLs and Krate's internal routes are left alone. The
// target is root-relative and keeps the escaped path and the query.
func slashlessTarget(r *http.Request) string {
	p := r.URL.Path
	if len(p) < 2 || !strings.HasSuffix(p, "/") || isInternalRoutePath(p) || strings.HasPrefix(p, "/__krate") || notFoundPageURL(p) {
		return ""
	}
	target := strings.TrimRight(r.URL.EscapedPath(), "/")
	// Collapse leading slashes: "//evil.example/" must not become a
	// scheme-relative Location.
	target = "/" + strings.TrimLeft(target, "/\\")
	if target == "/" {
		return ""
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	return target
}

// frontRedirects answers the trailingSlash "never" redirect (308, like Next's)
// and then the config redirects, in front of middleware and routing, so
// middleware only sees canonical URLs. "always" keeps its slash redirect on
// the page handlers, behind middleware and rewrites.
func frontRedirects(mode string, redirects []config.Redirect, next http.Handler) http.Handler {
	next = configRedirectMiddleware(redirects, next)
	if mode != "never" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if target := slashlessTarget(r); target != "" {
			w.Header().Set("Location", target)
			w.WriteHeader(http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// serveIndexInPlace lets the file server answer a folder URL without its
// trailing slash from the folder's index.html, instead of its own redirect to
// the slash form. It is what trailingSlash "never" and "ignore" need.
func serveIndexInPlace(absOut string, next http.Handler) http.Handler {
	idx := lookupOutIndex(absOut)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && p != "" && !strings.HasSuffix(p, "/") && !isFileLikePath(p) {
			ok := false
			if idx != nil {
				ok = idx.isFile(relJoin(cleanRel(p), "index.html"))
			} else {
				index := filepath.Join(absOut, filepath.FromSlash(path.Clean("/"+p)), "index.html")
				fi, err := os.Stat(index)
				ok = err == nil && fi.Mode().IsRegular()
			}
			if ok {
				r2 := r.Clone(r.Context())
				r2.URL.Path = p + "/"
				if r2.URL.RawPath != "" {
					r2.URL.RawPath += "/"
				}
				next.ServeHTTP(w, r2)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// notFoundPageURL reports whether p is the URL of the not-found page itself.
// Those answer 404 like any missing page, never the file with 200 or a redirect.
func notFoundPageURL(p string) bool {
	return p == "/404" || p == "/404/" || p == "/404.html"
}

// pageMethodAllowed enforces that page routes are read-only. OPTIONS answers
// with the allowed methods; anything else but GET and HEAD is a 405. It
// reports whether the request may proceed.
func pageMethodAllowed(w http.ResponseWriter, r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return true
	case http.MethodOptions:
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

// devBindHost is the address the dev server listens on: devServer.host, then
// server.host, then loopback so the /__krate/* endpoints are not reachable from
// the network unless the developer asks for it.
func devBindHost(cfg *config.Config) string {
	if h := strings.TrimSpace(cfg.DevServer.Host); h != "" {
		return h
	}
	if h := strings.TrimSpace(cfg.Server.Host); h != "" {
		return h
	}
	return "127.0.0.1"
}

func isWildcardHost(h string) bool {
	switch strings.Trim(h, "[]") {
	case "0.0.0.0", "::", "":
		return true
	}
	return false
}

func isLoopbackHostname(h string) bool {
	h = strings.ToLower(strings.Trim(h, "[]"))
	return h == "localhost" || strings.HasSuffix(h, ".localhost") || h == "::1" || h == "127.0.0.1" || strings.HasPrefix(h, "127.")
}

// hostnameOf strips the port from a Host header value.
func hostnameOf(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.Trim(hostport, "[]")
}

// devEndpointGuard protects the dev-only /__krate/* endpoints (open in editor,
// client errors, status, live reload) from other web pages and DNS rebinding.
// The Host must be a loopback name or the configured bind address (any Host
// when the developer bound a wildcard address on purpose), and a browser's
// Origin or Sec-Fetch-Site must say the request comes from the dev server
// itself. Requests without browser headers (curl) pass the Host check only.
func devEndpointGuard(bindHost string, next http.Handler) http.Handler {
	anyHost := isWildcardHost(bindHost)
	bindName := strings.ToLower(hostnameOf(bindHost))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.ToLower(hostnameOf(r.Host))
		if !anyHost && !isLoopbackHostname(name) && name != bindName {
			http.Error(w, "forbidden: unexpected Host for a dev endpoint", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(u.Host, r.Host) {
				http.Error(w, "forbidden: cross-origin request to a dev endpoint", http.StatusForbidden)
				return
			}
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "forbidden: cross-site request to a dev endpoint", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
