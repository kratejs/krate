package shell

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kratejs/krate/packages/native/plugin"
)

// CLI is the whole main function of a krate-shell binary:
//
//	krate-shell [--browser] [--addr 127.0.0.1:0] [--dev] [appDir] [files and URLs...]
//	krate-shell --doctor
//
// The first argument is the app directory only when it holds an app.json;
// otherwise the app is the one next to the binary and every argument is a file
// or URL the app is asked to open (see sdk/app).
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
	// Arguments after "--" belong to the app (a launcher puts them there); the
	// app reads them from the command line itself.
	shellArgs := DropLaunchServicesArgs(args)
	for i, a := range shellArgs {
		if a == "--" {
			shellArgs = shellArgs[:i]
			break
		}
	}
	if err := fs.Parse(shellArgs); err != nil {
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
	appDir := ""
	if rest := fs.Args(); len(rest) > 0 && IsAppDir(rest[0]) {
		// Arguments after the app directory (or all of them, when the first is
		// not one) are files and URLs the app is asked to open.
		appDir = rest[0]
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "krate-shell:", err)
		return 1
	}
	if err := checkLaunch(exe, appDir, *dev); err != nil {
		fmt.Fprintln(os.Stderr, "krate-shell:", err)
		return 2
	}
	if appDir == "" {
		appDir = DefaultAppDir(exe)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = Run(appDir, RunOptions{
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

// IsAppDir reports whether p is an app directory (it holds app.json), so that
// the shell can tell it from a file the app is asked to open.
func IsAppDir(p string) bool {
	st, err := os.Stat(filepath.Join(p, ManifestFile))
	return err == nil && st.Mode().IsRegular()
}

// DropLaunchServicesArgs removes the "-psn_<n>_<n>" process serial number
// argument that older macOS versions pass when Finder launches an app: it is
// not a flag of the shell.
func DropLaunchServicesArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if !strings.HasPrefix(a, "-psn_") {
			out = append(out, a)
		}
	}
	return out
}

// checkLaunch refuses what a distributed app must not do. An executable with
// an app directory next to it (DefaultAppDir) is that app as built by krate
// native build: it runs only that app, and takes --dev only when the app was
// built with native.debug. Otherwise anyone who can start the executable could
// run their own app.json and code, or drive the app's pages over the dev
// control channel, under the app's identity: its code signature, keychain
// items, granted files and camera or microphone consent. A bare krate-shell
// (krate native dev, krate native test) has no app next to it and is not
// limited. appArg is the app directory given on the command line, or "".
func checkLaunch(exe, appArg string, dev bool) error {
	own := DefaultAppDir(exe)
	if !IsAppDir(own) {
		return nil
	}
	if appArg != "" && !samePathOnDisk(appArg, own) {
		return fmt.Errorf("%s is a distributed app; it runs the app next to it (%s), not another app directory (%s). Run other apps with krate-shell", filepath.Base(exe), own, appArg)
	}
	if dev {
		man, err := ReadManifest(own)
		if err != nil {
			return err
		}
		if !man.Debug {
			return fmt.Errorf("--dev is not available in a distributed app: it would let whoever starts %s run script in its pages. Use krate native dev, run krate-shell --dev with the app directory, or build with native.debug", filepath.Base(exe))
		}
	}
	return nil
}

// samePathOnDisk reports whether a and b name the same directory.
func samePathOnDisk(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return false
	}
	ra, _ = filepath.Abs(ra)
	rb, _ = filepath.Abs(rb)
	if st1, e1 := os.Stat(ra); e1 == nil {
		if st2, e2 := os.Stat(rb); e2 == nil {
			return os.SameFile(st1, st2)
		}
	}
	return ra == rb
}
