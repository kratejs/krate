---
title: MCP Server (Agent-Native)
order: 16
description: Expose the Krate compiler, the running dev server and the compiled site to AI agents over the Model Context Protocol with krate mcp.
sidebar:
  label: MCP Server
  order: 14
---

# MCP Server (Agent-Native)

`krate mcp` starts an [MCP](https://modelcontextprotocol.io) (Model Context
Protocol) server that exposes the Krate compiler to AI agents. The agent talks
to the real compiler - routes, AST, diagnostics, content graph - instead of
grepping a filesystem.

The server speaks **JSON-RPC 2.0 over stdio** and has **no external
dependencies**.

## Setup

Register the server once in your agent/editor config. The working directory (or
a trailing path argument) selects the project root.

For clients that support **Streamable HTTP** instead of stdio:

```sh
krate mcp --http --port 7331 --token "$KRATE_MCP_TOKEN"
```

The HTTP endpoint is `http://127.0.0.1:<port>/mcp`. POST JSON-RPC requests to
it; GET opens an SSE keepalive stream. It binds to localhost by default and
requires the bearer token when one is configured. stdio remains the default.

Add read/index roots for a multi-root workspace with repeatable `--root`:

```sh
krate mcp --root docs=../docs --root ui=../packages/ui
```

The primary project remains the build and write root; extra roots are available
to `search_project`, `list_files`, `read_file`/`read_files`, `find_references`,
and `workspace_symbols`. Their paths are returned as `root:path`.

### Claude Desktop / Claude Code

`claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "krate": { "command": "krate", "args": ["mcp", "/abs/path/to/site"] }
  }
}
```

Omit the path to use the current working directory: `"args": ["mcp"]`.

### Cursor / VS Code / opencode

The shape is the same, under the client's own key:

- Cursor - `.cursor/mcp.json`
- VS Code - `.vscode/mcp.json` (under `"servers"`)
- opencode - `opencode.json`

```json
{
  "mcpServers": {
    "krate": { "command": "krate", "args": ["mcp"] }
  }
}
```

## Breaking change

`edit_ast` has been removed. Edit source directly with `edit_file`,
`apply_patch`, or `batch_edit`; use `read_page` with `format: "ast"` only for
inspection. Applied edits are transactional and reversible with `undo`.

## Tools

Krate ships 60 tools. They fall into groups, and each tool has a JSON
input schema, a description written for model tool selection, and bounded output
(`limit` and `cursor` on every list).

### Live app (the running `krate dev`)

| Tool | Description |
|------|-------------|
| `dev_server_status` | Is `krate dev` running (URL, pid, last build ok, pending build and browser errors) |
| `get_errors` | Build errors with KR codes and file:line:col, browser runtime errors with stacks, and server errors from the dev log; falls back to a fresh build when no dev server is running |
| `get_logs` | Tail of the dev server log with `grep`, `level` and `since` filters; secrets are redacted |
| `dev_server` | Start, stop or restart `krate dev` as a child of the MCP server (dry-run by default; needs `dev` in `allowTasks`) |
| `get_server_action_by_id` | Resolve a server action id to its export, file, line and endpoint |

### Page truth

| Tool | Description |
|------|-------------|
| `explain_page` | Render mode and why, layouts, component tier of every module and the rule that decided it, islands, build-time-only modules, and exact JS/CSS bytes (raw and gzip) |
| `why_hydrated` | Why a page ships JavaScript, with file:line evidence, the import chain to each hydrating module and concrete suggestions |
| `why_in_bundle` | The import chain from every route that reaches a file or npm package, and whether a server component keeps it out of the browser |
| `list_islands` | Client components and npm package islands, ranked by how many routes pay for them |
| `get_page_metadata` | Route, mode, layouts, segment pages, metadata exports and the built `<head>` with missing SEO basics |
| `get_rendered_html` | The built HTML of a route (head, body or visible text) in byte-bounded chunks |
| `render_preview` | Structural preview without a browser: outline, landmarks, links, images without alt, forms, text |
| `read_page` | A page's source, kind-tagged AST document, or rendered HTML (`format` selectable) |
| `list_routes` | Every route with source file, render mode, and dynamic params |
| `explain` | A route from the compiled site graph: dependencies, dependents, emitted output files |

