// Package dbusiface is the minimal D-Bus client surface the UI half of the
// SDK needs, over the shared internal/dbus client (see Client). The backends
// (notify, tray, shortcut) and their tests use this surface instead of the
// typed internal/dbus API because they build and inspect nested values.
//
// Argument and result values are plain Go values with fixed D-Bus types:
// byte y, bool b, int16 n, uint16 q, int32 i, uint32 u, int64 x, uint64 t,
// float64 d, string s, ObjectPath o, Signature g, Variant v, []T arrays,
// map[K]V dictionaries and structs (exported fields in order). Plain int is
// rejected because its size is ambiguous. Decoded values use the same types,
// except that arrays of non-bytes are []any, dictionaries are map[string]any
// (string and object path keys) or map[any]any, and structs are Struct.
package dbusiface

// ObjectPath is a D-Bus object path (type o).
type ObjectPath string

// Signature is a D-Bus type signature (type g).
type Signature string

// Variant is a D-Bus variant (type v).
type Variant struct{ Value any }

// Struct is a decoded D-Bus struct.
type Struct []any

// Error is a D-Bus error. A method handler returns one to choose the error
// name sent to the caller; Call returns one for an error reply.
type Error struct {
	Name    string
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return e.Name
	}
	return e.Name + ": " + e.Message
}

// Signal is a received D-Bus signal.
type Signal struct {
	Sender    string
	Path      string
	Interface string
	Name      string
	Body      []any
}

// Method handles one exported method call. args are the decoded arguments.
type Method func(args []any) ([]any, error)

// Conn is a connection to a message bus.
type Conn interface {
	// Call invokes a method and returns the reply body.
	Call(dest, path, iface, method string, args ...any) ([]any, error)
	// Subscribe delivers signals matching a bus match rule such as
	// "type='signal',interface='org.x.Y',member='Z'" until cancel is called.
	Subscribe(match string, fn func(sig Signal)) (cancel func(), err error)
	// Export serves methods of iface at path. Calling it again for the same
	// path and interface replaces the methods.
	Export(path, iface string, methods map[string]Method)
	// ExportProperties serves org.freedesktop.DBus.Properties for iface at
	// path; get is called for every read.
	ExportProperties(path, iface string, get func() map[string]Variant)
	// Emit sends a signal from path.
	Emit(path, iface, name string, args ...any) error
	// RequestName claims a well-known bus name.
	RequestName(name string) error
	// Close ends the connection.
	Close() error
}

// UniqueNamer is implemented by connections that know their unique bus name.
type UniqueNamer interface{ UniqueName() string }
