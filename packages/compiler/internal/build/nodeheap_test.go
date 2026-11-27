package build

import (
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
