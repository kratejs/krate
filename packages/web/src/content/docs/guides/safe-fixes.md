---
title: Safe Fixes
description: Apply conservative codemods for Krate diagnostics without guessing.
order: 16
---

# Safe Fixes

`krate fix` rewrites only patterns whose intent is unambiguous. It is a dry run
by default and never prompts:

```sh
krate fix
krate fix --apply
krate fix path/to/project
```

Each candidate file is parsed before it is changed. A file with a parse error is
skipped, never partially rewritten, and the file is only written after every
candidate in it has been validated. Running `krate fix --apply` twice is a
no-op. Writes are atomic and preserve the file mode.

## What is rewritten

| Diagnostic | Rewrite | Example |
| --- | --- | --- |
| `KR5002` | Lowercase React event name to its camelCase form | `onclick={fn}` -> `onClick={fn}` |
| `KR5002` | Lowercase capture handler | `onclickcapture={fn}` -> `onClickCapture={fn}` |
| `KR5005` | Remove an ignored form default when a controlled prop is present | `value={v} defaultValue="x"` -> `value={v}` |
| `KR5005` | Remove the ignored checked default | `checked={on} defaultChecked` -> `checked={on}` |
| `KR5007` | Rename `onChange` on `contentEditable` to `onInput` | `onChange={edit}` -> `onInput={edit}` |
| `KR7125` | Set `tsconfig.json` `baseUrl` to the project root when it names a directory that every `paths` target repeats | `"baseUrl": "./src"` with `"@/*": ["./src/*"]` -> `"baseUrl": "."` |

The `KR7125` rule edits `tsconfig.json` (comments and layout stay as they are)
and only fires when each `paths` target starts with the `baseUrl` directory,
exists under the project root and does not exist under `baseUrl`. That is the
0.4 project template; see [Upgrading](/docs/guides/upgrading/#tsconfigjson-baseurl-is-strict).
Any other `baseUrl`/`paths` combination is left for you to edit.

String-valued inline handlers such as `onclick="native()"` are HTML event
attributes, not React handlers, and are left unchanged. Component props,
custom-element props, and unknown event names are also left alone. A
`defaultValue` is kept when `value` is `undefined` or `void 0`, and a
`contentEditable` handler is kept when `onInput` already exists.

Automation uses the same plan. From MCP, `get_diagnostics` returns a `codemods`
array with each rewrite's code, path, location, and from/to text. Pass those
codes to `apply_safe_fixes` (`{ "codes": ["KR5005"], "paths": ["src/**"] }`) to
preview or apply only that subset.

## Manual migration

Most diagnostics need a human decision. The diagnostic hint names the intended
fix:

| Diagnostic | Manual path |
| --- | --- |
| `KR5001` | Rename the prop to a React DOM event, or attach a custom listener with `ref.current.addEventListener` in an effect. |
| `KR5003` | Replace a string ref with `useRef`/`createRef` or a callback ref. |
| `KR5004` | Add `onChange`, use `defaultValue`, or mark the field `readOnly`; the right choice depends on intent. |
| `KR5006` | Use `""` (or `false` for checked) instead of `undefined` so the field stays controlled. |
| `KR6001` | Give each dynamic list row a stable `key`. |
| `KR6002` | Make duplicate keys unique; do not rely on index fallback. |
| `KR7001` | Move an inline `"use server"` function out of the `"use client"` module (or make its component async if it captures values), or into a `"use server"` module. |
| `KR8007` | Move the Krate component out of the Preact island subtree, or replace it with a React/Preact-compatible module. |

`krate check` prints the same codes and hints against the built output. See the
[diagnostic reference](/docs/reference/diagnostics/) for every code.
