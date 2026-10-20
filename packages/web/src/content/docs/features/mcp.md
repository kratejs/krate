---
title: MCP Server (Agent-Native)
order: 15
description: Expose the Krate compiler to AI agents over the Model Context Protocol with `krate mcp`.
sidebar:
  label: MCP Server
  order: 14
---

# MCP Server (Agent-Native)

`krate mcp` starts an [MCP](https://modelcontextprotocol.io) (Model Context
Protocol) server that exposes the Krate compiler to AI agents. The agent talks
to the real compiler — routes, AST, diagnostics, content graph — instead of
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

- Cursor — `.cursor/mcp.json`
- VS Code — `.vscode/mcp.json` (under `"servers"`)
- opencode — `opencode.json`

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

| Tool | Description |
|------|-------------|
| `list_routes` | Every route with source file, render mode, and dynamic params |
| `read_page` | A page's source, kind-tagged AST document, or rendered HTML (`format` selectable) |
| `read_content` | Content-collection entries: list a collection or read one entry (raw file + frontmatter + body) |
| `search_docs` | Search Krate's built-in framework docs (ranked, via the docfind engine); returns slugs, excerpts, and optional full text |
| `build` | Build the site; returns diagnostics and a per-route summary |
| `check` | Run the quality gates (a11y/SEO/perf); builds first when needed |
| `run_task` | Run an allowlisted task: build, check, test, or typecheck |
| `explain` | Explain a route from the compiled site graph: mode, source, dependencies, dependents, and emitted output files |
| `search_project` | Search project source/content with literal or regex queries, globs, context, and limits |
| `read_files` | Read several project files in one call with byte caps |
| `get_diagnostics` | Run build/check and return structured diagnostics with file, range, code, severity, and hint |
| `find_references` | Find source references to a symbol, with context and limits |
| `impact` | Show routes affected by a file, or a route's dependencies/dependents |
| `list_changes` / `get_history` | Recent MCP transactions for audit and undo |
| `create_page` | Create a page from a template; returns a diff (dry-run by default) |
| `edit_file` | Edit any project file's source directly (full replace, find+replace, expected hash/mtime); returns a diff |
| `apply_patch` | Apply a unified diff to one or many files atomically; returns a diff |
| `batch_edit` | Apply structured create/replace/delete/move operations atomically |
| `undo` | Undo the most recent transaction (or one by ID) |
| `format_file` | Format a TypeScript/JavaScript source file; returns a diff |
| `apply_fix` | Apply an explicit find/replace repair to a diagnostic finding |
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

### Read tools

`list_routes`, `read_page`, `read_content`, `search_docs`, `build`, and `check`
never write source files.

`read_page` lets the agent choose what it needs with `format` (default `source`):

- `source` — the raw file text, so the agent can read and edit the actual page.
- `ast` — the **kind-tagged AST document** (the same format JS plugins
  receive), with `lossyTypes` reporting TypeScript constructs the parser drops.
- `html` — the rendered output when the site has been built.
- `all` — source, AST, and rendered HTML together.

`read_content` lists a collection's entries (slug, project path, frontmatter)
when `collection` alone is given, or returns a single entry's raw file, parsed
frontmatter, and markdown body (plus rendered HTML for `.md`) when `slug` is
added.

`search_docs` searches **Krate's own framework documentation** — the content
that powers the docs site — which is embedded into the compiler at build time,
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
includes the build output and whether the build succeeded.

`find_references` searches for a symbol with word boundaries. `impact` uses the
compiled site graph to report dependent routes for a file or dependencies and
dependents for a route.

`list_changes` and `get_history` expose recent MCP transactions, including file
lists, timestamps, and undone state.

### File tools

The file tools let the agent discover and manage arbitrary project files
(components, styles, assets), not just pages:

- **`list_files`** lists project-relative paths (skipping `node_modules`,
  `.git`, `dist`, …). Filter with `glob` (`src/components/**/*.tsx`), scope with
  `dir`, and set `includeDenied: true` to see policy-denied paths.
- **`read_file`** returns a file's text with a language hint (or a `binary`
  marker). Sensitive paths are refused (see the policy below).
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
`edit_content`, and `apply_fix` **default to a dry-run** that returns a unified diff. Pass
`"apply": true` to write. This gives the agent (and you) an approval step before
anything changes on disk.

All writes go through one transaction layer: edits are validated before commit,
multi-file operations are rolled back on failure, and applied transactions can
be reversed with `undo`. `apply_patch` accepts unified diffs; `batch_edit`
accepts structured operations; both support per-file `expectedSha256` checks.

### Permissions (`.krate/mcp.json`)

By default the agent may read and write any project file - pages, CSS,
components, API routes, content, docs, and config - **except** a small set
of sensitive paths — environment files (`.env*`), private keys (`*.pem`,
`*.key`, `id_rsa`, …), `.git/`, npm/pypi/netrc credential files, and the policy
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
  "allowTasks": ["build", "check", "test", "typecheck"]
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
- **`allowTasks`** gates `run_task`. Generated/output trees (`node_modules`,
  `dist`, `.krate`, `build`) are never writable.

Every write also rejects absolute paths and traversal outside the project root,
resolving symlinks so a symlinked directory cannot redirect a write elsewhere.

- **`create_page`** scaffolds a page from a template — `static` (default),
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
## Prompts

Prompts package recurring workflows so agents reach for the right tools:

- `add-page` — create a page the Krate way: match house style first, then
  `create_page` with the chosen template, and confirm via `build`/`check`.
- `publish-content` — author a new entry into a typed content collection via
  `create_content` (schema-validated) and wire it into a page.
- `fix-checks` - run the quality gates, then fix the worst findings with
  `apply_fix` or `apply_patch`.
- `repair-diagnostics` - diagnose build/check failures, apply minimal repairs,
  and verify with `get_diagnostics` and `run_task`.
- `refactor` - map references and impact, propose a multi-file diff, then apply
  it and verify.
- `audit-dependencies` - report unused or risky dependencies without changing
  files.
- `scaffold-component` - create a component that matches nearby house style.
- `explore` — summarize the project from routes, config, and outstanding checks.

## Protocol

- **Structured results**: tools that return JSON also include
  `structuredContent`, so clients can consume diagnostics and listings without
  parsing the text block.
- **HTTP transport**: `krate mcp --http` exposes Streamable HTTP at `/mcp`
  (POST requests, GET SSE). Authentication is bearer-token based.
- **Progress**: requests with `_meta.progressToken` receive
  `notifications/progress` during build, check, diagnostics, and search.
- **Pagination**: `list_files`, `search_project`, `list_changes`, and
  `get_history` return `nextCursor` when more results exist.
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

## Example session

An agent asked to add a pricing page can:

1. `list_routes` — see existing routes and their source files.
2. `read_page` on a similar page (`format: "source"`) — learn component and
   styling conventions.
3. `create_page { route: "/pricing" }` — receive a diff.
4. You approve, then the agent calls it again with `"apply": true`.
5. `build` — surface diagnostics in the same turn.
6. `check` — fix any accessibility or SEO findings with `edit_file`.

Because the server holds the compiler in-process, each step is fast and the
agent never has to invoke a shell.

For how the server is implemented (no SDK dependency, stdout discipline, path
anchoring), see
[Contributing → Architecture](/docs/contributing/architecture/#mcp-server).