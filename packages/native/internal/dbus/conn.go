package dbus

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Well-known bus names and interfaces.
const (
	BusName       = "org.freedesktop.DBus"
	BusPath       = ObjectPath("/org/freedesktop/DBus")
	BusInterface  = "org.freedesktop.DBus"
	PropsIface    = "org.freedesktop.DBus.Properties"
	IntrospectIfc = "org.freedesktop.DBus.Introspectable"
	PeerIface     = "org.freedesktop.DBus.Peer"
)

// ErrNoBus means no session bus address could be found.
var ErrNoBus = errors.New("dbus: no session bus (DBUS_SESSION_BUS_ADDRESS is unset)")

// ErrClosed is returned by calls on a closed connection.
var ErrClosed = errors.New("dbus: connection closed")

// SessionAddress returns the session bus address from the environment, or the
// conventional $XDG_RUNTIME_DIR/bus socket when only that exists.
func SessionAddress() (string, error) {
	if a := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); a != "" {
		return a, nil
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		p := dir + "/bus"
		if st, err := os.Stat(p); err == nil && st.Mode()&os.ModeSocket != 0 {
			return "unix:path=" + p, nil
		}
	}
	return "", ErrNoBus
}

// parseAddress splits "unix:path=/x,guid=1;unix:abstract=y" into network
// addresses for net.Dial. Entries with other transports are skipped.
func parseAddress(addr string) ([]string, error) {
	var out []string
	for _, entry := range strings.Split(addr, ";") {
		transport, rest, ok := strings.Cut(entry, ":")
		if !ok || transport != "unix" {
			continue
		}
		for _, kv := range strings.Split(rest, ",") {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			val, err := url.PathUnescape(v)
			if err != nil {
				return nil, fmt.Errorf("dbus: bad address %q: %w", entry, err)
			}
			switch k {
			case "path":
				out = append(out, val)
			case "abstract":
				out = append(out, "@"+val)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("dbus: no usable unix address in %q", addr)
	}
	return out, nil
}

// MethodFunc handles one call to an exported method. It returns the reply
// signature and values, or an error: an *Error is sent as is, any other error
// as org.freedesktop.DBus.Error.Failed.
type MethodFunc func(msg *Message) (sig string, body []any, err error)

type export struct {
	methods map[string]MethodFunc
}

type sub struct {
	m  Match
	fn func(*Message)
}

// Conn is a connection to a bus.
type Conn struct {
	nc net.Conn
	br *bufio.Reader

	wmu    sync.Mutex
	serial uint32
	unique string

	mu      sync.Mutex
	pending map[uint32]chan *Message
	subs    map[*sub]struct{}
	exports map[exportKey]*export
	closed  bool
	err     error
	done    chan struct{}

	qmu   sync.Mutex
	qcond *sync.Cond
	queue []*Message
}

type exportKey struct {
	path  ObjectPath
	iface string
}

// Dial connects to the session bus.
func Dial(ctx context.Context) (*Conn, error) {
	addr, err := SessionAddress()
	if err != nil {
		return nil, err
	}
	return DialAddress(ctx, addr)
}

// DialAddress connects to the bus at a D-Bus address string and says Hello.
func DialAddress(ctx context.Context, addr string) (*Conn, error) {
	paths, err := parseAddress(addr)
	if err != nil {
		return nil, err
	}
	var nc net.Conn
	var derr error
	for _, p := range paths {
		var d net.Dialer
		nc, derr = d.DialContext(ctx, "unix", p)
		if derr == nil {
			break
		}
	}
	if derr != nil {
		return nil, fmt.Errorf("dbus: connecting: %w", derr)
	}
	c, err := NewConn(ctx, nc)
	if err != nil {
		nc.Close()
		return nil, err
	}
	return c, nil
}

// NewConn authenticates over nc, sends Hello and starts the reader. On
// success the Conn owns nc.
func NewConn(ctx context.Context, nc net.Conn) (*Conn, error) {
	if dl, ok := ctx.Deadline(); ok {
		nc.SetDeadline(dl)
	} else {
		nc.SetDeadline(time.Now().Add(15 * time.Second))
	}
	br := bufio.NewReader(nc)
	if err := authExternal(nc, br); err != nil {
		return nil, err
	}
	nc.SetDeadline(time.Time{})
	c := &Conn{
		nc:      nc,
		br:      br,
		pending: map[uint32]chan *Message{},
		subs:    map[*sub]struct{}{},
		exports: map[exportKey]*export{},
		done:    make(chan struct{}),
	}
	c.qcond = sync.NewCond(&c.qmu)
	go c.readLoop()
	go c.dispatchLoop()
	res, err := c.Call(ctx, BusName, BusPath, BusInterface, "Hello", "")
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("dbus: Hello: %w", err)
	}
	if len(res) == 1 {
		c.unique, _ = res[0].(string)
	}
	return c, nil
}

func authExternal(nc net.Conn, br *bufio.Reader) error {
	uid := strconv.Itoa(os.Getuid())
	if _, err := fmt.Fprintf(nc, "\x00AUTH EXTERNAL %s\r\n", hex.EncodeToString([]byte(uid))); err != nil {
		return fmt.Errorf("dbus: auth: %w", err)
	}
	line, err := br.ReadString('\n')
	if err != nil {
		return fmt.Errorf("dbus: auth: %w", err)
	}
	if !strings.HasPrefix(line, "OK ") && !strings.HasPrefix(line, "OK\r") {
		return fmt.Errorf("dbus: auth rejected: %s", strings.TrimSpace(line))
	}
	if _, err := nc.Write([]byte("BEGIN\r\n")); err != nil {
		return fmt.Errorf("dbus: auth: %w", err)
	}
	return nil
}

// UniqueName is the bus-assigned name of this connection.
func (c *Conn) UniqueName() string { return c.unique }

// Done is closed when the connection ends.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Close closes the connection and fails pending calls.
func (c *Conn) Close() error {
	c.shutdown(ErrClosed)
	return nil
}

func (c *Conn) shutdown(cause error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.err = cause
	pending := c.pending
	c.pending = map[uint32]chan *Message{}
	c.mu.Unlock()
	c.nc.Close()
	close(c.done)
	for _, ch := range pending {
		close(ch)
	}
	c.qmu.Lock()
	c.qcond.Broadcast()
	c.qmu.Unlock()
}

func (c *Conn) closeErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return ErrClosed
}

