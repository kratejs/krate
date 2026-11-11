package bridge

import (
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

// Topic fans events out to every open stream of one event id. Publish never
// blocks: a subscriber that is more than queueLen events behind loses the
// oldest ones.
type Topic struct {
	mu   sync.Mutex
	subs map[*topicSub]struct{}
}

const queueLen = 256

type topicSub struct {
	ch chan []byte
}

// Publish encodes one value with write and delivers it to the subscribers.
func (t *Topic) Publish(write func(e *wire.Encoder)) {
	e := wire.NewEncoder()
	write(e)
	frame, err := e.Finish()
	if err != nil {
		return
	}
	t.mu.Lock()
	for s := range t.subs {
		select {
		case s.ch <- frame:
		default:
			select {
			case <-s.ch:
			default:
			}
			select {
			case s.ch <- frame:
			default:
			}
		}
	}
	t.mu.Unlock()
}

// Stream is the scheme.StreamFunc that serves the topic.
func (t *Topic) Stream() scheme.StreamFunc {
	return func(ctx krate.Ctx, args *wire.Decoder, emit func(write func(*wire.Encoder)) error) error {
		if err := args.BeginArray(); err != nil {
			return krate.Errorf("bad_args", "%w", err)
		}
		if err := args.EndArray(); err != nil {
			return krate.Errorf("bad_args", "%w", err)
		}
		s := &topicSub{ch: make(chan []byte, queueLen)}
		t.mu.Lock()
		if t.subs == nil {
			t.subs = map[*topicSub]struct{}{}
		}
		t.subs[s] = struct{}{}
		t.mu.Unlock()
		defer func() {
			t.mu.Lock()
			delete(t.subs, s)
			t.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case frame := <-s.ch:
				err := emit(func(e *wire.Encoder) {
					d, err := wire.NewDecoder(frame)
					if err == nil {
						err = wire.Copy(e, d)
					}
					if err != nil {
						e.Null()
					}
				})
				if err != nil {
					return err
				}
			}
		}
	}
}

// Subscribers is the number of open streams, for tests.
func (t *Topic) Subscribers() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.subs)
}
