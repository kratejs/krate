// Package permission defines the SDK permission names, parses the app.json
// "permissions" list into a Set and gates registration of UI-facing ids.
//
// Go host code that calls the SDK packages directly is trusted and never
// checked, and so is TypeScript host code (its __krateHost.call reaches the
// Trusted twin of each id). Permissions only decide which "krate/*" ids the UI
// may call. A call to an id whose permission is not granted fails
// with a structured error:
//
//	{code: "permission_denied", data: {permission: "fs"}}
//
// Permission table
//
//	os:info             yes (default)  app dirs, OS info
//	kv                  yes (default)  key-value store
//	db                  yes (default)  SQLite database
//	fs                  no             read and write inside allowed roots; implies fs:read
//	fs:read             no             read-only access inside allowed roots
//	os:secrets          no             set and delete secrets
//	os:secrets.read     no             read secrets (never implied by os:secrets)
//	shell:openExternal  no             open http, https and mailto URLs in the OS handler
//	net                 no             reserved for network policy; no UI id yet
//	window              yes (default)  open and control windows
//	app                 yes (default)  quit, relaunch, capability checks, second-instance events
//	menu                yes (default)  application menu, context menus
//	dialog              yes (default)  native file and message dialogs
//	notify              yes (default)  desktop notifications
//	clipboard           yes (default)  write the clipboard
//	clipboard:read      no             read the clipboard
//	tray                no             system tray icon
//	shortcut            no             global keyboard shortcuts
//	updater             no             check for, download, apply updates and relaunch
//	badge               yes (default)  badge and progress on the app icon
//	screen              yes (default)  list displays, center windows, display change events
//	power               yes (default)  power and idle state, suspend, resume, lock and battery events
//	power:inhibit       no             keep the system or display awake
//	login               no             read and change whether the app starts at login
//	theme               yes (default)  system appearance and title bar theme
//	crash               no             list, read and delete crash reports, crash events
//
// The compiler keeps a copy of the names in names.json (a test here keeps the
// file in step; run "go test ./sdk/permission -update" to rewrite it).
//
// An empty or missing "permissions" list means the defaults above. A non-empty
// list grants exactly what it names (plus what a name implies), so an app that
// lists ["fs"] also loses kv and db unless it lists them. Other SDK areas call
// Define from an init function to add their names.
package permission

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/internal/glue"
	"github.com/kratejs/krate/packages/native/wire"
)

// Info describes one permission.
type Info struct {
	Name string
	Doc  string
	// Default reports whether an app with no "permissions" list has it.
	Default bool
	// Implies lists permissions granted along with this one.
	Implies []string
}

var (
	mu    sync.RWMutex
	table = map[string]Info{}
)

// Define adds a permission to the table. It panics on a duplicate name.
func Define(i Info) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := table[i.Name]; dup {
		panic(fmt.Sprintf("permission: %q defined twice", i.Name))
	}
	table[i.Name] = i
}

// DefineOnce adds a permission unless one of that name exists, and reports
// whether it added it. Plugins use it: a plugin host may be created many times
// in one process and a plugin permission is defined by its manifest.
func DefineOnce(i Info) bool {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := table[i.Name]; dup {
		return false
	}
	table[i.Name] = i
	return true
}

func init() {
	for _, i := range []Info{
		{Name: "os:info", Doc: "app directories and OS information", Default: true},
		{Name: "kv", Doc: "key-value store", Default: true},
		{Name: "db", Doc: "SQLite database", Default: true},
		{Name: "fs", Doc: "read and write files inside the app directories and granted paths", Implies: []string{"fs:read"}},
		{Name: "fs:read", Doc: "read files inside the app directories and granted paths"},
		{Name: "os:secrets", Doc: "set and delete secrets in the OS credential store"},
		{Name: "os:secrets.read", Doc: "read secrets from the OS credential store"},
		{Name: "shell:openExternal", Doc: "open http, https and mailto URLs in the default handler"},
		{Name: "net", Doc: "reserved for network policy; no UI id yet"},
		{Name: "window", Doc: "open and control windows", Default: true},
		{Name: "app", Doc: "quit or relaunch the app, query capabilities, receive second-instance events", Default: true},
		{Name: "menu", Doc: "set the application menu and show context menus", Default: true},
		{Name: "dialog", Doc: "show native file and message dialogs", Default: true},
		{Name: "notify", Doc: "post desktop notifications", Default: true},
		{Name: "clipboard", Doc: "write the system clipboard", Default: true},
		{Name: "clipboard:read", Doc: "read the system clipboard (sees what the user copied elsewhere)"},
		{Name: "tray", Doc: "show a system tray icon"},
		{Name: "shortcut", Doc: "register global keyboard shortcuts"},
		{Name: "updater", Doc: "check for, download and apply app updates, and relaunch"},
	} {
		Define(i)
	}
}

