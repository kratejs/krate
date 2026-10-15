// Package runtime provides helpers for krate Go API route handlers. Import it as
//
//	runtime "github.com/kratejs/krate/packages/compiler/goext/runtime"
package runtime

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

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
	srv := &http.Server{Handler: mux}
	if sock := os.Getenv("KRATE_GOAPI_SOCKET"); sock != "" {
		_ = os.Remove(sock)
		ln, err := net.Listen("unix", sock)
		if err == nil {
			stop := make(chan os.Signal, 1)
			signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
			go func() { <-stop; _ = srv.Close() }()
			_ = srv.Serve(ln)
			_ = os.Remove(sock)
			return
		}
		// Fall through to loopback TCP if the socket could not be created.
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
	_ = srv.ListenAndServe()
}
