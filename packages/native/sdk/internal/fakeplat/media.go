package fakeplat

import (
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Media is an App whose platform layer routes webview permission requests,
// reads OS privacy state and lists capture sources, as the real layers do.
type Media struct {
	*App

	mu      sync.Mutex
	handler caps.PermissionHandler

	// Status is the OS consent per kind; a missing kind is unknown.
	Status map[caps.PermissionKind]caps.PrivacyStatus
	// OnAsk is the state RequestPrivacy leaves a not-determined kind in.
	OnAsk caps.PrivacyStatus
	Asked []caps.PermissionKind
	// Sources is what CaptureSources returns.
	Sources []caps.CaptureSource
}

var (
	_ caps.PermissionRequests = (*Media)(nil)
	_ caps.Privacy            = (*Media)(nil)
	_ caps.CaptureSources     = (*Media)(nil)
)

// NewMedia returns a Media app.
func NewMedia() *Media {
	return &Media{App: New(), Status: map[caps.PermissionKind]caps.PrivacyStatus{}, OnAsk: caps.PrivacyGranted}
}

func (m *Media) SetPermissionHandler(h caps.PermissionHandler) {
	m.mu.Lock()
	m.handler = h
	m.mu.Unlock()
}

// HasHandler reports whether the SDK installed a permission handler.
func (m *Media) HasHandler() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.handler != nil }

var permID struct {
	sync.Mutex
	n uint64
}

// Request makes the webview of view ask for kinds and returns the answer.
// With no handler installed it is denied, like the real layers.
func (m *Media) Request(view caps.ViewRef, kinds ...caps.PermissionKind) bool {
	m.mu.Lock()
	h := m.handler
	m.mu.Unlock()
	if h == nil {
		return false
	}
	permID.Lock()
	permID.n++
	id := permID.n
	permID.Unlock()
	ch := make(chan bool, 1)
	h(caps.PermissionRequest{ID: id, View: view, Origin: "krate://app", Kinds: kinds}, func(b bool) {
		select {
		case ch <- b:
		default:
		}
	})
	select {
	case b := <-ch:
		return b
	case <-time.After(10 * time.Second):
		return false
	}
}

func (m *Media) PrivacyStatus(k caps.PermissionKind) (caps.PrivacyStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.Status[k]; ok {
		return s, nil
	}
	return caps.PrivacyUnknown, nil
}

func (m *Media) RequestPrivacy(k caps.PermissionKind) (caps.PrivacyStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Asked = append(m.Asked, k)
	if m.Status[k] == caps.PrivacyNotDetermined {
		m.Status[k] = m.OnAsk
	}
	return m.Status[k], nil
}

func (m *Media) CaptureSources(thumbWidth int) ([]caps.CaptureSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]caps.CaptureSource(nil), m.Sources...)
	if thumbWidth == 0 {
		for i := range out {
			out[i].Thumbnail = nil
		}
	}
	return out, nil
}
