---
title: Server Actions
order: 9
description: Call server code from forms and event handlers with a file-level "use server" directive, React 19 action hooks, and progressive enhancement.
---

# Server Actions

A module whose first statement is `"use server"` is a **server-action
module**. Every export (an `async function`, or a function built by a wrapper
such as `createAction(...)`) becomes a POST endpoint. Import those functions
into a page or component and use them as a form action, a button `formAction`,
or call them directly. A function with its own `"use server"` directive inside a
server component is an [inline action](#inline-actions).

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
The same form works inside an async server component: the action is rendered
as a native POST target and the page still needs no client React runtime.

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
  back to the page. The state the action returned travels across that redirect
  in a short-lived (60 s), single-use `krate_flash` cookie (HMAC-signed, so
  another origin cannot plant one; set `KRATE_FLASH_SECRET` to share the key between
  instances, see [Deployment](/docs/guides/deployment/#flash-cookie-secret)), and a
  `useActionState` for the same action that mounts on the next page starts from
  it, so validation errors appear as soon as the page hydrates.
  Pages are pre-rendered HTML, so a browser that never runs the script would
  still see the build-time initial state. To give it feedback, the server
  renders the strings of the returned state (a string, or the string values of
  an object such as `{ error: "..." }` or `{ errors: { name: "..." } }`) into
  `<div class="krate-flash" role="alert">` at the top of the form that posted,
  and clears the cookie. This is a notice built from the returned text, not a
  re-render of your component: markup that depends on the state (conditional
  elements, lists) stays as built until the script runs, and when it does the
  form starts from the same state and removes the notice. Style
  `.krate-flash` as you like. A non-literal
  initial state is sent as `null` and reported as
  [`KR7003`](#diagnostics). The state must stay under about 3 KB once encoded;
  larger results are dropped with a server warning.

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

## Wrapped actions

Every export of a `"use server"` module is an action, and an export can be any
expression whose value is a function, including the result of a wrapper that
adds validation, authorization or rate limits:

```ts
// src/actions/projects.ts
"use server";
import { createAction } from "../lib/create-action";

export const renameProject = createAction(
  { input: schema, rateLimit: { max: 10, window: "1m" } },
  async ({ input, actor }) => {
    // ...
  },
);

export default createAction({ input: other }, async ({ input }) => { /* ... */ });

const archive = createAction({}, async () => { /* ... */ });
export { archive as archiveProject };
export { sendInvite } from "./invites";
```

- The build cannot see through a wrapper call, so it checks the value when the
  action module loads on the server. An export that is not a function is
  reported in the server log by name (`export "renameProject" is not a function
  (got an object)`), and calling it fails; the module's other actions keep
  working.
- An export that can never be a function (a literal, object, array, template
  string, class or JSX element, also through `export { x }`) is rejected at
  build time with [`KR7004`](#diagnostics).
- `export { name } from "./other"` and `export { default as name } from "./other"`
  are allowed. The re-exported function gets an endpoint of its own in the
  re-exporting module, so re-export only what should be public.
  `export *` and `export * as ns` are rejected ([`KR7005`](#diagnostics)).

## Inline actions

A function that starts with a `"use server"` directive inside a component is an
inline action. Krate hoists it at build time into a generated server-action
module, so the code never reaches the browser:

```tsx
import { signIn, signOut } from "../auth";

export default async function Header({ params }) {
  const { projectId } = await params;
  const session = await getSession();
  async function leaveProject(formData: FormData) {
    "use server";
    await removeMember(projectId, session.userId, String(formData.get("reason")));
  }
  return (
    <>
      <form action={leaveProject}>
        <input name="reason" />
        <button>Leave</button>
      </form>
      <form
        action={async () => {
          "use server";
          await signOut({ redirectTo: "/" });
        }}
      >
        <button>Sign out</button>
      </form>
    </>
  );
}
```

Use it as `<form action>` or a button `formAction`, as with an imported action;
it works with and without JavaScript, passes the `FormData`, and shows up in the
action manifest like any other action.

Values the function closes over (`projectId` and `session` above) are the
component's props and locals. They do not travel to the browser in the clear:
when the component renders, the server seals them into a token (AES-256-GCM,
authenticated against the action id) that the form submits back, and the action
runs with the original values. A token that was altered, or that belongs to a
different action, is rejected with `400`.

- Capturing values needs an **async server component** (the values are sealed
  on each request). In a synchronous component an inline action can only close
  over nothing, and the build reports [`KR7001`](#diagnostics) otherwise.
- Captured values must be strings, numbers, booleans, `null`, `undefined`,
  `bigint`, `Date`, or plain objects and arrays of those. A function, class
  instance, `Map`, symbol or circular value fails the render naming the
  variable; read it inside the action instead. The token travels in the URL, so
  capture ids and look the rest up in the action (it must stay under about 3.5
  KB).
- Module-level imports and constants the function uses move into the generated
  module with it. The function must not use `this`, and `.bind` on it is not
  supported.
- Declare the function before it is used: the declaration becomes a `const` at
  the same place, so it is not hoisted like a plain function declaration.
- Inline actions are invalid in a module that starts with `"use client"`, as in
  Next.js ([`KR7001`](#diagnostics)).
- The captured values are encrypted with `KRATE_ACTION_SECRET` (at least 16
  characters) when it is set. Otherwise Krate creates a key in
  `.krate/action-key` and copies it into `.krate/actions/manifest.json`, which is
  never written to `dist/`. Set `KRATE_ACTION_SECRET` to the same value on every
  instance and release when you run more than one server, or keep pages open
  across deploys: a page rendered under another key submits a token the new
  server cannot open and is rejected.
- Like any action, an inline action is a public endpoint: the token proves the
  captured values were produced by your server for this action, not who is
  submitting, and it can be replayed. Authorize inside the action.

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
  const session = cookies().get("session")?.value;
  const agent = headers().get("user-agent");
  cookies().delete("session");
  redirect("/");
}
```

- `headers()` returns the request `Headers`.
- `cookies()` has `get` (returns `{ name, value }` or `undefined`, as in Next.js), `getAll`, `has`, `set(name, value, options)` and
  `delete(name)`. `set` defaults to `HttpOnly; Path=/; SameSite=Lax`, plus `Secure` when the request came over
  https or `sameSite` is `"none"`.
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
  argument (including a `useActionState` previous state) is client input. The
  closed-over values of an inline action are the exception: they are sealed on
  the server and cannot be chosen or read by the client.

## Diagnostics

| Code | Meaning |
| --- | --- |
| `KR7001` | An inline `"use server"` function that cannot be hoisted: it is in a `"use client"` module or a class, uses `this`, or closes over values in a component that is not async. |
| `KR7002` | `useFormStatus()` in a component that does not return a DOM element; its status stays idle. |
| `KR7003` | `useActionState` initial state is not known at build time; the no-JS fallback sends `null`. |
| `KR7004` | A `"use server"` module exports something that can never be a function (a literal, object, array or class). |
| `KR7005` | A `"use server"` module uses `export *` or `export * as`. |
| `KR7006` | An import names an export the `"use server"` module does not have. |

## Limits

- An inline action that closes over values must be defined in an async server
  component (see [Inline actions](#inline-actions)); in a synchronous
  component, or in a `"use client"` module, it is a build error (`KR7001`).
- Every export in an action file must be a function when the module loads. Star
  re-exports are rejected with a build error.
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
