---
title: Server Actions
order: 7
description: Call server code from forms and event handlers with a file-level "use server" directive, React 19 action hooks, progressive enhancement, and no client-side action code.
---

# Server Actions

A module whose first statement is `"use server"` is a **server-action
module**. Every exported async function becomes a POST endpoint. Import those
functions into a page or component and use them as a form action, a button
`formAction`, or call them directly.

```ts
// src/actions/feedback.ts
"use server";

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
  `action.bind(null, id)` returns another proxy with `id` as its first argument.
- `<form action={saveFeedback}>` is rendered as:

  ```html
  <form action="/__krate/action/a..." method="post" data-krate-action>
  ```

- **Without JavaScript**, the browser submits the form to the endpoint. The
  action runs, then the server responds with `303 See Other` back to the form
  page (or to the action's redirect).
- **With JavaScript**, the runtime intercepts the submit and runs the action
  like React 19: inside an async transition, with
  [`useFormStatus`](#useformstatus) pending state, **without reloading the
  page**. After the action succeeds the form's uncontrolled fields are reset
  (controlled fields keep their state). Add `data-krate-reload` to a form to
  get the previous reload-after-submit behavior instead.
- Call the imported action directly to receive its return value:

  ```ts
  const result = await saveFeedback({ text: "from a click handler" });
  ```

- SSR pages use the same proxy. The action source is excluded from both the
  client bundle and the SSR page bundle, so server-only imports stay on the
  server.

## React 19 actions

React source works unchanged: `action` and `formAction` accept server actions
or client functions, and the React 19 hooks are compiled to Krate signals.

### Client function actions

```tsx
export default function Search() {
  const [query, setQuery] = useState("");
  async function search(formData: FormData) {
    setQuery(String(formData.get("q")));
  }
  return (
    <form action={search}>
      <input name="q" />
      <button>Search</button>
      <button formAction={async () => setQuery("")}>Clear</button>
    </form>
  );
}
```

The function receives the form's `FormData` (including the submitter's
name/value). A submit button's `formAction` wins over the form's `action`. If an
`onSubmit` handler calls `preventDefault()`, the action does not run. Client
function actions have no native fallback; use a server action for forms that
must work without JavaScript.

### useActionState

```tsx
// src/actions/todos.ts
"use server";
export async function addTodo(prev: string[], formData: FormData) {
  return [...prev, String(formData.get("title"))];
}
```

```tsx
import { useActionState } from "react";
import { addTodo } from "../actions/todos";

export default function Todos() {
  const [todos, formAction, isPending] = useActionState(addTodo, [], "/todos/");
  return (
    <form action={formAction}>
      <input name="title" />
      <button disabled={isPending}>Add</button>
      <p>{todos.length} todos</p>
    </form>
  );
}
```

- `formAction` works as `<form action>`, as a button `formAction`, or called
  directly (`formAction(payload)`, also inside `startTransition`).
- The action receives `(previousState, payload)`. Calls are queued and run one
  at a time in order; each gets the state the previous call returned.
- `isPending` is true from the first dispatch until the queue is empty.
- For a server action, the previous state is sent to the server as the first
  argument and the returned state comes back in the response, so it must be
  JSON-serializable.
- If an action throws, queued calls are cancelled and the error is reported
  (see [Errors](#errors)).
- **Without JavaScript**, the form posts to the server action with the initial
  state (evaluated at build time) as the previous state, and the server
  redirects to the optional `permalink` (third argument, a same-origin path) or
  back to the page. The returned state is not shown in that case. A non-literal
  initial state is sent as `null` and reported as
  [`KR7003`](#diagnostics).

### useFormStatus

```tsx
import { useFormStatus } from "react-dom";

function SubmitButton() {
  const { pending, data, method, action } = useFormStatus();
  return <button disabled={pending}>{pending ? "Saving..." : "Save"}</button>;
}
```

`useFormStatus()` reports the form **containing** the component while an action
submitted from that form is pending: `pending`, the submitted `data`
(`FormData`), `method`, and the `action`. Other forms are unaffected, and a
component reading the form it renders itself sees an idle status, as in React.
Krate connects the status through the element the component returns, so the
component must return a DOM element ([`KR7002`](#diagnostics) otherwise).

### useOptimistic

```tsx
const [messages, setMessages] = useState<string[]>([]);
const [shown, addShown] = useOptimistic(messages, (list, text: string) => [...list, text + " (sending)"]);

