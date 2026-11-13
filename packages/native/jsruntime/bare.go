package jsruntime

import (
	_ "embed"
	"fmt"
)

//go:embed bare.js
var bareSource string

var bareModule = &preludeModule{name: "bare", source: bareSource}

// bareFeatures are the prelude features a bare VM installs: the compile-time
// evaluator may call structuredClone and console (see globalBuiltins in
// internal/renderer/ssreval.go); everything else it allows is an engine
// built-in.
var bareFeatures = []string{"clone", "console"}

// NewBare creates a runtime for compile-time expression folding. It has the
// ECMAScript built-ins, structuredClone and console, and nothing else: no
// fetch, crypto, timers, process or require, no host function that reaches
// the network, the filesystem, randomness or request state, and no loader for
// the rest of the prelude. Built-ins whose result depends on the locale or
// time zone (localeCompare, toLocale*String, Date.prototype.toString, ...)
// throw, so a folded value never depends on the build machine.
func NewBare() (*Runtime, error) {
	r, err := newRuntime(Options{})
	if err != nil {
		return nil, err
	}
	if err := r.installBare(); err != nil {
		r.vm.Close()
		return nil, fmt.Errorf("failed to set up the build-time JS runtime: %w", err)
	}
	return r, nil
}

func (r *Runtime) installBare() error {
	s, err := prelude()
	if err != nil {
		return err
	}
	if err := r.evalModule(s.core); err != nil {
		return err
	}
	for _, name := range bareFeatures {
		if err := r.loadFeature(name); err != nil {
			return err
		}
	}
	return r.evalModule(bareModule)
}
