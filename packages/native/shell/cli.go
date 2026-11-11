package shell

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kratejs/krate/packages/native/plugin"
)

// CLI is the whole main function of a krate-shell binary:
//
//	krate-shell [--browser] [--addr 127.0.0.1:0] [--dev] [appDir]
//	krate-shell --doctor
//
// plugins are the native plugins linked into this binary; the per-app shell
// that `krate native dev` builds for an app with plugins passes them. The
// caller must have locked the main thread (runtime.LockOSThread in an init
// function) and exits with the returned code.
func CLI(args []string, plugins []*plugin.Plugin) int {
	// Keep stdout for the control protocol; anything else that prints to
	// os.Stdout (the host's console.log) lands on stderr.
	ctlOut := os.Stdout
	os.Stdout = os.Stderr

	fs := flag.NewFlagSet("krate-shell", flag.ContinueOnError)
	browser := fs.Bool("browser", false, "serve on a loopback port behind a session token instead of opening a window")
	addr := fs.String("addr", "127.0.0.1:0", "browser-mode listen address")
	dev := fs.Bool("dev", false, "development mode: developer tools, stack traces, and control commands on stdin")
	doctor := fs.Bool("doctor", false, "print a JSON report of the system webview and display, then exit")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: krate-shell [--browser] [--addr 127.0.0.1:0] [--dev] [appDir]")
		fmt.Fprintln(os.Stderr, "       krate-shell --doctor")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *doctor {
		if err := WriteDoctor(ctlOut); err != nil {
			fmt.Fprintln(os.Stderr, "krate-shell:", err)
			return 1
		}
		return 0
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return 2
	}
	appDir := fs.Arg(0)
	if appDir == "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "krate-shell:", err)
			return 1
		}
		appDir = DefaultAppDir(exe)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := Run(appDir, RunOptions{
		Browser: *browser,
		Addr:    *addr,
		Dev:     *dev,
		Out:     ctlOut,
		Context: ctx,
		Plugins: plugins,
		OnReady: func(ctl *Control) {
			fmt.Fprintln(ctlOut, "ready")
			if !*dev {
				return
			}
			// stdin EOF means the driving process is gone.
			if !ServeControl(os.Stdin, ctlOut, ctl) {
				ctl.Quit()
			}
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "krate-shell:", err)
		return 1
	}
	return 0
}
