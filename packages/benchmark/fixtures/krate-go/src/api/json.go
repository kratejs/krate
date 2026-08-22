package api

import (
	"net/http"

	"krate-goapi/runtime"
)

func GET(w http.ResponseWriter, r *http.Request) {
	runtime.WriteJSON(w, 200, map[string]interface{}{
		"message": "hello",
		"id":      42,
	})
}
