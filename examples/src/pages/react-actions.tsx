import { useActionState, useOptimistic, useState } from "react";
import { useFormStatus } from "react-dom";
import { addTodo, saveFeedback, sendMessage } from "../actions/feedback";

function ActionForm(props: { action: unknown; testid: string }) {
  return (
    <form action={props.action} data-testid={props.testid} class="space-y-4 rounded-xl border border-slate-700 bg-slate-900/60 p-5">
      <label class="block space-y-2 text-sm text-slate-300">
        Message
        <input name="text" defaultValue="hello from a form" data-testid="feedback-text" class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100" />
      </label>
      <button type="submit" class="rounded-lg bg-indigo-500 px-4 py-2 font-semibold text-white">
        Save feedback
      </button>
    </form>
  );
}

// Reads the status of the form it is rendered in (React 19 useFormStatus).
function SubmitButton(props: { label: string; testid: string }) {
  const { pending } = useFormStatus();
  return (
    <button type="submit" disabled={pending} data-testid={props.testid} class="rounded-lg bg-emerald-500 px-4 py-2 font-semibold text-white disabled:opacity-50">
      {pending ? "Working..." : props.label}
    </button>
  );
}

export default function ReactActionsPage() {
  const [direct, setDirect] = useState("idle");
  // Server action with state: the previous todos go to the server and the
  // returned list becomes the new state. Without JavaScript the form posts to
  // the action and the server redirects back here (the permalink).
  const [todos, todoAction, todoPending] = useActionState(addTodo, [], "/react-actions/");

  // Optimistic UI around a slow server action.
  const [messages, setMessages] = useState(["first message"]);
  const [shown, addShown] = useOptimistic(messages, (list: string[], text: string) => [...list, text + " (sending)"]);
  const send = async (formData: FormData) => {
    addShown(String(formData.get("message")));
    const saved = await sendMessage(formData);
    setMessages((list) => [...list, saved.text]);
  };

  const runDirect = async () => {
    setDirect("running");
    try {
      const result = await saveFeedback({ text: "direct call" });
      setDirect(`${result.text}:${result.length}:${result.via}`);
    } catch (error) {
      setDirect(String(error));
    }
  };

  return (
    <>
      <Head>
        <title>Server actions - Krate</title>
      </Head>
      <div class="mx-auto max-w-2xl space-y-6 px-6 py-10">
        <header class="space-y-2">
          <p class="text-xs font-semibold uppercase tracking-wide text-indigo-300">Server actions</p>
          <h1 class="text-3xl font-bold text-slate-100">Forms that run on the server</h1>
          <p class="text-sm text-slate-400">
            These forms work without JavaScript and are enhanced by the shared runtime: no page
            reload, pending state, and form reset after success.
          </p>
        </header>

        <ActionForm action={saveFeedback} testid="feedback-form" />

        <form data-testid="formaction-form" class="flex flex-wrap items-center gap-3">
          <input name="text" defaultValue="submitted by formAction" class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100" />
          <button type="submit" formAction={saveFeedback} class="rounded-lg border border-indigo-400 px-4 py-2 font-semibold text-indigo-200">
            Save via formAction
          </button>
        </form>

        <form action={todoAction} data-testid="todo-form" class="space-y-3 rounded-xl border border-slate-700 p-5">
          <input name="title" placeholder="New todo" data-testid="todo-title" class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100" />
          <SubmitButton label="Add todo" testid="todo-submit" />
          <p class="text-sm text-slate-400">
            <span data-testid="todo-count">{todos.length}</span> todos, last:{" "}
            <span data-testid="todo-last">{todos.length ? todos[todos.length - 1] : "none"}</span>
            <span data-testid="todo-pending">{todoPending ? " (saving)" : ""}</span>
          </p>
        </form>

        <form action={send} data-testid="message-form" class="space-y-3 rounded-xl border border-slate-700 p-5">
          <input name="message" placeholder="Message" data-testid="message-text" class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100" />
          <SubmitButton label="Send" testid="message-submit" />
          <p class="text-sm text-slate-400">
            <span data-testid="message-count">{shown.length}</span> messages, last:{" "}
            <span data-testid="message-last">{shown[shown.length - 1]}</span>
          </p>
        </form>

        <p class="flex flex-wrap items-center gap-3 text-sm text-slate-400">
          <button type="button" onClick={runDirect} class="rounded-lg border border-slate-700 px-4 py-2 text-slate-200">
            Run action directly
          </button>
          <span data-testid="direct-result">{direct}</span>
        </p>
      </div>
    </>
  );
}
