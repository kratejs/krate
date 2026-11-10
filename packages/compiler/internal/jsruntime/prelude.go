package jsruntime

import (
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
)

// The web API prelude is split into one module per compat ledger feature
// (webapi/<feature>.js) plus webapi/core.js, the shared internals every VM
// installs first. A VM installs the features it is asked to preload; for every
// other feature it installs accessors on the feature's globals that load the
// module the first time one of them is read. Each module is compiled to
// bytecode once per process, in a VM of its own (compileIsolated).

//go:embed webapi/*.js
var preludeFS embed.FS

// preludeModule is one compiled prelude source.
type preludeModule struct {
	name     string
	source   string
	requires []string
	globals  []string
	// host registers the Go host functions the module calls.
	host func(r *Runtime) error

	bc sharedBytecode
}

func (m *preludeModule) bytecode() ([]byte, error) {
	bc, err := m.bc.get(m.source)
	if err != nil {
		return nil, fmt.Errorf("compiling web API module %s: %w", m.name, err)
	}
	return bc, nil
}

// preludeHosts maps each feature to the host functions its module calls.
var preludeHosts = map[string]func(r *Runtime) error{
	"console":  (*Runtime).registerConsoleHost,
	"url":      (*Runtime).registerURLHost,
	"crypto":   (*Runtime).registerCryptoHost,
	"formdata": (*Runtime).registerFormDataHost,
	"fetch":    (*Runtime).registerFetchHost,
}

type preludeSet struct {
	core *preludeModule
	// mods holds the feature modules; order lists them so that every module
	// comes after the ones it requires.
	mods  map[string]*preludeModule
	order []string

	lazyMu sync.Mutex
	lazyBC map[string][]byte
}

var prelude = sync.OnceValues(func() (*preludeSet, error) {
	read := func(name string) (string, error) {
		b, err := preludeFS.ReadFile("webapi/" + name + ".js")
		return string(b), err
	}
	core, err := read("core")
	if err != nil {
		return nil, err
	}
	l, err := compatLedger()
	if err != nil {
		return nil, err
	}
	s := &preludeSet{
		core:   &preludeModule{name: "core", source: core},
		mods:   map[string]*preludeModule{},
		lazyBC: map[string][]byte{},
	}
	for name, f := range l.Features {
		src, err := read(name)
		if err != nil {
			// Features outside the prelude (node, engine, global) have no module.
			if len(f.Globals) > 0 {
				return nil, fmt.Errorf("compat.json: feature %s lists globals but has no webapi/%s.js", name, name)
			}
			continue
		}
		s.mods[name] = &preludeModule{name: name, source: src, requires: f.Requires, globals: f.Globals, host: preludeHosts[name]}
	}
	names := make([]string, 0, len(s.mods))
	for name := range s.mods {
		names = append(names, name)
	}
	sort.Strings(names)
	seen := map[string]int{}
	var visit func(name string) error
	visit = func(name string) error {
		switch seen[name] {
		case 1:
			return fmt.Errorf("compat.json: feature %s requires itself", name)
		case 2:
			return nil
		}
		m, ok := s.mods[name]
		if !ok {
			return fmt.Errorf("compat.json: a prelude feature requires %s, which has no module", name)
		}
		seen[name] = 1
		for _, req := range m.requires {
			if err := visit(req); err != nil {
				return err
			}
		}
		seen[name] = 2
		s.order = append(s.order, name)
		return nil
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return s, nil
})

// PreludeFeatures returns the names of the features the web API prelude
// provides, each after the features it requires.
func PreludeFeatures() []string {
	s, err := prelude()
	if err != nil {
		return nil
	}
	return slices.Clone(s.order)
}

// installPrelude installs core.js, the preloaded features and the loading
// accessors for the rest. preload nil means every feature.
func (r *Runtime) installPrelude(preload []string) error {
	s, err := prelude()
	if err != nil {
		return err
	}
	if err := r.vm.RegisterHostFunc("__krate_load_feature", r.loadFeatureHost); err != nil {
		return err
	}
	if err := r.evalModule(s.core); err != nil {
		return err
	}
	if preload == nil {
		preload = s.order
	}
	for _, name := range preload {
		if _, ok := s.mods[name]; !ok {
			// Scanned feature lists also name groups outside the prelude.
			continue
		}
		if err := r.loadFeature(name); err != nil {
			return err
		}
	}
	return r.installLazy(s)
}

// installLazy defines the loading accessors for every feature not loaded yet.
// The installer script depends only on that set, so its bytecode is cached.
func (r *Runtime) installLazy(s *preludeSet) error {
	var lazy []string
	for _, name := range s.order {
		if !r.loaded[name] {
			lazy = append(lazy, name)
		}
	}
	if len(lazy) == 0 {
		return nil
	}
	key := strings.Join(lazy, ",")
	s.lazyMu.Lock()
	bc, ok := s.lazyBC[key]
	s.lazyMu.Unlock()
	if !ok {
		spec := map[string][]string{}
		for _, name := range lazy {
			spec[name] = s.mods[name].globals
		}
		js, _ := json.Marshal(spec)
		var err error
		bc, err = compileIsolated("globalThis[Symbol.for('krate.web')].lazy(" + string(js) + ");")
		if err != nil {
			return fmt.Errorf("compiling web API loaders: %w", err)
		}
		s.lazyMu.Lock()
		s.lazyBC[key] = bc
		s.lazyMu.Unlock()
	}
	if _, err := r.vm.EvalBytecode(bc); err != nil {
		return fmt.Errorf("installing web API loaders: %w", err)
	}
	return nil
}

// loadFeatureHost is the __krate_load_feature host function core.js calls on
// first use of a lazily loaded feature. It reports failure as a message, never
// as a Go error, and core.js throws it: modernc QuickJS aborts under checkptr
// (go test -race) when a host function returns an error. Evaluating a module
// re-arms the VM's eval deadline, so each feature a script loads grants it at
// most one more timeout budget.
func (r *Runtime) loadFeatureHost(args []any) (res any, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			res, err = fmt.Sprintf("panic: %v", rec), nil
		}
	}()
	name, _ := firstArg(args).(string)
	if e := r.loadFeature(name); e != nil {
		return e.Error(), nil
	}
	return "", nil
}

func firstArg(args []any) any {
	if len(args) == 0 {
		return nil
	}
	return args[0]
}

// loadFeature installs a feature and the features it requires. It runs both
// while a VM is created and re-entrantly from loadFeatureHost.
func (r *Runtime) loadFeature(name string) error {
	if r.loaded[name] {
		return nil
	}
	s, err := prelude()
	if err != nil {
		return err
	}
	m, ok := s.mods[name]
	if !ok {
		return fmt.Errorf("unknown web API feature %q", name)
	}
	r.loaded[name] = true
	for _, req := range m.requires {
		if err := r.loadFeature(req); err != nil {
			r.loaded[name] = false
			return err
		}
	}
	if m.host != nil {
		if err := m.host(r); err != nil {
			r.loaded[name] = false
			return err
		}
	}
	if err := r.evalModule(m); err != nil {
		r.loaded[name] = false
		return err
	}
	return nil
}

func (r *Runtime) evalModule(m *preludeModule) error {
	bc, err := m.bytecode()
	if err != nil {
		return err
	}
	if _, err := r.vm.EvalBytecode(bc); err != nil {
		return fmt.Errorf("evaluating web API module %s: %w", m.name, err)
	}
	return nil
}

// LoadedFeatures returns the prelude features installed in r so far.
func (r *Runtime) LoadedFeatures() []string {
	var out []string
	for name, ok := range r.loaded {
		if ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
