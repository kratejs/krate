package csssignals

// Role describes the ARIA surface a CSS-signal scope synthesizes for a given
// `as` preset. Structural roles/attributes are emitted statically. A role with a
// Trigger is operated through the small keyboard/ARIA enhancer (internal/renderer
// cssaria.go): it makes the label triggers focusable, maps Enter/Space/Escape and
// arrow keys, manages focus, and keeps aria-selected/aria-expanded in sync. The
// pointer path stays pure CSS.
type Role struct {
	// Name is the preset name (the `as` value); "" for none.
	Name string
	// Container is the role for the scope anchor (the element carrying the
	// scope class), or "" for none.
	Container string
	// Trigger is the role for each opening trigger label, or "" for none.
	Trigger string
	// Item is the role for labels that close/select (the closed-sentinel
	// triggers: menu items, dialog close buttons). Defaults to "button".
	Item string
	// Panel is the role for each panel element, or "" for none.
	Panel string
	// Control is the role placed on the hidden controller input itself (a
	// native checkbox with role=switch). Such inputs stay focusable and named.
	Control string
	// Orientation, when set, is emitted as aria-orientation on the container.
	Orientation string
	// Haspopup, when set, is emitted as aria-haspopup on triggers.
	Haspopup string
	// Modal marks dialogs that need aria-modal="true".
	Modal bool
	// Anchored positions the panel relative to its trigger (menus, popovers).
	Anchored bool
	// SyncSelected marks triggers whose aria-selected must be maintained by the
	// enhancer (a native radio cannot carry role=tab/option semantics).
	SyncSelected bool
	// SyncExpanded marks triggers whose aria-expanded must be maintained by the
	// enhancer.
	SyncExpanded bool
}

// NeedsRuntime reports whether the role needs the enhancer script. Roles that
// map cleanly onto native controls leave this false, so the zero-JS default
// holds for them.
func (r Role) NeedsRuntime() bool { return r.Trigger != "" || r.SyncSelected || r.SyncExpanded }

// rolePresets maps an `as` value to its ARIA surface. The defaults are chosen so
// the emitted markup uses native semantics where possible; presets that cannot
// be expressed natively opt into the enhancer.
var rolePresets = map[string]Role{
	// Default choice: a native radio group already announces selection.
	"radiogroup": {Name: "radiogroup", Container: "radiogroup"},
	"radio":      {Name: "radio", Container: "radiogroup"},

	// Real tablist semantics need synthesized aria-selected. The tablist role
	// itself belongs on the component's list element (it must only own tabs).
	"tabs":    {Name: "tabs", Trigger: "tab", Panel: "tabpanel", SyncSelected: true, Orientation: "horizontal"},
	"tablist": {Name: "tablist", Trigger: "tab", Panel: "tabpanel", SyncSelected: true, Orientation: "horizontal"},

	// Listbox / options.
	"listbox": {Name: "listbox", Container: "listbox", Trigger: "option", SyncSelected: true},

	// Menus: the trigger is a button that opens a role=menu panel of menuitems.
	"menu":    {Name: "menu", Trigger: "button", Item: "menuitem", Panel: "menu", Haspopup: "menu", SyncExpanded: true, Anchored: true},
	"menubar": {Name: "menubar", Trigger: "button", Item: "menuitem", Panel: "menu", Haspopup: "menu", SyncExpanded: true, Anchored: true, Orientation: "horizontal"},

	// A disclosure navigation: buttons that reveal a panel of links.
	"nav": {Name: "nav", Trigger: "button", SyncExpanded: true, Anchored: true},

	// Disclosure family: aria-expanded must be synthesized.
	"accordion":  {Name: "accordion", Trigger: "button", SyncExpanded: true},
	"disclosure": {Name: "disclosure", Trigger: "button", SyncExpanded: true},
	"dialog":     {Name: "dialog", Trigger: "button", Panel: "dialog", Haspopup: "dialog", Modal: true, SyncExpanded: true},
	"modal":      {Name: "modal", Trigger: "button", Panel: "alertdialog", Haspopup: "dialog", Modal: true, SyncExpanded: true},
	"popover":    {Name: "popover", Trigger: "button", Panel: "dialog", Haspopup: "dialog", SyncExpanded: true, Anchored: true},

	// A native checkbox carries checked state; role=switch goes on the input.
	"switch":   {Name: "switch", Control: "switch"},
	"checkbox": {Name: "checkbox"},
}

// defaultRoleFor returns the preset used when no `as` is given. The empty Role
// means "emit no ARIA" - the native radio/checkbox semantics are already
// correct and keep the output zero-JS.
func defaultRoleFor(kind Kind) Role {
	return Role{}
}

// roleFor resolves a scope's ARIA surface: an explicit `as` preset (falling
// back to no ARIA for an unknown preset), with raw `aria: {...}` overrides
// applied on top. AriaMode "off" suppresses all ARIA.
func roleFor(kind Kind, as, ariaMode string, attrs map[string]string) Role {
	r := defaultRoleFor(kind)
	if as != "" {
		if p, ok := rolePresets[as]; ok {
			r = p
		}
	}
	if ariaMode == "off" {
		r = Role{}
	}
	// Raw overrides win over the preset (including resetting a role to "").
	if v, ok := attrs["role"]; ok {
		r.Trigger = v
	}
	if v, ok := attrs["panel-role"]; ok {
		r.Panel = v
	}
	if v, ok := attrs["container-role"]; ok {
		r.Container = v
	}
	return r
}

// KnownRole reports whether `as` names a supported preset. Used for validation
// so a typo is a hard error rather than silently-nothing.
func KnownRole(as string) bool {
	_, ok := rolePresets[as]
	return ok
}
