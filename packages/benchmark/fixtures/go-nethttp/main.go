package main

import (
	"encoding/json"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	payload, _ := json.Marshal(map[string]interface{}{"message": "hello", "id": 42})

	http.HandleFunc("/api/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(payload)
	})

	http.ListenAndServe("127.0.0.1:"+port, nil)
}
