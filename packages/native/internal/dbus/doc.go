// Package dbus is a small, self-contained D-Bus client in pure Go. It speaks
// the wire protocol over a unix socket with EXTERNAL authentication and
// covers what the Krate SDK needs:
//
//   - method calls with replies and errors (Conn.Call)
//   - signal subscription with bus match rules (Conn.Subscribe)
//   - the Properties interface (GetProperty, SetProperty, GetAllProperties)
//   - exporting objects with methods (Conn.Export), well-known names
//     (Conn.RequestName) and emitting signals (Conn.Emit)
//
// It does not do file descriptor passing, introspection data beyond a
// minimal reply, or non-unix transports.
//
// # Values
//
// Arguments and results are Go values chosen by signature:
//
//	y byte        b bool         n int16     q uint16
//	i int32       u uint32       x int64     t uint64
//	d float64     s string       o ObjectPath   g Signature
//	v Variant     ay []byte      as []string    ao []ObjectPath
//	a{sv} map[string]any whose values are Variant (any map or slice works
//	      when encoding; decoding yields map[string]any, map[any]any or []any)
//	(...) []any, one entry per field
//
// Decoding a variant yields Variant{Sig, Value} with Value decoded by Sig.
//
// # Threading
//
// A Conn is safe for concurrent use. Signal callbacks run on one dispatcher
// goroutine per Conn and may call back into the Conn. Exported methods run on
// their own goroutines.
package dbus
