// Package hostchild holds the JavaScript runtime of a TypeScript host child:
// the program that serves the host call protocol (see PROTOCOL.md) for host
// code running on Node, Bun or Deno, the way package gohost does for Go.
//
// Source is one ES module, generated from src/runtime.ts (which imports the
// frame codec from packages/app/src/wire.ts, so there is a single codec).
// Regenerate it with:
//
//	go test ./hostchild -run TestBundleUpToDate -update
//
// A host bundle imports this module first, registers its functions with the
// globalThis.__krateHost object the import installs, then calls start().
package hostchild

import (
	_ "embed"

	"github.com/kratejs/krate/packages/native/gohost"
)

// Source is the runtime as an ES module. It exports start() and the wire codec
// (buildFrame, decodeFrame, decodeFrames, encodeFrame, encodeValue).
//
//go:embed hostchild.mjs
var Source string

// SocketEnv names the environment variable that carries the socket path; it is
// the same variable the Go host child reads.
const SocketEnv = "KRATE_GOHOST_SOCKET"

// ListPath is the extra, non-standard endpoint a host child serves so that the
// shell can learn which ids it registered at run time: a JSON object with
// funcs, streams and signals arrays.
const ListPath = gohost.ListPath
