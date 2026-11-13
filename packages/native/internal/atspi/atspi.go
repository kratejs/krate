// Package atspi is a small AT-SPI client over the accessibility bus, used by
// the accessibility tests to walk an application's accessibility tree the way
// a screen reader does. It speaks to org.a11y.atspi.Accessible and friends
// through the pure Go D-Bus client.
package atspi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/native/internal/dbus"
)

// Roles used by the tests (AtspiRole values).
const (
	RoleCheckMenuItem = 8
	RoleDialog        = 16
	RoleFileChooser   = 19
	RoleFiller        = 20
	RoleFrame         = 23
	RoleMenu          = 33
	RoleMenuBar       = 34
	RoleMenuItem      = 35
	RolePanel         = 39
	RolePushButton    = 43
	RoleRadioMenuItem = 45
	RoleSeparator     = 50
	RoleSplitPane     = 53
	RoleWindow        = 69
	RoleDocumentWeb   = 95
	RoleLabel         = 29
	RoleAlert         = 2
	RoleEntry         = 79
)

// State bits (AtspiStateType).
const (
	StateActive     = 1
	StateEnabled    = 8
	StateSelected   = 23
	StateChecked    = 4
	StateExpandable = 9
	StateFocusable  = 11
	StateFocused    = 12
	StateShowing    = 25
	StateVisible    = 30
	StateSensitive  = 24
)

// Node is one accessible object.
type Node struct {
	c    *Client
	Dest string
	Path dbus.ObjectPath
}

// Client is a connection to the accessibility bus.
type Client struct {
	conn *dbus.Conn
}

// Dial connects to the accessibility bus at addr.
func Dial(ctx context.Context, addr string) (*Client, error) {
	conn, err := dbus.DialAddress(ctx, addr)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

// Close drops the connection.
func (c *Client) Close() { _ = c.conn.Close() }

// Conn exposes the D-Bus connection.
func (c *Client) Conn() *dbus.Conn { return c.conn }

// Root is the desktop root; its children are applications.
func (c *Client) Root() Node {
	return Node{c: c, Dest: "org.a11y.atspi.Registry", Path: "/org/a11y/atspi/accessible/root"}
}

const ifaceAcc = "org.a11y.atspi.Accessible"

func (n Node) call(ctx context.Context, iface, member, sig string, args ...any) ([]any, error) {
	return n.c.conn.Call(ctx, n.Dest, n.Path, iface, member, sig, args...)
}

func (n Node) prop(iface, name string) (dbus.Variant, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return n.c.conn.GetProperty(ctx, n.Dest, n.Path, iface, name)
}

// Name is the accessible name.
func (n Node) Name() string {
	v, err := n.prop(ifaceAcc, "Name")
	if err != nil {
		return ""
	}
	s, _ := v.Value.(string)
	return s
}

// Description is the accessible description.
func (n Node) Description() string {
	v, err := n.prop(ifaceAcc, "Description")
	if err != nil {
		return ""
	}
	s, _ := v.Value.(string)
	return s
}

// Role is the AtspiRole number.
func (n Node) Role() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := n.call(ctx, ifaceAcc, "GetRole", "")
	if err != nil || len(out) == 0 {
		return -1
	}
	r, _ := out[0].(uint32)
	return int(r)
}

// RoleName is the non-localized role name ("push button").
func (n Node) RoleName() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := n.call(ctx, ifaceAcc, "GetRoleName", "")
	if err != nil || len(out) == 0 {
		return ""
	}
	s, _ := out[0].(string)
	return s
}

// Interfaces lists the AT-SPI interfaces the object implements.
func (n Node) Interfaces() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := n.call(ctx, ifaceAcc, "GetInterfaces", "")
	if err != nil || len(out) == 0 {
		return nil
	}
	l, _ := out[0].([]string)
	r := make([]string, 0, len(l))
	for _, e := range l {
		r = append(r, strings.TrimPrefix(e, "org.a11y.atspi."))
	}
	return r
}

