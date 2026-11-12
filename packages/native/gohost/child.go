package gohost

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/internal/hosttransport"
	"github.com/kratejs/krate/packages/native/scheme"
)

// Base is the base URL the child's client uses; the host part is a
// placeholder because every connection dials the socket.
const Base = "http://gohost"

const (
	defaultReadyTimeout = 30 * time.Second
	stopGrace           = 2 * time.Second
	// A unix socket path must fit sun_path (104 bytes on macOS, 108 on Linux).
	maxSocketPath = 100
)

// ChildOptions configures a Child.
type ChildOptions struct {
	// Logf receives the child's stdout and stderr lines and lifecycle notes,
	// each already prefixed. Default: write to stderr.
	Logf func(format string, args ...any)
	// Prefix is put before each forwarded line. Default "[gohost] ".
	Prefix string
	// ReadyTimeout bounds the wait for "ready". Default 30 s.
	ReadyTimeout time.Duration
	// Args are passed to the program after its name. A JavaScript host child
	// is started as a runtime (node, bun, deno) with its script in Args.
	Args []string
	// Dir is the child's working directory. Default: the parent's.
	Dir string
	// Name separates the socket directory of one Child from another's in the
	// same process (keep it short: a unix socket path is limited to about 100
	// bytes). Empty for the only or first child.
	Name string
	// ReadyAnyLine accepts the "ready" line anywhere on stdout, not only as
	// the first line. A JavaScript host child needs it: the host code may log
	// while it loads, before the runtime can report that it is listening.
	ReadyAnyLine bool
	// Transport is "unix" (default) or "tcp". A tcp child listens on a
	// loopback port it chooses and announces on its ready line; every request
	// carries a random token. JavaScript host children use it on Windows,
	// where Node, Bun and Deno cannot use AF_UNIX (see TransportFor).
	Transport string
}

// Child manages one Go host child process at a time.
type Child struct {
	o      ChildOptions
	remote *scheme.Remote

	mu      sync.Mutex
	dir     string
	run     *run
	last    *run // the run that stopRunLocked ended, for ExitStatus
	seq     int
	binary  string
	env     []string
	started bool
}

// run is one child process.
type run struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	socket   string // unix socket path; empty for tcp
	client   *http.Client
	tr       *http.Transport
	exited   chan struct{} // closed when the process has been reaped
	waitErr  error
	ioDone   sync.WaitGroup
	lastLine lastLines
}

// NewChild returns a Child that is not started.
func NewChild(o ChildOptions) *Child {
	if o.Prefix == "" {
		o.Prefix = "[gohost] "
	}
	if o.ReadyTimeout <= 0 {
		o.ReadyTimeout = defaultReadyTimeout
	}
	if o.Logf == nil {
		o.Logf = func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
	}
	return &Child{o: o, remote: scheme.NewRemote(nil, Base)}
}

// Remote is the forwarder bound to whichever child is running. Register
// the child's ids with Remote().RegisterInto; Start and Restart re-point it.
func (c *Child) Remote() *scheme.Remote { return c.remote }

// Client is the HTTP client of the running child, or nil.
func (c *Child) Client() *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.run == nil {
		return nil
	}
	return c.run.client
}

// Base is the base URL to use with Client.
func (c *Child) Base() string { return Base }

// Exited is closed when the current child process has exited, or nil if no
// child runs. It is closed on a crash as well as on Stop.
func (c *Child) Exited() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.run == nil {
		return nil
	}
	return c.run.exited
}

// ExitStatus reports how the current child process ended: ok is false while
// it still runs (or none was started). code is -1 when a signal ended it.
func (c *Child) ExitStatus() (code int, err error, ok bool) {
	c.mu.Lock()
	r := c.run
	if r == nil {
		r = c.last
	}
	c.mu.Unlock()
	if r == nil {
		return 0, nil, false
	}
	select {
	case <-r.exited:
	default:
		return 0, nil, false
	}
	if r.cmd.ProcessState != nil {
		code = r.cmd.ProcessState.ExitCode()
	}
	return code, r.waitErr, true
}