// All returns the permission table sorted by name.
func All() []Info {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Info, 0, len(table))
	for _, i := range table {
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// Set is a parsed permission list. The zero Set allows nothing.
type Set struct {
	names map[string]bool
	sc    *scopes
}

// scopes narrows the permissions of single windows. It is shared by every
// copy of a Set.
type scopes struct {
	mu    sync.RWMutex
	byWin map[string]map[string]bool
}

func newSet() Set {
	return Set{names: map[string]bool{}, sc: &scopes{byWin: map[string]map[string]bool{}}}
}

// Defaults returns the permissions of an app that declares none.
func Defaults() Set {
	mu.RLock()
	defer mu.RUnlock()
	s := newSet()
	for _, i := range table {
		if i.Default {
			s.add(i)
		}
	}
	return s
}

// add must be called with mu held (read is enough).
func (s Set) add(i Info) {
	s.names[i.Name] = true
	for _, n := range i.Implies {
		s.names[n] = true
		if sub, ok := table[n]; ok {
			s.add(sub)
		}
	}
}

// AllowAll returns a Set with every defined permission.
func AllowAll() Set {
	mu.RLock()
	defer mu.RUnlock()
	s := newSet()
	for _, i := range table {
		s.add(i)
	}
	return s
}

// None returns a Set that allows nothing (apart from the empty name). An
// app.json with "strictPermissions" and no "permissions" gets it.
func None() Set { return newSet() }

// Parse builds a Set from an app.json list. An empty list gives Defaults.
// An unknown name is an error that lists the valid ones.
func Parse(list []string) (Set, error) {
	if len(list) == 0 {
		return Defaults(), nil
	}
	mu.RLock()
	defer mu.RUnlock()
	s := newSet()
	for _, n := range list {
		i, ok := table[n]
		if !ok {
			names := make([]string, 0, len(table))
			for k := range table {
				names = append(names, k)
			}
			sort.Strings(names)
			return Set{}, fmt.Errorf("unknown permission %q (known: %s)", n, strings.Join(names, ", "))
		}
		s.add(i)
	}
	return s, nil
}

// Allows reports whether name is granted to the app. The empty name is always
// allowed. Per-window scopes are not consulted; see AllowsFor.
func (s Set) Allows(name string) bool { return name == "" || s.names[name] }

// AllowsFor reports whether a call made by the given window may use name: the
// app grants it and the window's scope, when it has one, does too.
func (s Set) AllowsFor(window, name string) bool {
	if !s.Allows(name) {
		return false
	}
	if name == "" || s.sc == nil || window == "" {
		return true
	}
	s.sc.mu.RLock()
	defer s.sc.mu.RUnlock()
	sc, ok := s.sc.byWin[window]
	return !ok || sc[name]
}

// Scope narrows the permissions of calls made by window to list (names and
// what they imply). A list may only name permissions the app grants; the
// first one it does not is returned as a Denied error. An empty, non-nil list
// leaves the window with nothing.
func (s Set) Scope(window string, list []string) error {
	if s.sc == nil {
		return errors.New("permission: the zero Set cannot scope windows")
	}
	narrow, err := Parse(list)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		narrow = None()
	}
	for n := range narrow.names {
		if !s.Allows(n) {
			return Denied(n)
		}
	}
	s.sc.mu.Lock()
	s.sc.byWin[window] = narrow.names
	s.sc.mu.Unlock()
	return nil
}

// ScopeChild scopes a window that the window caller is opening. With a list
// (has) the new window gets exactly those permissions, which the caller must
// itself have; without one it inherits the caller's scope. It reports whether
// the new window ended up scoped.
func (s Set) ScopeChild(caller, window string, list []string, has bool) (bool, error) {
	if s.sc == nil {
		return false, nil
	}
	if !has {
		names, ok := s.ScopeOf(caller)
		if !ok {
			return false, nil
		}
		return true, s.Scope(window, names)
	}
	narrow, err := Parse(list)
	if err != nil {
		return false, err
	}
	if len(list) == 0 {
		narrow = None()
	}
	for n := range narrow.names {
		if !s.AllowsFor(caller, n) {
			return false, Denied(n)
		}
	}
	return true, s.Scope(window, list)
}

// Unscope removes the scope of a window (it closed).
func (s Set) Unscope(window string) {
	if s.sc == nil {
		return
	}
	s.sc.mu.Lock()
	delete(s.sc.byWin, window)
	s.sc.mu.Unlock()
}

// ScopeOf returns the granted names of a scoped window, sorted, and whether
// the window has a scope.
func (s Set) ScopeOf(window string) ([]string, bool) {
	if s.sc == nil {
		return nil, false
	}
	s.sc.mu.RLock()
	defer s.sc.mu.RUnlock()
	sc, ok := s.sc.byWin[window]
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(sc))
	for n := range sc {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, true
}

// List returns the granted names, sorted.
func (s Set) List() []string {
	out := make([]string, 0, len(s.names))
	for n := range s.names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Denied returns the error a call fails with when permission name is missing.
func Denied(name string) error {
	e := krate.Errorf("permission_denied", "permission %q is not granted in app.json", name).(*krate.Error)
	e.Data = map[string]string{"permission": name}
	return e
}

// Register adds f under id when the id's permission (from the sdk table) is
// granted, and a stub that fails with Denied otherwise. It panics when id is
// not in the table: every UI id must be listed in sdk.IDs.
func Register(r *scheme.Registry, s Set, id string, f scheme.Func) {
	row, ok := sdk.Lookup(id)
	if !ok {
		panic(fmt.Sprintf("permission: %q is not in the sdk id table", id))
	}
	r.Trusted(id, f)
	if s.Allows(row.Permission) {
		perm := row.Permission
		r.Func(id, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
			if !s.AllowsFor(ctx.Window(), perm) {
				return Denied(perm)
			}
			return f(ctx, args, out)
		})
		return
	}
	perm := row.Permission
	r.Func(id, func(krate.Ctx, *wire.Decoder, *wire.Encoder) error { return Denied(perm) })
}

// RegisterIn registers the permission area: krate/permission.query.
func RegisterIn(r *scheme.Registry, s Set) {
	const id = "krate/permission.query"
	Register(r, s, id, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		a, err := glue.Open(id, args)
		if err != nil {
			return err
		}
		name, err := a.String("name")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		out.Bool(s.Allows(name))
		return nil
	})
}
