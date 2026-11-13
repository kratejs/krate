package krate

import (
	"testing"
	"time"
)

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
