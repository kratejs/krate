package membudget

import (
	"errors"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		in    string
		total int64
		want  int64
	}{
		{"1073741824", 0, gib},
		{"4G", 0, 4 * gib},
		{"4g", 0, 4 * gib},
		{"4GiB", 0, 4 * gib},
		{"4gb", 0, 4 * gib},
		{" 512M ", 0, 512 * mib},
		{"512 MiB", 0, 512 * mib},
		{"2.5G", 0, 5 * gib / 2},
		{"1T", 0, 1 << 40},
		{"64k", 0, 64 * kib},
		{"50%", 8 * gib, 4 * gib},
		{"12.5%", 8 * gib, gib},
		{"100%", 8 * gib, 8 * gib},
	}
	for _, c := range cases {
		got, err := ParseSize(c.in, c.total)
		if err != nil || got != c.want {
			t.Errorf("ParseSize(%q, %d) = %d, %v; want %d", c.in, c.total, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "G", "-1G", "0", "4X", "4GG", "abc", "50%%", "150%", "0%", "1e400"} {
		if _, err := ParseSize(bad, 8*gib); err == nil {
			t.Errorf("ParseSize(%q) succeeded; want an error", bad)
		}
	}
	if _, err := ParseSize("50%", 0); err == nil {
		t.Error("a percentage of an unknown total succeeded")
	}
}

func TestDetect(t *testing.T) {
	sys := system{total: 32 * gib, avail: 24 * gib}
	b, err := detect("", sys)
	if err != nil || b.Bytes != 18*gib || b.Source != "available" || b.Total != 32*gib {
		t.Fatalf("available: %+v, %v", b, err)
	}
	b, _ = detect("", system{total: 32 * gib, avail: 4 * gib, cgroup: true})
	if b.Bytes != 3*gib || b.Source != "cgroup" {
		t.Fatalf("cgroup: %+v", b)
	}
	b, _ = detect("6G", sys)
	if b.Bytes != 6*gib || b.Source != EnvVar {
		t.Fatalf("env: %+v", b)
	}
	b, _ = detect("25%", sys)
	if b.Bytes != 8*gib {
		t.Fatalf("env percent: %+v", b)
	}
	// A tiny budget is raised to the minimum: the jobs then run one at a time.
	b, _ = detect("1M", sys)
	if b.Bytes != minBudget {
		t.Fatalf("env tiny: %+v", b)
	}
	b, _ = detect("", system{avail: 100 * mib})
	if b.Bytes != minBudget {
		t.Fatalf("little available: %+v", b)
	}
	// A bad override falls back to the system's budget and says why.
	b, err = detect("lots", sys)
	if err == nil || !strings.Contains(err.Error(), EnvVar) || b.Bytes != 18*gib {
		t.Fatalf("bad env: %+v, %v", b, err)
	}
	// Nothing known: a fixed fallback, at most half the machine.
	if b, _ = detect("", system{}); b.Bytes != fallbackBudget || b.Source != "default" {
		t.Fatalf("unknown: %+v", b)
	}
	if b, _ = detect("", system{total: 2 * gib}); b.Bytes != gib {
		t.Fatalf("total only: %+v", b)
	}
}

func TestParseMeminfo(t *testing.T) {
	const meminfo = `MemTotal:       32819516 kB
MemFree:        19872380 kB
MemAvailable:   25100800 kB
Buffers:          123456 kB
`
	total, avail := parseMeminfo(strings.NewReader(meminfo))
	if total != 32819516*kib || avail != 25100800*kib {
		t.Fatalf("got %d, %d", total, avail)
	}
	if total, avail := parseMeminfo(strings.NewReader("garbage\n\n")); total != 0 || avail != 0 {
		t.Fatalf("garbage: %d, %d", total, avail)
	}
}

func TestStatmRSS(t *testing.T) {
	if got := statmRSS("1000 250 30 4 0 200 0\n", 4096); got != 250*4096 {
		t.Fatalf("got %d", got)
	}
	if got := statmRSS("", 4096); got != 0 {
		t.Fatalf("empty: %d", got)
	}
}

// fakeFS is a file tree for the system readers.
type fakeFS map[string]string

func (f fakeFS) read(name string) ([]byte, error) {
	if s, ok := f[name]; ok {
		return []byte(s), nil
	}
	return nil, fs.ErrNotExist
}

func TestLinuxSystemCgroup(t *testing.T) {
	files := fakeFS{
		"/proc/meminfo":     "MemTotal: 33554432 kB\nMemAvailable: 25165824 kB\n",
		"/proc/self/cgroup": "0::/user.slice/app.slice/job.scope\n",
		// The leaf has no limit; its parent has 8 GiB, with 6 GiB in use of
		// which 1 GiB is reclaimable page cache.
		"/sys/fs/cgroup/user.slice/app.slice/job.scope/memory.max":     "max\n",
		"/sys/fs/cgroup/user.slice/app.slice/job.scope/memory.current": "1073741824\n",
		"/sys/fs/cgroup/user.slice/app.slice/memory.max":               "8589934592\n",
		"/sys/fs/cgroup/user.slice/app.slice/memory.current":           "6442450944\n",
		"/sys/fs/cgroup/user.slice/app.slice/memory.stat":              "anon 5368709120\ninactive_file 1073741824\nactive_file 0\n",
		"/sys/fs/cgroup/user.slice/memory.max":                         "17179869184\n",
		"/sys/fs/cgroup/user.slice/memory.current":                     "1073741824\n",
	}
	sys := linuxSystem(files.read, "/sys/fs/cgroup")
	if !sys.cgroup || sys.total != 8*gib || sys.avail != 3*gib {
		t.Fatalf("got %+v", sys)
	}

	// No limit anywhere: the machine's numbers.
	delete(files, "/sys/fs/cgroup/user.slice/app.slice/memory.max")
	delete(files, "/sys/fs/cgroup/user.slice/memory.max")
	sys = linuxSystem(files.read, "/sys/fs/cgroup")
	if sys.cgroup || sys.total != 32*gib || sys.avail != 24*gib {
		t.Fatalf("unlimited: %+v", sys)
	}

	// A cgroup v1 process (no "0::" line) and no meminfo: nothing known.
	sys = linuxSystem(fakeFS{"/proc/self/cgroup": "4:memory:/docker/abc\n"}.read, "/sys/fs/cgroup")
	if sys.total != 0 || sys.avail != 0 {
		t.Fatalf("v1: %+v", sys)
	}
}

func TestCgroupOverLimit(t *testing.T) {
	read := func(name string) (string, error) {
		switch name {
		case "/memory.max":
			return "1073741824", nil
		case "/memory.current":
			return "2147483648", nil
		}
		return "", errors.New("missing")
	}
	limit, room, ok := cgroupLimits(read, "")
	if !ok || limit != gib || room != 0 {
		t.Fatalf("got %d %d %v", limit, room, ok)
	}
}

func TestShort(t *testing.T) {
	budget := 10 * gib
	roomy := system{total: 32 * gib, avail: 20 * gib}
	if short(budget, 2*gib, 5*gib, roomy) {
		t.Error("pressure with room to spare")
	}
	if !short(budget, 7*gib, 8*gib, roomy) {
		t.Error("no pressure with live data at 70% of the budget")
	}
	if !short(budget, 2*gib, 19*gib/2, roomy) {
		t.Error("no pressure at 95% of the budget")
	}
	if !short(budget, gib, gib, system{total: 32 * gib, avail: gib}) {
		t.Error("no pressure with the machine short of memory")
	}
	if short(budget, gib, gib, system{total: 32 * gib, avail: gib, guessed: true}) {
		t.Error("pressure from a guessed figure")
	}
}

func TestMonitorCaches(t *testing.T) {
	var reads int
	m := &monitor{
		read:  func() system { reads++; return system{total: 32 * gib, avail: 20 * gib} },
		usage: func() (int64, int64) { return gib, 2 * gib },
	}
	now := time.Now()
	m.check(now)
	m.check(now.Add(checkEvery / 2))
	if reads != 1 {
		t.Fatalf("read %d times within %s", reads, checkEvery)
	}
	m.check(now.Add(2 * checkEvery))
	if reads != 2 {
		t.Fatalf("read %d times after %s", reads, 2*checkEvery)
	}
}

func TestGateSlots(t *testing.T) {
	g := newGate(3, func() bool { return false })
	var cur, peak atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Acquire()
			n := cur.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			cur.Add(-1)
			g.Release()
		}()
	}
	wg.Wait()
	if p := peak.Load(); p != 3 {
		t.Fatalf("peak %d jobs at once; want 3", p)
	}
}

