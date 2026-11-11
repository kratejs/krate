package shell

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	tokenParam  = "krate_token"
	tokenHeader = "X-Krate-Token"
)

// tokenCookieName is per port: cookies are shared by every port of a host, so
// two sessions on 127.0.0.1 would otherwise overwrite each other's token.
func tokenCookieName(port string) string { return "krate_token_" + port }

// sameOriginRequest reports whether a browser request that carries the cookie
// came from the app page itself. SameSite does not help here: another dev
// server on localhost (any port) is the same site, and the cookie would be sent
// with its cross-origin POSTs. A request without Origin and Sec-Fetch-Site
// headers is not from a browser page (curl, the shell's own client).
func sameOriginRequest(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
		return false
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	}
	return false
}

// guardHandler admits only requests that carry the session token, and only
// when addressed to a loopback name on the listener's port (which stops DNS
// rebinding). A visit to any URL with ?krate_token=... sets the cookie and
// redirects to the same URL without it.
func guardHandler(next http.Handler, token string, port string) http.Handler {
	hosts := map[string]bool{
		net.JoinHostPort("127.0.0.1", port): true,
		net.JoinHostPort("localhost", port): true,
		net.JoinHostPort("::1", port):       true,
	}
	cookieName := tokenCookieName(port)
	valid := func(got string) bool {
		return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[r.Host] {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		q := r.URL.Query()
		if t := q.Get(tokenParam); t != "" {
			if !valid(t) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: cookieName, Value: token, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
			q.Del(tokenParam)
			dest := url.URL{Path: r.URL.Path, RawQuery: q.Encode()}
			if dest.Path == "" {
				dest.Path = "/"
			}
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, dest.String(), http.StatusFound)
			return
		}
		if c, err := r.Cookie(cookieName); err == nil && valid(c.Value) {
			if !sameOriginRequest(r) {
				http.Error(w, "forbidden: cross-origin request", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if valid(r.Header.Get(tokenHeader)) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "forbidden: open the URL printed by krate-shell", http.StatusForbidden)
	})
}

func newToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (a *app) runBrowser() error {
	a.attachSDK(nil)
	addr := a.opts.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("shell: listening on %s: %w", addr, err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	token, err := newToken()
	if err != nil {
		ln.Close()
		return err
	}
	srv := &http.Server{Handler: guardHandler(a.handler, token, port)}
	var once sync.Once
	stopped := make(chan struct{})
	a.quit = func() { once.Do(func() { close(stopped) }) }
	a.ctl.url = fmt.Sprintf("http://127.0.0.1:%s/?%s=%s", port, tokenParam, token)

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	fmt.Fprintf(a.opts.Out, "url %s\n", a.ctl.url)
	if a.opts.OnReady != nil {
		go a.opts.OnReady(a.ctl)
	}
	var ctxDone <-chan struct{}
	if a.opts.Context != nil {
		ctxDone = a.opts.Context.Done()
	}
	select {
	case <-stopped:
	case <-ctxDone:
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	cctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = srv.Shutdown(cctx)
	_ = srv.Close()
	return nil
}
