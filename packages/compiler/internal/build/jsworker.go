package build

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/jsworker"
	"github.com/kratejs/krate/packages/compiler/internal/membudget"
)

// Build-time JavaScript (metadata, generateStaticParams, resource-hint
// renders, island server renders) runs on a pool of Node or Bun worker
// processes when one is installed, and in the embedded QuickJS engine
// otherwise, or once a worker fails. KRATE_BUILD_JS picks the engine.

// buildJS holds the build's worker pool; it lives as long as the Builder and
// its processes stop at the end of each build.
type buildJS struct {
	mu   sync.Mutex
	once bool
	pool *jsworker.Pool
}

// jsWorker returns the worker pool, or nil when build-time JavaScript runs
// in QuickJS.
func (b *Builder) jsWorker() *jsworker.Pool {
	b.js.mu.Lock()
	defer b.js.mu.Unlock()
	if !b.js.once {
		b.js.once = true
		var prefs []string
		if b.Cfg != nil {
			prefs = []string{b.Cfg.Runtime, b.Cfg.SSR.SSRRuntime, b.Cfg.SSR.APIRuntime}
		}
		if name, path := jsworker.Select(prefs...); name != "" {
			p := jsworker.New(name, path, 0)
			p.Observe = func(label string, d time.Duration) {
				b.Prof.Add("js:"+name+"-"+label, d)
			}
			// The workers' memory counts against the build's budget.
			membudget.SetExternal(func() int64 {
				var n int64
				for _, pid := range p.PIDs() {
					n += membudget.ProcessRSS(pid)
				}
				return n
			})
			b.js.pool = p
		}
	}
	return b.js.pool
}

// closeJSWorker stops the worker processes. A later evaluation starts them
// again.
func (b *Builder) closeJSWorker() {
	b.js.mu.Lock()
	p := b.js.pool
	b.js.mu.Unlock()
	if p != nil {
		if err := p.Broken(); err != nil {
			fmt.Fprintf(os.Stderr, "  %sWarning: the build-time %s worker failed; the embedded engine ran the rest:%s %v\n", cYellow, p.Name, cReset, err)
		}
		p.Close()
	}
}

// workerScript runs code as a classic script on the worker pool. ok is false
// when no worker could run it and the caller must use QuickJS.
func (b *Builder) workerScript(label, code string, reads []string) (res jsworker.Result, ok bool) {
	p := b.jsWorker()
	if p == nil {
		return jsworker.Result{}, false
	}
	res, err := p.Run(jsworker.Request{Code: code, Reads: reads, Label: label})
	if err != nil || res.Unsupported || len(res.Reads) != len(reads) {
		return jsworker.Result{}, false
	}
	return res, true
}

// quickJSDone returns the jseval hook that records a QuickJS run of label.
func (b *Builder) quickJSDone(label string) func(time.Duration) {
	return func(d time.Duration) { b.Prof.Add("js:quickjs-"+label, d) }
}

// timeQuickJS records a QuickJS run of label in the profile.
func (b *Builder) timeQuickJS(label string) func() {
	return b.phase("js:quickjs-" + label)
}
