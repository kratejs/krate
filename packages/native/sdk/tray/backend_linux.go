//go:build linux

package tray

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
)

const (
	watcherName  = "org.kde.StatusNotifierWatcher"
	watcherPath  = "/StatusNotifierWatcher"
	itemPath     = "/StatusNotifierItem"
	itemIface    = "org.kde.StatusNotifierItem"
	menuPath     = "/MenuBar"
	menuIface    = "com.canonical.dbusmenu"
	maxIconEdge  = 256
	dbusNameDest = "org.freedesktop.DBus"
)

type pixmap struct {
	W, H int32
	Data []byte
}

type tooltip struct {
	IconName string
	Icon     []pixmap
	Title    string
	Desc     string
}

type layout struct {
	ID       int32
	Props    map[string]dbusiface.Variant
	Children []dbusiface.Variant
}

type propsEntry struct {
	ID    int32
	Props map[string]dbusiface.Variant
}

type sni struct {
	conn    dbusiface.Conn
	publish func(caps.TrayEvent)
	name    string

	xembed caps.XEmbedTray // legacy tray fallback, nil when the platform has none

	mu       sync.Mutex
	useXE    bool // the icon lives in the XEmbed tray, not on the bus
	active   bool
	icon     caps.TrayIcon
	revision uint32
	nextID   int32
	ids      map[int32]string // dbusmenu id -> item id
	flat     map[int32]layoutItem
	root     layout
}

type layoutItem struct{ props map[string]dbusiface.Variant }

func newDefaultBackend(app any, publish func(caps.TrayEvent)) (backend, error) {
	conn, err := sessionbus.Get()
	if err != nil {
		return nil, err
	}
	s := &sni{conn: conn, publish: publish, name: fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())}
	s.xembed, _ = app.(caps.XEmbedTray)
	s.export()
	return s, nil
}

// current reports whether the backend still uses the live session bus
// connection.
func (s *sni) current() bool {
	c, err := sessionbus.Get()
	return err == nil && c == s.conn
}

func (s *sni) release() {
	s.mu.Lock()
	xe, useXE := s.xembed, s.useXE
	s.useXE = false
	s.mu.Unlock()
	if useXE {
		_ = xe.SetXEmbedTray(nil) // going away; nothing to report
	}
}

// noHostError is what the user sees when neither tray kind exists.
func noHostError() error {
	return fmt.Errorf("%w: no system tray is running: neither a StatusNotifier host nor an XEmbed tray. On GNOME install and enable the AppIndicator extension (gnome-shell-extension-appindicator)", krate.ErrUnsupported)
}

func (s *sni) export() {
	ev := func(kind string) dbusiface.Method {
		return func(args []any) ([]any, error) {
			s.publish(caps.TrayEvent{Kind: kind})
			return nil, nil
		}
	}
	s.conn.Export(itemPath, itemIface, map[string]dbusiface.Method{
		"Activate":          ev("click"),
		"SecondaryActivate": ev("middle-click"),
		// Ayatana hosts (Ubuntu panels) call this instead of SecondaryActivate.
		"XAyatanaSecondaryActivate": ev("middle-click"),
		"ContextMenu":               ev("right-click"),
		"Scroll":                    func(args []any) ([]any, error) { return nil, nil },
	})
	s.conn.ExportProperties(itemPath, itemIface, s.itemProps)
	s.conn.Export(menuPath, menuIface, map[string]dbusiface.Method{
		"GetLayout":          s.getLayout,
		"GetGroupProperties": s.getGroupProperties,
		"GetProperty":        s.getProperty,
		"Event":              s.event,
		"EventGroup":         s.eventGroup,
		"AboutToShow":        func(args []any) ([]any, error) { return []any{false}, nil },
		"AboutToShowGroup":   func(args []any) ([]any, error) { return []any{[]int32{}, []int32{}}, nil },
	})
	s.conn.ExportProperties(menuPath, menuIface, func() map[string]dbusiface.Variant {
		return map[string]dbusiface.Variant{
			"Version":       {Value: uint32(3)},
			"TextDirection": {Value: "ltr"},
			"Status":        {Value: "normal"},
			"IconThemePath": {Value: []string{}},
		}
	})
}

