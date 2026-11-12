package power

import (
	"sync"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// tracker derives battery and idle events from samples and merges in the
// events the platform pushes. It is pure state; the manager supplies the
// samples.
type tracker struct {
	mu        sync.Mutex
	haveBatt  bool
	onBattery bool
	idleSince bool // currently in the idle state
	locked    bool
	threshold float64
}

// setThreshold sets the idle threshold in seconds; 0 disables idle events.
func (t *tracker) setThreshold(s float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s < 0 {
		s = 0
	}
	t.threshold = s
	if s == 0 {
		t.idleSince = false
	}
}

// push records a platform event (lock state follows lock and unlock).
func (t *tracker) push(k caps.PowerEventKind) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch k {
	case caps.PowerLock:
		t.locked = true
	case caps.PowerUnlock:
		t.locked = false
	}
}

// update takes a sample and returns the events it implies, in order.
func (t *tracker) update(s caps.PowerSample) []caps.PowerEventKind {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []caps.PowerEventKind
	if t.haveBatt && s.OnBattery != t.onBattery {
		if s.OnBattery {
			out = append(out, caps.PowerOnBatt)
		} else {
			out = append(out, caps.PowerOnAC)
		}
	}
	t.haveBatt, t.onBattery = true, s.OnBattery
	if t.threshold > 0 && s.IdleSeconds >= 0 {
		idle := s.IdleSeconds >= t.threshold
		if idle && !t.idleSince {
			out = append(out, caps.PowerIdle)
		} else if !idle && t.idleSince {
			out = append(out, caps.PowerActive)
		}
		t.idleSince = idle
	}
	return out
}

// state builds the State for a sample.
func (t *tracker) state(s caps.PowerSample) State {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := State{OnBattery: s.OnBattery, IdleSeconds: s.IdleSeconds, Locked: s.Locked || t.locked, IdleState: "active"}
	switch {
	case st.Locked:
		st.IdleState = "locked"
	case s.IdleSeconds < 0:
		st.IdleState = "unknown"
	case t.threshold > 0 && s.IdleSeconds >= t.threshold:
		st.IdleState = "idle"
	}
	return st
}