// HasState reports whether the state bit is set.
func (n Node) HasState(bit int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := n.call(ctx, ifaceAcc, "GetState", "")
	if err != nil || len(out) == 0 {
		return false
	}
	words, _ := out[0].([]any)
	w := bit / 32
	if w >= len(words) {
		return false
	}
	v, _ := words[w].(uint32)
	return v&(1<<uint(bit%32)) != 0
}

// States lists the set state bits.
func (n Node) States() []int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := n.call(ctx, ifaceAcc, "GetState", "")
	if err != nil || len(out) == 0 {
		return nil
	}
	var r []int
	words, _ := out[0].([]any)
	for i, w := range words {
		v, _ := w.(uint32)
		for b := 0; b < 32; b++ {
			if v&(1<<uint(b)) != 0 {
				r = append(r, i*32+b)
			}
		}
	}
	return r
}

// Children lists the child objects.
func (n Node) Children() []Node {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := n.call(ctx, ifaceAcc, "GetChildren", "")
	if err != nil || len(out) == 0 {
		return nil
	}
	l, _ := out[0].([]any)
	var r []Node
	for _, e := range l {
		st, ok := e.(dbus.Struct)
		if !ok || len(st) < 2 {
			continue
		}
		d, _ := st[0].(string)
		p, _ := st[1].(dbus.ObjectPath)
		if d == "" || p == "/org/a11y/atspi/null" {
			continue
		}
		r = append(r, Node{c: n.c, Dest: d, Path: p})
	}
	return r
}

// Action is one entry of the Action interface.
type Action struct{ Name, Description, KeyBinding string }

// Actions lists the object's actions with their key bindings.
func (n Node) Actions() []Action {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := n.call(ctx, "org.a11y.atspi.Action", "GetActions", "")
	if err != nil || len(out) == 0 {
		return nil
	}
	l, _ := out[0].([]any)
	var r []Action
	for _, e := range l {
		if st, ok := e.(dbus.Struct); ok && len(st) >= 3 {
			a := Action{}
			a.Name, _ = st[0].(string)
			a.Description, _ = st[1].(string)
			a.KeyBinding, _ = st[2].(string)
			r = append(r, a)
		}
	}
	return r
}

// DoAction performs action index i.
func (n Node) DoAction(i int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := n.call(ctx, "org.a11y.atspi.Action", "DoAction", "i", int32(i))
	if err != nil || len(out) == 0 {
		return false
	}
	ok, _ := out[0].(bool)
	return ok
}

// GrabFocus asks the Component to take focus.
func (n Node) GrabFocus() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := n.call(ctx, "org.a11y.atspi.Component", "GrabFocus", "")
	if err != nil || len(out) == 0 {
		return false
	}
	ok, _ := out[0].(bool)
	return ok
}

// Extents is the screen rectangle (x, y, width, height).
func (n Node) Extents() (x, y, w, h int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := n.call(ctx, "org.a11y.atspi.Component", "GetExtents", "u", uint32(0))
	if err != nil || len(out) == 0 {
		return
	}
	if st, ok := out[0].(dbus.Struct); ok && len(st) == 4 {
		a, _ := st[0].(int32)
		b, _ := st[1].(int32)
		c, _ := st[2].(int32)
		d, _ := st[3].(int32)
		return int(a), int(b), int(c), int(d)
	}
	return
}

// String describes a node for failure messages.
func (n Node) String() string {
	return fmt.Sprintf("%s(%q)", n.RoleName(), n.Name())
}

// Walk visits n and its descendants depth first. fn returns false to skip
// the children of a node. depth is limited to keep a cycle from hanging.
func (n Node) Walk(fn func(n Node, depth int) bool) {
	var rec func(n Node, d int)
	rec = func(n Node, d int) {
		if d > 40 {
			return
		}
		if !fn(n, d) {
			return
		}
		for _, c := range n.Children() {
			rec(c, d+1)
		}
	}
	rec(n, 0)
}

