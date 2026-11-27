package build

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// nodeMemoryFixture is an app that would grow the Node processes without
// bound if nothing limited them: an ISR route that renders a large page for
// any slug, so every request adds a cache entry, and Node API routes.
func nodeMemoryFixture(t *testing.T, edit func(*config.Config)) (*Builder, string) {
	t.Helper()
	b, root := scProject(t, map[string]string{
		"package.json":        `{"type":"module"}`,
		"src/pages/index.tsx": `export default function Home() { return <h1>home</h1>; }`,
		"src/lib/text.ts":     `export function block(seed: string, n: number) { return (seed + "-").repeat(Math.ceil(n / (seed.length + 1))).slice(0, n); }`,
		"src/pages/doc/[slug].tsx": `import { block } from "../../lib/text";
export const revalidate = 3600;
export default async function Doc({ params }: { params: { slug: string } }) {
  return <pre id="body">{block(params.slug, 300000)}</pre>;
}`,
		"src/api/one.ts": `import { block } from "../lib/text"; export function GET() { return Response.json({ n: block("a", 10).length }); }`,
		"src/api/two.ts": `import { block } from "../lib/text"; export function GET() { return Response.json({ n: block("b", 20).length }); }`,
	})
	b.Cfg.SSR.APIRuntime = "node"
	if edit != nil {
		edit(b.Cfg)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	logs := builderStderr(b)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, logs())
	}
	return b, root
}

// nodeChild is a Node process the server started.
type nodeChild struct {
	pid     int
	cmdline string
}

// nodeChildren lists the Node processes descending from this process.
func nodeChildren(t *testing.T) []nodeChild {
	t.Helper()
	parents := map[int]int{}
	cmds := map[int]string{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		// The command name is in parentheses and may hold spaces.
		s := string(stat)
		i := strings.LastIndexByte(s, ')')
		if i < 0 {
			continue
		}
		fields := strings.Fields(s[i+1:])
		if len(fields) < 2 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		parents[pid] = ppid
		if cmd, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline")); err == nil {
			cmds[pid] = strings.ReplaceAll(string(cmd), "\x00", " ")
		}
	}
	self := os.Getpid()
	descends := func(pid int) bool {
		for n := 0; pid > 1 && n < 64; n++ {
			if pid = parents[pid]; pid == self {
				return true
			}
		}
		return false
	}
	var out []nodeChild
	for pid, cmd := range cmds {
		if descends(pid) && (strings.Contains(cmd, "server-renderer") || strings.Contains(cmd, "api-server.mjs")) {
			out = append(out, nodeChild{pid: pid, cmdline: cmd})
		}
	}
	return out
}

// rssMB is a process's resident set size in MB.
func rssMB(t *testing.T, pid int) float64 {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			kb, _ := strconv.Atoi(strings.Fields(line)[1])
			return float64(kb) / 1024
		}
	}
	t.Fatalf("no VmRSS for %d", pid)
	return 0
}

// loadNodeMemory requests n distinct ISR variants (each a new 300 KB cache
// entry) and the API routes, with a few requests in flight at a time.
func loadNodeMemory(t *testing.T, base string, n int) {
	t.Helper()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	var failed sync.Once
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			for _, path := range []string{fmt.Sprintf("/doc/v%d/", i), "/api/one", "/api/two"} {
				res, err := http.Get(base + path)
				if err != nil {
					failed.Do(func() { t.Errorf("GET %s: %v", path, err) })
					return
				}
				_, _ = io.Copy(io.Discard, res.Body)
				res.Body.Close()
				if res.StatusCode != 200 {
					failed.Do(func() { t.Errorf("GET %s: %d", path, res.StatusCode) })
				}
			}
		}(i)
	}
	wg.Wait()
}

// TestNodeProcessesStayWithinMemoryBudget is the memory regression check of
// the Node sidecars: the API sidecar is one process by default, and the SSR
// renderer's caches stay inside ssr.cacheMemory however many variants are
// requested, so its resident memory after the load does not track the
// rendered HTML. Set KRATE_MEMORY_REPORT=1 to log the numbers.
func TestNodeProcessesStayWithinMemoryBudget(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc")
	}
	b, root := nodeMemoryFixture(t, func(c *config.Config) {
		c.SSR.CacheMemory = 8
		c.SSR.NodeHeapLimit = 256
	})
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Warm both processes, then measure the baseline.
	loadNodeMemory(t, base, 8)
	children := nodeChildren(t)
	var renderer, api []nodeChild
	for _, c := range children {
		if strings.Contains(c.cmdline, "server-renderer") {
			renderer = append(renderer, c)
		} else {
			api = append(api, c)
		}
	}
	if len(renderer) != 1 || len(api) != 1 {
		t.Fatalf("want one renderer and one API sidecar process, got %d and %d:\n%v", len(renderer), len(api), children)
	}
	for _, c := range children {
		if !strings.Contains(c.cmdline, "--max-old-space-size=256") {
			t.Errorf("Node process %d runs without the heap limit: %s", c.pid, c.cmdline)
		}
	}
	before := rssMB(t, renderer[0].pid)
	// 400 variants of 300 KB: about 120 MB of HTML, which a cache bounded
	// only by entry count (512) would keep.
	loadNodeMemory(t, base, 400)
	time.Sleep(2 * time.Second)
	after := rssMB(t, renderer[0].pid)
	apiRSS := rssMB(t, api[0].pid)
	if os.Getenv("KRATE_MEMORY_REPORT") != "" {
		t.Logf("renderer RSS %.0f MB -> %.0f MB, API sidecar %.0f MB", before, after, apiRSS)
	}
	if grew := after - before; grew > nodeMemoryGrowthBudgetMB {
		t.Errorf("renderer RSS grew %.0f MB (%.0f -> %.0f) over the load, budget %d MB: the render caches are not bounded by ssr.cacheMemory", grew, before, after, nodeMemoryGrowthBudgetMB)
	}
	if apiRSS > nodeMemoryAPIBudgetMB {
		t.Errorf("API sidecar RSS %.0f MB, budget %d MB", apiRSS, nodeMemoryAPIBudgetMB)
	}
}

// The budgets leave room for V8's heap headroom and allocator noise.
const (
	nodeMemoryGrowthBudgetMB = 100
	nodeMemoryAPIBudgetMB    = 200
)
