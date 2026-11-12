package shell

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/gohost"
	"github.com/kratejs/krate/packages/native/internal/hosttransport"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/sdk/worker"
)

// workerRunner starts worker processes as host children with their own SDK
// endpoint.
type workerRunner struct{ a *app }

// setupWorkers builds the supervisor for the manifest's workers.
func (a *app) setupWorkers(man *Manifest) error {
	if len(man.Workers) == 0 {
		worker.Configure(worker.Config{})
		return nil
	}
	sup, err := worker.NewSupervisor(&workerRunner{a: a}, man.Workers)
	if err != nil {
		return fmt.Errorf("shell: %w", err)
	}
	a.workers = sup
	worker.Configure(worker.Config{
		Supervisor: sup,
		CanReceive: func(sp worker.Spec) bool {
			set, err := permission.Parse(sp.Permissions)
			return err == nil && set.Allows("port:receive")
		},
	})
	return nil
}

// startAutoWorkers starts the workers declared autostart; the SDK must be
// attached first because a worker may call it at once.
func (a *app) startAutoWorkers() {
	if a.workers == nil {
		return
	}
	for _, sp := range a.workers.Specs() {
		if !sp.Autostart {
			continue
		}
		go func(name string) {
			if _, err := a.workers.Start(context.Background(), name, nil, nil); err != nil {
				a.logf("shell: starting the %s worker: %v", name, err)
			}
		}(sp.Name)
	}
}

func isJSRuntime(rt string) bool { return rt == "node" || rt == "bun" || rt == "deno" }

// defaultCommand is how a script entry starts when the manifest gives no
// command.
func defaultCommand(rt string) []string {
	switch rt {
	case "node":
		return []string{"node", "{entry}"}
	case "bun":
		return []string{"bun", "run", "{entry}"}
	}
	return nil
}

func (r *workerRunner) Start(ctx context.Context, in worker.Instance) (worker.Proc, error) {
	a := r.a
	sp := in.Spec
	command := sp.Command
	if len(command) == 0 && isJSRuntime(sp.Runtime) {
		if command = defaultCommand(sp.Runtime); command == nil {
			return nil, fmt.Errorf("the %s runtime needs a command in the worker declaration", sp.Runtime)
		}
	}
	prog, args, err := a.commandOf(HostChildSpec{Runtime: sp.Runtime, Entry: sp.Entry, Command: command})
	if err != nil {
		return nil, err
	}
	args = append(append(args, sp.Args...), in.Args...)
	if err := a.perms.Scope(in.Owner(), sp.Permissions); err != nil {
		return nil, err
	}
	js := isJSRuntime(sp.Runtime)
	transport := gohost.TransportFor(runtime.GOOS, js)
	parent, closeSDK, err := a.serveWorkerSDK(in, transport == hosttransport.TCP)
	if err != nil {
		return nil, err
	}
	man := a.man
	env := []string{
		"KRATE_APP_DIR=" + a.dir,
		"KRATE_APP_ID=" + man.AppID,
		"KRATE_APP_NAME=" + man.ProductName,
		"KRATE_APP_VERSION=" + man.Version,
		"KRATE_WORKER_ID=" + in.ID,
		"KRATE_WORKER_NAME=" + sp.Name,
		gohost.ParentEnv + "=" + parent,
	}
	if a.opts.Dev {
		env = append(env, "KRATE_DEV=1", "NODE_ENV=development")
	} else {
		env = append(env, "NODE_ENV=production")
	}
	for k, v := range in.Env {
		env = append(env, k+"="+v)
	}
	child := gohost.NewChild(gohost.ChildOptions{
		Transport:    transport,
		Logf:         func(format string, args ...any) { a.logf(format, args...) },
		Prefix:       "[worker " + sp.Name + "] ",
		Args:         args,
		Dir:          a.childBase(),
		Name:         "wk" + in.ID,
		ReadyAnyLine: js,
		ReadyTimeout: 60 * time.Second,
	})
	if err := child.Start(ctx, prog, env); err != nil {
		closeSDK()
		return nil, err
	}
	reg := scheme.NewRegistry()
	child.Remote().RegisterInto(reg, []string{worker.MessageFn}, nil, nil)
	return &workerProc{child: child, exited: child.Exited(), reg: reg, closeSDK: closeSDK, owner: in.Owner(), perms: a}, nil
}

// serveWorkerSDK serves the app's registry to one worker, tagging every
// request with the worker's identity so that the permission scope set in
// Start decides what it may call.
func (a *app) serveWorkerSDK(in worker.Instance, tcp bool) (addr string, closeFn func(), err error) {
	h := scheme.New(scheme.Options{
		Registry: a.reg,
		Identify: func(*http.Request) (string, bool) { return in.Owner(), true },
	})
	var ln net.Listener
	var handler http.Handler = h
	var cleanup func()
	if tcp {
		var ep hosttransport.Endpoint
		ln, ep, err = hosttransport.ListenTCP()
		if err != nil {
			return "", nil, err
		}
		handler = hosttransport.Guard(ep, h)
		addr = ep.String()
	} else {
		dir := filepath.Join(sockDir(), "wsdk")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", nil, err
		}
		sock := filepath.Join(dir, fmt.Sprintf("%d-%s.sock", os.Getpid(), in.ID))
		if len(sock) > 100 {
			d, err := os.MkdirTemp("", "krw")
			if err != nil {
				return "", nil, err
			}
			sock = filepath.Join(d, in.ID+".sock")
			cleanup = func() { os.RemoveAll(d) }
		}
		_ = os.Remove(sock)
		ln, err = net.Listen("unix", sock)
		if err != nil {
			return "", nil, err
		}
		addr = "unix:" + sock
		prev := cleanup
		cleanup = func() {
			os.Remove(sock)
			if prev != nil {
				prev()
			}
		}
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }() // ends with ErrServerClosed
	var once sync.Once
	return addr, func() {
		once.Do(func() {
			_ = srv.Close()
			if cleanup != nil {
				cleanup()
			}
		})
	}, nil
}

type workerProc struct {
	child    *gohost.Child
	exited   <-chan struct{}
	reg      *scheme.Registry
	closeSDK func()
	owner    string
	perms    *app
	stopOnce sync.Once
}

func (p *workerProc) Post(ctx context.Context, args []byte) error {
	_, err := p.reg.Invoke(krate.WithWindow(ctx, ""), worker.MessageFn, args)
	return err
}

func (p *workerProc) Exited() <-chan struct{} { return p.exited }

func (p *workerProc) Result() (int, error) {
	code, err, _ := p.child.ExitStatus()
	p.release()
	return code, err
}

func (p *workerProc) Pid() int { return p.child.Pid() }

func (p *workerProc) Stop() {
	_ = p.child.Stop()
	p.release()
}

// release frees what the process held: its SDK endpoint and permission scope.
func (p *workerProc) release() {
	p.stopOnce.Do(func() {
		p.closeSDK()
		p.perms.perms.Unscope(p.owner)
	})
}
