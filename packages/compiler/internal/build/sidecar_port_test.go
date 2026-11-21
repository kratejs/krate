package build

import (
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// The OS hands ports in its ephemeral range to bind(0) and to the local end of
// outgoing connections, so a sidecar port chosen there can be taken by any
// socket before the sidecar binds it (seconds later, after register()).
func TestInternalPortAvoidsTheEphemeralRange(t *testing.T) {
	lo, hi := testEphemeralRange(t)
	for i := 0; i < 20; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		preferred := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		if preferred < lo || preferred > hi {
			t.Fatalf("bind(0) gave %d, outside the ephemeral range %d-%d this test assumes", preferred, lo, hi)
		}
		if got := internalPort(preferred); got >= lo && got <= hi {
			t.Fatalf("internalPort(%d) = %d, inside the ephemeral range %d-%d", preferred, got, lo, hi)
		}
	}
	// A preferred port outside the range is kept when it is free.
	for p := lo - 1; p > lo-200 && p > 1024; p-- {
		if portFree(p) && !portHandedOutLocked(p) {
			if got := internalPort(p); got != p {
				t.Fatalf("internalPort(%d) = %d, want the preferred port", p, got)
			}
			return
		}
	}
}

func portHandedOutLocked(p int) bool {
	handedOutPortsMu.Lock()
	defer handedOutPortsMu.Unlock()
	return portHandedOut(p)
}

func testEphemeralRange(t *testing.T) (int, int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		return 49152, 65535
	}
	data, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err != nil {
		t.Fatal(err)
	}
	f := strings.Fields(string(data))
	lo, err1 := strconv.Atoi(f[0])
	hi, err2 := strconv.Atoi(f[1])
	if err1 != nil || err2 != nil {
		t.Fatalf("ip_local_port_range: %q", data)
	}
	return lo, hi
}

// The host proxies to the Node sidecar at the address the sidecar listens on
// and the readiness probe checked (127.0.0.1). "localhost" resolves to ::1
// first, where another server can hold the same port: Windows lets a wildcard
// listener sit next to the sidecar's loopback one.
func TestNodeSidecarProxyReachesTheSidecarItProbed(t *testing.T) {
	e2eEnv(t)
	b, root := buildInstProject(t, instFiles(`export async function register() {
  (globalThis as any).__krateInst = "ready";
}`))
	b.Cfg.SSR.NodeWorkers = 1
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	v, ok := sidecarPorts.Load(b.Cfg.Server.Port)
	if !ok {
		t.Fatal("no sidecar port recorded")
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("::1", strconv.Itoa(v.(int))))
	if err != nil {
		t.Fatalf("listen on the IPv6 loopback side of the sidecar port: %v", err)
	}
	other := &http.Server{Handler: http.NotFoundHandler()}
	go func() { _ = other.Serve(ln) }()
	t.Cleanup(func() { _ = other.Close() })
	if code, body := getText(t, base+"/api/pid"); code != 200 || !strings.Contains(body, `"state":"ready"`) {
		t.Fatalf("GET /api/pid: %d %s", code, body)
	}
}
