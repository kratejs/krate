package krate

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCtx(t *testing.T) {
	if Background().Window() != "" {
		t.Fatal("Background has no window")
	}
	parent, cancel := context.WithCancel(context.Background())
	c := WithWindow(parent, "w1")
	if c.Window() != "w1" {
		t.Fatal(c.Window())
	}
	cancel()
	select {
	case <-c.Done():
	default:
		t.Fatal("cancel must propagate")
	}
}

func TestErrorf(t *testing.T) {
	cause := io.ErrUnexpectedEOF
	err := Errorf("bad_input", "field %q: %w", "title", cause)
	var ke *Error
	if !errors.As(err, &ke) || ke.Code != "bad_input" || ke.Message != `field "title": unexpected EOF` {
		t.Fatalf("%#v", ke)
	}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is must see the wrapped error")
	}
	plain := Errorf("x", "no wrap %d", 1)
	if errors.Unwrap(plain) != nil || plain.Error() != "x: no wrap 1" {
		t.Fatalf("%v", plain)
	}
	wrapped := Errorf("outer", "ctx: %w", err)
	if !errors.As(wrapped, &ke) || ke.Code != "outer" {
		t.Fatal("outermost *Error wins")
	}
	ke.Data = map[string]int{"n": 1}
	if (&Error{Code: "c"}).Error() != "c" {
		t.Fatal("empty message")
	}
}

func TestSignalGetSet(t *testing.T) {
	s := NewSignal(1)
	s.Set(2)
	s.Update(func(v int) int { return v * 10 })
	if s.Get() != 20 {
		t.Fatal(s.Get())
	}
	var zero Signal[string]
	if zero.Get() != "" {
		t.Fatal("zero value")
	}
	zero.Set("a")
	if zero.Get() != "a" {
		t.Fatal("zero value Set")
	}
}

func TestSubscribeReceivesLaterValues(t *testing.T) {
	s := NewSignal(0)
	got := make(chan int, 4)
	cancel := s.Subscribe(func(v int) { got <- v })
	defer cancel()
	select {
	case v := <-got:
		t.Fatalf("Subscribe must not deliver the current value, got %d", v)
	case <-time.After(20 * time.Millisecond):
	}
	s.Set(1)
	if v := <-got; v != 1 {
		t.Fatal(v)
	}
	s.Update(func(v int) int { return v + 4 })
	if v := <-got; v != 5 {
		t.Fatal(v)
	}
}

func TestWatchDeliversCurrentFirst(t *testing.T) {
	s := NewSignal("a")
	got := make(chan string, 4)
	cancel := s.Watch(func(v string) { got <- v })
	defer cancel()
	if v := <-got; v != "a" {
		t.Fatal(v)
	}
	s.Set("b")
	if v := <-got; v != "b" {
		t.Fatal(v)
	}
}

func TestSubscribeCoalescesForSlowSubscriber(t *testing.T) {
	s := NewSignal(0)
	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var seen []int
	done := make(chan struct{})
	var once sync.Once
	cancel := s.Subscribe(func(v int) {
		once.Do(func() { close(started) })
		<-release
		mu.Lock()
		seen = append(seen, v)
		last := v == 100
		mu.Unlock()
		if last {
			close(done)
		}
	})
	defer cancel()

	s.Set(1)
	<-started // the subscriber is now blocked inside fn with value 1
	setDone := make(chan struct{})
	go func() {
		for i := 2; i <= 100; i++ {
			s.Set(i)
		}
		close(setDone)
	}()
	select {
	case <-setDone: // Set must not block on the slow subscriber
	case <-time.After(5 * time.Second):
		t.Fatal("Set blocked on a slow subscriber")
	}
	close(release)
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != 1 || seen[1] != 100 {
		t.Fatalf("want [1 100], got %v", seen)
	}
}

func TestCancelStopsDelivery(t *testing.T) {
	s := NewSignal(0)
	var n atomic.Int32
	cancel := s.Subscribe(func(int) { n.Add(1) })
	cancel()
	cancel() // idempotent
	s.Set(1)
	time.Sleep(20 * time.Millisecond)
	if n.Load() != 0 {
		t.Fatal("delivered after cancel")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.subs) != 0 {
		t.Fatal("subscriber leaked")
	}
}

func TestConcurrentUpdate(t *testing.T) {
	s := NewSignal(0)
	var last atomic.Int64
	cancel := s.Subscribe(func(v int) { last.Store(int64(v)) })
	defer cancel()
	const workers, per = 16, 500
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range per {
				s.Update(func(v int) int { return v + 1 })
				_ = s.Get()
			}
		}()
	}
	wg.Wait()
	if s.Get() != workers*per {
		t.Fatalf("lost updates: %d", s.Get())
	}
	deadline := time.Now().Add(5 * time.Second)
	for last.Load() != workers*per {
		if time.Now().After(deadline) {
			t.Fatalf("subscriber ended at %d, want the final value", last.Load())
		}
		time.Sleep(time.Millisecond)
	}
}