// Find returns the first descendant (or n) for which pred is true.
func (n Node) Find(pred func(Node) bool) (found Node, ok bool) {
	n.Walk(func(c Node, _ int) bool {
		if ok {
			return false
		}
		if pred(c) {
			found, ok = c, true
			return false
		}
		return true
	})
	return
}

// FindAll returns every descendant (and n) for which pred is true.
func (n Node) FindAll(pred func(Node) bool) []Node {
	var r []Node
	n.Walk(func(c Node, _ int) bool {
		if pred(c) {
			r = append(r, c)
		}
		return true
	})
	return r
}

// Dump renders the subtree as indented text.
func (n Node) Dump() string {
	var b strings.Builder
	n.Walk(func(c Node, d int) bool {
		fmt.Fprintf(&b, "%s%s(%d) name=%q ifaces=%v\n", strings.Repeat("  ", d), c.RoleName(), c.Role(), c.Name(), c.Interfaces())
		return true
	})
	return b.String()
}

// AppByName finds the application whose accessible name is name.
func (c *Client) AppByName(name string) (Node, bool) {
	for _, a := range c.Root().Children() {
		if a.Name() == name {
			return a, true
		}
	}
	return Node{}, false
}

const (
	deviceDest = "org.a11y.atspi.Registry"
	devicePath = "/org/a11y/atspi/registry/deviceeventcontroller"
	deviceIf   = "org.a11y.atspi.DeviceEventController"
)

// Key types for GenerateKeyboardEvent.
const (
	keyPress   = 0
	keyRelease = 1
	keySym     = 3
)

// Keysyms for KeySym.
var keysyms = map[string]int32{
	"Tab": 0xff09, "Return": 0xff0d, "Escape": 0xff1b, "space": 0x20,
	"Left": 0xff51, "Up": 0xff52, "Right": 0xff53, "Down": 0xff54,
	"Home": 0xff50, "End": 0xff57, "F6": 0xffc3, "F8": 0xffc5, "F10": 0xffc7,
	"Menu": 0xff67,
	"a":    0x61, "f": 0x66, "n": 0x6e, "s": 0x73,
}

// KeySym sends a key press and release for the keysym with this name (see
// keysyms) through the registry, as an assistive technology would. For a SYM
// event the registry takes the keysym number in the keycode argument.
func (c *Client) KeySym(name string) error {
	sym, ok := keysyms[name]
	if !ok {
		return fmt.Errorf("atspi: unknown keysym %q", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.conn.Call(ctx, deviceDest, devicePath, deviceIf, "GenerateKeyboardEvent", "isu", sym, "", uint32(keySym))
	return err
}

// X11 keycodes of the modifier keys on a standard (evdev) keymap.
const (
	KeycodeShift = 50
	KeycodeCtrl  = 37
	KeycodeAlt   = 64
)

// KeyCombo holds the modifier keycodes, presses and releases the key by
// keysym name, then releases the modifiers.
func (c *Client) KeyCombo(mods []int32, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw := func(code int32, typ uint32) error {
		_, err := c.conn.Call(ctx, deviceDest, devicePath, deviceIf, "GenerateKeyboardEvent", "isu", code, "", typ)
		return err
	}
	for _, m := range mods {
		if err := raw(m, keyPress); err != nil {
			return err
		}
	}
	err := c.KeySym(key)
	for i := len(mods) - 1; i >= 0; i-- {
		if e := raw(mods[i], keyRelease); err == nil {
			err = e
		}
	}
	return err
}

// Value reads the Value interface's CurrentValue (a slider, a split pane).
func (n Node) Value() (float64, bool) {
	v, err := n.prop("org.a11y.atspi.Value", "CurrentValue")
	if err != nil {
		return 0, false
	}
	f, ok := v.Value.(float64)
	return f, ok
}
