//go:build linux

package media

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
)

// Linux has no system-wide switch for the camera, microphone or location; a
// sandboxed app (Flatpak, Snap) is gated by the desktop portals. The camera
// portal (org.freedesktop.portal.Camera) is asked for access when the app runs
// in a sandbox; outside one there is nothing to ask and the state is unknown,
// which allows the request. The microphone, screen sharing and location go
// through PipeWire, the ScreenCast portal and GeoClue, which WebKitGTK talks
// to itself: screen sharing always shows the portal's own picker.

const (
	portalDest  = "org.freedesktop.portal.Desktop"
	portalPath  = "/org/freedesktop/portal/desktop"
	portalReqNS = "/org/freedesktop/portal/desktop/request/"
)

type portalPrivacy struct {
	conn      func() (dbusiface.Conn, error)
	sandboxed func() bool
	timeout   time.Duration
}

func defaultPrivacy() caps.Privacy {
	return &portalPrivacy{conn: sessionbus.Get, sandboxed: inSandbox, timeout: 2 * time.Minute}
}

// inSandbox reports whether the process runs under Flatpak or Snap.
func inSandbox() bool {
	if _, err := os.Stat("/.flatpak-info"); err == nil {
		return true
	}
	return os.Getenv("SNAP_NAME") != ""
}

func (p *portalPrivacy) PrivacyStatus(kind caps.PermissionKind) (caps.PrivacyStatus, error) {
	if kind == caps.PermCamera && p.sandboxed() {
		// The portal stores the answer; asking is cheap and silent when the
		// user answered before.
		return caps.PrivacyNotDetermined, nil
	}
	return caps.PrivacyUnknown, nil
}

func (p *portalPrivacy) RequestPrivacy(kind caps.PermissionKind) (caps.PrivacyStatus, error) {
	if kind != caps.PermCamera || !p.sandboxed() {
		return caps.PrivacyUnknown, nil
	}
	conn, err := p.conn()
	if err != nil {
		return caps.PrivacyUnknown, err
	}
	var tok [8]byte
	_, _ = rand.Read(tok[:])
	token := "krate" + hex.EncodeToString(tok[:])
	sender := "unknown"
	if u, ok := conn.(dbusiface.UniqueNamer); ok {
		sender = strings.ReplaceAll(strings.TrimPrefix(u.UniqueName(), ":"), ".", "_")
	}
	path := portalReqNS + sender + "/" + token
	resp := make(chan uint32, 1)
	cancel, err := conn.Subscribe(fmt.Sprintf("type='signal',interface='org.freedesktop.portal.Request',member='Response',path='%s'", path), func(s dbusiface.Signal) {
		if len(s.Body) > 0 {
			if v, ok := s.Body[0].(uint32); ok {
				select {
				case resp <- v:
				default:
				}
			}
		}
	})
	if err != nil {
		return caps.PrivacyUnknown, err
	}
	defer cancel()
	_, err = conn.Call(portalDest, portalPath, "org.freedesktop.portal.Camera", "AccessCamera",
		map[string]dbusiface.Variant{"handle_token": {Value: token}})
	if err != nil {
		if e, ok := err.(*dbusiface.Error); ok && strings.Contains(e.Name, "UnknownMethod") || strings.Contains(err.Error(), "ServiceUnknown") {
			return caps.PrivacyUnknown, fmt.Errorf("%w: no camera portal on this desktop", krate.ErrUnsupported)
		}
		return caps.PrivacyUnknown, err
	}
	select {
	case code := <-resp:
		if code == 0 {
			return caps.PrivacyGranted, nil
		}
		return caps.PrivacyDenied, nil
	case <-time.After(p.timeout):
		return caps.PrivacyNotDetermined, nil
	}
}
