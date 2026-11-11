// Package dbusmini is a small pure-Go D-Bus client: session or system bus
// over a unix socket, EXTERNAL authentication, method calls, signals, and
// exported objects. It implements dbusiface.Conn.
package dbusmini

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
)

const (
	msgCall   = 1
	msgReturn = 2
	msgError  = 3
	msgSignal = 4

	flagNoReply = 1

	fPath      = 1
	fInterface = 2
	fMember    = 3
	fErrorName = 4
	fReplyTo   = 5
	fDest      = 6
	fSender    = 7
	fSignature = 8

	callTimeout = 25 * time.Second
	maxMessage  = 128 << 20
)

type message struct {
	typ     byte
	flags   byte
	serial  uint32
	path    string
	iface   string
	member  string
	errName string
	replyTo uint32
	dest    string
	sender  string
	sig     string
	body    []byte
}

type objKey struct{ path, iface string }

type subscription struct {
	rule  rule
	match string
	fn    func(dbusiface.Signal)
}

// Conn is a bus connection.
type Conn struct {
	c      net.Conn
	wmu    sync.Mutex
	serial uint32
	uname  string

	mu      sync.Mutex
	pending map[uint32]chan *message
	objs    map[objKey]map[string]dbusiface.Method
	props   map[objKey]func() map[string]dbusiface.Variant
	subs    map[*subscription]struct{}
	closed  chan struct{}
	err     error
}

var _ dbusiface.Conn = (*Conn)(nil)

// SessionAddress returns the session bus address from the environment.
func SessionAddress() string {
	if a := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); a != "" {
		return a
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return "unix:path=" + d + "/bus"
	}
	return ""
}

// ConnectSession connects to the session bus.
func ConnectSession() (*Conn, error) { return Connect(SessionAddress()) }

// Connect dials a bus address ("unix:path=...", "unix:abstract=...") and
// completes authentication and Hello.
func Connect(addr string) (*Conn, error) {
	if addr == "" {
		return nil, errors.New("dbus: no session bus address (DBUS_SESSION_BUS_ADDRESS is unset)")
	}
	var last error
	for _, one := range strings.Split(addr, ";") {
		c, err := dial(one)
		if err != nil {
			last = err
			continue
		}
		conn, err := handshake(c)
		if err != nil {
			c.Close()
			last = err
			continue
		}
		return conn, nil
	}
	return nil, last
}

func dial(addr string) (net.Conn, error) {
	transport, rest, ok := strings.Cut(addr, ":")
	if !ok || transport != "unix" {
		return nil, fmt.Errorf("dbus: unsupported bus address %q", addr)
	}
	for _, kv := range strings.Split(rest, ",") {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "path":
			return net.Dial("unix", v)
		case "abstract":
			return net.Dial("unix", "@"+v)
		}
	}
	return nil, fmt.Errorf("dbus: bus address %q has no path", addr)
}

func handshake(c net.Conn) (*Conn, error) {
	c.SetDeadline(time.Now().Add(10 * time.Second))
	uid := strconv.Itoa(os.Getuid())
	if _, err := fmt.Fprintf(c, "\x00AUTH EXTERNAL %s\r\n", hex.EncodeToString([]byte(uid))); err != nil {
		return nil, err
	}
	rd := bufio.NewReader(c)
	line, err := rd.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("dbus: authentication: %w", err)
	}
	if !strings.HasPrefix(line, "OK") {
		return nil, fmt.Errorf("dbus: authentication refused: %s", strings.TrimSpace(line))
	}
	if _, err := io.WriteString(c, "BEGIN\r\n"); err != nil {
		return nil, err
	}
	c.SetDeadline(time.Time{})
	conn := &Conn{
		c:       c,
		pending: map[uint32]chan *message{},
		objs:    map[objKey]map[string]dbusiface.Method{},
		props:   map[objKey]func() map[string]dbusiface.Variant{},
		subs:    map[*subscription]struct{}{},
		closed:  make(chan struct{}),
	}
	go conn.readLoop(rd)
	out, err := conn.Call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "Hello")
	if err != nil {
		conn.Close()
		return nil, err
	}
	if len(out) == 1 {
		conn.uname, _ = out[0].(string)
	}
	return conn, nil
}

