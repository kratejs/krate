//go:build linux

package linux

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Accessibility (ATK, which GTK bridges to AT-SPI). GTK names menus, menu
// items, dialogs and buttons itself. What Krate adds: windows that are not
// content stay out of the accessibility tree (offscreen windows), split-pane
// dividers get a name, and the webviews of a split window are told apart.

type atkAPI struct {
	widgetAccessible func(widget uintptr) uintptr
	setName          func(obj uintptr, name string)
	setDescription   func(obj uintptr, desc string)
	setRole          func(obj uintptr, role int32)
	roleForName      func(name string) int32
}

var (
	atkOnce sync.Once
	atkLib  *atkAPI
)

// loadATK binds the ATK calls. It returns nil when libatk is missing, in
// which case every helper below does nothing.
func loadATK() *atkAPI {
	atkOnce.Do(func() {
		gtk, err := purego.Dlopen("libgtk-3.so.0", rtldNow|rtldGlobal)
		if err != nil {
			return
		}
		atk, err := purego.Dlopen("libatk-1.0.so.0", rtldNow|rtldGlobal)
		if err != nil {
			return
		}
		b := &binder{}
		l := &atkAPI{}
		b.bind(&l.widgetAccessible, gtk, "gtk_widget_get_accessible")
		b.bind(&l.setName, atk, "atk_object_set_name")
		b.bind(&l.setDescription, atk, "atk_object_set_description")
		b.bind(&l.setRole, atk, "atk_object_set_role")
		b.bind(&l.roleForName, atk, "atk_role_for_name")
		if len(b.missing) == 0 {
			atkLib = l
		}
	})
	return atkLib
}

// accessibleName sets the accessible name and description of a widget (main
// thread). An empty description is left alone.
func accessibleName(widget uintptr, name, desc string) {
	l := loadATK()
	if l == nil || widget == 0 {
		return
	}
	obj := l.widgetAccessible(widget)
	if obj == 0 {
		return
	}
	if name != "" {
		l.setName(obj, name)
	}
	if desc != "" {
		l.setDescription(obj, desc)
	}
}

// hideFromAssistive keeps a toplevel out of the accessibility tree. GTK
// leaves a window out of the application's children when its accessible has
// the role "redundant object", provided the role is set before the window is
// shown (main thread).
func hideFromAssistive(win uintptr) {
	l := loadATK()
	if l == nil || win == 0 {
		return
	}
	role := l.roleForName("redundant object")
	if role == 0 {
		return
	}
	if obj := l.widgetAccessible(win); obj != 0 {
		l.setRole(obj, role)
	}
}

// dividerName is the accessible name and description of the divider between
// two panes of a split window.
func dividerName(axis caps.Axis) (name, desc string) {
	if axis == caps.Vertical {
		return "Pane divider", "Separates the panes above and below. Focus it and use the up and down arrow keys to resize."
	}
	return "Pane divider", "Separates the panes left and right. Focus it and use the left and right arrow keys to resize."
}

// viewLabel is the accessible name of the pane of view i (0-based) of n in a
// split window.
func viewLabel(i, n int) string { return fmt.Sprintf("Pane %d of %d", i+1, n) }

// leafViews lists the views of a layout in reading order.
func leafViews(n *caps.LayoutNode, out []caps.ViewRef) []caps.ViewRef {
	if n == nil {
		return out
	}
	if n.IsLeaf() {
		return append(out, n.View)
	}
	return leafViews(n.Second, leafViews(n.First, out))
}

// labelPanes names the dividers and, when the window shows several views,
// each view's widget (main thread, after the layout is built).
func (w *Window) labelPanes(layout *caps.LayoutNode) {
	if loadATK() == nil {
		return
	}
	for _, p := range w.panes {
		n, d := dividerName(p.axis)
		accessibleName(p.widget, n, d)
	}
	leaves := leafViews(layout, nil)
	if len(leaves) < 2 {
		return
	}
	for i, ref := range leaves {
		if v := w.viewByRef(ref); v != nil && v.widget != 0 {
			accessibleName(v.widget, viewLabel(i, len(leaves)), "")
		}
	}
}
