//go:build js

package jsruntime

// installBare on js/wasm only applies the locale and time zone stubs. The
// prelude (core.js and its features) is not loaded: it would replace fetch,
// URL, crypto and console on the host global object that the wasm module
// shares with the page. The ECMAScript built-ins, structuredClone and console
// are the host's own. The stubs change String, Number, Array and Date
// prototypes of the host, so the engine must run in a dedicated worker.
func (r *Runtime) installBare() error {
	if _, err := r.Execute("delete globalThis.__krate_bin"); err != nil {
		return err
	}
	return r.evalModule(bareModule)
}