// UniqueName is the connection's unique bus name.
func (c *Conn) UniqueName() string { return c.uname }

// Done is closed when the connection ends.
func (c *Conn) Done() <-chan struct{} { return c.closed }

// Close ends the connection.
func (c *Conn) Close() error {
	c.fail(errors.New("dbus: connection closed"))
	return nil
}

func (c *Conn) fail(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	close(c.closed)
	pend := c.pending
	c.pending = map[uint32]chan *message{}
	c.mu.Unlock()
	c.c.Close()
	for _, ch := range pend {
		close(ch)
	}
}

func (c *Conn) readLoop(rd *bufio.Reader) {
	for {
		m, err := readMessage(rd)
		if err != nil {
			c.fail(err)
			return
		}
		switch m.typ {
		case msgReturn, msgError:
			c.mu.Lock()
			ch := c.pending[m.replyTo]
			delete(c.pending, m.replyTo)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case msgSignal:
			c.dispatchSignal(m)
		case msgCall:
			go c.handleCall(m)
		}
	}
}

func readMessage(rd *bufio.Reader) (*message, error) {
	var fixed [16]byte
	if _, err := io.ReadFull(rd, fixed[:]); err != nil {
		return nil, err
	}
	if fixed[0] != 'l' {
		return nil, errors.New("dbus: big-endian messages are not supported")
	}
	m := &message{typ: fixed[1], flags: fixed[2]}
	bodyLen := binary.LittleEndian.Uint32(fixed[4:])
	m.serial = binary.LittleEndian.Uint32(fixed[8:])
	fieldsLen := binary.LittleEndian.Uint32(fixed[12:])
	if fieldsLen > maxMessage || bodyLen > maxMessage {
		return nil, errors.New("dbus: message too large")
	}
	hdrLen := int(fieldsLen)
	pad := (8 - (16+hdrLen)%8) % 8
	buf := make([]byte, hdrLen+pad+int(bodyLen))
	if _, err := io.ReadFull(rd, buf); err != nil {
		return nil, err
	}
	// Header fields: array of struct(byte, variant); the array length was
	// consumed with the fixed header, so decode entries directly.
	d := &decoder{buf: append(make([]byte, 16), buf[:hdrLen]...), off: 16}
	for d.off < 16+hdrLen && d.err == nil {
		d.pad(8)
		code := d.value("y").(byte)
		s := d.sig()
		switch code {
		case fPath, fInterface, fMember, fErrorName, fDest, fSender:
			v := d.value(s)
			str := fmt.Sprint(v)
			switch code {
			case fPath:
				m.path = str
			case fInterface:
				m.iface = str
			case fMember:
				m.member = str
			case fErrorName:
				m.errName = str
			case fDest:
				m.dest = str
			case fSender:
				m.sender = str
			}
		case fReplyTo:
			m.replyTo, _ = d.value(s).(uint32)
		case fSignature:
			m.sig = string(d.value(s).(dbusiface.Signature))
		default:
			d.value(s)
		}
	}
	if d.err != nil {
		return nil, d.err
	}
	m.body = buf[hdrLen+pad:]
	return m, nil
}

func (c *Conn) send(m *message) error {
	e := &encoder{}
	e.buf = append(e.buf, 'l', m.typ, m.flags, 1)
	e.buf = binary.LittleEndian.AppendUint32(e.buf, uint32(len(m.body)))
	e.buf = binary.LittleEndian.AppendUint32(e.buf, m.serial)
	// Field array: length placeholder, then entries.
	e.u32(0)
	lenAt := len(e.buf) - 4
	start := len(e.buf)
	field := func(code byte, sig string, val func()) {
		e.pad(8)
		e.buf = append(e.buf, code)
		e.sig(sig)
		val()
	}
	if m.path != "" {
		field(fPath, "o", func() { e.str(m.path) })
	}
	if m.iface != "" {
		field(fInterface, "s", func() { e.str(m.iface) })
	}
	if m.member != "" {
		field(fMember, "s", func() { e.str(m.member) })
	}
	if m.errName != "" {
		field(fErrorName, "s", func() { e.str(m.errName) })
	}
	if m.typ == msgReturn || m.typ == msgError {
		field(fReplyTo, "u", func() { e.u32(m.replyTo) })
	}
	if m.dest != "" {
		field(fDest, "s", func() { e.str(m.dest) })
	}
	if m.sig != "" {
		field(fSignature, "g", func() { e.sig(m.sig) })
	}
	binary.LittleEndian.PutUint32(e.buf[lenAt:], uint32(len(e.buf)-start))
	e.pad(8)
	e.buf = append(e.buf, m.body...)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err := c.c.Write(e.buf)
	return err
}