async function send(formData: FormData) {
  addShown(String(formData.get("message")));
  const saved = await sendMessage(formData);
  setMessages((list) => [...list, saved.text]);
}
```

Optimistic updates apply on top of the latest base state while any action or
async transition is pending and are dropped when it settles, so the value
reverts to the base state (which usually includes the real result by then).
Several updates compose in order; without an update function the update
replaces the value. An update made outside any action is dropped on the next
microtask.

### requestFormReset

`requestFormReset(form)` from `react-dom` resets the form's uncontrolled fields
when the action pending on it settles, or immediately when none is pending.

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

## Request input and context

Actions receive exactly what the caller passed:

- Form submissions pass a `FormData` object (`formData.get`, `getAll`,
  `entries`, and file values from the Node runtime).
- Direct calls pass the arguments positionally:
  `saveFeedback({ text })` calls `saveFeedback({ text })` on the server.
- Bound arguments (`action.bind(null, id)`) and the `useActionState` previous
  state come first, the `FormData` last.

Inside an action, `@krate/runtime/server` exposes the request:

```ts
"use server";
import { cookies, headers, redirect } from "@krate/runtime/server";

export async function signOut() {
  const session = cookies().get("session");
  const agent = headers().get("user-agent");
  cookies().delete("session");
  redirect("/");
}
```

- `headers()` returns the request `Headers`.
- `cookies()` has `get`, `getAll`, `has`, `set(name, value, options)` and
  `delete(name)`. `set` defaults to `HttpOnly; Path=/; SameSite=Lax`.
- `getActionContext()` returns `{ actionId, url, method, headers, cookies }`, or
  `undefined` outside an action. `headers()`/`cookies()` throw outside one.
- `redirect(url)` ends the action and navigates the client (`303` for native
  posts). Returning `{ redirect: "/path" }` does the same.
- `revalidatePath(path)` / `revalidateTag(tag)` (from `@krate/runtime/cache`)
  purge ISR pages and cached data; they are applied before the action responds.
  See [Caching and Revalidation](/docs/guides/caching-and-revalidation/).

## Wire format

The client and the sidecar exchange JSON envelopes on
`POST /__krate/action/<id>`:

| | Body |
| --- | --- |
| Call, JSON args | `content-type: application/json`, `{"args":[...]}` |
| Call with a `FormData` | multipart form fields; `__krate_args` holds a JSON array of the leading arguments |
| Native form post | the form body; `__krate_args` and `__krate_return` ride in the action URL query |
| Success | `200 {"ok":true,"result":R,"redirect":"/path"?,"revalidated":true?,"revalidation":{"paths":[...],"tags":[...]}?}` |
| Failure | `4xx/5xx {"ok":false,"error":{"message":M,"digest":D}}` |

Enhanced requests send `x-krate-action: fetch`; native posts get a `303`.
When the action revalidated, the client dispatches a `krate:revalidate` window
event with `detail: { paths, tags }`.

## Errors

A failing action responds with `{"ok":false,"error":{...}}`. In production the
message is generic and `digest` is an id logged next to the full error on the
server; in `krate dev` the real message is sent. Direct calls reject with a
`ServerActionError` (`message`, `digest`, `status`).

Errors from form actions and `useActionState` are reported to an error
boundary hook (`globalThis.__krateActionError(error, element)`, return `true`
to claim it); otherwise a bubbling, cancelable `krate:action-error` event is
dispatched on the form (or `document`) and the error is logged.

## Security

- The endpoint only executes ids present in the build manifest.
- POSTs from cross-site browser contexts are rejected (`Origin` and
  `Sec-Fetch-Site`). Non-browser clients and same-origin form posts are allowed.
- Request bodies use the API body limit (`server.maxBodySize`); larger requests
  get `413`.
- Action responses are `Cache-Control: no-store`, and form return paths
  (permalinks, `X-Krate-Return`, `Referer`) are restricted to same-origin paths.
- Server error details are not sent to clients in production.
- **Actions are public endpoints.** Treat an action like an API route and
  authorize inside it. The action id is an identifier, not a secret, and every
  argument (including a `useActionState` previous state) is client input.

## Diagnostics

| Code | Meaning |
| --- | --- |
| `KR7001` | Inline `"use server"` inside a function body. Move the function into a `"use server"` module and import it. |
| `KR7002` | `useFormStatus()` in a component that does not return a DOM element; its status stays idle. |
| `KR7003` | `useActionState` initial state is not known at build time; the no-JS fallback sends `null`. |
| `KR7004` | A `"use server"` module exports a non-function. |
| `KR7005` | A `"use server"` module re-exports another module. |
| `KR7006` | An import names an export the `"use server"` module does not have. |

## Limits

- Only file-level `"use server"` is supported. Inline `"use server"` functions
  inside components are a build error (`KR7001`) because Krate does not capture
  closures for the server.
- Every export in an action file must be a function. Re-exports are rejected
  with a build error.
- A `useActionState` dispatch passed to a child component as a prop works with
  JavaScript but renders no native fallback. Render the `<form>` in the
  component that calls `useActionState` when it must work without JavaScript.
- Action results update client state (signals, `useActionState`), not
  server-rendered markup: there is no RSC-style page refresh. Navigate or use a
  redirect to show re-rendered server output.

## Related

- [React compatibility](/docs/guides/react-compatibility/)
- [Compatibility matrix](/docs/guides/compatibility/)
- [Migrating from Next.js](/docs/guides/migrating-from-nextjs/)
