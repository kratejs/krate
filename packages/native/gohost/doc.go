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
//   - The child exits when its stdin reaches EOF (the parent died or closed
//     it) or on SIGTERM.
package gohost

// SocketEnv names the environment variable that carries the socket path.
const SocketEnv = "KRATE_GOHOST_SOCKET"

// ReadyLine is what the child writes to stdout once it is listening.
const ReadyLine = "ready"
