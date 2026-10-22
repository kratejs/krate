import { createSignal } from "@krate/runtime";
import { saveFeedback } from "../actions/feedback";

export const config = { ssr: true };

function ActionForm(props: { action: unknown; testid: string }) {
  return (
    <form action={props.action} data-testid={props.testid} class="space-y-4 rounded-xl border border-slate-700 bg-slate-900/60 p-5">
      <label class="block space-y-2 text-sm text-slate-300">
        Message
        <input name="text" value="hello from a form" class="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100" />
      </label>
      <button type="submit" class="rounded-lg bg-indigo-500 px-4 py-2 font-semibold text-white">
        Save feedback
      </button>
    </form>
  );
}

export default function ReactActionsPage() {
  const [direct, setDirect] = createSignal("idle");

  const runDirect = async () => {
    setDirect("running");
    try {
      const result = await saveFeedback({ text: "direct call" });
      setDirect(`${result.text}:${result.length}`);
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
            This form works without JavaScript and is enhanced by the shared runtime. The direct
            button invokes the same action through a generated client proxy.
          </p>
        </header>

        <ActionForm action={saveFeedback} testid="feedback-form" />

        <form data-testid="formaction-form" class="flex flex-wrap items-center gap-3">
          <input name="text" value="submitted by formAction" class="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100" />
          <button type="submit" formAction={saveFeedback} class="rounded-lg border border-indigo-400 px-4 py-2 font-semibold text-indigo-200">
            Save via formAction
          </button>
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
