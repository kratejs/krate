package csssignals

// Role describes the ARIA surface a CSS-signal scope synthesizes for a given
// `as` preset. Structural roles/attributes are emitted statically (zero JS).
// The two "state" flags mark widgets whose state ARIA the user agent does not
// maintain on a native control (tabs, listbox, disclosure/dialog/popover), and
// which therefore require the opt-in micro-runtime to keep in sync.
type Role struct {
	// Container is the role for the scope anchor (the element carrying the
	// scope class), or "" for none.
	Container string
	// Trigger is the role for each trigger label, or "" for none.
	Trigger string
	// Panel is the role for each panel, or "" for none.
	Panel string
	// Orientation, when set, is emitted as aria-orientation on the container.
	Orientation string
	// Haspopup, when set, is emitted as aria-haspopup on triggers.
	Haspopup string
	// Modal marks dialogs that need aria-modal="true".
	Modal bool
	// SyncSelected marks triggers whose aria-selected must be maintained by the
	// micro-runtime (a native radio cannot carry role=tab/option semantics).
	SyncSelected bool
	// SyncExpanded marks triggers whose aria-expanded must be maintained by the
	// micro-runtime.
	SyncExpanded bool
}

// NeedsRuntime reports whether the role requires the tiny ARIA micro-runtime to
// keep synthesized state (aria-selected / aria-expanded) in sync. Roles that map
// cleanly onto native controls leave this false, so the zero-JS default holds.
func (r Role) NeedsRuntime() bool { return r.SyncSelected || r.SyncExpanded }

// rolePresets maps an `as` value to its ARIA surface. The defaults are chosen so
// the emitted markup uses native semantics and needs no JS; presets that cannot
// be expressed natively opt into the micro-runtime.
var rolePresets = map[string]Role{
	// Default choice: a native radio group already announces selection.
	"radiogroup": {Container: "radiogroup"},
	"radio":      {Container: "radiogroup"},

	// Real tablist semantics need synthesized aria-selected.
	"tabs":    {Container: "tablist", Trigger: "tab", Panel: "tabpanel", SyncSelected: true, Orientation: "horizontal"},
	"tablist": {Container: "tablist", Trigger: "tab", Panel: "tabpanel", SyncSelected: true, Orientation: "horizontal"},

	// Listbox / options.
	"listbox": {Container: "listbox", Trigger: "option", SyncSelected: true},
	"menu":    {Container: "menu", Trigger: "menuitemradio", Panel: "menu", SyncSelected: true, Haspopup: "true"},
	"menubar": {Container: "menubar", Trigger: "menuitemradio", Panel: "menu", SyncSelected: true, Haspopup: "true", Orientation: "horizontal"},

	// Disclosure family: aria-expanded must be synthesized.
	"accordion":  {Trigger: "button", Panel: "region", SyncExpanded: true},
	"disclosure": {Trigger: "button", Panel: "region", SyncExpanded: true},
	"dialog":     {Trigger: "button", Panel: "dialog", Haspopup: "dialog", Modal: true, SyncExpanded: true},
	"modal":      {Trigger: "button", Panel: "dialog", Haspopup: "dialog", Modal: true, SyncExpanded: true},
	"popover":    {Trigger: "button", Panel: "group", Haspopup: "true", SyncExpanded: true},

	// Native checkbox carries checked state for free.
	"switch":   {Trigger: "switch"},
	"checkbox": {},
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
