---
title: Safe Fixes
description: Apply conservative codemods for Krate diagnostics without guessing.
order: 12
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

String-valued inline handlers such as `onclick="native()"` are HTML event
attributes, not React handlers, and are left unchanged. Component props,
custom-element props, and unknown event names are also left alone.

## Manual migration

Most diagnostics need a human decision. The diagnostic hint names the intended
fix:

| Diagnostic | Manual path |
| --- | --- |
| `KR5001` | Rename the prop to a React DOM event, or attach a custom listener with `ref.current.addEventListener` in an effect. |
| `KR5003` | Replace a string ref with `useRef`/`createRef` or a callback ref. |
| `KR6001` | Give each dynamic list row a stable `key`. |
| `KR6002` | Make duplicate keys unique; do not rely on index fallback. |
| `KR7001` | Move an inline `"use server"` function to a `"use server"` module. |
| `KR8007` | Move the Krate component out of the Preact island subtree, or replace it with a React/Preact-compatible module. |

`krate check` prints the same codes and hints against the built output. See the
[diagnostic reference](/docs/reference/diagnostics/) for every code.
