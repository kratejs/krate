//go:build linux

// Package sessionbus shares one session-bus connection among the SDK areas
// that talk to desktop services (notifications, tray, portals).
package sessionbus

import (
	"fmt"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
)

var (
	mu   sync.Mutex
	conn *dbusiface.Client
	addr string
)

// Get returns the shared connection, connecting on first use and again when
// the session bus address changed or the connection died. A missing bus is
// krate.ErrUnsupported.
func Get() (dbusiface.Conn, error) {
	mu.Lock()
	defer mu.Unlock()
	a := dbusiface.SessionAddress()
	if conn != nil {
		select {
		case <-conn.Done():
			conn = nil
		default:
			if a == addr {
				return conn, nil
			}
			conn.Close()
			conn = nil
		}
	}
	c, err := dbusiface.Connect(a)
	if err != nil {
		return nil, fmt.Errorf("%w: no session bus: %v", krate.ErrUnsupported, err)
	}
	conn, addr = c, a
	return c, nil
}

// Reset closes the shared connection (tests).
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	if conn != nil {
		conn.Close()
		conn = nil
	}
}

// NameHasOwner reports whether a well-known bus name currently has an owner.
func NameHasOwner(name string) bool {
	c, err := Get()
	if err != nil {
		return false
	}
	out, err := c.Call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "NameHasOwner", name)
	if err != nil || len(out) != 1 {
		return false
	}
	ok, _ := out[0].(bool)
	return ok
}
