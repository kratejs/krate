// Package gohost runs an app's Go host code in a separate child process, so
// that a Go edit restarts only the child and the window survives.
//
// The child is a generated main that calls Serve with the app's registration
// function. The parent (the shell) starts it with Child, which owns the
// process, its private unix socket and a scheme.Remote that forwards calls,
// streams and signals to it.
//
// Child protocol:
//
//   - The parent sets KRATE_GOHOST_SOCKET to a unix socket path. AF_UNIX
//     works on Windows 10 and later.
//   - The child listens on that path, serves a scheme handler (calls,
//     streams and signals only; no assets) and writes "ready\n" to stdout.
//     Any other stdout or stderr line is the child's log.
//   - The shell sets KRATE_GOHOST_PARENT to "unix:<socket path>", where it
//     serves the SDK's UI operations (windows, dialogs, clipboard, menus,
//     tray, notifications, shortcuts, lifecycle) for the child: the child has
//     no platform of its own and the SDK packages forward to this socket.
//   - The child exits when its stdin reaches EOF (the parent died or closed
//     it) or on SIGTERM.
package gohost

// ParentEnv names the environment variable that carries the address of the
// shell's SDK socket (see the package comment).
const ParentEnv = "KRATE_GOHOST_PARENT"

// SocketEnv names the environment variable that carries the socket path.
const SocketEnv = "KRATE_GOHOST_SOCKET"

// ListPath is a request path the JavaScript host children (package hostchild)
// serve in addition to the scheme handler's: a JSON object with the funcs,
// streams and signals arrays of the ids they registered. The shell merges it
// with the lists in app.json, which matters for exports whose kind is only
// known when the code runs.
const ListPath = "/__krate/hostchild/list"

// ReadyLine is what the child writes to stdout once it is listening.
const ReadyLine = "ready"
