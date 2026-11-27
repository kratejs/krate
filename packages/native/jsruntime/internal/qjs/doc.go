// Package qjs is the JavaScript engine facade of jsruntime.
//
// On native targets it re-exports modernc.org/quickjs unchanged. On js/wasm
// (the in-browser playground) QuickJS has no port, so the same API is
// implemented on syscall/js over the host JS engine: the browser or Node that
// runs the wasm module. That variant has no isolation between VMs (all of
// them share the host global object) and ignores memory, stack and time
// limits; it is meant for compile-time evaluation only.
package qjs
