package build

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Environment variables that tell the shell of a dev run to start the Go host
// under Delve (they are read by packages/native/shell).
const (
	GoHostDebugEnv = "KRATE_GOHOST_DEBUG"
	GoHostDlvEnv   = "KRATE_GOHOST_DLV"
	// DlvPathEnv overrides the lookup of the dlv binary.
	DlvPathEnv = "KRATE_DLV"
)

// DefaultDebugAddr is where Delve listens unless told otherwise.
const DefaultDebugAddr = "127.0.0.1:2345"

// DelveInstallHint is how to get dlv.
const DelveInstallHint = "go install github.com/go-delve/delve/cmd/dlv@latest"

// NativeDebugOptions configure the debugging side of `krate native dev`.
type NativeDebugOptions struct {
	// GoHost runs the Go host under Delve (needs dlv).
	GoHost bool
	// Addr is the address Delve listens on; empty picks DefaultDebugAddr, or
	// a free port when that is taken.
	Addr string
	// Config names an editor to write a launch configuration for: "vscode".
	Config string
	// Host starts the node, bun and deno host children with --inspect.
	Host bool
	// Dlv is the dlv binary; empty finds it (see FindDlv).
	Dlv string
}

// ErrDelveUnsupported is FindDlv's error on a platform Delve does not run on
// (Windows on ARM).
var ErrDelveUnsupported = errors.New("Delve does not support Windows on ARM (windows/arm64); debug the Go host on x64 or another OS")

// FindDlv locates the Delve debugger: $KRATE_DLV, then PATH, then the go bin
// directories, where `go install` puts it.
func FindDlv() (string, error) {
	if runtime.GOOS == "windows" && runtime.GOARCH == "arm64" {
		return "", ErrDelveUnsupported
	}
	exe := "dlv"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if p := os.Getenv(DlvPathEnv); p != "" {
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return "", fmt.Errorf("$%s is %q, which is not a file", DlvPathEnv, p)
		}
		return p, nil
	}
	if p, err := exec.LookPath("dlv"); err == nil {
		return p, nil
	}
	var dirs []string
	for _, key := range []string{"GOBIN", "GOPATH"} {
		out, err := exec.Command("go", "env", key).Output()
		if err != nil {
			continue
		}
		v := strings.TrimSpace(string(out))
		if v == "" {
			continue
		}
		if key == "GOPATH" {
			for _, gp := range filepath.SplitList(v) {
				dirs = append(dirs, filepath.Join(gp, "bin"))
			}
		} else {
			dirs = append(dirs, v)
		}
	}
	for _, d := range dirs {
		p := filepath.Join(d, exe)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("dlv (the Delve debugger) is not installed, so the Go host cannot be debugged\n" +
		"  install it:  " + DelveInstallHint + "\n" +
		"  then make sure the go bin directory is on your PATH, or set $" + DlvPathEnv + " to the binary")
}

// ResolveDebugAddr validates a Delve listen address and settles on a port.
// Only loopback addresses are accepted: the Delve API can run arbitrary code
// in the program, so it must not be reachable from the network. Port 0 picks a
// free port. With no address the default is used when free, else any free port.
func ResolveDebugAddr(want string) (addr string, fellBack bool, err error) {
	explicit := want != ""
	if !explicit {
		want = DefaultDebugAddr
	}
	host, port, err := net.SplitHostPort(want)
	if err != nil {
		return "", false, fmt.Errorf("--debug-addr %q: %w (use host:port, for example %s)", want, err, DefaultDebugAddr)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", false, fmt.Errorf("--debug-addr %q: the debugger can run code in your app, so it only listens on a loopback address (127.0.0.1, ::1 or localhost)", want)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return "", false, fmt.Errorf("--debug-addr %q: %q is not a port", want, port)
	}
	free := func(p string) (string, bool) {
		l, err := net.Listen("tcp", net.JoinHostPort(host, p))
		if err != nil {
			return "", false
		}
		defer l.Close()
		_, got, _ := net.SplitHostPort(l.Addr().String())
		return net.JoinHostPort(host, got), true
	}
	if a, ok := free(port); ok {
		return a, false, nil
	}
	if explicit {
		return "", false, fmt.Errorf("--debug-addr %q is already in use", want)
	}
	a, ok := free("0")
	if !ok {
		return "", false, errors.New("no free port for the debugger")
	}
	return a, true, nil
}

// InspectAddrs returns n consecutive host:port addresses starting at base, or
// nil when base is empty.
func InspectAddrs(base string, n int) []string {
	if base == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(base)
	if err != nil {
		return nil
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return nil
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, net.JoinHostPort(host, strconv.Itoa(p+i)))
	}
	return out
}

// vscodeConfig is one entry of .vscode/launch.json.
type vscodeConfig map[string]any

// VSCodeGoAttach is the VS Code attach configuration for the Go host.
func VSCodeGoAttach(addr string) map[string]any {
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	return vscodeConfig{
		"name":       "Krate: attach to the Go host",
		"type":       "go",
		"request":    "attach",
		"mode":       "remote",
		"host":       host,
		"port":       p,
		"apiVersion": 2,
	}
}

// VSCodeNodeAttach is the VS Code attach configuration for a host child that
// runs with --inspect. restart reconnects after the child is restarted by a
// host code edit.
func VSCodeNodeAttach(runtime, addr string) map[string]any {
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	return vscodeConfig{
		"name":             "Krate: attach to the " + runtime + " host",
		"type":             "node",
		"request":          "attach",
		"address":          host,
		"port":             p,
		"restart":          true,
		"sourceMaps":       true,
		"skipFiles":        []string{"<node_internals>/**"},
		"localRoot":        "${workspaceFolder}",
		"remoteRoot":       "${workspaceFolder}",
		"continueOnAttach": false,
	}
}

