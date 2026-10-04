---
title: API Routes
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

JS/TS routes are compiled to JS and served via a Node.js sidecar (port 3001)
or the embedded QuickJS runtime.

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
(`.krate/goapi-server[.exe]`, port `+2`) for maximum performance and true
concurrency.

```go
// src/api/hello.go — GET /api/hello
package api

import (
	"net/http"

	"krate-goapi/runtime"
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

Go routes require the Go toolchain at build time and should stick to the
stdlib plus the `krate-goapi/runtime` helper package.

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
