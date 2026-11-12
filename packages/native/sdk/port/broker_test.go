package port

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
)

func code(err error) string {
	var ke *krate.Error
	if errors.As(err, &ke) {
		return ke.Code
	}
	return ""
}

type got struct {
	frames chan []byte
	closed chan bool
}

func listen(t *testing.T, b *Broker, caller, id string) *got {
	t.Helper()
	g := &got{closed: make(chan bool, 1), frames: make(chan []byte, 64)}
	go func() {
		closed, _ := b.Listen(context.Background(), caller, id, func(f []byte) error { g.frames <- f; return nil })
		g.closed <- closed
	}()
	return g
}

func (g *got) next(t *testing.T) string {
	t.Helper()
	select {
	case f := <-g.frames:
		return string(f)
	case <-time.After(3 * time.Second):
		t.Fatal("no message")
	}
	return ""
}

func TestPostIsDeliveredInOrderToTheOtherEnd(t *testing.T) {
	b := NewBroker()
	a, c := b.Channel("v1")
	for _, m := range []string{"1", "2", "3"} {
		if err := b.Post("v1", a, []byte(m)); err != nil {
			t.Fatal(err)
		}
	}
	g := listen(t, b, "v1", c)
	for _, m := range []string{"1", "2", "3"} {
		if s := g.next(t); s != m {
			t.Fatalf("got %q want %q", s, m)
		}
	}
	if err := b.Post("v1", c, []byte("back")); err != nil {
		t.Fatal(err)
	}
	g2 := listen(t, b, "v1", a)
	if s := g2.next(t); s != "back" {
		t.Fatal(s)
	}
}

func TestOnlyTheHolderMayUseAnEnd(t *testing.T) {
	b := NewBroker()
	a, c := b.Channel("v1")
	if err := b.Post("v2", a, []byte("x")); code(err) != "permission_denied" {
		t.Fatalf("post by a stranger: %v", err)
	}
	if err := b.Transfer("v2", c, "v2"); code(err) != "permission_denied" {
		t.Fatalf("transfer by a stranger: %v", err)
	}
	if _, err := b.Listen(context.Background(), "v2", a, func([]byte) error { return nil }); code(err) != "permission_denied" {
		t.Fatalf("listen by a stranger: %v", err)
	}
	// Host code ("") may act on any end.
	if err := b.Post("", a, []byte("x")); err != nil {
		t.Fatal(err)
	}
}

func TestTransferNeedsAReceiver(t *testing.T) {
	b := NewBroker()
	b.CanReceive = func(o string) bool { return o == "ok" }
	b.Alive = func(o string) bool { return o != "gone" }
	_, c := b.Channel("v1")
	if err := b.Transfer("v1", c, "nope"); code(err) != "permission_denied" {
		t.Fatalf("a view without port:receive got an end: %v", err)
	}
	if err := b.Transfer("v1", c, "gone"); code(err) != "not_found" {
		t.Fatalf("a missing view got an end: %v", err)
	}
	if err := b.Transfer("v1", c, WorkerOwnerPrefix+"w1"); code(err) != "not_found" {
		t.Fatalf("worker without a checker: %v", err)
	}
	if err := b.Transfer("v1", c, "ok"); err != nil {
		t.Fatal(err)
	}
	if o, _ := b.Owner(c); o != "ok" {
		t.Fatal(o)
	}
	// v1 lost the end.
	if err := b.Post("v1", c, []byte("x")); code(err) != "permission_denied" {
		t.Fatal(err)
	}
	b.Worker = func(o string) (bool, bool) { return true, o == WorkerOwnerPrefix+"w1" }
	_, c2 := b.Channel("v1")
	if err := b.Transfer("v1", c2, WorkerOwnerPrefix+"w2"); code(err) != "permission_denied" {
		t.Fatal(err)
	}
	if err := b.Transfer("v1", c2, WorkerOwnerPrefix+"w1"); err != nil {
		t.Fatal(err)
	}
}

func TestQueuedMessagesFollowTheEnd(t *testing.T) {
	b := NewBroker()
	a, c := b.Channel("v1")
	_ = b.Post("v1", a, []byte("early"))
	if err := b.Transfer("v1", c, "v2"); err != nil {
		t.Fatal(err)
	}
	g := listen(t, b, "v2", c)
	if s := g.next(t); s != "early" {
		t.Fatal(s)
	}
}

func TestTransferEndsTheOldListener(t *testing.T) {
	b := NewBroker()
	_, c := b.Channel("v1")
	g := listen(t, b, "v1", c)
	if err := b.Transfer("v1", c, "v2"); err != nil {
		t.Fatal(err)
	}
	select {
	case closed := <-g.closed:
		if closed {
			t.Fatal("moving an end is not a close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the old listener kept running")
	}
}

func TestCloseDrainsThenReportsClosed(t *testing.T) {
	b := NewBroker()
	a, c := b.Channel("v1")
	_ = b.Post("v1", a, []byte("last"))
	if err := b.Close("v1", a); err != nil {
		t.Fatal(err)
	}
	if err := b.Post("v1", a, []byte("x")); code(err) != "closed" && code(err) != "not_found" {
		t.Fatalf("post after close: %v", err)
	}
	g := listen(t, b, "v1", c)
	if s := g.next(t); s != "last" {
		t.Fatal(s)
	}
	select {
	case closed := <-g.closed:
		if !closed {
			t.Fatal("expected closed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no close")
	}
	if b.Count() != 0 {
		t.Fatalf("%d ends leaked", b.Count())
	}
}

func TestCloseWithNobodyListeningFreesTheEnds(t *testing.T) {
	b := NewBroker()
	a, _ := b.Channel("v1")
	_ = b.Close("v1", a)
	if b.Count() != 0 {
		t.Fatalf("%d ends leaked", b.Count())
	}
}

func TestBackpressureAndLimits(t *testing.T) {
	b := NewBroker()
	a, _ := b.Channel("v1")
	for i := 0; i < MaxQueued; i++ {
		if err := b.Post("v1", a, []byte("m")); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	if err := b.Post("v1", a, []byte("m")); code(err) != "port_full" {
		t.Fatalf("expected port_full, got %v", err)
	}
	if err := b.Post("v1", a, make([]byte, MaxMessage+1)); code(err) != "too_large" {
		t.Fatalf("expected too_large, got %v", err)
	}
}

func TestDropOwnerClosesItsChannels(t *testing.T) {
	b := NewBroker()
	_, c := b.Channel("v1")
	a2, c2 := b.Channel("v2")
	_ = b.Transfer("v2", c2, "v1")
	b.DropOwner("v1")
	if _, ok := b.Owner(c); ok {
		t.Fatal("the dropped owner's end is still there")
	}
	if err := b.Post("v2", a2, []byte("x")); err == nil {
		t.Fatal("posting to a dropped peer should fail")
	}
}

func TestSecondListenReplacesTheFirst(t *testing.T) {
	b := NewBroker()
	a, c := b.Channel("v1")
	g1 := listen(t, b, "v1", c)
	time.Sleep(20 * time.Millisecond)
	g2 := listen(t, b, "v1", c)
	select {
	case <-g1.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("first listener kept running")
	}
	_ = b.Post("v1", a, []byte("hi"))
	if s := g2.next(t); s != "hi" {
		t.Fatal(s)
	}
}
