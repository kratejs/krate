package lib

import (
	"strings"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
)

// Count and Titles are host signals.
var (
	Count  = krate.NewSignal(int64(0))
	Titles = krate.NewSignal([]string{})
	Latest = krate.NewSignal(Task{})
)

func List(ctx krate.Ctx) ([]Task, error) {
	return []Task{{Base: Base{ID: 1}, Title: "one", Status: "open"}}, nil
}

func Add(ctx krate.Ctx, title string, tags []string) (Task, error) {
	if strings.TrimSpace(title) == "" {
		return Task{}, krate.Errorf("invalid", "title is empty")
	}
	t := Task{
		Base:    Base{ID: 7, Created: time.Date(2026, 11, 11, 7, 23, 15, 0, time.UTC)},
		Title:   title,
		Tags:    tags,
		Status:  "open",
		Blob:    []byte(title),
		Timeout: 3 * time.Second,
		Meta:    &Meta{Labels: map[string]string{"b": "2", "a": "1"}, Weight: 1.5},
		Secret:  "x",
	}
	Count.Update(func(n int64) int64 { return n + 1 })
	Latest.Set(t)
	return t, nil
}

func Version() string { return "1.2.3" }

func Clear() error { return nil }

func Reset() {}

func Fail(code string) error { return krate.Errorf(code, "failed with %s", code) }

func Roundtrip(b []byte) []byte { return b }

func Echo(b []byte, at time.Time) (Echoed, error) { return Echoed{Data: b, At: at}, nil }

func Shift(t time.Time, d time.Duration) time.Time { return t.Add(d) }

func Walk(t Tree) Tree {
	t.Name = strings.ToUpper(t.Name)
	for i := range t.Kids {
		t.Kids[i] = Walk(t.Kids[i])
	}
	return t
}

func Narrow(a int8, b uint16, c float32, d int) int32 {
	return int32(a) + int32(b) + int32(c) + int32(d)
}

func Pick(p *Task, labels Labels) (*Task, error) {
	if p != nil && labels != nil {
		p.Title += "!"
	}
	return p, nil
}

func Watch(ctx krate.Ctx, n int) (<-chan Task, error) {
	if n < 0 {
		return nil, krate.Errorf("invalid", "n must not be negative")
	}
	ch := make(chan Task)
	go func() {
		defer close(ch)
		for i := 0; i < n; i++ {
			select {
			case ch <- Task{Base: Base{ID: int64(i)}, Title: "t"}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func Ticks(n int) <-chan int {
	ch := make(chan int)
	go func() {
		defer close(ch)
		for i := 0; i < n; i++ {
			ch <- i
		}
	}()
	return ch
}

// Unused is exported but never imported by the UI, so it is not analyzed.
func Unused(v any, f func()) {}

func helper() {}
