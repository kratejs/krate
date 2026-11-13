package power

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
	"github.com/kratejs/krate/packages/native/wire"
)

// provider reads and watches the system's power state.
type provider interface {
	sample() (caps.PowerSample, error)
	// watch delivers suspend, resume, lock and unlock until stop is called.
	watch(fn func(caps.PowerEventKind)) (stop func())
	inhibit(reason string, display bool) (release func(), err error)
}

type platformProvider struct{ p caps.Power }

func (p platformProvider) sample() (caps.PowerSample, error) { return p.p.PowerSample() }
func (p platformProvider) watch(fn func(caps.PowerEventKind)) func() {
	p.p.OnPower(func(e caps.PowerEvent) { fn(e.Kind) })
	return func() {}
}
func (p platformProvider) inhibit(reason string, display bool) (func(), error) {
	return p.p.PreventSleep(reason, display)
}

// pollEvery is how often battery and idle are sampled while a stream is open.
var pollEvery = 3 * time.Second

// manager owns the tracker, the sampling loop and the blockers.
type manager struct {
	prov provider
	tr   tracker
	pub  func(Event)

	mu       sync.Mutex
	users    int
	stopPoll chan struct{}
	stopWat  func()
	blockers map[string]func()
	nextID   int
}

func (m *manager) emit(k caps.PowerEventKind) {
	m.tr.push(k)
	m.pub(Event{Kind: string(k)})
}

// open is called when an event stream opens; close when it ends.
func (m *manager) open() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users++
	if m.users > 1 {
		return
	}
	m.stopWat = m.prov.watch(m.emit)
	m.stopPoll = make(chan struct{})
	go m.poll(m.stopPoll)
}

func (m *manager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users--
	if m.users > 0 {
		return
	}
	close(m.stopPoll)
	if m.stopWat != nil {
		m.stopWat()
		m.stopWat = nil
	}
}

func (m *manager) poll(stop chan struct{}) {
	m.pollOnce() // baseline
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			m.pollOnce()
		}
	}
}

func (m *manager) pollOnce() {
	s, err := m.prov.sample()
	if err != nil {
		return
	}
	for _, k := range m.tr.update(s) {
		m.pub(Event{Kind: string(k)})
	}
}

func (m *manager) state() (State, error) {
	s, err := m.prov.sample()
	if err != nil {
		return State{}, err
	}
	return m.tr.state(s), nil
}

func (m *manager) prevent(reason string, display bool) (string, error) {
	rel, err := m.prov.inhibit(reason, display)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.blockers == nil {
		m.blockers = map[string]func(){}
	}
	m.nextID++
	id := "b" + strconv.Itoa(m.nextID)
	m.blockers[id] = rel
	return id, nil
}

func (m *manager) allow(id string) {
	m.mu.Lock()
	rel := m.blockers[id]
	delete(m.blockers, id)
	m.mu.Unlock()
	if rel != nil {
		rel()
	}
}

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("power")
	var m *manager
	if p, ok := env.App.(caps.Power); ok {
		m = &manager{prov: platformProvider{p}}
	} else if p := newDefaultProvider(); p != nil {
		m = &manager{prov: p}
	}
	unsupported := func() error {
		return fmt.Errorf("%w: power information is not available on this platform", krate.ErrUnsupported)
	}
	if m == nil {
		t.Stream(IDEvents, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, emit func(write func(*wire.Encoder)) error) error {
			return unsupported()
		})
	} else {
		m.pub = func(e Event) { jsonwire.Publish(topic, e) }
		base := topic.Stream()
		t.Stream(IDEvents, Permission, true, scheme.StreamFunc(func(ctx krate.Ctx, args *wire.Decoder, emit func(write func(*wire.Encoder)) error) error {
			m.open()
			defer m.close()
			return base(ctx, args, emit)
		}))
	}

	t.Func(IDState, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDState).Done(); err != nil {
			return err
		}
		if m == nil {
			return unsupported()
		}
		st, err := m.state()
		if err != nil {
			return err
		}
		return jsonwire.Result(out, st)
	})
	t.Func(IDIdleTime, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDIdleTime).Done(); err != nil {
			return err
		}
		if m == nil {
			return unsupported()
		}
		s, err := m.prov.sample()
		if err != nil {
			return err
		}
		return jsonwire.Result(out, s.IdleSeconds)
	})
	t.Func(IDSetIdleThreshold, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetIdleThreshold)
		s := r.Float()
		if err := r.Done(); err != nil {
			return err
		}
		if m == nil {
			return unsupported()
		}
		m.tr.setThreshold(s)
		out.Null()
		return nil
	})
	t.Func(IDPreventSleep, PermissionInhibit, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDPreventSleep)
		reason := r.String()
		display := false
		if r.More() {
			display = r.Bool()
		}
		if err := r.Done(); err != nil {
			return err
		}
		if m == nil {
			return unsupported()
		}
		id, err := m.prevent(reason, display)
		if err != nil {
			return err
		}
		return jsonwire.Result(out, id)
	})
	t.Func(IDAllowSleep, PermissionInhibit, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDAllowSleep)
		id := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		if m != nil {
			m.allow(id)
		}
		out.Null()
		return nil
	})
}

// Available reports whether power information can be read.
func Available(env *bridge.Env) bool {
	if _, ok := env.App.(caps.Power); ok {
		return true
	}
	return defaultAvailable()
}