func (s *sni) set(icon *caps.TrayIcon) error {
	s.mu.Lock()
	if s.useXE {
		xe := s.xembed
		if icon == nil {
			s.useXE = false
		}
		s.mu.Unlock()
		return xe.SetXEmbedTray(icon)
	}
	if icon == nil {
		was := s.active
		s.active = false
		s.mu.Unlock()
		if was {
			_, _ = s.conn.Call(dbusNameDest, "/org/freedesktop/DBus", dbusNameDest, "ReleaseName", s.name)
		}
		return nil
	}
	s.icon = *icon
	s.buildMenuLocked()
	was := s.active
	s.mu.Unlock()
	if !was {
		if err := s.conn.RequestName(s.name); err != nil {
			return err
		}
		if _, err := s.conn.Call(watcherName, watcherPath, watcherName, "RegisterStatusNotifierItem", s.name); err != nil {
			var de *dbusiface.Error
			if errors.As(err, &de) && (strings.HasSuffix(de.Name, "ServiceUnknown") || strings.HasSuffix(de.Name, "NameHasNoOwner")) {
				_, _ = s.conn.Call(dbusNameDest, "/org/freedesktop/DBus", dbusNameDest, "ReleaseName", s.name)
				return s.fallbackXEmbed(icon)
			}
			return err
		}
		s.mu.Lock()
		s.active = true
		s.mu.Unlock()
		return nil
	}
	for _, sig := range []string{"NewIcon", "NewToolTip", "NewTitle"} {
		if err := s.conn.Emit(itemPath, itemIface, sig); err != nil {
			return err
		}
	}
	s.mu.Lock()
	rev := s.revision
	s.mu.Unlock()
	return s.conn.Emit(menuPath, menuIface, "LayoutUpdated", rev, int32(0))
}

// fallbackXEmbed shows the icon in an XEmbed tray when there is no
// StatusNotifier host.
func (s *sni) fallbackXEmbed(icon *caps.TrayIcon) error {
	if s.xembed == nil || !s.xembed.XEmbedTrayAvailable() {
		return noHostError()
	}
	s.xembed.OnXEmbedTray(s.publish)
	if err := s.xembed.SetXEmbedTray(icon); err != nil {
		return err
	}
	s.mu.Lock()
	s.useXE = true
	s.mu.Unlock()
	return nil
}

func (s *sni) itemProps() map[string]dbusiface.Variant {
	s.mu.Lock()
	ic := s.icon
	s.mu.Unlock()
	pm := toPixmaps(ic.PNG)
	if pm == nil {
		pm = []pixmap{}
	}
	return map[string]dbusiface.Variant{
		"Category":            {Value: "ApplicationStatus"},
		"Id":                  {Value: os.Getenv("KRATE_APP_ID")},
		"Title":               {Value: ic.Title},
		"Status":              {Value: "Active"},
		"WindowId":            {Value: int32(0)},
		"IconName":            {Value: ""},
		"IconPixmap":          {Value: pm},
		"OverlayIconName":     {Value: ""},
		"OverlayIconPixmap":   {Value: []pixmap{}},
		"AttentionIconName":   {Value: ""},
		"AttentionIconPixmap": {Value: []pixmap{}},
		"AttentionMovieName":  {Value: ""},
		"ToolTip":             {Value: tooltip{IconName: "", Icon: []pixmap{}, Title: ic.Tooltip, Desc: ""}},
		"ItemIsMenu":          {Value: false},
		"Menu":                {Value: dbusiface.ObjectPath(menuPath)},
	}
}

// toPixmaps converts PNG bytes to the ARGB32 (network byte order) pixmap the
// StatusNotifierItem protocol uses.
func toPixmaps(b []byte) []pixmap {
	if len(b) == 0 {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	r := img.Bounds()
	if r.Dx() > maxIconEdge || r.Dy() > maxIconEdge || r.Dx() == 0 || r.Dy() == 0 {
		return nil
	}
	nrgba := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(nrgba, nrgba.Bounds(), img, r.Min, draw.Src)
	data := make([]byte, 0, r.Dx()*r.Dy()*4)
	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < r.Dx(); x++ {
			c := nrgba.NRGBAAt(x, y)
			data = append(data, c.A, c.R, c.G, c.B)
		}
	}
	return []pixmap{{W: int32(r.Dx()), H: int32(r.Dy()), Data: data}}
}

// buildMenuLocked rebuilds the dbusmenu tree from the current items.
func (s *sni) buildMenuLocked() {
	s.revision++
	s.nextID = 0
	s.ids = map[int32]string{}
	s.flat = map[int32]layoutItem{}
	s.root = layout{ID: 0, Props: map[string]dbusiface.Variant{"children-display": {Value: "submenu"}}}
	s.root.Children = s.buildChildren(s.icon.Menu)
}

