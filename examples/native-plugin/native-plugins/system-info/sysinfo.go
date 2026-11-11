// Package sysinfo is a Krate Native plugin that reports operating system, CPU
// and memory information. Info and Memory answer once, CPULoad streams the CPU
// load, and Load is a signal a background sampler keeps current while the app
// runs.
package sysinfo

import (
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/plugin"
)

// SystemInfo describes the machine.
type SystemInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Hostname  string `json:"hostname"`
	CPUs      int    `json:"cpus"`
	GoVersion string `json:"goVersion"`
}

// MemoryInfo is the physical memory in bytes. Available is 0 where the
// platform does not report it.
type MemoryInfo struct {
	Total     uint64 `json:"total"`
	Available uint64 `json:"available"`
}

// Load is the CPU load of the whole machine in percent, 0 to 100, as
// the sampler last measured it.
var Load = krate.NewSignal(0.0)

// Info returns the operating system, architecture, host name and CPU count.
func Info() (SystemInfo, error) {
	host, err := os.Hostname()
	if err != nil {
		return SystemInfo{}, err
	}
	return SystemInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, Hostname: host, CPUs: runtime.NumCPU(), GoVersion: runtime.Version()}, nil
}

// Memory returns the physical memory.
func Memory() (MemoryInfo, error) { return readMemory() }

// CPULoad streams the CPU load in percent every intervalMs milliseconds (at
// least 200) until the page stops reading.
func CPULoad(ctx krate.Ctx, intervalMs int) (<-chan float64, error) {
	if intervalMs < 200 {
		intervalMs = 200
	}
	s, err := newSampler()
	if err != nil {
		return nil, err
	}
	out := make(chan float64)
	go func() {
		defer close(out)
		t := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				v, err := s.sample()
				if err != nil {
					continue
				}
				select {
				case out <- v:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// Init keeps the Load signal current while the app runs: a sampler starts with
// the platform and stops when the app quits.
func Init(c *plugin.Context) error {
	var (
		mu   sync.Mutex
		stop chan struct{}
	)
	c.OnStart(func() error {
		s, err := newSampler()
		if err != nil {
			c.Logf("no CPU load sampler: %v", err)
			return nil
		}
		mu.Lock()
		stop = make(chan struct{})
		done := stop
		mu.Unlock()
		go func() {
			t := time.NewTicker(2 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					if v, err := s.sample(); err == nil {
						Load.Set(v)
					}
				}
			}
		}()
		return nil
	})
	c.OnQuit(func() {
		mu.Lock()
		defer mu.Unlock()
		if stop != nil {
			close(stop)
			stop = nil
		}
	})
	return nil
}
