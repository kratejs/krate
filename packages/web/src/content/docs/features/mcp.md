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

## Tools

| Tool | Description |
|------|-------------|
| `list_routes` | Every route with source file, render mode, and dynamic params |
| `read_page` | A page's source, kind-tagged AST document, or rendered HTML (`format` selectable) |
| `read_content` | Content-collection entries: list a collection or read one entry (raw file + frontmatter + body) |
| `search_docs` | Search Krate's built-in framework docs (ranked, via the docfind engine); returns slugs, excerpts, and optional full text |
| `build` | Build the site; returns diagnostics and a per-route summary |
| `check` | Run the quality gates (a11y/SEO/perf); builds first when needed |
| `explain` | Explain a route from the compiled site graph: mode, source, dependencies, dependents, and emitted output files |
| `create_page` | Create a page from a template; returns a diff (dry-run by default) |
| `edit_ast` | Replace a page's AST with an edited document; returns a diff |
| `edit_page` | Edit any project file's source directly (full replace or find+replace); returns a diff |
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

### File tools

The file tools let the agent discover and manage arbitrary project files
(components, styles, assets), not just pages:

- **`list_files`** lists project-relative paths (skipping `node_modules`,
  `.git`, `dist`, …). Filter with `glob` (`src/components/**/*.tsx`), scope with
  `dir`, and set `includeDenied: true` to see policy-denied paths.
- **`read_file`** returns a file's text with a language hint (or a `binary`
  marker). Sensitive paths are refused (see the policy below).
- **`create_file`**, **`delete_file`**, and **`move_file`** round out the
  lifecycle. `create_file` refuses to overwrite (use `edit_page` to modify) and
  parse-gates editable source; all three default to a dry-run preview.
- **`create_component`** scaffolds a component under
  `src/components/<kebab-name>/`: `kind` selects `client` (imports the runtime),
  `static`, or `server`; `withCss: true` adds a companion `.css` file. Continue
  with `edit_page` for changes.

### Write tools

`create_page`, `edit_ast`, `edit_page`, `create_file`, `delete_file`,
`move_file`, `create_component`, `create_content`, and `edit_content`
**default to a dry-run** that returns a unified diff. Pass `"apply": true` to
write. This gives the agent (and you) an approval step before anything changes
on disk.

### Permissions (`.krate/mcp.json`)

By default the agent may read and write any project file **except** a small set
of sensitive paths — environment files (`.env*`), private keys (`*.pem`,
`*.key`, `id_rsa`, …), `.git/`, npm/pypi/netrc credential files, and the policy
file itself. Those require an explicit `allow` entry. A committed policy at
`.krate/mcp.json` customises this:

```json
{
  "allow": ["config/public.env"],
  "deny": ["src/generated/**"],
  "readOnly": ["krate.config.ts"]
}
```

- **`allow`** overrides the default-deny list (e.g. permit one checked-in
  `.env.example`).
- **`deny`** always wins, even over `allow`.
- **`readOnly`** permits reading but never writing.

Every write also rejects absolute paths and traversal outside the project root,
resolving symlinks so a symlinked directory cannot redirect a write elsewhere.

- **`create_page`** scaffolds a page from a template — `static` (default),
  `content-list`, `detail`, and `blank` (raw source override). For
  content-backed templates you can pass `collection` and a `contentEntry`
  object so the matching markdown entry is authored alongside the page, and
  `withLayout: true` creates a missing `_layout.tsx`. Applying a page also
  regenerates the generated route/content types.
- **`edit_ast`** accepts a kind-tagged AST document — typically one read with
  `read_page`, modified, and sent back. The compiler validates it by decoding
  the document and re-parsing the printed source before writing. If the page
  uses TypeScript constructs the parser drops (interfaces, type aliases,
  annotations), `edit_ast` **refuses** rather than silently stripping them.
- **`edit_page`** edits the actual file — before reaching for a patch. It takes
  a `route` (a page route like `/about`, or a project-relative path like
  `src/pages/about.tsx`, `src/styles/main.css`, or `src/content/blog/hello.md`)
  plus one of two modes:
  - `content` — replace the entire file (creates the file when it does not
    exist).
  - `find` + `replace` — a targeted in-place edit. `find` must match exactly
    once, or pass `replaceAll: true` to replace every occurrence; a missing or
    ambiguous match returns a helpful error instead of guessing.

  Editable source (`.ts`, `.tsx`, `.js`, `.jsx`) is re-parsed after the edit:
  anything that would not parse is never written (dry-run or apply). Existing
  line endings (LF/CRLF) are preserved, and paths are anchored to the project
  root with traversal rejected. A diff is returned in either mode.

- **`create_content`** writes a new entry to a content collection. `data` is
  validated against the collection schema (required fields, type checks) before
  any file is created; the entry is refused if it already exists or the slug
  would escape the collection directory. Plugin-contributed collections (like
  the docs collection) are supported alongside configured `content:` entries.

- **`edit_content`** edits an existing content entry. Three modes: full-file
  replace via `content`, frontmatter rewrite via `data` (body preserved unless
  `body` is given), or a targeted find+replace. The result is re-parsed and
  validated against the collection schema before any write, so a broken
  frontmatter change is never written to disk.

## Resources and resource templates

Resources provide pull-based context an agent can attach automatically.

| URI | Contents |
|-----|----------|
| `krate://routes` | Every route in the project (JSON) |
| `krate://page/{route}` | A single page by route, e.g. `krate://page/about` |
| `krate://docs/{slug}` | A Krate framework documentation page as raw markdown, e.g. `krate://docs/features/mcp` |
| `krate://content` | Effective content collections (configured `content:` plus plugin-contributed, e.g. docs) and their entries, with each collection's schema fields |
| `krate://manifest` | The built site manifest (empty when unbuilt) |
| `krate://config` | The resolved Krate config (relative paths, no env values) |
| `krate://graph` | The compiled site graph: routes with mode, dependencies, dependents, and emitted output files (JSON) |

`krate://page/{route}` and `krate://docs/{slug}` are **resource templates**,
advertised through `resources/templates/list`. The server also provides
**argument completions**
(`completions/complete`) for routes, page templates, and content collections
(and collection slugs for the content tools), so clients can offer values while
the agent is filling in a tool call or prompt argument.

## Prompts

Prompts package recurring workflows so agents reach for the right tools:

- `add-page` — create a page the Krate way: match house style first, then
  `create_page` with the chosen template, and confirm via `build`/`check`.
- `publish-content` — author a new entry into a typed content collection via
  `create_content` (schema-validated) and wire it into a page.
- `fix-checks` — run the quality gates, then fix the worst findings with
  `edit_page`.
- `explore` — summarize the project from routes, config, and outstanding checks.

## Protocol

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
6. `check` — fix any accessibility or SEO findings with `edit_page`.

Because the server holds the compiler in-process, each step is fast and the
agent never has to invoke a shell.

For how the server is implemented (no SDK dependency, stdout discipline, path
anchoring), see
[Contributing → Architecture](/docs/contributing/architecture/#mcp-server).
