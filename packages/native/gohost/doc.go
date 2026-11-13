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
//     works for Go on Windows 10 and later. A JavaScript child on Windows
//     cannot use AF_UNIX (Node, Bun and Deno treat the path as a named pipe),
//     so there the value is "tcp:127.0.0.1:0" and KRATE_GOHOST_TOKEN carries a
//     random token: the child listens on a loopback port it picks, announces
//     it as "ready tcp:127.0.0.1:<port>", and rejects (401) every request
//     whose X-Krate-Token header differs. See package hosttransport.
//   - The child listens there, serves a scheme handler (calls,
//     streams and signals only; no assets) and writes "ready\n" to stdout.
//     Any other stdout or stderr line is the child's log.
//   - The shell sets KRATE_GOHOST_PARENT to "unix:<socket path>" (or, for a
//     JavaScript child on Windows, "tcp:127.0.0.1:<port>?token=<hex>"), where it
//     serves the SDK's UI operations (windows, dialogs, clipboard, menus,
//     tray, notifications, shortcuts, lifecycle) for the child: the child has
//     no platform of its own and the SDK packages forward to this socket.
//   - The child exits when its stdin reaches EOF (the parent died or closed
//     it) or on SIGTERM.
package gohost

import "github.com/kratejs/krate/packages/native/internal/hosttransport"

// ParentEnv names the environment variable that carries the address of the
// shell's SDK socket (see the package comment).
const ParentEnv = "KRATE_GOHOST_PARENT"

// DevEnv is set to "1" by a shell started with Dev; only then do panic
// errors of the child carry stacks.
const DevEnv = "KRATE_DEV"

// SocketEnv names the environment variable that carries the socket path.
const SocketEnv = "KRATE_GOHOST_SOCKET"

// TokenEnv names the environment variable that carries the request token of a
// tcp transport.
const TokenEnv = hosttransport.TokenEnv

// TransportFor picks the transport of a child: "tcp" for a JavaScript child
// on Windows, "unix" otherwise.
func TransportFor(goos string, jsChild bool) string { return hosttransport.For(goos, jsChild) }

// ListPath is a request path the JavaScript host children (package hostchild)
// serve in addition to the scheme handler's: a JSON object with the funcs,
// streams and signals arrays of the ids they registered. The shell merges it
// with the lists in app.json, which matters for exports whose kind is only
// known when the code runs.
const ListPath = "/__krate/hostchild/list"

// ReadyLine is what the child writes to stdout once it is listening.
const ReadyLine = hosttransport.ReadyLine