// send assigns a serial (when m.Serial is zero) and writes m.
func (c *Conn) send(m *Message) (uint32, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.serial++
	m.Serial = c.serial
	b, err := m.Marshal()
	if err != nil {
		return 0, err
	}
	if _, err := c.nc.Write(b); err != nil {
		c.shutdown(err)
		return 0, err
	}
	return m.Serial, nil
}

// Call invokes a method and returns the reply values. A D-Bus error reply
// comes back as *Error.
func (c *Conn) Call(ctx context.Context, dest string, path ObjectPath, iface, member, sig string, args ...any) ([]any, error) {
	m := &Message{Type: TypeMethodCall, Destination: dest, Path: path, Interface: iface, Member: member, Signature: sig, Body: args}
	ch := make(chan *Message, 1)
	// The serial is known only inside send; register under the write lock.
	c.wmu.Lock()
	c.serial++
	m.Serial = c.serial
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		c.wmu.Unlock()
		return nil, c.closeErr()
	}
	c.pending[m.Serial] = ch
	c.mu.Unlock()
	b, err := m.Marshal()
	if err == nil {
		_, err = c.nc.Write(b)
		if err != nil {
			go c.shutdown(err)
		}
	}
	c.wmu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, m.Serial)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return nil, c.closeErr()
		}
		if r.Type == TypeError {
			return nil, &Error{Name: r.ErrorName, Body: r.Body}
		}
		return r.Body, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, m.Serial)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *Conn) readLoop() {
	for {
		m, err := ReadMessage(c.br)
		if err != nil {
			c.shutdown(err)
			return
		}
		switch m.Type {
		case TypeMethodReturn, TypeError:
			c.mu.Lock()
			ch := c.pending[m.ReplySerial]
			delete(c.pending, m.ReplySerial)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case TypeSignal:
			c.qmu.Lock()
			c.queue = append(c.queue, m)
			c.qcond.Signal()
			c.qmu.Unlock()
		case TypeMethodCall:
			go c.serve(m)
		}
	}
}

