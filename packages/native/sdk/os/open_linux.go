//go:build linux

package sdkos

import (
	"fmt"

	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
)

// portalOpen asks org.freedesktop.portal.OpenURI to open raw. The portal
// shows the host's own handler, which a sandbox cannot start itself.
func portalOpen(raw string) error {
	conn, err := sessionbus.Get()
	if err != nil {
		return err
	}
	return openURI(conn, raw)
}

func openURI(conn dbusiface.Conn, raw string) error {
	_, err := conn.Call("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop",
		"org.freedesktop.portal.OpenURI", "OpenURI", "", raw, map[string]dbusiface.Variant{})
	if err != nil {
		return fmt.Errorf("sdkos: OpenURI portal: %w", err)
	}
	return nil
}