func TestGateUnderPressureRunsOneAtATime(t *testing.T) {
	// Memory stays short: every job still runs, one at a time.
	g := newGate(8, func() bool { return true })
	var cur, peak, done atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Acquire()
			if n := cur.Add(1); n > peak.Load() {
				peak.Store(n)
			}
			time.Sleep(time.Millisecond)
			cur.Add(-1)
			done.Add(1)
			g.Release()
		}()
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("jobs did not finish under constant pressure")
	}
	if peak.Load() != 1 || done.Load() != 10 {
		t.Fatalf("peak %d, done %d; want 1, 10", peak.Load(), done.Load())
	}
}

func TestGateNestedGatesProgress(t *testing.T) {
	// A job of one gate that waits for jobs of another still finishes when
	// memory is short: each gate always runs one job.
	outer := newGate(4, func() bool { return true })
	inner := newGate(4, func() bool { return true })
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outer.Acquire()
			defer outer.Release()
			var iw sync.WaitGroup
			for range 3 {
				iw.Add(1)
				go func() {
					defer iw.Done()
					inner.Acquire()
					time.Sleep(time.Millisecond)
					inner.Release()
				}()
			}
			iw.Wait()
		}()
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("nested gates deadlocked")
	}
}

func TestGateResumesWhenPressureEases(t *testing.T) {
	var short atomic.Bool
	short.Store(true)
	g := newGate(4, func() bool { return short.Load() })
	g.Acquire() // the first job always runs
	admitted := make(chan struct{})
	go func() {
		g.Acquire()
		close(admitted)
	}()
	select {
	case <-admitted:
		t.Fatal("second job admitted under pressure")
	case <-time.After(3 * pollEvery):
	}
	short.Store(false)
	select {
	case <-admitted:
	case <-time.After(5 * time.Second):
		t.Fatal("second job not admitted once the pressure eased")
	}
	g.Release()
	g.Release()
}

