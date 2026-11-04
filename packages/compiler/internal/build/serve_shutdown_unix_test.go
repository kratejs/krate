//go:build !windows

package build

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"
)

// TestServeGracefulShutdownDrains starts a slow API request, signals the
// server mid-flight and requires the request to complete and Serve to return
// nil (clean exit).
func TestServeGracefulShutdownDrains(t *testing.T) {
	e2eEnv(t)
	b, root := buildW5(t)
	port := freePort(t)
	b.Cfg.Server.Port = port
	errCh := make(chan error, 1)
	go func() { errCh <- Serve(root, b.Cfg, time.Now()) }()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
	type result struct {
		status int
		body   string
		err    error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get(base + "/api/slow")
		if err != nil {
			got <- result{err: err}
			return
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		got <- result{status: resp.StatusCode, body: string(data)}
	}()
	time.Sleep(700 * time.Millisecond)
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	select {
	case r := <-got:
		if r.err != nil || r.status != 200 || r.body != "finished" {
			t.Fatalf("in-flight request was not drained: %+v", r)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("slow request never finished")
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve returned %v after a clean shutdown, want nil", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Serve did not return after shutdown")
	}
}
