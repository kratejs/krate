package plugin

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/native/scheme"
)

// IDPrefix is the namespace of a plugin's ops: "plugin/<name>.".
func IDPrefix(name string) string { return "plugin/" + name + "." }

// OpID is the host id of an op.
func OpID(name, op string) string { return IDPrefix(name) + op }

// PermissionName is the app.json name of a permission a plugin declares.
func PermissionName(name, local string) string { return "plugin:" + name + "." + local }

var (
	nameRe  = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	opRe    = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
	localRe = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
)

// ValidName reports whether s can be a plugin name: lowercase words joined by
// hyphens, 2 to 40 characters.
func ValidName(s string) bool { return len(s) >= 2 && len(s) <= 40 && nameRe.MatchString(s) }

// ValidOp reports whether s can be an op name: lowerCamel, like the SDK ids
// ("info", "cpuLoad").
func ValidOp(s string) bool { return opRe.MatchString(s) }

// ValidPermission reports whether s can be the local name of a permission
// (lowerCamel).
func ValidPermission(s string) bool { return localRe.MatchString(s) }

// PermissionInfo declares one permission of a plugin.
type PermissionInfo struct {
	// Name is the local name; the app.json name is "plugin:<plugin>.<Name>".
	Name string
	Doc  string
}

// OpInfo declares one op.
type OpInfo struct {
	Name string
	// Permission is the local name of the permission that gates the op, or ""
	// for an op that is always allowed.
	Permission string
	// Manual marks an op the plugin registers itself with Context.Func or
	// Context.Stream from Init, instead of generated glue.
	Manual bool
}

// Meta is what the manifest says about a plugin, as the compiler embeds it
// in the generated main package.
type Meta struct {
	Name        string
	Version     string
	Permissions []PermissionInfo
	Ops         []OpInfo
}

// Plugin is one linked plugin.
type Plugin struct {
	Meta Meta
	// Register registers the generated glue on r; it may only register ids
	// of the op set of Meta (see Host.Install).
	Register func(r *scheme.Registry)
	// Init is the plugin's exported Init function, or nil.
	Init func(c *Context) error
}

// Validate checks the metadata.
func (m Meta) Validate() error {
	if !ValidName(m.Name) {
		return fmt.Errorf("plugin name %q is not valid (lowercase words joined by hyphens, 2 to 40 characters)", m.Name)
	}
	perms := map[string]bool{}
	for _, p := range m.Permissions {
		if !ValidPermission(p.Name) {
			return fmt.Errorf("plugin %s: permission name %q is not valid (lowerCamel, for example read or readAll)", m.Name, p.Name)
		}
		if perms[p.Name] {
			return fmt.Errorf("plugin %s: permission %q is declared twice", m.Name, p.Name)
		}
		perms[p.Name] = true
	}
	ops := map[string]bool{}
	for _, o := range m.Ops {
		if !ValidOp(o.Name) {
			return fmt.Errorf("plugin %s: op name %q is not valid (lowerCamel, like the SDK ops: info, cpuLoad)", m.Name, o.Name)
		}
		if ops[o.Name] {
			return fmt.Errorf("plugin %s: op %q is declared twice", m.Name, o.Name)
		}
		ops[o.Name] = true
		if o.Permission != "" && !perms[o.Permission] {
			return fmt.Errorf("plugin %s: op %q needs permission %q, which the plugin does not declare", m.Name, o.Name, o.Permission)
		}
	}
	return nil
}

func (m Meta) op(name string) (OpInfo, bool) {
	for _, o := range m.Ops {
		if o.Name == name {
			return o, true
		}
	}
	return OpInfo{}, false
}

// opFromID returns the op of a full host id, or an error when the id is not
// in the plugin's namespace.
func (m Meta) opFromID(id string) (string, error) {
	pre := IDPrefix(m.Name)
	if !strings.HasPrefix(id, pre) || len(id) == len(pre) {
		return "", &NamespaceError{Plugin: m.Name, ID: id}
	}
	if op := id[len(pre):]; !ValidOp(op) {
		return "", &NamespaceError{Plugin: m.Name, ID: id, NotCamel: true}
	}
	return id[len(pre):], nil
}

// NamespaceError is returned when a plugin tries to register an id outside
// its own namespace, or an op its manifest does not declare.
type NamespaceError struct {
	Plugin string
	ID     string
	// Undeclared is set when the id is in the namespace but not declared.
	Undeclared bool
	// NotCamel is set when the id is in the namespace but its op is not lowerCamel.
	NotCamel bool
}

func (e *NamespaceError) Error() string {
	if e.NotCamel {
		return fmt.Sprintf("plugin %s registers %q, but op names are lowerCamel like the SDK ids (%sinfo, %scpuLoad)", e.Plugin, e.ID, IDPrefix(e.Plugin), IDPrefix(e.Plugin))
	}
	if e.Undeclared {
		return fmt.Sprintf("plugin %s registers %q, which its manifest does not declare as an op", e.Plugin, e.ID)
	}
	return fmt.Sprintf("plugin %s registers %q, which is outside its namespace %q", e.Plugin, e.ID, IDPrefix(e.Plugin))
}