// Pid is the process id of the running child, or 0.
func (c *Child) Pid() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.run == nil || c.run.cmd.Process == nil {
		return 0
	}
	return c.run.cmd.Process.Pid
}

// Start launches binaryPath with env (appended to the parent's environment)
// and waits until it reports ready. ctx bounds the wait, not the child's life.
func (c *Child) Start(ctx context.Context, binaryPath string, env []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return errors.New("gohost: child already started; use Restart")
	}
	abs, err := filepath.Abs(binaryPath)
	if err != nil {
		return err
	}
	c.binary, c.env = abs, env
	if err := c.startLocked(ctx); err != nil {
		return err
	}
	c.started = true
	return nil
}

// Restart stops the running child (stdin close, then kill after 2 s), starts
// binaryPath (empty means the previous binary) and re-points the Remote.
func (c *Child) Restart(binaryPath string) error {
	return c.RestartContext(context.Background(), binaryPath)
}

// RestartContext is Restart with a context that bounds the wait for ready.
func (c *Child) RestartContext(ctx context.Context, binaryPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started {
		return errors.New("gohost: child was never started")
	}
	if binaryPath != "" {
		abs, err := filepath.Abs(binaryPath)
		if err != nil {
			return err
		}
		c.binary = abs
	}
	c.stopRunLocked()
	return c.startLocked(ctx)
}

// Stop ends the child and removes its socket directory. The Remote then
// answers "unavailable". It is safe to call more than once.
func (c *Child) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopRunLocked()
	c.started = false
	if c.dir != "" {
		_ = os.RemoveAll(c.dir)
		if c.o.Name != "" {
			_ = os.Remove(filepath.Dir(c.dir)) // succeeds only when no sibling child remains
		}
		c.dir = ""
	}
	return nil
}

func (c *Child) socketDir() (string, error) {
	if c.dir != "" {
		return c.dir, nil
	}
	var base string
	if cache, err := os.UserCacheDir(); err == nil {
		base = filepath.Join(cache, "krate", "native", strconv.Itoa(os.Getpid()))
		if c.o.Name != "" {
			base = filepath.Join(base, c.o.Name)
		}
		if err := os.MkdirAll(base, 0o700); err != nil {
			base = ""
		}
	}
	if base == "" || len(filepath.Join(base, "g000.sock")) > maxSocketPath {
		// The cache path is unusable or too long for a socket.
		d, err := os.MkdirTemp("", "krate-gh")
		if err != nil {
			return "", fmt.Errorf("gohost: no directory for the socket: %w", err)
		}
		base = d
	}
	if err := os.Chmod(base, 0o700); err != nil && !errors.Is(err, os.ErrPermission) {
		return "", err
	}
	c.dir = base
	return base, nil
}

