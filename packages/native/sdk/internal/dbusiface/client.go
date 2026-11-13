package dbusiface

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/internal/dbus"
)

const callTimeout = 25 * time.Second

// Client implements Conn on top of internal/dbus, the one D-Bus client of the
// SDK. It infers signatures from Go values and exposes decoded values in the
// types documented on this package.
type Client struct {
	c *dbus.Conn

	mu    sync.Mutex
	props map[dbus.ObjectPath]map[string]func() map[string]Variant
}

var _ Conn = (*Client)(nil)

// SessionAddress returns the session bus address from the environment, or ""
// when there is none.
func SessionAddress() string {
	a, err := dbus.SessionAddress()
	if err != nil {
		return ""
	}
	return a
}

// Connect dials a bus address and completes authentication and Hello.
func Connect(addr string) (*Client, error) {
	if addr == "" {
		return nil, errors.New("dbus: no session bus address (DBUS_SESSION_BUS_ADDRESS is unset)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := dbus.DialAddress(ctx, addr)
	if err != nil {
		return nil, err
	}
	return &Client{c: c, props: map[dbus.ObjectPath]map[string]func() map[string]Variant{}}, nil
}

// UniqueName returns the connection's unique bus name.
func (c *Client) UniqueName() string { return c.c.UniqueName() }

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.c.Done() }

// Close ends the connection.
func (c *Client) Close() error { return c.c.Close() }

func convertErr(err error) error {
	var de *dbus.Error
	if errors.As(err, &de) {
		msg := ""
		if len(de.Body) > 0 {
			msg, _ = de.Body[0].(string)
		}
		return &Error{Name: de.Name, Message: msg}
	}
	return err
}

// Call implements Conn.
func (c *Client) Call(dest, path, iface, method string, args ...any) ([]any, error) {
	sig, body, err := encodeArgs(args)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	out, err := c.c.Call(ctx, dest, dbus.ObjectPath(path), iface, method, sig, body...)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("dbus: call %s.%s timed out", iface, method)
		}
		return nil, convertErr(err)
	}
	return fromWireAll(out), nil
}

// RequestName implements Conn.
func (c *Client) RequestName(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := c.c.RequestName(ctx, name, 0); err != nil {
		return convertErr(err)
	}
	return nil
}

// Emit implements Conn.
func (c *Client) Emit(path, iface, name string, args ...any) error {
	sig, body, err := encodeArgs(args)
	if err != nil {
		return err
	}
	return c.c.Emit(dbus.ObjectPath(path), iface, name, sig, body...)
}

// Export implements Conn.
func (c *Client) Export(path, iface string, methods map[string]Method) {
	wrapped := make(map[string]dbus.MethodFunc, len(methods))
	for name, m := range methods {
		wrapped[name] = func(msg *dbus.Message) (string, []any, error) {
			out, err := m(fromWireAll(msg.Body))
			if err != nil {
				var de *Error
				if errors.As(err, &de) {
					return "", nil, dbus.NewError(de.Name, de.Message)
				}
				return "", nil, dbus.NewError("org.freedesktop.DBus.Error.Failed", err.Error())
			}
			sig, body, err := encodeArgs(out)
			if err != nil {
				return "", nil, dbus.NewError("org.freedesktop.DBus.Error.Failed", err.Error())
			}
			return sig, body, nil
		}
	}
	c.c.Export(dbus.ObjectPath(path), iface, wrapped)
}

const propsIface = "org.freedesktop.DBus.Properties"

// ExportProperties implements Conn. Properties are read-only.
func (c *Client) ExportProperties(path, iface string, get func() map[string]Variant) {
	p := dbus.ObjectPath(path)
	c.mu.Lock()
	byIface, ok := c.props[p]
	if !ok {
		byIface = map[string]func() map[string]Variant{}
		c.props[p] = byIface
	}
	byIface[iface] = get
	c.mu.Unlock()
	if ok {
		return // the Properties methods of this path are already exported
	}
	lookup := func(iface string) func() map[string]Variant {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.props[p][iface]
	}
	unknown := dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", "no such property")
	reply := func(v any) (string, []any, error) {
		sig, body, err := encodeArgs([]any{v})
		if err != nil {
			return "", nil, dbus.NewError("org.freedesktop.DBus.Error.Failed", err.Error())
		}
		return sig, body, nil
	}
	c.c.Export(p, propsIface, map[string]dbus.MethodFunc{
		"Get": func(msg *dbus.Message) (string, []any, error) {
			if len(msg.Body) != 2 {
				return "", nil, unknown
			}
			i, _ := msg.Body[0].(string)
			n, _ := msg.Body[1].(string)
			if g := lookup(i); g != nil {
				if v, ok := g()[n]; ok {
					return reply(v)
				}
			}
			return "", nil, unknown
		},
		"GetAll": func(msg *dbus.Message) (string, []any, error) {
			all := map[string]Variant{}
			if len(msg.Body) == 1 {
				i, _ := msg.Body[0].(string)
				if g := lookup(i); g != nil {
					all = g()
				}
			}
			return reply(all)
		},
		"Set": func(*dbus.Message) (string, []any, error) {
			return "", nil, dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", "read only")
		},
	})
}

// Subscribe implements Conn.
func (c *Client) Subscribe(match string, fn func(sig Signal)) (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	cancelSub, err := c.c.Subscribe(ctx, parseMatch(match), func(m *dbus.Message) {
		fn(Signal{Sender: m.Sender, Path: string(m.Path), Interface: m.Interface, Name: m.Member, Body: fromWireAll(m.Body)})
	})
	if err != nil {
		return nil, convertErr(err)
	}
	return cancelSub, nil
}

// parseMatch reads the keys of a bus match rule that the client filters on.
func parseMatch(s string) dbus.Match {
	var m dbus.Match
	for _, part := range strings.Split(s, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		v = strings.Trim(v, "'")
		switch k {
		case "sender":
			m.Sender = v
		case "interface":
			m.Interface = v
		case "member":
			m.Member = v
		case "path":
			m.Path = dbus.ObjectPath(v)
		case "path_namespace":
			m.PathNamespace = dbus.ObjectPath(v)
		case "arg0":
			m.Arg0 = v
		}
	}
	return m
}
