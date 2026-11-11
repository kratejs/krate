// Package permission defines the SDK permission names, parses the app.json
// "permissions" list into a Set and gates registration of UI-facing ids.
//
// Go host code that calls the SDK packages directly is trusted and never
// checked. Permissions only decide which "krate/*" ids the UI (and TypeScript
// host code) may call. A call to an id whose permission is not granted fails
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
//
// An empty or missing "permissions" list means the defaults above. A non-empty
// list grants exactly what it names (plus what a name implies), so an app that
// lists ["fs"] also loses kv and db unless it lists them. Other SDK areas call
// Define from an init function to add their names.
package permission

import (
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
}

// Defaults returns the permissions of an app that declares none.
func Defaults() Set {
	mu.RLock()
	defer mu.RUnlock()
	s := Set{names: map[string]bool{}}
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
	s := Set{names: map[string]bool{}}
	for _, i := range table {
		s.add(i)
	}
	return s
}

// Parse builds a Set from an app.json list. An empty list gives Defaults.
// An unknown name is an error that lists the valid ones.
func Parse(list []string) (Set, error) {
	if len(list) == 0 {
		return Defaults(), nil
	}
	mu.RLock()
	defer mu.RUnlock()
	s := Set{names: map[string]bool{}}
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

// Allows reports whether name is granted. The empty name is always allowed.
func (s Set) Allows(name string) bool { return name == "" || s.names[name] }

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
	if s.Allows(row.Permission) {
		r.Func(id, f)
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