func (c *Conn) dispatchLoop() {
	for {
		c.qmu.Lock()
		for len(c.queue) == 0 {
			select {
			case <-c.done:
				c.qmu.Unlock()
				return
			default:
			}
			c.qcond.Wait()
		}
		m := c.queue[0]
		c.queue = c.queue[1:]
		c.qmu.Unlock()

		c.mu.Lock()
		var fns []func(*Message)
		for s := range c.subs {
			if s.m.matches(m) {
				fns = append(fns, s.fn)
			}
		}
		c.mu.Unlock()
		for _, fn := range fns {
			fn(m)
		}
	}
}

// Match selects signals. Empty fields match anything.
type Match struct {
	Sender    string
	Path      ObjectPath
	Interface string
	Member    string
}

func (m Match) matches(msg *Message) bool {
	if m.Path != "" && m.Path != msg.Path {
		return false
	}
	if m.Interface != "" && m.Interface != msg.Interface {
		return false
	}
	if m.Member != "" && m.Member != msg.Member {
		return false
	}
	// A well-known Sender is not comparable with the unique sender of the
	// message; the bus has already filtered on it through the match rule.
	if strings.HasPrefix(m.Sender, ":") && m.Sender != msg.Sender {
		return false
	}
	return true
}

func (m Match) rule() string {
	parts := []string{"type='signal'"}
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+"='"+strings.ReplaceAll(v, "'", `'\''`)+"'")
		}
	}
	add("sender", m.Sender)
	add("path", string(m.Path))
	add("interface", m.Interface)
	add("member", m.Member)
	return strings.Join(parts, ",")
}

// Subscribe asks the bus for signals matching m and calls fn for each one.
// fn runs on the dispatcher goroutine, in arrival order. The returned cancel
// removes the subscription.
func (c *Conn) Subscribe(ctx context.Context, m Match, fn func(*Message)) (cancel func(), err error) {
	s := &sub{m: m, fn: fn}
	c.mu.Lock()
	c.subs[s] = struct{}{}
	c.mu.Unlock()
	if _, err := c.Call(ctx, BusName, BusPath, BusInterface, "AddMatch", "s", m.rule()); err != nil {
		c.mu.Lock()
		delete(c.subs, s)
		c.mu.Unlock()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.subs, s)
			c.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			c.Call(ctx, BusName, BusPath, BusInterface, "RemoveMatch", "s", m.rule())
		})
	}, nil
}

// Request flags for RequestName.
const (
	NameAllowReplacement uint32 = 1
	NameReplaceExisting  uint32 = 2
	NameDoNotQueue       uint32 = 4
)

// RequestName asks the bus for a well-known name. It returns an error unless
// this connection became the primary owner (or already was).
func (c *Conn) RequestName(ctx context.Context, name string, flags uint32) error {
	res, err := c.Call(ctx, BusName, BusPath, BusInterface, "RequestName", "su", name, flags)
	if err != nil {
		return err
	}
	if len(res) == 1 {
		if code, _ := res[0].(uint32); code == 1 || code == 4 {
			return nil
		}
	}
	return fmt.Errorf("dbus: name %q is taken", name)
}

// Emit sends a signal from path.
func (c *Conn) Emit(path ObjectPath, iface, member, sig string, args ...any) error {
	_, err := c.send(&Message{Type: TypeSignal, Path: path, Interface: iface, Member: member, Signature: sig, Body: args})
	return err
}

// Export registers methods of iface on path. Calling Export again for the
// same path and interface replaces it. Peer.Ping and a minimal
// Introspectable.Introspect are answered automatically.
func (c *Conn) Export(path ObjectPath, iface string, methods map[string]MethodFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.exports[exportKey{path, iface}] = &export{methods: methods}
}

// Unexport removes an export.
func (c *Conn) Unexport(path ObjectPath, iface string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.exports, exportKey{path, iface})
}