func (c *Conn) nextSerial() uint32 {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.serial++
	return c.serial
}

// Call implements dbusiface.Conn.
func (c *Conn) Call(dest, path, iface, method string, args ...any) ([]any, error) {
	sig, body, err := encodeArgs(args)
	if err != nil {
		return nil, err
	}
	m := &message{typ: msgCall, serial: c.nextSerial(), dest: dest, path: path, iface: iface, member: method, sig: sig, body: body}
	ch := make(chan *message, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	c.pending[m.serial] = ch
	c.mu.Unlock()
	if err := c.send(m); err != nil {
		c.mu.Lock()
		delete(c.pending, m.serial)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return nil, errors.New("dbus: connection closed")
		}
		out, err := decodeBody(r.sig, r.body)
		if err != nil {
			return nil, err
		}
		if r.typ == msgError {
			e := &dbusiface.Error{Name: r.errName}
			if len(out) > 0 {
				e.Message, _ = out[0].(string)
			}
			return nil, e
		}
		return out, nil
	case <-time.After(callTimeout):
		c.mu.Lock()
		delete(c.pending, m.serial)
		c.mu.Unlock()
		return nil, fmt.Errorf("dbus: call %s.%s timed out", iface, method)
	}
}

// RequestName implements dbusiface.Conn.
func (c *Conn) RequestName(name string) error {
	out, err := c.Call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "RequestName", name, uint32(0))
	if err != nil {
		return err
	}
	if len(out) == 1 {
		if r, _ := out[0].(uint32); r != 1 && r != 4 {
			return fmt.Errorf("dbus: name %s is already owned", name)
		}
	}
	return nil
}

// Emit implements dbusiface.Conn.
func (c *Conn) Emit(path, iface, name string, args ...any) error {
	sig, body, err := encodeArgs(args)
	if err != nil {
		return err
	}
	return c.send(&message{typ: msgSignal, flags: flagNoReply, serial: c.nextSerial(), path: path, iface: iface, member: name, sig: sig, body: body})
}

// Export implements dbusiface.Conn.
func (c *Conn) Export(path, iface string, methods map[string]dbusiface.Method) {
	c.mu.Lock()
	c.objs[objKey{path, iface}] = methods
	c.mu.Unlock()
}

// ExportProperties implements dbusiface.Conn.
func (c *Conn) ExportProperties(path, iface string, get func() map[string]dbusiface.Variant) {
	c.mu.Lock()
	c.props[objKey{path, iface}] = get
	c.mu.Unlock()
}

func (c *Conn) reply(to *message, vals []any, err error) {
	if to.flags&flagNoReply != 0 {
		return
	}
	if err != nil {
		name := "org.freedesktop.DBus.Error.Failed"
		var de *dbusiface.Error
		if errors.As(err, &de) {
			name = de.Name
			err = errors.New(de.Message)
		}
		_, body, _ := encodeArgs([]any{err.Error()})
		c.send(&message{typ: msgError, serial: c.nextSerial(), replyTo: to.serial, dest: to.sender, errName: name, sig: "s", body: body})
		return
	}
	sig, body, eerr := encodeArgs(vals)
	if eerr != nil {
		_, b, _ := encodeArgs([]any{eerr.Error()})
		c.send(&message{typ: msgError, serial: c.nextSerial(), replyTo: to.serial, dest: to.sender, errName: "org.freedesktop.DBus.Error.Failed", sig: "s", body: b})
		return
	}
	c.send(&message{typ: msgReturn, serial: c.nextSerial(), replyTo: to.serial, dest: to.sender, sig: sig, body: body})
}