### Project truth

| Tool | Description |
|------|-------------|
| `get_project_metadata` | Package, framework versions, Node, directories, route counts by mode, API routes, actions, middleware, build and dev server state, MCP policy |
| `get_config` | The resolved config as dotted keys, each tagged with its source (config file, tsconfig, default) |
| `list_api_routes` | API routes with language, HTTP methods read from exports, params, and Go-over-TypeScript shadowing |
| `list_middleware` | `middleware.ts` matchers (test a path with `path`), plus config redirects, rewrites and headers |
| `list_actions` | Every `"use server"` export with its stable id |
| `list_collections` | Content collections with entry counts, sample slugs and frontmatter schema |
| `env_check` | Declared vs used environment variables, missing, unused, secret-like `KRATE_PUBLIC_` names; names only, never values |
| `bundle_report` | Output size by type, largest assets, per-route cost, and routes over the `checks.budget` limits |
| `compat_check` | Whether an npm package fits Krate, from its metadata and the compatibility docs |
| `read_content` | Content-collection entries: list a collection or read one entry |
| `search_docs` | Search Krate's built-in framework docs (ranked, via the docfind engine) |

### Quality and diagnostics

| Tool | Description |
|------|-------------|
| `build` | Build the site; returns diagnostics and a per-route summary |
| `check` | Run the quality gates (a11y/SEO/perf); builds first when needed |
| `audit_page` | The quality gates filtered by category, severity or route, paginated, with rule ids and hints |
| `get_diagnostics` | Run build/check and return structured diagnostics with file, range, code, severity, hint and a matching codemod |
| `explain_diagnostic` | A KR code explained: why it is reported, what to do, and whether `apply_safe_fixes` repairs it |
| `typecheck` | `tsc --noEmit` after generating route types, as structured file:line diagnostics (needs `typecheck` in `allowTasks`) |
| `run_task` | Run an allowlisted task: build, check, test, or typecheck |
| `upgrade` | Guided 0.4 to 0.5 upgrade: version check, `krate fix` codemods, script/Dockerfile/CI scan, build errors; dry-run by default |
| `migrate_nextjs` | Plan a Next.js to Krate migration: files, route inventory, conflicts, manual decisions; writes nothing |
| `page_check` | Open a route in headless Chromium: console, page errors, failed requests, timing, accessibility tree, screenshot |
| `browser_eval` | `page_check` plus an expression evaluated in the page |

### Code intelligence

| Tool | Description |
|------|-------------|
| `search_project` | Search project source/content with literal or regex queries, globs, context, and limits |
| `read_files` | Read several project files in one call with byte caps |
| `find_references` | Semantic references to a symbol from the incremental workspace index |
| `workspace_symbols` | Search declarations across the primary and extra workspace roots |
| `impact` | Show routes affected by a file, or a route's dependencies/dependents |
| `list_changes` / `get_history` | Recent MCP transactions for audit and undo |

### Editing

| Tool | Description |
|------|-------------|
| `create_page` | Create a page from a template; returns a diff (dry-run by default) |
| `edit_file` | Edit any project file's source directly (full replace, find+replace, expected hash/mtime); returns a diff |
| `apply_patch` | Apply a unified diff to one or many files atomically; returns a diff |
| `batch_edit` | Apply structured create/replace/delete/move operations atomically |
| `undo` | Undo the most recent transaction (or one by ID) |
| `format_file` | Format a TypeScript/JavaScript source file; returns a diff |
| `apply_fix` | Apply an explicit find/replace repair to a diagnostic finding |
| `apply_safe_fixes` | Preview or apply conservative codemods, optionally filtered by codes and paths |
| `list_files` | List project files (skips `node_modules`/`.git`/`dist`), with an optional `glob` |
| `read_file` | Read any project file by path (text + language, or a binary marker) |
| `create_file` | Create a new file (refuses to overwrite; parse-gated); returns a diff |
| `delete_file` | Delete a file (files only); preview unless `apply` |
| `move_file` | Move/rename a file; preview unless `apply` |
| `create_component` | Scaffold a component (`client`/`static`/`server`, optional CSS); returns a diff |
| `create_content` | Create an entry in a content collection, validated against its schema; returns a diff |
| `edit_content` | Edit a content entry (full replace, frontmatter rewrite, or find+replace); schema-checked |

