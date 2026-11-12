package theme

import (
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// provider reads the system appearance.
type provider interface {
	read() (caps.ThemeInfo, error)
	// watch calls fn when the appearance may have changed.
	watch(fn func()) (cancel func())
}

// manager keeps the override and the last system reading, applies the result
// to the windows and reports changes.
type manager struct {
	prov provider
	// apply sets the window chrome. source is the override ("system",
	// "light" or "dark") and resolved the scheme it comes to.
	apply   func(source Source, resolved string)
	publish func(Theme)

	mu     sync.Mutex
	source Source
	sys    caps.ThemeInfo
	have   bool
	last   Theme
}

func scheme(dark bool) string {
	if dark {
		return "dark"
	}
	return "light"
}

// current returns the appearance, reading the system on first use.
func (m *manager) current() Theme {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.have {
		m.readLocked()
	}
	return m.themeLocked()
}

func (m *manager) readLocked() {
	if info, err := m.prov.read(); err == nil {
		m.sys = info
	}
	m.have = true
}

func (m *manager) themeLocked() Theme {
	src := m.source
	if src == "" {
		src = System
	}
	t := Theme{System: scheme(m.sys.Dark), HighContrast: m.sys.HighContrast, Source: src}
	t.Scheme = t.System
	if src == Light || src == Dark {
		t.Scheme = string(src)
	}
	return t
}

// refresh re-reads the system, applies it and publishes when something
// changed. force applies even without a change (a new override).
func (m *manager) refresh(force bool) {
	m.mu.Lock()
	m.readLocked()
	t := m.themeLocked()
	changed := t != m.last
	m.last = t
	apply, publish := m.apply, m.publish
	m.mu.Unlock()
	if (changed || force) && apply != nil {
		apply(t.Source, t.Scheme)
	}
	if changed && publish != nil {
		publish(t)
	}
}

// setSource changes the override.
func (m *manager) setSource(s string) error {
	switch Source(strings.ToLower(s)) {
	case System, Light, Dark:
	case "":
		s = string(System)
	default:
		return errBadSource(s)
	}
	m.mu.Lock()
	m.source = Source(strings.ToLower(s))
	m.mu.Unlock()
	m.refresh(true)
	return nil
}