func (s *sni) buildChildren(items []caps.MenuItem) []dbusiface.Variant {
	out := []dbusiface.Variant{}
	for _, it := range items {
		s.nextID++
		id := s.nextID
		props := map[string]dbusiface.Variant{"enabled": {Value: !it.Disabled}, "visible": {Value: true}}
		if it.Kind == caps.MenuItemSeparator {
			props["type"] = dbusiface.Variant{Value: "separator"}
		} else {
			props["label"] = dbusiface.Variant{Value: mnemonic(it.Label)}
			switch it.Kind {
			case caps.MenuItemCheck:
				props["toggle-type"] = dbusiface.Variant{Value: "checkmark"}
				props["toggle-state"] = dbusiface.Variant{Value: toggle(it.Checked)}
			case caps.MenuItemRadio:
				props["toggle-type"] = dbusiface.Variant{Value: "radio"}
				props["toggle-state"] = dbusiface.Variant{Value: toggle(it.Checked)}
			}
			if sc := shortcutProp(it.Accelerator); sc != nil {
				props["shortcut"] = dbusiface.Variant{Value: sc}
			}
		}
		n := layout{ID: id, Props: props, Children: []dbusiface.Variant{}}
		if len(it.Submenu) > 0 || it.Kind == caps.MenuItemSubmenu {
			props["children-display"] = dbusiface.Variant{Value: "submenu"}
			n.Children = s.buildChildren(it.Submenu)
		} else if it.ID != "" {
			s.ids[id] = it.ID
		}
		s.flat[id] = layoutItem{props: props}
		out = append(out, dbusiface.Variant{Value: n})
	}
	return out
}

func toggle(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

func mnemonic(label string) string {
	var b strings.Builder
	for i := 0; i < len(label); i++ {
		switch c := label[i]; {
		case c == '&' && i+1 < len(label) && label[i+1] == '&':
			b.WriteByte('&')
			i++
		case c == '&':
			b.WriteByte('_')
		case c == '_':
			b.WriteString("__")
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func shortcutProp(acc string) [][]string {
	if acc == "" {
		return nil
	}
	var keys []string
	for _, p := range strings.Split(acc, "+") {
		switch strings.ToLower(p) {
		case "cmdorctrl", "ctrl", "control", "cmd", "command":
			keys = append(keys, "Control")
		case "shift":
			keys = append(keys, "Shift")
		case "alt", "option":
			keys = append(keys, "Alt")
		case "super", "meta", "win":
			keys = append(keys, "Super")
		default:
			keys = append(keys, p)
		}
	}
	return [][]string{keys}
}

func (s *sni) getLayout(args []any) ([]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parent := int32(0)
	if len(args) > 0 {
		parent, _ = args[0].(int32)
	}
	if parent == 0 {
		return []any{s.revision, s.root}, nil
	}
	if n, ok := findNode(s.root, parent); ok {
		return []any{s.revision, n}, nil
	}
	return nil, &dbusiface.Error{Name: "org.freedesktop.DBus.Error.InvalidArgs", Message: "no such menu item"}
}

func findNode(n layout, id int32) (layout, bool) {
	if n.ID == id {
		return n, true
	}
	for _, c := range n.Children {
		if child, ok := c.Value.(layout); ok {
			if r, ok := findNode(child, id); ok {
				return r, true
			}
		}
	}
	return layout{}, false
}

func (s *sni) getGroupProperties(args []any) ([]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []propsEntry{}
	var ids []any
	if len(args) > 0 {
		ids, _ = args[0].([]any)
	}
	if len(ids) == 0 {
		for id := range s.flat {
			ids = append(ids, id)
		}
	}
	for _, v := range ids {
		id, _ := v.(int32)
		if it, ok := s.flat[id]; ok {
			out = append(out, propsEntry{ID: id, Props: it.props})
		}
	}
	return []any{out}, nil
}

func (s *sni) getProperty(args []any) ([]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(args) == 2 {
		id, _ := args[0].(int32)
		name, _ := args[1].(string)
		if it, ok := s.flat[id]; ok {
			if v, ok := it.props[name]; ok {
				return []any{v}, nil
			}
		}
	}
	return nil, &dbusiface.Error{Name: "org.freedesktop.DBus.Error.InvalidArgs", Message: "no such property"}
}

func (s *sni) click(id int32, eventID string) {
	if eventID != "clicked" {
		return
	}
	s.mu.Lock()
	item := s.ids[id]
	s.mu.Unlock()
	if item != "" {
		s.publish(caps.TrayEvent{Kind: "menu", ID: item})
	}
}

func (s *sni) event(args []any) ([]any, error) {
	if len(args) >= 2 {
		id, _ := args[0].(int32)
		ev, _ := args[1].(string)
		s.click(id, ev)
	}
	return nil, nil
}

func (s *sni) eventGroup(args []any) ([]any, error) {
	if len(args) >= 1 {
		list, _ := args[0].([]any)
		for _, e := range list {
			if st, ok := e.(dbusiface.Struct); ok && len(st) >= 2 {
				id, _ := st[0].(int32)
				ev, _ := st[1].(string)
				s.click(id, ev)
			}
		}
	}
	return []any{[]int32{}}, nil
}

// Available reports whether a StatusNotifier host or an XEmbed tray is
// running.
func Available(env *bridge.Env) bool {
	if _, ok := env.App.(caps.Tray); ok {
		return true
	}
	if sessionbus.NameHasOwner(watcherName) {
		return true
	}
	xe, ok := env.App.(caps.XEmbedTray)
	return ok && xe.XEmbedTrayAvailable()
}