func (c *Conn) reply(call *Message, sig string, body []any, err error) {
	if call.Flags&FlagNoReplyExpected != 0 {
		return
	}
	r := &Message{ReplySerial: call.Serial, Destination: call.Sender}
	if err != nil {
		var de *Error
		if !errors.As(err, &de) {
			de = NewError("org.freedesktop.DBus.Error.Failed", err.Error())
		}
		r.Type = TypeError
		r.ErrorName = de.Name
		r.Body = de.Body
		r.Signature = strings.Repeat("s", len(de.Body))
	} else {
		r.Type = TypeMethodReturn
		r.Signature = sig
		r.Body = body
	}
	if _, serr := c.send(r); serr != nil && r.Type == TypeMethodReturn {
		// The reply could not be encoded; tell the caller instead of hanging.
		c.send(&Message{Type: TypeError, ReplySerial: call.Serial, Destination: call.Sender,
			ErrorName: "org.freedesktop.DBus.Error.Failed", Signature: "s", Body: []any{serr.Error()}})
	}
}

func (c *Conn) serve(m *Message) {
	if m.Interface == PeerIface && m.Member == "Ping" {
		c.reply(m, "", nil, nil)
		return
	}
	c.mu.Lock()
	var ifaces []string
	var target *export
	for k, e := range c.exports {
		if k.path != m.Path {
			continue
		}
		ifaces = append(ifaces, k.iface)
		if k.iface == m.Interface {
			target = e
		}
	}
	c.mu.Unlock()
	if m.Interface == IntrospectIfc && m.Member == "Introspect" {
		var sb strings.Builder
		sb.WriteString(`<!DOCTYPE node PUBLIC "-//freedesktop//DTD D-BUS Object Introspection 1.0//EN" "http://www.freedesktop.org/standards/dbus/1.0/introspect.dtd"><node>`)
		for _, i := range ifaces {
			sb.WriteString(`<interface name="` + i + `"/>`)
		}
		sb.WriteString("</node>")
		c.reply(m, "s", []any{sb.String()}, nil)
		return
	}
	if target == nil {
		if len(ifaces) == 0 {
			c.reply(m, "", nil, NewError("org.freedesktop.DBus.Error.UnknownObject", "no object at "+string(m.Path)))
		} else {
			c.reply(m, "", nil, NewError("org.freedesktop.DBus.Error.UnknownInterface", "no interface "+m.Interface))
		}
		return
	}
	fn := target.methods[m.Member]
	if fn == nil {
		c.reply(m, "", nil, NewError("org.freedesktop.DBus.Error.UnknownMethod", "no method "+m.Member))
		return
	}
	var sig string
	var body []any
	var err error
	func() {
		defer func() {
			if v := recover(); v != nil {
				err = fmt.Errorf("panic in %s.%s: %v", m.Interface, m.Member, v)
			}
		}()
		sig, body, err = fn(m)
	}()
	c.reply(m, sig, body, err)
}

// GetProperty reads one property.
func (c *Conn) GetProperty(ctx context.Context, dest string, path ObjectPath, iface, prop string) (Variant, error) {
	res, err := c.Call(ctx, dest, path, PropsIface, "Get", "ss", iface, prop)
	if err != nil {
		return Variant{}, err
	}
	if len(res) != 1 {
		return Variant{}, errors.New("dbus: bad Properties.Get reply")
	}
	v, ok := res[0].(Variant)
	if !ok {
		return Variant{}, errors.New("dbus: bad Properties.Get reply")
	}
	return v, nil
}

// SetProperty writes one property.
func (c *Conn) SetProperty(ctx context.Context, dest string, path ObjectPath, iface, prop string, v Variant) error {
	_, err := c.Call(ctx, dest, path, PropsIface, "Set", "ssv", iface, prop, v)
	return err
}

// GetAllProperties reads every property of iface.
func (c *Conn) GetAllProperties(ctx context.Context, dest string, path ObjectPath, iface string) (map[string]Variant, error) {
	res, err := c.Call(ctx, dest, path, PropsIface, "GetAll", "s", iface)
	if err != nil {
		return nil, err
	}
	if len(res) != 1 {
		return nil, errors.New("dbus: bad Properties.GetAll reply")
	}
	raw, _ := res[0].(map[string]any)
	out := make(map[string]Variant, len(raw))
	for k, v := range raw {
		if vv, ok := v.(Variant); ok {
			out[k] = vv
		}
	}
	return out, nil
}
