// Package jsruntime is Krate's embedded JavaScript engine: QuickJS
// (modernc.org/quickjs, no cgo) with a lazily loaded Web API prelude (fetch,
// URL, streams, crypto, timers, FormData and more, listed in compat.json),
// Go-backed fetch and crypto, raw ArrayBuffer transfer instead of base64, and
// resource limits.
//
// The compiler uses it for build-time evaluation, plugins and the embedded API
// and middleware runtime (APIRouteRuntime, MiddlewareRuntime). Native apps use
// it through package hostjs.
//
// A Runtime is not safe for concurrent use: one goroutine at a time may touch
// a VM. Evaluations are bounded by Options.EvalTimeout, and EnableAsyncFetch
// switches fetch from blocking the VM to completing through a callback the
// owner of the VM runs.
package jsruntime
