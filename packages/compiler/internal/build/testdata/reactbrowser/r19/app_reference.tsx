import { Suspense, use, useActionState, useOptimistic, useRef, useState } from "react";
import { useFormStatus } from "react-dom";
import { delay, log } from "../lib/log";

type Todo = { id: number; title: string; pending?: boolean };

const initialTodos = delay(150, [
  { id: 1, title: "read the React 19 notes" },
  { id: 2, title: "port the settings form" },
]);

function SubmitButton() {
  const { pending } = useFormStatus();
  return (
    <button id="add" type="submit" disabled={pending}>
      {pending ? "adding" : "add"}
    </button>
  );
}

function TodoList(props: { initial: Promise<Todo[]> }) {
  const loaded = use(props.initial);
  const [todos, setTodos] = useState<Todo[]>(loaded);
  const [optimistic, addOptimistic] = useOptimistic(todos, (state: Todo[], title: string) => [
    ...state,
    { id: -1, title, pending: true },
  ]);
  const nextId = useRef(3);

  const [error, submit, isPending] = useActionState(async (_prev: string, formData: FormData) => {
    const title = String(formData.get("title")).trim();
    if (!title) return "title is required";
    addOptimistic(title);
    // Long enough that the snapshot 60ms after the click always lands while
    // the action is pending: with 200ms a slow step (Playwright actionability
    // waits on a loaded machine) let React finish first, so the expected DOM
    // depended on timing.
    await delay(800, null);
    log("saved", title);
    setTodos((t) => [...t, { id: nextId.current++, title }]);
    return "";
  }, "");

  return (
    <section>
      <ul id="todos">
        {optimistic.map((t) => (
          <li key={t.id} className={t.pending ? "pending" : ""}>
            {t.title}
          </li>
        ))}
      </ul>
      <form action={submit}>
        <input id="title" name="title" placeholder="new todo" />
        <SubmitButton />
      </form>
      {error && <p id="error">{error}</p>}
      <p id="busy">{isPending ? "saving" : "ready"}</p>
    </section>
  );
}

// React 19 reference app: use(promise) under Suspense for the initial data,
// useActionState for validation and pending state, useOptimistic for the
// pending row, useFormStatus in the submit button, form auto-reset.
export default function AppReference() {
  return (
    <main>
      <h1>Todos</h1>
      <Suspense fallback={<p id="loading">loading todos</p>}>
        <TodoList initial={initialTodos} />
      </Suspense>
    </main>
  );
}
