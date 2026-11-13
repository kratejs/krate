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