Every tool advertises [annotations](https://modelcontextprotocol.io/specification/2025-06-18/basic/tools#tool-annotations)
(read-only, additive, destructive) so clients know what to auto-approve and
what to flag for confirmation.

### Live app tools

`krate dev` writes `.krate/dev-server.json` (its URL and pid) when it starts and
mirrors its console output to `.krate/dev.log`, one timestamped line per line of
output. The MCP server finds the running dev server through the first file and
reads its build state and recent browser errors from the loopback-only
`/__krate/state` endpoint, so the answers come from the process you are looking
at, not from a second build.

- **`get_errors`** returns build errors (KR code, `file:line:col`, hint and the
  source line), runtime errors reported by open browser tabs (message and a
  trimmed stack), and server-side errors found in the dev log (render, middleware
  and API sidecar failures from the last 15 minutes, `sinceMinutes` changes the
  window). Without a running dev server it runs a fresh build and reports build
  errors only, and says so. Use `kind` to pick one source.
- **`get_logs`** reads `.krate/dev.log` with `lines`, `grep`, `level` and `since`.
  Environment secrets and the absolute project path are removed.
- **`dev_server`** starts, stops or restarts `krate dev` as a child of the MCP
  server. It is a dry-run until `apply: true`, it needs `"dev"` in `allowTasks`
  (it runs project code), it is off in `readOnlyMode`, it refuses to start a
  second server, and it only stops a server it started itself. The child is
  stopped when the MCP server exits.
- **`dev_server_status`** says whether a server is up, its URL, whether the last
  build passed and how many errors are pending.

A typical loop: `dev_server` (start) -> edit with `edit_file` -> `get_errors` ->
fix -> `get_errors` clean. Every `edit_file` is reversible with `undo`.

### Page tools

`explain_page`, `why_hydrated`, `why_in_bundle` and `list_islands` read the page
sources with the same import resolution as the bundler (relative imports,
extensions, `index` files, tsconfig `paths` aliases) and classify every module's
component tier with the documented priority: decorator, leading directive, file
name, config lists, directories, `async`, then client. They need no build.
`explain_page` adds the exact bytes (raw and gzip) of the JS and CSS the built
page loads when the site is built. File sizes of modules are source sizes; the
bundle sizes are the per-asset ones.

`why_hydrated` separates component hydration from the shared runtime: a page with
no hooks, signals or handlers but a script tag is reported as loading runtime
features only. A `client:visible` or `client:idle` directive on a child does not
make the page that writes it hydrate; it defers that child.

`get_rendered_html` and `render_preview` read the built documents. They never
start a server, so they are quick and safe on large sites; `render_preview` gives
the outline, landmarks, links, images without alt text, forms and the opening
text, enough to check what a page says.

### Project tools

`get_config` flattens the resolved config into dotted keys and tags each with its
source. A value equal to the built-in default is `default`; one that differs came
from the config file or, for `pathAliases`, tsconfig. Paths are relative and keys
named like secrets are redacted.

`env_check` is built so a secret cannot leak: it reads `.env*` files only for the
variable names, scans source for `process.env.X`, `process.env["X"]` and
`import.meta.env.X`, and reports names with `file:line`. It flags variables the
code reads but nothing declares, declared variables nothing reads, secret-looking
names with the `KRATE_PUBLIC_` prefix (their values are inlined into client
JavaScript), and non-public reads in client-tier code. No value is ever returned,
and a variable set in the shell counts as provided.

`list_middleware` compiles the matcher with the same rules as the server (a port
of the runtime matcher, lookahead included), so `path` answers "does this URL run
the middleware?" exactly.

`bundle_report` needs a build. Budgets come from `checks.budget` in the config
(KB per route); routes over `js` or `route` are listed.

### Quality tools

`audit_page` is `check` with filters and pagination. `explain_diagnostic` reads
the compiler's diagnostic registry, so every `KR` code in any tool output can be
explained, and it says when `apply_safe_fixes` can repair the code.

`typecheck` runs the project's own `tsc --noEmit` after generating the route
types and returns `file`, `line`, `col`, the `TS` code and the message; the
diagnostics about tier decorators that Krate removes at compile time are
filtered out. It needs `"typecheck"` in `allowTasks`.

`upgrade` walks the 0.4 to 0.5 checklist against the project: package versions
on one minor, the `krate fix` codemods (`tsconfig` `baseUrl`, lowercase events,
and so on), scripts, Dockerfiles and CI that use `krate start`, `krate dev` in a
container, `dist/manifest.json` or `bin/krate`, Tailwind v4 and the config
sandbox, and a build to surface errors 0.4 let through. Every step has a status
(`ok`, `action`, `manual`, `info`) and `file:line` evidence. With `apply: true` it
runs the safe codemods as one transaction that `undo` reverses; edits to scripts,
Dockerfiles and dependencies are listed, never made. Pass `build: false` to skip
the build step.

`migrate_nextjs` is the `krate migrate nextjs` plan as data. It writes nothing;
the apply step stays a CLI command because it backs up and overwrites many files.

### Browser tools

`page_check` and `browser_eval` drive the **project's own Playwright install**:
Krate adds no browser dependency to the binary. They need Node.js, `playwright`
resolvable from the project, an installed Chromium, and `"browser"` in
`allowTasks`. Without Playwright they fail with the exact install commands
(`npm i -D playwright && npx playwright install chromium`).

The page opens on the running dev server, or, when none is running, on a static
server over the build output (request-time pages need the dev server). Requests
to any other origin are aborted, so an expression cannot send data out of the
machine. The result has console messages, uncaught page errors, failed requests,
4xx/5xx responses, load timing, JS bytes, the number of blocked external
requests, optionally the accessibility tree (`aria`) and a PNG screenshot as an
image block, and for `browser_eval` the JSON value of the expression.

### Read tools

`list_routes`, `read_page`, `read_content`, `search_docs`, `build`, and `check`
never write source files.

`read_page` lets the agent choose what it needs with `format` (default `source`):

- `source` - the raw file text, so the agent can read and edit the actual page.
- `ast` - the **kind-tagged AST document** (the same format JS plugins
  receive), with `lossyTypes` reporting TypeScript constructs the parser drops.
- `html` - the rendered output when the site has been built.
- `all` - source, AST, and rendered HTML together.

`read_content` lists a collection's entries (slug, project path, frontmatter)
when `collection` alone is given, or returns a single entry's raw file, parsed
frontmatter, and markdown body (plus rendered HTML for `.md`) when `slug` is
added.

`search_docs` searches **Krate's own framework documentation** - the content
that powers the docs site - which is embedded into the compiler at build time,
so it works the same from any project and offline. It is backed by the
[docfind](https://github.com/microsoft/docfind) WASM engine running in-process
(the same engine Krate uses for docs search during development builds): results
are ranked, and each hit carries a short `excerpt`, a `slug`, and a `resource`
(`krate://docs/{slug}`) for reading the whole page.

- `limit` caps the number of hits (default 8).
- `maxChars` sizes the `excerpt` window (default 240); it only affects the
  excerpt, never the full text.
- `full: true` adds a `content` field with the hit's complete cleaned text
  (plain text with newlines).

It does **not** scan the current project's content; use
`read_content`/`krate://content` for that.

### Context tools

`search_project` walks project source and content files, honors the policy and
ignored directories, and returns matches with surrounding context. Use `regex`
for patterns, `glob` to scope paths, and `context`/`limit` to control payload
size.

`read_files` is the batch form of `read_file`, with per-file byte caps.

`get_diagnostics` runs a build (and optionally `check`) and returns structured
entries: code, severity, file, line/column, route, message, and hint. It also
includes the build output and whether the build succeeded. A `codemods` array
lists conservative safe rewrites with their exact location and from/to text.
Pass `codes` and/or `paths` to `apply_safe_fixes` to apply only that subset
(dry-run unless `apply: true`).

`find_references` and `workspace_symbols` share an incremental semantic index.
The index parses TS/JS/TSX/JSX declarations, imports, exports, identifiers, and
JSX component references, so comments and string literals are not reported as
references. Files are re-indexed only when their size or mtime changes.
`workspace_symbols` returns declaration kind, exported state, root, path, and
location; `find_references` returns reference/definition/import kinds plus a
`definitions` list.

`impact` uses the compiled site graph to report dependent routes for a file or
dependencies and dependents for a route. Both index queries accept a `root`
filter; multi-root result paths use `root:path`. The primary build/write root is
unchanged by extra roots.

`list_changes` and `get_history` expose recent MCP transactions, including file
lists, timestamps, and undone state.

### File tools

The file tools let the agent discover and manage arbitrary project files
(components, styles, assets), not just pages:

- **`list_files`** lists project-relative paths (skipping `node_modules`,
  `.git`, `dist`, ...). Filter with `glob` (`src/components/**/*.tsx`), scope with
  `dir`/`root`, and set `includeDenied: true` to see policy-denied paths.
  Multi-root paths are returned as `root:path`.
- **`read_file`** returns a file's text with a language hint (or a `binary`
  marker). Sensitive paths are refused (see the policy below). Use `root` or a
  `root:path` target for extra workspace roots.
- **`create_file`**, **`delete_file`**, and **`move_file`** round out the
  lifecycle. `create_file` refuses to overwrite (use `edit_file` to modify) and
  parse-gates editable source; all three default to a dry-run preview.
- **`create_component`** scaffolds a component under
  `src/components/<kebab-name>/`: `kind` selects `client` (imports the runtime),
  `static`, or `server`; `withCss: true` adds a companion `.css` file. Continue
  with `edit_file` for changes.

### Write tools

`create_page`, `edit_file`, `apply_patch`, `batch_edit`, `create_file`,
`delete_file`, `move_file`, `create_component`, `create_content`,
`edit_content`, `apply_fix`, and `apply_safe_fixes` **default to a dry-run** that returns a unified diff. Pass
`"apply": true` to write. This gives the agent (and you) an approval step before
anything changes on disk.

All writes go through one transaction layer: edits are validated before commit,
multi-file operations are rolled back on failure, and applied transactions can
be reversed with `undo`. `apply_patch` accepts unified diffs; `batch_edit`
accepts structured operations; both support per-file `expectedSha256` checks.

### Permissions (`.krate/mcp.json`)

By default the agent may read and write any project file - pages, CSS,
components, API routes, content, docs, and config - **except** a small set
of sensitive paths - environment files (`.env*`), private keys (`*.pem`,
`*.key`, `id_rsa`, ...), `.git/`, npm/pypi/netrc credential files, and the policy
file itself. Those require an explicit `allow` entry. A committed policy at
`.krate/mcp.json` customises this:

```json
{
  "allow": ["config/public.env"],
  "deny": ["src/generated/**"],
  "readOnly": ["krate.config.ts"],
  "writeRoots": ["src", "public"],
  "maxFileBytes": 2097152,
  "maxBatchFiles": 50,
  "allowBinary": false,
  "readOnlyMode": false,
  "allowTasks": ["build", "check", "test", "typecheck", "dev", "browser"],
  "auditLog": ".krate/mcp-audit.jsonl",
  "rateLimit": { "requests": 120, "windowSeconds": 60 },
  "allowedHosts": [],
  "redact": ["my-literal-private-value"]
}
```

- **`allow`** overrides the default-deny list (e.g. permit one checked-in
  `.env.example`).
- **`deny`** always wins, even over `allow`.
- **`readOnly`** permits reading but never writing.
- **`writeRoots`** limits writes to these paths. An empty list keeps the legacy
  project-wide behavior; `["src", "public"]` is a safer default for most sites.
- **`maxFileBytes`** and **`maxBatchFiles`** bound memory and transaction size.
- **`allowBinary`** permits binary writes; it is off by default.
- **`readOnlyMode`** disables every write tool, `run_task`, `dev_server`,
  `typecheck`, `page_check` and `browser_eval` for a shared or remote MCP profile.
- **`allowTasks`** gates `run_task` and the tools that run project code:
  `dev` enables `dev_server`, `typecheck` enables the `typecheck` tool, and
  `browser` enables `page_check` and `browser_eval`. The default is `build` and
  `check`. Generated/output trees (`node_modules`, `dist`, `.krate`, `build`) are
  never writable.
- **`auditLog`** writes redacted JSONL tool-call records. It defaults to
  `.krate/mcp-audit.jsonl`; set it to `"off"` to disable audit logging.
- **`rateLimit`** bounds HTTP requests per client address (default 120 per 60
  seconds). `{ "disabled": true }` turns the limiter off.
- **`allowedHosts`** lets a reverse proxy expose MCP under a public hostname.
  Loopback hosts are always accepted; other `Host` headers are rejected.
- **`redact`** lists extra literal values to mask in tool output and audit logs,
  in addition to environment values with secret-like names.

Every write also rejects absolute paths and traversal outside the project root,
resolving symlinks so a symlinked directory cannot redirect a write elsewhere.
Path rules are applied to a symlink's own name and to the path it resolves to,
so a link named `notes.txt` that points at `.env` is as protected as `.env`. The
default-deny list, `deny`, `readOnly` and the generated trees match without regard
to case (`.ENV` is `.env` on macOS and Windows); `allow` and `writeRoots` match
exactly.

### Remote operations

For a remote deployment, bind a non-loopback interface and always set a bearer
token:

```sh
krate mcp --http --host 0.0.0.0 --port 7331 --token "$KRATE_MCP_TOKEN"
```

`krate mcp` refuses unauthenticated binding outside loopback. The HTTP handler
also rejects cross-origin browser requests and unexpected `Host` headers, so a
DNS-rebinding page cannot drive a local server. Bearer tokens, secret-looking
environment values, sensitive JSON fields, and policy `redact` literals are
masked in tool output, errors, and audit records.

Hosted health and metrics endpoints are available alongside `/mcp`:

- `GET /healthz` - unauthenticated liveness with uptime and active sessions.
- `GET /metrics` - bearer-protected Prometheus text: request, authorization,
  rate-limit, origin-rejection, and active-session counters.

- **`create_page`** scaffolds a page from a template - `static` (default),
  `content-list`, `detail`, and `blank` (raw source override). For
  content-backed templates you can pass `collection` and a `contentEntry`
  object so the matching markdown entry is authored alongside the page, and
  `withLayout: true` creates a missing `_layout.tsx`. Applying a page also
  regenerates the generated route/content types.
- **`apply_patch`** accepts a unified diff (`---`, `+++`, `@@`) for one or many
  files. Context must match exactly, and the whole patch is rolled back if any
  file fails validation or policy checks. It supports create, modify, delete,
  and rename operations.
- **`batch_edit`** accepts a list of structured operations (`create`, `replace`,
  `delete`, `move`). Each operation may include `expectedSha256`; the batch is
  committed as one transaction.
- **`undo`** restores the most recent applied transaction, or a transaction by
  ID. It refuses if affected files changed afterward unless `force: true`.
- **`format_file`** formats TypeScript/JavaScript using the compiler printer.
  Files containing TypeScript syntax the AST would drop are refused.
- **`edit_file`** edits the actual file. It takes a `route` (a page route like
  `/about`, or a project-relative path) plus one of two modes:
  - `content` - replace the entire file (creates the file when it does not
    exist).
  - `find` + `replace` - a targeted in-place edit. `find` must match exactly
    once, or pass `replaceAll: true`; a missing or ambiguous match returns a
    helpful error instead of guessing.
  - `expectedSha256` or `expectedMtime` refuses stale edits.
## Compared with the Next.js MCP server

Next.js 16 serves an MCP endpoint at `/_next/mcp` from its dev server, and the
`next-devtools-mcp` package connects an agent to it. The table lists every tool
of both (checked against the
[Next.js guide](https://nextjs.org/docs/app/guides/mcp) and the
[`next-devtools-mcp` README](https://github.com/vercel/next-devtools-mcp)) and
the Krate tool that covers it.

| Next.js | What it does | Krate |
|---------|--------------|-------|
| `get_errors` | Build, runtime and config errors from the dev server | `get_errors` (also server-log errors, KR codes and hints) |
| `get_logs` | Path to the dev log file | `get_logs` returns the lines, filtered and redacted, instead of a path |
| `get_page_metadata` | Render contributions of a page open in a browser | `get_page_metadata`, `explain_page` (no browser session needed) |
| `get_project_metadata` | Project path and dev server URL | `get_project_metadata` (relative paths; versions, routes, build state, policy) |
| `get_routes` | Routes grouped by router | `list_routes`, `list_api_routes`, `list_middleware` |
| `get_server_action_by_id` | Action id to file and export | `get_server_action_by_id`, `list_actions` |
| `get_compilation_issues` | Compilation issues for all routes (Turbopack only) | `get_diagnostics`, `build`, `typecheck` (any build) |
| `compile_route` | Compile one route without a request | `build`, `get_errors` (the dev server rebuilds on save) |
| `get_request_insights` | Request timelines and cache behaviour (experimental) | `get_logs` for requests and errors; no request recorder yet |
| `nextjs_index` | Discover running dev servers and their tools | `dev_server_status`, `tools/list` |
| `nextjs_call` | Call a runtime tool on a dev server | Not needed: the tools are first-class |
| `nextjs_docs` | Points at the docs in `node_modules` | `search_docs` and `krate://docs/{slug}` run the search and return the text |
| `browser_eval` | Points at the `agent-browser` CLI | `browser_eval` and `page_check` run the browser (Playwright) |
| `init`, `upgrade_nextjs_16`, `enable_cache_components` (removed in 0.4.0) | Setup and upgrade prompts, now skills | `upgrade`, `migrate_nextjs`, and the `upgrade-krate` and `migrate-from-nextjs` prompts |

What Krate adds that Next.js does not offer:

- **Edits with a safety net**: `edit_file`, `apply_patch`, `batch_edit` and the
  rest are dry-run by default, policy-checked, transactional and reversible with
  `undo`; Next.js has no write tools.
- **Why, not just what**: `explain_page`, `why_hydrated`, `why_in_bundle` and
  `list_islands` answer why a page is dynamic or interactive and which import
  pulled a package in, with `file:line` evidence, because the compiler owns the
  tier and hydration rules.
- **Project truth**: `get_config` with sources, `env_check` without values,
  `bundle_report` against budgets, `compat_check`, `list_collections`.
- **Quality and migration**: `audit_page`, `explain_diagnostic` for every KR code,
  `typecheck`, `upgrade`, `migrate_nextjs`.
- **A server that works without a running app**: most tools answer from sources
  and the build output; the live tools say when they fall back to a build.
- **Policy and audit**: every call is rate limited over HTTP, redacted and written
  to the audit log, and tools that run project code are opt-in per `allowTasks`.

## Prompts

Prompts package recurring workflows so agents reach for the right tools:

- `add-page` - create a page the Krate way: match house style first, then
  `create_page` with the chosen template, and confirm via `build`/`check`.
- `publish-content` - author a new entry into a typed content collection via
  `create_content` (schema-validated) and wire it into a page.
- `fix-checks` - run the quality gates, then fix the worst findings with
  `apply_fix` or `apply_patch`.
- `repair-diagnostics` - diagnose build/check failures, apply safe codemods
  first (`apply_safe_fixes`, filtered by the diagnostics' codes), then minimal
  manual repairs, and verify with `get_diagnostics` and `run_task`.
- `refactor` - map references and impact, propose a multi-file diff, then apply
  it and verify.
- `audit-dependencies` - report unused or risky dependencies without changing
  files.
- `scaffold-component` - create a component that matches nearby house style.
- `explore` - summarize the project from routes, config, and outstanding checks.
- `debug-running-app` - start or find the dev server, read `get_errors` and
  `get_logs`, explain each KR code, repair, and confirm clean.
- `diagnose-page` - `explain_page`, `why_hydrated`, `why_in_bundle` and
  `audit_page` for one route, then the smallest changes as a dry-run diff.
- `upgrade-krate` - the guided 0.4 to 0.5 upgrade with `upgrade`, safe codemods
  first.
- `migrate-from-nextjs` - plan with `migrate_nextjs`, check packages with
  `compat_check`, verify after the CLI apply.
- `performance-review` - `bundle_report`, `list_islands` and per-route
  explanations, ranked fixes, no edits.

Prompt quality is evaluated in `internal/mcp/prompt_eval_test.go`. The suite
checks every registered prompt, required arguments, optional defaults, absence
of unresolved placeholders, required tool sequence, and safety constraints such
as dry-run-before-apply and read-only dependency audits. Add a case there
whenever a prompt is added or its workflow changes.

## Protocol

- **Structured results**: tools that return JSON also include
  `structuredContent`, so clients can consume diagnostics and listings without
  parsing the text block.
- **HTTP transport**: `krate mcp --http` exposes Streamable HTTP at `/mcp`
  (POST requests, GET SSE). Authentication is bearer-token based; `/healthz`
  and `/metrics` provide liveness and Prometheus observability.
- **Progress**: requests with `_meta.progressToken` receive
  `notifications/progress` during build, check, diagnostics, and search.
- **Pagination**: `list_files`, `search_project`, `workspace_symbols`,
  `list_changes`, `get_history`, and every list from the live-app, page,
  project and quality tools (`get_errors`, `list_api_routes`, `list_actions`,
  `list_islands`, `bundle_report`, `audit_page`, `get_config`, ...) return
  `nextCursor` when more results exist. Text-returning tools (`get_logs`,
  `get_rendered_html`) are bounded by `lines` and `maxBytes` and report
  `truncated` or `nextOffset`.
- **Subscriptions**: clients can `resources/subscribe` and
  `resources/unsubscribe`; write transactions emit
  `notifications/resources/updated` for manifest, routes, graph, and content.
- **Version negotiation**: `initialize` handles `protocolVersion` negotiation,
  supporting `2024-11-05` through `2025-11-25`.
- **Cancellation**: `notifications/cancelled` aborts a pending request; the
  server responds with error code `-32800` (Request Cancelled) when appropriate.
  Long-running `build`/`check` calls observe the cancellation context.
- **Instructions**: `initialize` includes agent-facing instructions that
  describe the tool set, the dry-run convention, and Krate's pages/layout/
  content conventions.
- **Notifications** (`notifications/initialized`, `exit`) never produce a
  response.

## Example session: debugging a running app

1. `dev_server_status` - nothing running, so `dev_server { action: "start" }`
   (dry-run), then again with `"apply": true`.
2. `get_errors` - `KR7115` in `src/pages/pricing.tsx:12:1`; `explain_diagnostic
   { code: "KR7115" }` explains the decorator rule.
3. `edit_file` the fix (dry-run diff, then `apply`), wait for the rebuild, and
   `get_errors` again: `buildOk: true`.
4. `why_hydrated { route: "/pricing" }` shows the page still ships 48 KB of
   JavaScript because of one `useState`; move it into a small client component
   and `explain_page` shows the new total.

## Example session: adding a page

An agent asked to add a pricing page can:

1. `list_routes` - see existing routes and their source files.
2. `read_page` on a similar page (`format: "source"`) - learn component and
   styling conventions.
3. `create_page { route: "/pricing" }` - receive a diff.
4. You approve, then the agent calls it again with `"apply": true`.
5. `build` - surface diagnostics in the same turn.
6. `check` - fix any accessibility or SEO findings with `edit_file`.

Because the server holds the compiler in-process, each step is fast and the
agent never has to invoke a shell.

For how the server is implemented (no SDK dependency, stdout discipline, path
anchoring), see
[Contributing -> Architecture](/docs/contributing/architecture/#mcp-server).