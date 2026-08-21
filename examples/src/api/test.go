package api

import (
	"fmt"
	"net/http"
	"time"

	"krate-goapi/runtime"
)

// GET handles GET /api/hello — a simple Go API route.
func GET(w http.ResponseWriter, r *http.Request) {
	runtime.WriteJSON(w, 200, map[string]interface{}{
		"message": "Hello from Go API!",
		"time":    time.Now().Unix(),
		"method":  r.Method,
	})
}

// POST handles POST /api/hello — echoes the request body back.
func POST(w http.ResponseWriter, r *http.Request) {
	buf := make([]byte, 4096)
	n, _ := r.Body.Read(buf)
	runtime.WriteJSON(w, 201, map[string]interface{}{
		"received": string(buf[:n]),
	})
}

var _ = fmt.Sprintf