func (c *Conn) handleCall(m *message) {
	args, err := decodeBody(m.sig, m.body)
	if err != nil {
		c.reply(m, nil, &dbusiface.Error{Name: "org.freedesktop.DBus.Error.InvalidArgs", Message: err.Error()})
		return
	}
	switch m.iface {
	case "org.freedesktop.DBus.Peer":
		c.reply(m, nil, nil)
		return
	case "org.freedesktop.DBus.Introspectable":
		c.reply(m, []any{"<node/>"}, nil)
		return
	case "org.freedesktop.DBus.Properties":
		c.handleProps(m, args)
		return
	}
	c.mu.Lock()
	methods := c.objs[objKey{m.path, m.iface}]
	c.mu.Unlock()
	h := methods[m.member]
	if h == nil {
		c.reply(m, nil, &dbusiface.Error{Name: "org.freedesktop.DBus.Error.UnknownMethod", Message: m.iface + "." + m.member})
		return
	}
	out, err := h(args)
	c.reply(m, out, err)
}

func (c *Conn) handleProps(m *message, args []any) {
	unknown := &dbusiface.Error{Name: "org.freedesktop.DBus.Error.UnknownProperty", Message: "no such property"}
	switch m.member {
	case "Get":
		if len(args) != 2 {
			c.reply(m, nil, unknown)
			return
		}
		iface, _ := args[0].(string)
		name, _ := args[1].(string)
		c.mu.Lock()
		get := c.props[objKey{m.path, iface}]
		c.mu.Unlock()
		if get != nil {
			if v, ok := get()[name]; ok {
				c.reply(m, []any{v}, nil)
				return
			}
		}
		c.reply(m, nil, unknown)
	case "GetAll":
		iface, _ := args[0].(string)
		c.mu.Lock()
		get := c.props[objKey{m.path, iface}]
		c.mu.Unlock()
		all := map[string]dbusiface.Variant{}
		if get != nil {
			all = get()
		}
		c.reply(m, []any{all}, nil)
	default:
		c.reply(m, nil, &dbusiface.Error{Name: "org.freedesktop.DBus.Error.PropertyReadOnly", Message: "read only"})
	}
}

// rule is a parsed match rule, used for client-side filtering.
type rule struct{ typ, iface, member, path, nsPath, arg0 string }

func parseRule(s string) rule {
	var r rule
	for _, part := range strings.Split(s, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		v = strings.Trim(v, "'")
		switch k {
		case "type":
			r.typ = v
		case "interface":
			r.iface = v
		case "member":
			r.member = v
		case "path":
			r.path = v
		case "path_namespace":
			r.nsPath = v
		case "arg0":
			r.arg0 = v
		}
	}
	return r
}

func (r rule) matches(sig dbusiface.Signal) bool {
	if r.iface != "" && r.iface != sig.Interface {
		return false
	}
	if r.member != "" && r.member != sig.Name {
		return false
	}
	if r.path != "" && r.path != sig.Path {
		return false
	}
	if r.nsPath != "" && sig.Path != r.nsPath && !strings.HasPrefix(sig.Path, strings.TrimSuffix(r.nsPath, "/")+"/") {
		return false
	}
	if r.arg0 != "" {
		if len(sig.Body) == 0 || fmt.Sprint(sig.Body[0]) != r.arg0 {
			return false
		}
	}
	return true
}

// Subscribe implements dbusiface.Conn.
func (c *Conn) Subscribe(match string, fn func(dbusiface.Signal)) (func(), error) {
	sub := &subscription{rule: parseRule(match), match: match, fn: fn}
	c.mu.Lock()
	c.subs[sub] = struct{}{}
	c.mu.Unlock()
	if _, err := c.Call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "AddMatch", match); err != nil {
		c.mu.Lock()
		delete(c.subs, sub)
		c.mu.Unlock()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.subs, sub)
			c.mu.Unlock()
			c.Call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "RemoveMatch", match)
		})
	}, nil
}

func (c *Conn) dispatchSignal(m *message) {
	body, err := decodeBody(m.sig, m.body)
	if err != nil {
		return
	}
	sig := dbusiface.Signal{Sender: m.sender, Path: m.path, Interface: m.iface, Name: m.member, Body: body}
	c.mu.Lock()
	var fns []func(dbusiface.Signal)
	for s := range c.subs {
		if s.rule.matches(sig) {
			fns = append(fns, s.fn)
		}
	}
	c.mu.Unlock()
	for _, fn := range fns {
		go fn(sig)
	}
}
