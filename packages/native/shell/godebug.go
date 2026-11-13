package shell

import (
	"net"
	"net/rpc/jsonrpc"
	"strings"
	"time"
)

// Environment variables of a dev run that debugs the Go host: DebugEnv is the
// address the Delve server listens on and DlvEnv the dlv binary ("dlv" when
// empty). `krate native dev --debug-go` sets them for the shell.
const (
	DebugEnv = "KRATE_GOHOST_DEBUG"
	DlvEnv   = "KRATE_GOHOST_DLV"
)

// dlvArgs is the command line that starts binary under a headless Delve
// server: API v2, several clients, and the program running at once so the app
// does not wait for a debugger to attach.
func dlvArgs(addr, binary string, args []string) []string {
	out := []string{
		"exec", "--headless", "--listen=" + addr, "--api-version=2",
		"--accept-multiclient", "--continue", "--log=false", binary,
	}
	if len(args) > 0 {
		out = append(out, "--")
		out = append(out, args...)
	}
	return out
}

// dlvWrap returns the Wrap and BeforeStop hooks of a gohost.Child that runs
// the Go host under Delve.
func dlvWrap(dlv, addr string) (wrap func(string, []string) (string, []string), stop func()) {
	if dlv == "" {
		dlv = "dlv"
	}
	wrap = func(binary string, args []string) (string, []string) {
		return dlv, dlvArgs(addr, binary, args)
	}
	stop = func() { dlvDetach(addr) }
	return wrap, stop
}

// dlvDetach asks the Delve server at addr to kill its program and exit. A
// headless server with several clients otherwise outlives the program, and
// the next start could not listen on the same address. Errors are ignored:
// the child is killed after its grace period anyway.
func dlvDetach(addr string) {
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(addr, "tcp:"), time.Second)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	c := jsonrpc.NewClient(conn)
	var reply struct{}
	_ = c.Call("RPCServer.Detach", map[string]any{"Kill": true}, &reply)
}
