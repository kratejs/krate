package port

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
)

// Limits on what one end buffers for a peer that is not listening yet.
const (
	MaxQueued     = 1024
	MaxQueueBytes = 16 << 20
	MaxMessage    = 8 << 20
)

// WorkerOwnerPrefix starts the owner id of a worker.
const WorkerOwnerPrefix = "worker:"

// Broker holds channels and their two ends. It is pure Go: delivery is done
// by Listen loops that the ops layer runs per stream. Owner "" is host code,
// which is trusted and may act on every end; "worker:<id>" is a worker; any
// other owner is the SDK id of a window or view.
type Broker struct {
	// CanReceive reports whether owner may be handed an end (the page's scope
	// has the port:receive permission). Nil allows everyone.
	CanReceive func(owner string) bool
	// Alive reports whether owner still exists. Nil means every owner does.
	Alive func(owner string) bool
	// Worker answers for owners that start with WorkerOwnerPrefix: whether the
	// worker exists and may be handed ports. Nil refuses workers.
	Worker func(owner string) (alive, canReceive bool)

	mu   sync.Mutex
	next int
	ends map[string]*end
}

type end struct {
	id     string
	peer   *end
	owner  string
	queue  [][]byte
	qbytes int
	closed bool
	// listening is true while a Listen loop serves the end.
	listening bool
	// gen increments on every change of owner or listener so a stale Listen
	// loop ends.
	gen  int
	wake chan struct{}
}

// NewBroker returns an empty Broker.
func NewBroker() *Broker { return &Broker{ends: map[string]*end{}} }

func portErr(code, format string, args ...any) error { return krate.Errorf(code, format, args...) }

// Channel creates a channel with two ends, both owned by owner, and returns
// their ids.
func (b *Broker) Channel(owner string) (a, c string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	n := strconv.Itoa(b.next)
	ea := &end{id: "ch" + n + "a", owner: owner, wake: make(chan struct{}, 1)}
	ec := &end{id: "ch" + n + "b", owner: owner, wake: make(chan struct{}, 1)}
	ea.peer, ec.peer = ec, ea
	b.ends[ea.id], b.ends[ec.id] = ea, ec
	return ea.id, ec.id
}

// get returns the end if caller may act on it. Host code ("") may act on any.
func (b *Broker) get(caller, id string) (*end, error) {
	e := b.ends[id]
	if e == nil {
		return nil, portErr("not_found", "no port %q (it was closed or never existed)", id)
	}
	if caller != "" && e.owner != caller {
		return nil, portErr("permission_denied", "port %q is not held by this window", id)
	}
	return e, nil
}

func (e *end) poke() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *end) kick() {
	e.gen++
	e.poke()
}

// Transfer hands an end to another owner. Messages already queued for the end
// go with it. The target must be allowed to receive ports.
func (b *Broker) Transfer(caller, id, to string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, err := b.get(caller, id)
	if err != nil {
		return err
	}
	if e.closed {
		return portErr("closed", "port %q is closed", id)
	}
	if IsWorker(to) {
		alive, can := false, false
		if b.Worker != nil {
			alive, can = b.Worker(to)
		}
		if !alive {
			return portErr("not_found", "no worker %q", to)
		}
		if !can {
			return portErr("permission_denied", "worker %q has no %q permission, so it cannot be handed a port", to, "port:receive")
		}
	} else if to != "" {
		if b.Alive != nil && !b.Alive(to) {
			return portErr("not_found", "no window, view or worker %q", to)
		}
		if b.CanReceive != nil && !b.CanReceive(to) {
			return portErr("permission_denied", "%q has no %q permission, so it cannot be handed a port", to, "port:receive")
		}
	}
	e.owner = to
	e.listening = false
	e.kick()
	return nil
}

// Post queues a message (one encoded wire value) for the peer of id.
func (b *Broker) Post(caller, id string, frame []byte) error {
	if len(frame) > MaxMessage {
		return portErr("too_large", "a port message is limited to %d bytes (got %d)", MaxMessage, len(frame))
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e, err := b.get(caller, id)
	if err != nil {
		return err
	}
	if e.closed {
		return portErr("closed", "port %q is closed", id)
	}
	p := e.peer
	if len(p.queue) >= MaxQueued || p.qbytes+len(frame) > MaxQueueBytes {
		return portErr("port_full", "the other end of %q is not reading: %d messages queued", id, len(p.queue))
	}
	p.queue = append(p.queue, frame)
	p.qbytes += len(frame)
	select {
	case p.wake <- struct{}{}:
	default:
	}
	return nil
}

// Close closes both ends. The peer still reads what was queued, then sees the
// close.
func (b *Broker) Close(caller, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, err := b.get(caller, id)
	if err != nil {
		return err
	}
	b.closeLocked(e)
	return nil
}

func (b *Broker) closeLocked(e *end) {
	for _, x := range []*end{e, e.peer} {
		if !x.closed {
			x.closed = true
			x.poke()
		}
		if !x.listening && len(x.queue) == 0 {
			delete(b.ends, x.id)
		}
	}
}

// DropOwner closes every channel with an end held by owner (the window, view
// or worker went away).
func (b *Broker) DropOwner(owner string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.ends {
		if e.owner == owner {
			e.queue, e.qbytes = nil, 0
			if !e.closed {
				b.closeLocked(e)
			}
			delete(b.ends, e.id)
		}
	}
}

// Count returns the number of live ends (tests and diagnostics).
func (b *Broker) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.ends)
}

// Owner reports who holds an end.
func (b *Broker) Owner(id string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.ends[id]
	if e == nil {
		return "", false
	}
	return e.owner, true
}

// Listen delivers the messages queued for id to emit, in order, until ctx
// ends, the end is handed to someone else, or the channel closes. closed is
// true when the channel closed (after the queue drained). A second Listen on
// the same end replaces the first.
func (b *Broker) Listen(ctx context.Context, caller, id string, emit func(frame []byte) error) (closed bool, err error) {
	b.mu.Lock()
	e, err := b.get(caller, id)
	if err != nil {
		b.mu.Unlock()
		return false, err
	}
	e.kick()
	gen := e.gen
	e.listening = true
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		if e.gen == gen {
			e.listening = false
		}
		b.mu.Unlock()
	}()
	for {
		b.mu.Lock()
		if e.gen != gen {
			b.mu.Unlock()
			return false, nil
		}
		var frame []byte
		if len(e.queue) > 0 {
			frame = e.queue[0]
			e.queue[0] = nil
			e.queue = e.queue[1:]
			e.qbytes -= len(frame)
		} else if e.closed {
			e.listening = false
			delete(b.ends, e.id)
			b.mu.Unlock()
			return true, nil
		}
		b.mu.Unlock()
		if frame != nil {
			if err := emit(frame); err != nil {
				return false, err
			}
			continue
		}
		select {
		case <-e.wake:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

// IsWorker reports whether owner names a worker.
func IsWorker(owner string) bool { return strings.HasPrefix(owner, WorkerOwnerPrefix) }

func (b *Broker) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return fmt.Sprintf("port.Broker{%d ends}", len(b.ends))
}
