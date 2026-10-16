# Security Policy

Krate takes security seriously. This project compiles user-supplied TypeScript
into HTML and JavaScript at build time, and runs plugin/API code in embedded
runtimes at serve time — so we care a lot about XSS, path traversal, and code
injection vectors.

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Instead, report privately by emailing the maintainers at
`noinkin@gmail.com`. You can also use GitHub's private vulnerability
reporting feature on the repository's *Security* tab.

Please include:

- A description of the vulnerability and the affected component
- Steps to reproduce (or a minimal test case)
- The impact and any potential exploit scenarios
- Suggested fixes, if you have them

You should receive an acknowledgement within 48 hours and a detailed response
(including next steps) within a week.

## Scope

The following are in scope:

- XSS via SSR output, hydration bindings, `$esc()`, or HTML entity escaping
- Path traversal in the bundler, CSS `@import` inlining, icons, plugins, or API route resolution
- Handler/script string injection (escaping of inline JS)
- The embedded QuickJS plugin/API runtime sandbox
- The Go API sidecar and middleware execution
- Request handling in `krate serve` (headers, body limits, timeouts, panic
  isolation)

## Server hardening defaults

`krate serve` and `krate dev` apply these by default:

- Read-header/read/write/idle timeouts on the HTTP server; SSE and streaming
  responses extend their own write deadline.
- Panic recovery per request: a panic returns `500` and is logged, it does not
  take the process down.
- Request bodies for API routes/middleware are capped (`maxRequestBodyBytes`).
- Security headers: `X-Content-Type-Options`, `X-Frame-Options`,
  `Referrer-Policy`, `Permissions-Policy`, and HSTS when `seo.baseUrl` is https.
- `Content-Security-Policy` as an HTTP header when `csp.enabled` is set
  (hashes are generated for inline scripts/styles; the header adds
  `frame-ancestors`, which a `<meta>` CSP cannot express).
- Request IDs (`X-Request-Id`) for correlation, with structured `log/slog`
  request logs.

## Safe Harbor

We support coordinated disclosure. Researchers who act in good faith — avoiding
privacy violations, destruction of data, and disruption of production services —
will not face legal action from us.

## Security Considerations for Contributors

- Never trust file paths derived from content (icons, CSS `@import`, plugins,
  API routes) — always validate against path traversal.
- Never interpolate user content into HTML or JS without escaping.
- The `$esc()` sanitizer and `escapeHTML()` are security boundaries. Do not
  weaken them.
- JS plugins run in QuickJS with a 32 MB memory limit, a 256 KB stack, and a
  `30s` eval timeout (`internal/jsruntime/jsruntime.go`). Plugin file writes go
  through `pluginapi.WriteFileToRoot`, which rejects paths that escape the
  project root (`WithinRoot`). Preserve these guarantees.
