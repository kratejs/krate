// Command krate-shell is the prebuilt Krate Native shell. It opens the window
// of an app directory (see packages/native/shell/README.md), serves the built
// UI, runs the app's host code and, in dev, takes commands on stdin.
//
//	krate-shell [--browser] [--addr 127.0.0.1:0] [--dev] [appDir]
//
// Without appDir the app is expected next to the binary: <exe dir>/app, or
// ../Resources/app inside a macOS .app bundle.
//
// stdout is the control channel and carries only protocol lines: "url <url>"
// (browser mode), "ready", and with --dev one "ok" or "error <message>" per
// command. Logs and host console output go to stderr.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/kratejs/krate/packages/native/shell"
)

// The platform layer needs the main goroutine on the main OS thread.
func init() { runtime.LockOSThread() }

func main() { os.Exit(run()) }

func run() int {
	// Keep stdout for the control protocol; anything else that prints to
	// os.Stdout (the host's console.log) lands on stderr.
	ctlOut := os.Stdout
	os.Stdout = os.Stderr

	fs := flag.NewFlagSet("krate-shell", flag.ContinueOnError)
	browser := fs.Bool("browser", false, "serve on a loopback port behind a session token instead of opening a window")
	addr := fs.String("addr", "127.0.0.1:0", "browser-mode listen address")
	dev := fs.Bool("dev", false, "development mode: developer tools, stack traces, and control commands on stdin")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: krate-shell [--browser] [--addr 127.0.0.1:0] [--dev] [appDir]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
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
		appDir = shell.DefaultAppDir(exe)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := shell.Run(appDir, shell.RunOptions{
		Browser: *browser,
		Addr:    *addr,
		Dev:     *dev,
		Out:     ctlOut,
		Context: ctx,
		OnReady: func(ctl *shell.Control) {
			fmt.Fprintln(ctlOut, "ready")
			if !*dev {
				return
			}
			// stdin EOF means the driving process is gone.
			if !shell.ServeControl(os.Stdin, ctlOut, ctl) {
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
