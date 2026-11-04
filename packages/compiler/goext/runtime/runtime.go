// Package runtime provides helpers for krate Go API route handlers. Import it as
//
//	runtime "github.com/kratejs/krate/packages/compiler/goext/runtime"
package runtime

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
)

// recoverPanics turns a panicking handler into a logged 500 JSON response
// instead of a dropped connection (which the main server would report as an
// empty 502).
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				fmt.Fprintf(os.Stderr, "krate: Go API panic on %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"Internal Server Error"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error writes a JSON error response.
func Error(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// Serve starts the API server and blocks until the process receives a
// termination signal. It listens on the AF_UNIX socket in KRATE_GOAPI_SOCKET
// when set (the main server dials it over UDS), otherwise on the
// KRATE_GOAPI_PORT loopback port.
func Serve(mux *http.ServeMux) {
	srv := &http.Server{Handler: recoverPanics(mux)}
	if sock := os.Getenv("KRATE_GOAPI_SOCKET"); sock != "" {
		// The parent dials exactly this socket, so a failure here must be
		// loud: silently falling back to TCP would leave it dialing nothing.
		_ = os.Remove(sock)
		ln, err := net.Listen("unix", sock)
		if err != nil {
			fmt.Fprintf(os.Stderr, "krate: Go API host cannot listen on %s: %v\n", sock, err)
			os.Exit(1)
		}
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		go func() { <-stop; _ = srv.Close() }()
		_ = srv.Serve(ln)
		_ = os.Remove(sock)
		return
	}
	port := os.Getenv("KRATE_GOAPI_PORT")
	if port == "" {
		port = "3002"
	}
	srv.Addr = "127.0.0.1:" + port
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch
		_ = srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "krate: Go API host cannot listen on %s: %v\n", srv.Addr, err)
		os.Exit(1)
	}
}
