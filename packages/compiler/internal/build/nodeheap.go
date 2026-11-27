package build

import (
	"os"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// nodeHeapShare is the part of the container's memory the Node processes'
// old-generation heaps may use together. The rest covers the Go server, the
// young generation, code and buffers outside the V8 heap.
const nodeHeapShare = 0.6

// minNodeHeapMB keeps a derived limit from starving a small container's
// processes into constant collection.
const minNodeHeapMB = 256

// nodeProcessCount is how many Node processes with a heap of their own the
// server may run: the SSR renderer's and the API sidecar's workers.
func nodeProcessCount(cfg *config.Config) int {
	return max(1, cfg.SSR.RendererWorkers) + max(1, cfg.SSR.NodeWorkers)
}

// nodeHeapArg returns the --max-old-space-size flag for each Node process the
// server starts, or "" to leave V8's default. ssr.nodeHeapLimit (MB) sets it;
// otherwise it is derived from the container's memory limit, split between
// the Node processes, so V8 collects garbage well before the container is
// killed instead of growing toward a heap sized from the host's memory. A
// limit already in NODE_OPTIONS wins, and ssr.nodeHeapLimit: -1 turns it off.
func nodeHeapArg(cfg *config.Config) string {
	if cfg == nil || cfg.SSR.NodeHeapLimit < 0 || strings.Contains(os.Getenv("NODE_OPTIONS"), "--max-old-space-size") {
		return ""
	}
	mb := cfg.SSR.NodeHeapLimit
	if mb == 0 {
		limit, ok := containerMemoryLimit()
		if !ok {
			return ""
		}
		mb = derivedNodeHeapMB(limit, nodeProcessCount(cfg))
	}
	return "--max-old-space-size=" + strconv.Itoa(mb)
}

func derivedNodeHeapMB(limitBytes int64, processes int) int {
	mb := int(float64(limitBytes) * nodeHeapShare / float64(max(1, processes)) / (1 << 20))
	return max(mb, minNodeHeapMB)
}

// containerMemoryLimit reads the memory limit of the cgroup the server runs
// in (v2, then v1). It reports false when there is none.
func containerMemoryLimit() (int64, bool) {
	for _, p := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if n, ok := parseCgroupLimit(string(data)); ok {
			return n, true
		}
	}
	return 0, false
}

// parseCgroupLimit parses a cgroup memory limit. "max" (v2) and the huge
// value v1 reports for no limit mean unlimited.
func parseCgroupLimit(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "max" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 || n >= 1<<60 {
		return 0, false
	}
	return n, true
}

// withNodeHeapArg puts flag ahead of a Node command's script argument.
func withNodeHeapArg(args []string, flag string) []string {
	if flag == "" {
		return args
	}
	return append([]string{flag}, args...)
}
