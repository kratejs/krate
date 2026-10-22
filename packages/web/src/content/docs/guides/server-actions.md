---
title: Server Actions
order: 7
description: Call server code from forms and event handlers with a file-level "use server" directive, progressive enhancement, and no client-side action code.
---

# Server Actions

A module whose first statement is `"use server"` is a **server-action
module**. Every exported async function becomes a POST endpoint. Import those
functions into a page or component and use them as a form action or call them
directly.

```ts
// src/actions/feedback.ts
export async function saveFeedback(formData: FormData) {
  const text = String(formData.get("text") || "").trim();
  // Write to a database, call another service, send mail, ...
  return { text, length: text.length };
}
```

```tsx
import { saveFeedback } from "../actions/feedback";

export default function FeedbackPage() {
  return (
    <form action={saveFeedback}>
      <input name="text" />
      <button type="submit">Send</button>
    </form>
  );
}
```

## How it works

- The build discovers `"use server"` modules, hashes each exported action to a
  stable id, and compiles the module to ESM under `.krate/actions/`. Action
  bundles contain server code and are never written to `dist/`.
- Imports in pages are replaced with small client proxies. A proxy has an
  `actionUrl` and `toString()` that returns it, so SSR renders the endpoint --
  including through a component prop -- without executing the action.
- `<form action={saveFeedback}>` is rendered as:

  ```html
  <form action="/__krate/action/a..." method="post" data-krate-action>
  ```

- Without JavaScript, the browser submits the form to the endpoint. The action
  runs, then the server responds with `303 See Other` back to the form page.
- With JavaScript, the shared runtime intercepts the submit, posts `FormData`,
  and reloads on success. Call the imported action directly to receive its
  return value:

  ```ts
  const result = await saveFeedback({ text: "from a click handler" });
  ```

- SSR pages use the same proxy. The action source is excluded from both the
  client bundle and the SSR page bundle, so server-only imports stay on the
  server.

## Execution runtime

Actions run in the Node-family sidecar (`node`, `bun`, or `deno`). They are not
executed in the embedded QuickJS runtime yet, so `krate serve` starts a Node
sidecar when an action manifest exists even if `ssr.apiRuntime` is `quickjs`.
`krate build` still needs no Node. A static-only deployment must provide a
server endpoint for `/__krate/action/*` or it cannot use actions. Restart
`krate dev` after adding the first action file so the sidecar and endpoint are
started.

Each action is bundled with its dependencies (third-party packages stay
external and resolve from the project's `node_modules`). The sidecar loads each
action module once in production and re-imports it after a rebuild in dev.

## Request input

Actions receive exactly what the caller passed:

- Form submissions pass a `FormData` object (`formData.get`, `getAll`,
  `entries`, and file values from the Node runtime).
- Direct calls pass the arguments positionally:
  `saveFeedback({ text })` calls `saveFeedback({ text })` on the server.
- A form can also submit an action that accepts a plain object by reading the
  `FormData` itself; Krate does not coerce form fields into an object.

Returning `{ redirect: "/path" }` makes the enhanced client navigate there
instead of reloading. Other return values are ignored by form submissions and
returned to direct callers.

## Security

- The endpoint only executes ids present in the build manifest.
- POSTs from cross-site browser contexts are rejected (`Origin` and
  `Sec-Fetch-Site`). Non-browser clients and same-origin form posts are allowed.
- Request bodies use the API body limit (`server.maxBodySize`).
- Action responses are `Cache-Control: no-store`, and form return paths are
  restricted to same-origin paths.
- **Actions are public endpoints.** Treat an action like an API route and
  authorize inside it. The action id is an identifier, not a secret.

## Limits

- Only file-level `"use server"` is supported. Inline function directives are
  not extracted.
- Every export in an action file must be a function. Re-exports are rejected
  with a build error.
- Actions do not yet receive a request context (`cookies()`, `headers()`, or a
  Request object). Read configuration from `process.env`.
- The action result is not used to re-render the page or apply optimistic
  patches. React 19 form-action hooks are covered separately by the
  [React compatibility roadmap](/roadmap/).

## Related

- [React compatibility](/docs/guides/react-compatibility/)
- [Compatibility matrix](/docs/guides/compatibility/)
- [Migrating from Next.js](/docs/guides/migrating-from-nextjs/)
