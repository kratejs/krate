package build

import (
	"os"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestParseCgroupLimit(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]int64{"2147483648\n": 2147483648, "max\n": 0, "": 0, "9223372036854771712": 0, "junk": 0} {
		got, ok := parseCgroupLimit(in)
		if got != want || ok != (want > 0) {
			t.Errorf("parseCgroupLimit(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
}

func TestDerivedNodeHeapMB(t *testing.T) {
	t.Parallel()
	// 4 GiB shared by the renderer and one API process: 60% of it, halved.
	if got := derivedNodeHeapMB(4<<30, 2); got != 1228 {
		t.Errorf("4 GiB / 2 processes = %d MB, want 1228", got)
	}
	if got := derivedNodeHeapMB(512<<20, 4); got != minNodeHeapMB {
		t.Errorf("a small container gets the floor, got %d", got)
	}
}

func TestNodeHeapArg(t *testing.T) {
	t.Setenv("NODE_OPTIONS", "")
	cfg := config.Default()
	cfg.SSR.NodeHeapLimit = 768
	if got := nodeHeapArg(cfg); got != "--max-old-space-size=768" {
		t.Errorf("configured limit: %q", got)
	}
	cfg.SSR.NodeHeapLimit = -1
	if got := nodeHeapArg(cfg); got != "" {
		t.Errorf("-1 must leave V8's default, got %q", got)
	}
	cfg.SSR.NodeHeapLimit = 768
	t.Setenv("NODE_OPTIONS", "--max-old-space-size=2048")
	if got := nodeHeapArg(cfg); got != "" {
		t.Errorf("NODE_OPTIONS must win, got %q", got)
	}
	if got := withNodeHeapArg([]string{"a.mjs", "x"}, "--f"); len(got) != 3 || got[0] != "--f" || got[1] != "a.mjs" {
		t.Errorf("withNodeHeapArg = %v", got)
	}
	cfg.SSR.RendererWorkers, cfg.SSR.NodeWorkers = 0, 3
	if n := nodeProcessCount(cfg); n != 4 {
		t.Errorf("nodeProcessCount = %d, want 4", n)
	}
}

func TestCgroupMemoryLimit(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/sys/fs/cgroup/memory.max":                              "max\n",
		"/sys/fs/cgroup/user.slice/memory.max":                   "8589934592\n",
		"/sys/fs/cgroup/user.slice/app.service/memory.max":       "4294967296\n",
		"/sys/fs/cgroup/memory/docker/abc/memory.limit_in_bytes": "2147483648\n",
		"/sys/fs/cgroup/memory/memory.limit_in_bytes":            "9223372036854771712\n",
	}
	read := func(p string) ([]byte, error) {
		if v, ok := files[p]; ok {
			return []byte(v), nil
		}
		return nil, os.ErrNotExist
	}
	// systemd MemoryMax on the service's own cgroup, under a looser slice.
	if n, ok := cgroupMemoryLimit("0::/user.slice/app.service\n", read); !ok || n != 4294967296 {
		t.Errorf("v2 service limit = %d, %v", n, ok)
	}
	// A private cgroup namespace sees its own cgroup as the root.
	if _, ok := cgroupMemoryLimit("0::/\n", read); ok {
		t.Error("an unlimited root must report no limit")
	}
	// cgroup v1 with the memory controller among others.
	if n, ok := cgroupMemoryLimit("4:cpu,cpuacct:/docker/abc\n3:memory:/docker/abc\n", read); !ok || n != 2147483648 {
		t.Errorf("v1 limit = %d, %v", n, ok)
	}
}
