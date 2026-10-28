package krate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRevalidateHelpers(t *testing.T) {
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/__krate/internal/revalidate" || r.Header.Get("X-Krate-Internal") != "s" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body)
		_, _ = w.Write([]byte(`{"revalidated":[{"target":"posts","kind":"tag"}],"skipped":[]}`))
	}))
	defer srv.Close()

	t.Setenv("KRATE_HOST_URL", "")
	if err := RevalidateTag("posts"); err == nil || !strings.Contains(err.Error(), "KRATE_HOST_URL") {
		t.Fatalf("want an error outside the Krate server, got %v", err)
	}

	t.Setenv("KRATE_HOST_URL", srv.URL)
	t.Setenv("KRATE_INTERNAL_SECRET", "s")
	if err := RevalidateTag("posts"); err != nil {
		t.Fatal(err)
	}
	if err := RevalidatePath("/blog", "layout"); err != nil {
		t.Fatal(err)
	}
	if err := RevalidatePath("blog"); err == nil {
		t.Fatal("relative path must be rejected")
	}
	if len(got) != 2 {
		t.Fatalf("host calls = %d, want 2", len(got))
	}
	paths := got[1]["paths"].([]any)
	p := paths[0].(map[string]any)
	if p["path"] != "/blog" || p["type"] != "layout" {
		t.Fatalf("path payload = %v", p)
	}

	t.Setenv("KRATE_INTERNAL_SECRET", "wrong")
	if err := RevalidateTag("posts"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("want a 403 error, got %v", err)
	}
}
