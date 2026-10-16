---
title: API Routes
description: TypeScript and Go API endpoints under /api.
order: 5
badge:
  text: New
  variant: new
---

# API Routes

API routes live in `src/api/` and can be written in **TypeScript/JavaScript**
or **Go**. Both compile into the same `/api/*` namespace and can be mixed per
route. When a `.go` and a `.ts`/`.js` file map to the same `/api` path, the
compiled Go route takes precedence.

Route mapping follows the page convention:

| File | Route |
|------|-------|
| `src/api/hello.ts` | `/api/hello` |
| `src/api/users/index.go` | `/api/users` |
| `src/api/users/[id].go` | `/api/users/{id}` |

## JS/TS routes

JS/TS routes are compiled and served by the **embedded QuickJS runtime** by
default (no Node.js process) or by a Node/Bun/Deno sidecar when configured.

Each route is compiled twice: a self-contained IIFE bundle for the embedded
runtime and an ESM output for the Node sidecar. Routes that import a Node
built-in or a native module that cannot be bundled are marked **Node-only** and
transparently forwarded to the sidecar.

### Runtime model and performance

- The embedded runtime keeps a **pool of long-lived VMs** (`ssr.apiWorkers`,
  default `min(NumCPU, 8)`). Each worker loads a route module once and reuses it,
  so a request is a single handler call rather than a fresh VM plus a full
  re-evaluation. Requests run in parallel across workers.
- **Module state persists per worker across requests** (the same semantics as
  Node/Next: a module's top-level state survives between requests). Set
  `ssr.apiIsolation: true` to run every request in a fresh VM instead.
- The Node sidecar runs `node:cluster` across `ssr.nodeWorkers` processes, and
  imports each route once (module cache) in production.
- Set `ssr.apiRuntime: "bun"` to run the JS/TS routes in a **Bun sidecar** using
  `Bun.serve` (no Node `http` stack, no cluster shim) — the fastest of the
  sidecar options. `ssr.apiRuntime: "node"` (or `"deno"`) uses the Node `http`
  sidecar instead. The top-level `runtime: "bun"` selects the same Bun server
  when no explicit `apiRuntime` is set.
- Go API routes (`src/api/*.go`) remain the fastest option; the sidecar binary
  is a compiled Go server.

### Modern pattern (recommended): named method exports

```typescript
// src/api/users.ts
export async function GET(request: Request) {
  const users = await db.getUsers();
  return Response.json(users);
}

export async function POST(request: Request) {
  const body = await request.json();
  const user = await db.createUser(body);
  return Response.json(user, { status: 201 });
}
```

### Legacy pattern: default export

```typescript
// src/api/legacy.ts
export default function handler(req, res) {
  res.json({ message: 'legacy pattern' });
}
```

## Go routes

Go routes (`src/api/*.go`) are compiled into a dedicated Go sidecar binary
(`.krate/goapi-server[.exe]`) for maximum performance and true concurrency. The
main server reaches it over an **AF_UNIX socket** (`.krate/run/goapi.sock`)
rather than a loopback port, so no port is consumed and the hop skips the TCP
stack; it falls back to a loopback port only if the socket cannot be created.

### Quick start (Go routes)

1. **Install the Go toolchain** (1.21+) and make sure `go` is on `PATH`. Go
   routes are compiled with `go build` at build time.
2. **Add a file** under `src/api` (the default `go.apiDirs`). No config is
   required for the default layout; add the [`go` block](/docs/reference/config/#go-components--api-routes)
   only to change paths or pull in third-party modules.
3. **Set up editor intellisense** (optional but recommended):

   ```sh
   krate go:init
   ```

   This writes a `go.mod` + `go.work`, vendors the helper module under
   `.krate/goext`, and adds `.krate/types` to your `tsconfig.json` so
   `goimports`/gopls resolve the `goext/runtime` import.

4. Run `krate dev` and hit the route.

```go
// src/api/hello.go — GET /api/hello
package api

import (
	"net/http"

	runtime "github.com/kratejs/krate/packages/compiler/goext/runtime"
)

func GET(w http.ResponseWriter, r *http.Request) {
	runtime.WriteJSON(w, 200, map[string]interface{}{"hello": "world"})
}
```

Each file defines a `Handler(w, r)` function for all methods, and/or named
`GET`/`POST`/`PUT`/`DELETE`/`PATCH`/`OPTIONS`/`HEAD` functions for per-method
dispatch. Dynamic segments use `[param]` file names and are read with
`r.PathValue("param")`:

```go
// src/api/users/[id].go — all methods on /api/users/{id}
func Handler(w http.ResponseWriter, r *http.Request) {
	runtime.WriteJSON(w, 200, map[string]interface{}{"id": r.PathValue("id")})
}
```

Go routes require the Go toolchain at build time.

The helper packages live at real import paths
(`github.com/kratejs/krate/packages/compiler/goext/runtime` and
`.../goext/krate`) so `goimports`/gopls provide intellisense. Run `krate go:init`
once to write a project `go.mod` + `go.work` and vendor the helper module under
`.krate/goext`, so the imports also resolve offline at build time. `krate doctor`
reports whether the setup is complete.

### Packages, helpers, and dependencies

The `src/api` tree is preserved when it is compiled: a file that defines no
handler is treated as a **helper** and keeps its package clause, so sibling and
internal packages stay importable at `krate-goapi/routes/<dir>`:

```go
// src/api/lib/util.go
package lib

func Message() string { return "hi" }

// src/api/hello.go
package api

import (
	"net/http"

	"krate-goapi/routes/lib"
	runtime "github.com/kratejs/krate/packages/compiler/goext/runtime"
)

func GET(w http.ResponseWriter, r *http.Request) {
	runtime.WriteJSON(w, 200, map[string]interface{}{"msg": lib.Message()})
}
```

Third-party modules are declared under `go` (or via a project-root `go.mod`,
which is merged — its `require`/`replace`/`exclude` directives are copied and
relative `replace` paths rebased):

```typescript
// krate.config.ts
go: {
  deps: [{ path: "github.com/google/uuid", version: "v1.6.0" }],
  replaces: [{ from: "example.com/local", to: "../local" }],
},
```

A `src/api/go.mod` (with an optional `go.sum`) is used verbatim for full
control. The compiled sidecar binary is cached by the content of `src/api` and
the module config, so unchanged routes skip `go build`. See
[Config Reference](/docs/reference/config/#go-components--api-routes).

## Custom API sidecar

You can run your own HTTP service (any language) and let Krate forward `/api/*`
to it. Krate forwards the request first; if your service returns `404`, the
request falls through to Krate's built-in Go/TS/QuickJS routes, and finally to a
`404` if nothing matches. Because the sidecar sees the full path, it can own
dynamic routes itself (`/api/users/123`, `/api/blog/2024/hello`) with no route
declaration:

```typescript
// krate.config.ts
export default defineConfig({
  api: {
    sidecar: {
      // Supervised: Krate starts/stops the process and restarts it if it exits.
      command: "node",
      args: ["server.js"],
      port: 8080,
      // ...or attach to an already-running service:
      // target: "http://127.0.0.1:8080",
    },
  },
});
```

The sidecar process receives `PORT` and `KRATE_API_SIDECAR_PORT` in its
environment. Set `prefix` (default `/api`) to give the sidecar a different
subtree. See [Config Reference](/docs/reference/config/#api-sidecar).
