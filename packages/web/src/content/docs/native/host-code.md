---
title: Host Code
order: 2
description: Mark Go and TypeScript code that runs in the native process of a Krate Native app - markers, precedence, conflicts, inline functions and target restrictions.
---

# Host Code

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.

**Host code** runs in the native process of a desktop app. **UI code** runs in
the webview. UI code imports host code and calls it as async functions; the
compiler generates the plumbing between them.

Host code is:

- Go (`.go` files). Always host code.
- TypeScript, when you mark it with one of the markers below.

## The six markers

Listed from highest to lowest precedence.

| Precedence | Form | Scope |
|-----------|------|-------|
| 1 | `@host` decorator on a function declaration, or the string `"@host"` as the first statement of a function body | That function |
| 2 | `// @host` comment on the first non-comment line of a file | The whole file |
| 3 | File name `*.host.ts` or `*.host.tsx` | The whole file |
| 4 | `hostFunctions` in the config: function names or file paths | A function (name) or a file (path) |
| 5 | `hostDirs` in the config: every module in the directory | The whole file |
| always | `.go` files | The whole file |

```tsx
// src/pages/index.tsx
@host
export async function addTask(title: string) {
  return db.insert({ title });
}

export async function removeTask(id: number) {
  "@host";
  return db.delete(id);
}

export default function Tasks() {
  return <button onClick={() => addTask("Buy milk")}>Add</button>;
}
```

```typescript
// krate.config.ts
export default defineConfig({
  hostFunctions: ["addTask", "src/lib/db.ts"], // a name, or a path
  hostDirs: ["src/host"],
});
```

A name in `hostFunctions` marks top-level functions with that name in any
module. An entry that contains a `/`, or ends in a script extension or `.go`, is
a path. A name that matches no function is reported as a warning.

### Precedence

Precedence decides which source is reported as the reason code is host code, and
lets a function-level marker (1) override the file for that function only. For
example, in a `// @host` file, `@client` on one function keeps that function out
of host code; `@host` in a `.server.tsx` file makes just that function host code.

Each host function is reported with its source and, for functions inside a
module that is not host code (inline functions), its byte range, so a later step
can split it into the host module.

## Conflicts (KR7170)

Sources that name different things for the same code fail the check. The
conflicts reported are:

| Pair | Example |
|------|---------|
| A host file source and a tier file source | `// @host` with `// @server` or `// @runtime`; a `.host.ts` file in `serverDirs`; `hostDirs` overlapping `runtimeDirs`; a `hostFunctions` path listed in `runtimeComponents` |
| A host file source and a tier decorator on the default export (or the only exported component) | `// @host` with `@server export default ...` |
| A `.go` file and a tier source | A Go file in `serverDirs` |
| A decorator and a body string on one function | `@host` with `"@server"` in the body |
| A `hostFunctions` name and a function-level tier marker | `hostFunctions: ["addTask"]` with `@client` on `addTask` |

Remove the marker that disagrees. A function-level marker on a function that is
not the default export or the only exported component overrides the file, so
that is not a conflict.

## Components cannot be host code (KR7171)

`@host` on a function that returns JSX, or on a PascalCase function that renders
JSX, fails. Host code cannot render UI. Use `@server` to render at build time, or
keep the component in the webview and call a host function from it. A component
inside a host file also fails unless it has its own function-level marker such
as `@client`.

## Inline host functions (KR7172)

A function marked `@host` inside a component module is moved to the host
process, so it can only use:

- its own parameters and locals
- module-level imports
- module-level host declarations (and other inline host functions)
- globals

It cannot capture the component's local state, signals or props:

```tsx
export default function Counter() {
  const [count, setCount] = createSignal(0);

  async function save() {
    "@host";
    await db.save(count()); // KR7172: count is declared in Counter
  }
  // Fix: async function save(n: number) { "@host"; await db.save(n); }
  //      ... onClick={() => save(count())}
}
```

Limits of the check: it finds names declared inside an enclosing function
(parameters, `const`/`let`/`var`, nested functions, classes, catch parameters,
loop bindings). It does not check module-level declarations that are not host
code, and it does not follow names through `eval` or dynamic access. The
decorator form applies to top-level declarations; the body-string form also works
on functions nested in a component.

## Native target restrictions

A native build has no server. These are errors in `krate native check` and, once
implemented, in `krate native build`:

| Code | What |
|------|------|
| KR7174 | Request-time rendering: `ssr.streaming`, `ppr`, `export const dynamic = "force-dynamic"`, `export const revalidate` other than `false`, `export const config = { ssr / isr / streaming }` |
| KR7175 | Any route under `src/api` (files starting with `_` are helpers) |
| KR7176 | `middleware.ts` (root or `src/`) |
| KR7177 | `@runtime` components: the decorator, `// @runtime`, `.runtime` files, `runtimeComponents`, `runtimeDirs`, and async components |
| KR7178 | Server actions: `"use server"` modules and inline `"use server"` functions |

Move per-request work into host code and call it from the UI.

## Host markers in a web build (KR7173)

In a normal web build, any host marker is an error: `@host`, `"@host"`,
`// @host`, `.host.ts` files, `hostFunctions` and `hostDirs`. Host code only runs
in a native build, so a marker in a web build would be silently ignored. Remove
the marker, or build with `krate native`.

## `krate native check`

```sh
krate native check [dir]
```

Loads the config, runs placement and the native-target checks, prints the
diagnostics the way `krate check` does, then lists the host modules:

```text
host modules: 2
  src/db.host.ts (file name): addTask, listTasks
  src/pages/index.tsx (inline): removeTask
```

It exits 1 when there are errors.
