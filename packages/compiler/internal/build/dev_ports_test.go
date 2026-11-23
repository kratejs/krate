package build

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Dev servers on neighbouring ports must not take each other's: the API
// sidecar of the server on N used to sit on N+1, so the next dev server on
// N+1 failed to bind (or, without --port, silently moved). With sidecars on
// OS-free ports, both servers get the port they asked for.
func TestDevServersOnNeighbouringPortsKeepTheirPorts(t *testing.T) {
	files := map[string]string{
		"src/pages/index.tsx": "export default function Home() { return <h1>home</h1>; }\n",
		"src/api/ping.ts":     "export function GET() { return Response.json({ ok: true }); }\n",
	}
	first := lowFreePort(t)
	atPort := func(p int) func(*config.Config) {
		return func(cfg *config.Config) {
			cfg.DevServer.Port = p
			cfg.DevServer.PortFixed = true
			cfg.SSR.APIRuntime = "node"
		}
	}
	a := startDevRun(t, files, atPort(first))
	// The second server asks for the very next port, which the first one's
	// sidecar would have held.
	b := startDevRun(t, files, atPort(first+1))

	for _, d := range []*devRun{a, b} {
		waitUntilTrue(t, 30*time.Second, "the API route through the sidecar of "+d.base, func() bool {
			code, body := getText(t, d.base+"/api/ping")
			return code == 200 && strings.Contains(body, "ok")
		})
	}
	sa, okA := sidecarPorts.Load(first)
	sb, okB := sidecarPorts.Load(first + 1)
	if !okA || !okB {
		t.Fatalf("sidecar ports not recorded: %v %v", okA, okB)
	}
	if sa == sb || sa.(int) == first+1 || sb.(int) == first {
		t.Errorf("sidecars %v and %v sit on a neighbouring dev port (%d, %d)", sa, sb, first, first+1)
	}
}

// lowFreePort returns a port below the OS ephemeral range whose next port and
// port+10 are free as well: the ports a dev server on it would derive
// sidecars from. (Sidecars never take ports inside the ephemeral range, so a
// test in it could not show the collision.)
func lowFreePort(t *testing.T) int {
	t.Helper()
	lo, _ := ephemeralPortRange()
	if lo < 12000 {
		t.Skip("the ephemeral port range starts too low")
	}
	for attempt := 0; attempt < 200; attempt++ {
		p := 10000 + rand.IntN(lo-10100)
		if portFree(p) && portFree(p+1) && portFree(p+2) && portFree(p+3) && portFree(p+10) {
			return p
		}
	}
	t.Skip("no free port block below the ephemeral range")
	return 0
}
