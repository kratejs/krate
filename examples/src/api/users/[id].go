package api

import (
	"net/http"

	"krate-goapi/runtime"
)

// Handler handles all methods on /api/users/{id} and reads the dynamic segment.
func Handler(w http.ResponseWriter, r *http.Request) {
	runtime.WriteJSON(w, 200, map[string]interface{}{
		"id":     r.PathValue("id"),
		"method": r.Method,
	})
}