func TestGateFIFO(t *testing.T) {
	g := newGate(1, func() bool { return false })
	g.Acquire()
	var order []int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Acquire()
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			g.Release()
		}()
		// Queue them in order.
		for {
			g.mu.Lock()
			n := len(g.queue)
			g.mu.Unlock()
			if n == i+1 {
				break
			}
			time.Sleep(time.Millisecond)
		}
	}
	g.Release()
	wg.Wait()
	for i, v := range order {
		if v != i {
			t.Fatalf("admitted in order %v; want 0..4", order)
		}
	}
}

func TestGoLimit(t *testing.T) {
	cases := []struct{ budget, reserve, want int64 }{
		// Half of a large budget.
		{18 * gib, gib, 9 * gib},
		// At least heapTarget when the budget allows it.
		{6 * gib, gib, heapTarget},
		// Never above the budget less the reserve.
		{3 * gib, gib / 4, 3*gib - gib/4},
		// The reserve takes at most half the budget.
		{gib, 4 * gib, gib / 2},
		{minBudget, minBudget, minBudget},
	}
	for _, c := range cases {
		if got := GoLimit(c.budget, c.reserve); got != c.want {
			t.Errorf("GoLimit(%d, %d) = %d; want %d", c.budget, c.reserve, got, c.want)
		}
	}
}

func TestSetCurrent(t *testing.T) {
	old, oldErr := Current()
	defer func() { SetCurrent(old); currentErr = oldErr }()
	SetCurrent(Budget{Bytes: 3 * gib, Source: "test"})
	if b, err := Current(); err != nil || b.Bytes != 3*gib {
		t.Fatalf("got %+v, %v", b, err)
	}
}
