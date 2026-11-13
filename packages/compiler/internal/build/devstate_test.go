package build

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestDevServerPublishesStateAndDiscoveryRecord(t *testing.T) {
	root, cfg := secProject(t)
	cfg.Compress.Gzip = false
	cfg.Redirects = nil
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	provideRuntime(t, root)
	port := freePort(t)
	cfg.Server.Port = port
	hub := NewDevHub()
	hub.Publish(DevEvent{Type: "reload", Diagnostics: []diag.Diagnostic{{File: "src/pages/x.tsx", Line: 2, Message: "broken", Code: "KR7115"}}})
	errCh := make(chan error, 1)
	go func() { errCh <- ServeDev(root, cfg, hub, time.Now()) }()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Registered first so it runs after the server has been stopped.
	t.Cleanup(func() {
		// registerServerStop cannot stop the server on Windows.
		if _, ok := ReadDevServerRecord(root); ok && runtime.GOOS != "windows" {
			t.Error("the discovery record must be removed when the dev server stops")
		}
	})
	deadline := time.Now().Add(30 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("dev server did not start")
		}
		select {
		case err := <-errCh:
			t.Fatalf("dev server exited: %v", err)
		default:
		}
		if resp, err := http.Get(base + "/"); err == nil {
			_ = resp.Body.Close()
			registerServerStop(t, errCh)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	rec, ok := ReadDevServerRecord(root)
	if !ok || rec.Port != port || rec.PID != os.Getpid() || !strings.HasPrefix(rec.URL, "http://") {
		t.Fatalf("discovery record = %+v ok=%v (port %d)", rec, ok, port)
	}

	post, _ := http.NewRequest("POST", base+"/__krate/client-error", strings.NewReader(`{"message":"TypeError: boom","url":"/p","line":4,"col":2}`))
	if resp, err := http.DefaultClient.Do(post); err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("client-error: %v %v", resp, err)
	}
	resp, err := http.Get(base + "/__krate/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var snap DevSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.BuildOK || len(snap.Diagnostics) != 1 || snap.Diagnostics[0].Code != "KR7115" || len(snap.ClientErrors) != 1 || snap.ClientErrors[0].Line != 4 {
		t.Fatalf("state = %+v", snap)
	}

	// The state endpoint is a dev endpoint: same guard as /__krate/status.
	req, _ := http.NewRequest("GET", base+"/__krate/state", nil)
	req.Host = "evil.example:80"
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != http.StatusForbidden {
		t.Errorf("DNS rebinding to /__krate/state = %v %v, want 403", r, err)
	}
	req, _ = http.NewRequest("GET", base+"/__krate/state", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site /__krate/state = %v %v, want 403", r, err)
	}
}
