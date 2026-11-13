package krate

import "sync"

// Signal is a concurrency-safe value holder that notifies subscribers when
// it changes. The zero value holds the zero T and is ready to use.
type Signal[T any] struct {
	mu   sync.Mutex
	v    T
	subs map[*subscriber[T]]struct{}
}

// NewSignal returns a Signal holding v.
func NewSignal[T any](v T) *Signal[T] {
	return &Signal[T]{v: v}
}

// Get returns the current value.
func (s *Signal[T]) Get() T {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v
}

// Set stores v and notifies subscribers.
func (s *Signal[T]) Set(v T) {
	s.mu.Lock()
	s.v = v
	s.notifyLocked()
	s.mu.Unlock()
}

// Update replaces the value with f(current) atomically, then notifies
// subscribers. f runs under the signal's lock and must not call the signal.
func (s *Signal[T]) Update(f func(T) T) {
	s.mu.Lock()
	s.v = f(s.v)
	s.notifyLocked()
	s.mu.Unlock()
}

// Subscribe registers fn to receive every later value. fn runs on its own
// goroutine, never under the signal's lock, and never blocks Set. Delivery is
// coalesced per subscriber: a slow fn sees the latest value, not every value.
// The returned cancel stops delivery and releases the goroutine; an fn call
// already in flight may finish after cancel returns. Cancel is idempotent.
func (s *Signal[T]) Subscribe(fn func(T)) (cancel func()) {
	return s.subscribe(fn, false)
}

// Watch is Subscribe that first delivers the current value, without the race
// a separate Get followed by Subscribe would have: fn never sees an older
// value after a newer one.
func (s *Signal[T]) Watch(fn func(T)) (cancel func()) {
	return s.subscribe(fn, true)
}

func (s *Signal[T]) subscribe(fn func(T), initial bool) func() {
	sub := &subscriber[T]{
		fn:   fn,
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	s.mu.Lock()
	if initial {
		sub.latest, sub.pending = s.v, true
		sub.wake <- struct{}{}
	}
	if s.subs == nil {
		s.subs = make(map[*subscriber[T]]struct{})
	}
	s.subs[sub] = struct{}{}
	s.mu.Unlock()

	go sub.run()

	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subs, sub)
			s.mu.Unlock()
			sub.stop()
		})
	}
}

func (s *Signal[T]) notifyLocked() {
	for sub := range s.subs {
		sub.push(s.v)
	}
}

type subscriber[T any] struct {
	fn   func(T)
	wake chan struct{}
	done chan struct{}

	mu      sync.Mutex
	latest  T
	pending bool
	stopped bool
}

func (b *subscriber[T]) push(v T) {
	b.mu.Lock()
	b.latest, b.pending = v, true
	b.mu.Unlock()
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *subscriber[T]) stop() {
	b.mu.Lock()
	b.stopped = true
	b.mu.Unlock()
	close(b.done)
}

func (b *subscriber[T]) run() {
	for {
		select {
		case <-b.wake:
		case <-b.done:
			return
		}
		b.mu.Lock()
		if b.stopped || !b.pending {
			b.mu.Unlock()
			continue
		}
		v := b.latest
		b.pending = false
		b.mu.Unlock()
		b.fn(v)
	}
}