// MergeLaunchJSON adds configs to the text of a launch.json (an empty text
// starts a new file) and returns the new text. A configuration with the same
// name is replaced. It refuses a file it cannot rewrite without losing
// something: comments or trailing commas make it invalid JSON, and the user's
// file is then left alone (ok is false).
func MergeLaunchJSON(existing []byte, configs []map[string]any) (out []byte, ok bool) {
	doc := map[string]any{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &doc); err != nil {
			return nil, false
		}
	}
	if _, has := doc["version"]; !has {
		doc["version"] = "0.2.0"
	}
	list, _ := doc["configurations"].([]any)
	if doc["configurations"] != nil && list == nil {
		return nil, false
	}
	for _, c := range configs {
		replaced := false
		for i, e := range list {
			if m, isMap := e.(map[string]any); isMap && m["name"] == c["name"] {
				list[i] = c
				replaced = true
			}
		}
		if !replaced {
			list = append(list, c)
		}
	}
	doc["configurations"] = list
	b, err := json.MarshalIndent(doc, "", "\t")
	if err != nil {
		return nil, false
	}
	return append(b, '\n'), true
}

// WriteVSCodeLaunch writes the attach configurations into
// <root>/.vscode/launch.json. When the file exists with comments it is not
// touched and written is false: the caller prints the snippet instead.
func WriteVSCodeLaunch(root string, configs []map[string]any) (path string, written bool, err error) {
	path = filepath.Join(root, ".vscode", "launch.json")
	old, rerr := os.ReadFile(path)
	if rerr != nil && !os.IsNotExist(rerr) {
		return path, false, rerr
	}
	merged, ok := MergeLaunchJSON(old, configs)
	if !ok {
		return path, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, false, err
	}
	return path, true, os.WriteFile(path, merged, 0o644)
}

// nativeDebug is the resolved debugging setup of one dev session.
type nativeDebug struct {
	o    NativeDebugOptions
	dlv  string
	addr string
	// fellBack is true when the default address was taken.
	fellBack bool
}

// prepareNativeDebug validates the options (dlv present, address usable) and
// switches the builder to debug builds. It returns nil without options.
func prepareNativeDebug(b *Builder, o *NativeDebugOptions) (*nativeDebug, error) {
	if o == nil || (!o.GoHost && !o.Host) {
		return nil, nil
	}
	d := &nativeDebug{o: *o}
	if o.GoHost {
		if o.Dlv != "" {
			d.dlv = o.Dlv
		} else {
			p, err := FindDlv()
			if err != nil {
				return nil, err
			}
			d.dlv = p
		}
		addr, fell, err := ResolveDebugAddr(o.Addr)
		if err != nil {
			return nil, err
		}
		d.addr, d.fellBack = addr, fell
		b.DebugGo = true
	}
	if o.Host {
		b.InspectHost = "127.0.0.1:9229"
	}
	return d, nil
}

// announce prints how to attach a debugger, writes the editor configuration
// when asked, and returns the environment variables the shell needs.
func (d *nativeDebug) announce(b *Builder, m *NativeManifest, root string) []string {
	var env []string
	var configs []map[string]any
	hasGo := m.GoHost != nil
	if d.o.GoHost {
		if !hasGo {
			printf("  %s! --debug-go: this app has no Go host code, so there is nothing to debug%s\n", cYellow, cReset)
		} else {
			env = append(env, GoHostDebugEnv+"="+d.addr, GoHostDlvEnv+"="+d.dlv)
			if d.fellBack {
				printf("  %s! %s is in use; Delve listens on %s instead%s\n", cYellow, DefaultDebugAddr, d.addr, cReset)
			}
			printf("%s%s  Debugging the Go host: Delve listens on %s%s\n", cBold, cCyan, d.addr, cReset)
			printf("  %sThe host is built with -N -l; the app runs at once, attach a debugger any time.%s\n", cGray, cReset)
			printf("  %sAfter a Go edit the host restarts: attach again (breakpoints are not kept).%s\n", cGray, cReset)
			printf("  %sdlv connect %s   |   VS Code: Krate: attach to the Go host   |   GoLand: Go Remote%s\n", cGray, d.addr, cReset)
			configs = append(configs, VSCodeGoAttach(d.addr))
		}
	}
	if d.o.Host {
		addrs := InspectAddrs(b.InspectHost, len(m.HostChildren))
		n := 0
		for _, c := range m.HostChildren {
			if n >= len(addrs) {
				break
			}
			printf("%s%s  Inspecting the %s host: --inspect on %s%s\n", cBold, cCyan, c.Runtime, addrs[n], cReset)
			if c.Runtime != "bun" {
				configs = append(configs, VSCodeNodeAttach(c.Runtime, addrs[n]))
			}
			n++
		}
		if n == 0 {
			printf("  %s! --debug-host: the host code runs in the embedded QuickJS runtime, which has no debugger; set native.host.runtime to node, bun or deno%s\n", cYellow, cReset)
		}
	}
	if d.o.Config != "" && len(configs) > 0 {
		switch d.o.Config {
		case "vscode":
			path, written, err := WriteVSCodeLaunch(root, configs)
			switch {
			case err != nil:
				printf("  %s! could not write %s: %v%s\n", cYellow, path, err, cReset)
			case written:
				printf("  %sWrote the VS Code attach configuration to %s%s\n", cGray, path, cReset)
			default:
				b, _ := json.MarshalIndent(configs, "    ", "  ")
				printf("  %s! %s has comments, so it was left alone; add these configurations to it:%s\n    %s\n", cYellow, path, cReset, b)
			}
		default:
			printf("  %s! --debug-config %s: only \"vscode\" is supported%s\n", cYellow, d.o.Config, cReset)
		}
	}
	return env
}
