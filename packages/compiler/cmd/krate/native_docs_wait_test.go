//go:build linux

package main

import (
	"fmt"
	"testing"
	"time"
)

// A page whose URL already changed but whose document is still loading (a
// large docs page on a slow runner) is waited for, not reported as not loaded.
func TestWaitPageLoadedWaitsForComplete(t *testing.T) {
	answers := []string{`ok "/docs/ loading"`, `ok "/docs/ interactive"`, `ok "/docs/ complete"`}
	n := 0
	send := func(string, time.Duration) (string, bool) {
		l := answers[min(n, len(answers)-1)]
		n++
		return l, true
	}
	if got, ok := waitPageLoaded(send, "/docs/"); !ok {
		t.Fatalf("gave up on a loading page: %q after %d queries", got, n)
	}
	stuck := func(string, time.Duration) (string, bool) { return fmt.Sprintf("ok %q", "/docs/ loading"), true }
	if got, ok := waitPageLoaded(stuck, "/docs/"); ok {
		t.Fatalf("a page that never completes passed: %q", got)
	}
}
