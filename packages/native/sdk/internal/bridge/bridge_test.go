package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

func TestTableDuplicatePanics(t *testing.T) {
	tb := NewTable()
	f := func(krate.Ctx, *wire.Decoder, *wire.Encoder) error { return nil }
	tb.Func("a", "p", true, f)
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate id accepted")
		}
	}()
	tb.Func("a", "p", true, f)
}

func TestRegisterUIFiltersByPermissionAndUIFlag(t *testing.T) {
	tb := NewTable()
	f := func(krate.Ctx, *wire.Decoder, *wire.Encoder) error { return nil }
	tb.Func("ok", "p1", true, f)
	tb.Func("denied", "p2", true, f)
	tb.Func("goonly", "p1", false, f)
	reg := scheme.NewRegistry()
	tb.RegisterUI(reg, func(p string) bool { return p == "p1" }, func(p string) error { return krate.Errorf("permission_denied", "needs %s", p) })
	// "denied" is a stub (registered), "goonly" is absent.
	reg.Func("goonly", f)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("denied stub was not registered")
			}
		}()
		reg.Func("denied", f)
	}()
	defer func() {
		if recover() == nil {
			t.Fatal("ok was not registered")
		}
	}()
	reg.Func("ok", f)
}

func TestTopicDropsOldestWhenSlow(t *testing.T) {
	var tp Topic
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan int64, 1024)
	started := make(chan struct{})
	block := make(chan struct{})
	go func() {
		args, _ := wire.NewDecoder([]byte("\x00\x00\x00\x02[]\x00\x00"))
		first := true
		tp.Stream()(krate.WithWindow(ctx, ""), args, func(write func(*wire.Encoder)) error {
			if first {
				first = false
				close(started)
				<-block
			}
			e := wire.NewEncoder()
			write(e)
			b, _ := e.Finish()
			d, _ := wire.NewDecoder(b)
			n, _ := d.Int()
			got <- n
			return nil
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for tp.Subscribers() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	tp.Publish(func(e *wire.Encoder) { e.Int(-1) })
	<-started
	for i := int64(0); i < queueLen*3; i++ {
		i := i
		tp.Publish(func(e *wire.Encoder) { e.Int(i) })
	}
	close(block)
	var last int64
	deadline = time.Now().Add(3 * time.Second)
	for last != queueLen*3-1 && time.Now().Before(deadline) {
		select {
		case n := <-got:
			last = n
		case <-time.After(10 * time.Millisecond):
		}
	}
	if last != queueLen*3-1 {
		t.Fatalf("the newest event was lost (last %d)", last)
	}
}

func TestCallWithoutAnyTarget(t *testing.T) {
	SetLocal(nil)
	t.Setenv(ParentEnv, "")
	ResetRemote()
	_, err := Call(context.Background(), "krate/x.Y", nil)
	if !krate.IsUnsupported(err) {
		t.Fatalf("err = %v", err)
	}
	t.Setenv(ParentEnv, "ftp://nope")
	ResetRemote()
	if _, err := Call(context.Background(), "krate/x.Y", nil); err == nil {
		t.Fatal("bad address accepted")
	}
}
