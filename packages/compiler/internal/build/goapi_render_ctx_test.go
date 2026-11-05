package build

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A visitor that disconnects must not leave the Go host render running until
// the client timeout.
func TestGoRenderStopsWhenRequestContextEnds(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	g := &goAPISupervisor{
		client:      &http.Client{Timeout: 30 * time.Second},
		renderURL:   srv.URL,
		regionCache: map[string]regionCacheEntry{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := g.RenderContext(ctx, []goRenderRegion{{ID: "r1", Name: "Card"}})
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled render must report an error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the Go host call kept running after its request context ended")
	}
}
