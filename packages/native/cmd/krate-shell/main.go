// Command krate-shell is the prebuilt Krate Native shell. It opens the window
// of an app directory (see packages/native/shell/README.md), serves the built
// UI, runs the app's host code and, in dev, takes commands on stdin.
//
//	krate-shell [--browser] [--addr 127.0.0.1:0] [--dev] [appDir] [files and URLs...]
//	krate-shell --doctor
//
// --doctor prints one line of JSON about the system webview (library
// versions and paths, display availability) and exits; krate native doctor
// calls it.
//
// Without appDir the app is expected next to the binary: <exe dir>/app, or
// ../Resources/app inside a macOS .app bundle.
//
// stdout is the control channel and carries only protocol lines: "url <url>"
// (browser mode), "ready", and with --dev one "ok" or "error <message>" per
// command. Logs and host console output go to stderr.
package main

import (
	"os"
	"runtime"

	"github.com/kratejs/krate/packages/native/shell"
)

// The platform layer needs the main goroutine on the main OS thread.
func init() { runtime.LockOSThread() }

func main() { os.Exit(shell.CLI(os.Args[1:], nil)) }