func (c *Child) startLocked(ctx context.Context) error {
	tcp := c.o.Transport == hosttransport.TCP
	var socket, listen, token string
	if tcp {
		var err error
		if token, err = hosttransport.NewToken(); err != nil {
			return err
		}
		listen = "tcp:127.0.0.1:0"
	} else {
		dir, err := c.socketDir()
		if err != nil {
			return err
		}
		c.seq++
		socket = filepath.Join(dir, fmt.Sprintf("g%03d.sock", c.seq%1000))
		if len(socket) > maxSocketPath {
			return fmt.Errorf("gohost: socket path %q is too long", socket)
		}
		listen = socket
	}

	cmd := exec.Command(c.binary, expandSocketArgs(c.o.Args, socket)...)
	cmd.Dir = c.o.Dir
	cmd.Env = append(append(os.Environ(), c.env...), SocketEnv+"="+listen)
	if tcp {
		cmd.Env = append(cmd.Env, TokenEnv+"="+token)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gohost: starting %s: %w", c.binary, err)
	}
	r := &run{cmd: cmd, stdin: stdin, socket: socket, exited: make(chan struct{})}
	ready := make(chan struct{})
	var readyOnce sync.Once
	var readyAddr string
	pump := func(rd io.Reader, isOut bool) {
		defer r.ioDone.Done()
		sc := bufio.NewScanner(rd)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		first := isOut
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r")
			if first || (isOut && c.o.ReadyAnyLine) {
				if addr, ok := hosttransport.ReadyAddr(line); ok && (addr != "") == tcp {
					first = false
					readyOnce.Do(func() { readyAddr = addr; close(ready) })
					continue
				}
			}
			first = false
			r.lastLine.add(line)
			c.o.Logf("%s%s", c.o.Prefix, line)
		}
	}
	r.ioDone.Add(2)
	go pump(stdout, true)
	go pump(stderr, false)
	go func() {
		r.ioDone.Wait()
		r.waitErr = cmd.Wait()
		close(r.exited)
	}()

	timer := time.NewTimer(c.o.ReadyTimeout)
	defer timer.Stop()
	select {
	case <-ready:
	case <-r.exited:
		return fmt.Errorf("gohost: %s exited before it was ready (%v)%s", filepath.Base(c.binary), r.waitErr, r.lastLine.suffix())
	case <-timer.C:
		r.kill()
		return fmt.Errorf("gohost: %s was not ready after %s%s", filepath.Base(c.binary), c.o.ReadyTimeout, r.lastLine.suffix())
	case <-ctx.Done():
		r.kill()
		return ctx.Err()
	}

	ep := hosttransport.Endpoint{Network: hosttransport.Unix, Addr: socket}
	if tcp {
		ep = hosttransport.Endpoint{Network: hosttransport.TCP, Addr: readyAddr, Token: token}
	}
	rt, tr := hosttransport.NewTransport(ep)
	r.tr = tr
	r.client = &http.Client{Transport: rt}
	c.run = r
	c.remote.SetTarget(r.client, Base)
	return nil
}

// kill ends the process without ceremony and waits for it.
func (r *run) kill() {
	_ = r.stdin.Close()
	if r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
	<-r.exited
}

func (c *Child) stopRunLocked() {
	r := c.run
	if r == nil {
		return
	}
	c.run = nil
	c.last = r
	// Detach first so nothing is sent to a dying child.
	c.remote.SetTarget(nil, Base)
	_ = r.stdin.Close()
	select {
	case <-r.exited:
	case <-time.After(stopGrace):
		c.o.Logf("%sdid not exit within %s; killing it", c.o.Prefix, stopGrace)
		if r.cmd.Process != nil {
			_ = r.cmd.Process.Kill()
		}
		<-r.exited
	}
	if r.tr != nil {
		r.tr.CloseIdleConnections()
	}
	if r.socket != "" {
		_ = os.Remove(r.socket)
	}
}

// lastLines keeps the last few output lines for error messages.
type lastLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *lastLines) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, s)
	if len(l.lines) > 8 {
		l.lines = l.lines[1:]
	}
}

func (l *lastLines) suffix() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(l.lines, "\n")
}

// SocketPlaceholder in an argument stands for the unix socket path of the run
// (g001.sock and so on, known only when the child starts). Deno needs the
// exact path in --allow-net to listen on it. For a child on the TCP transport
// there is no socket: the placeholder and the ",unix:" before it are dropped.
const SocketPlaceholder = "{socket}"

func expandSocketArgs(args []string, socket string) []string {
	if len(args) == 0 {
		return args
	}
	out := make([]string, len(args))
	for i, a := range args {
		if socket == "" {
			a = strings.ReplaceAll(a, ",unix:"+SocketPlaceholder, "")
		}
		out[i] = strings.ReplaceAll(a, SocketPlaceholder, socket)
	}
	return out
}
