package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

func subscribeInts(t *testing.T, tp *Topic) (<-chan int64, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan int64, 64)
	go func() {
		args, _ := wire.NewDecoder([]byte("\x00\x00\x00\x02[]\x00\x00"))
		_ = tp.Stream()(krate.WithWindow(ctx, ""), args, func(write func(*wire.Encoder)) error {
			e := wire.NewEncoder()
			write(e)
			b, _ := e.Finish()
			d, _ := wire.NewDecoder(b)
			n, _ := d.Int()
			got <- n
			return nil
		})
	}()
	return got, cancel
}

func TestPublishRetainedWaitsForTheFirstSubscriber(t *testing.T) {
	var tp Topic
	tp.Publish(func(e *wire.Encoder) { e.Int(1) }) // plain events are not kept
	tp.PublishRetained(func(e *wire.Encoder) { e.Int(2) })
	tp.PublishRetained(func(e *wire.Encoder) { e.Int(3) })
	got, cancel := subscribeInts(t, &tp)
	defer cancel()
	for _, want := range []int64{2, 3} {
		select {
		case n := <-got:
			if n != want {
				t.Fatalf("got %d, want %d", n, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("event %d was not delivered to the subscriber", want)
		}
	}
	// Delivered once: a later subscriber does not see them again.
	got2, cancel2 := subscribeInts(t, &tp)
	defer cancel2()
	select {
	case n := <-got2:
		t.Fatalf("event %d was replayed", n)
	case <-time.After(100 * time.Millisecond):
	}
	// With a subscriber present a retained publish is just a publish.
	tp.PublishRetained(func(e *wire.Encoder) { e.Int(4) })
	select {
	case n := <-got:
		if n != 4 {
			t.Fatalf("got %d, want 4", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("live event lost")
	}
}

func TestPublishRetainedKeepsTheNewest(t *testing.T) {
	var tp Topic
	for i := int64(0); i < maxRetained+3; i++ {
		i := i
		tp.PublishRetained(func(e *wire.Encoder) { e.Int(i) })
	}
	got, cancel := subscribeInts(t, &tp)
	defer cancel()
	select {
	case n := <-got:
		if n != 3 {
			t.Fatalf("first kept event %d, want 3", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing delivered")
	}
}
