package jsruntime

import (
	"sync"

	"modernc.org/quickjs"
)

// compileIsolated compiles trusted source in a fresh VM of its own. Shared
// bytecode is compiled the first time some VM needs it, often in the middle of
// a request; compiling there would let that VM's memory or stack use make the
// compile fail. QuickJS bytecode is portable across VMs of the same library
// version.
func compileIsolated(src string) ([]byte, error) {
	vm, err := quickjs.NewVM()
	if err != nil {
		return nil, err
	}
	defer vm.Close()
	return vm.Compile(src, EvalGlobal)
}

// sharedBytecode is bytecode compiled once per process. A failed compile is
// not cached, so the next caller tries again.
type sharedBytecode struct {
	mu sync.Mutex
	bc []byte
}

func (c *sharedBytecode) get(src string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bc == nil {
		bc, err := compileIsolated(src)
		if err != nil {
			return nil, err
		}
		c.bc = bc
	}
	return c.bc, nil
}
