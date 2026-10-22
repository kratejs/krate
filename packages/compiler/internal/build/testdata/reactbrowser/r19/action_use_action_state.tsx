import { useActionState } from "react";
import { delay, log } from "../lib/log";

type State = { count: number; message: string };

// The action delay is far longer than the harness's step latency on purpose:
// a snapshot is taken right after the submit, and with a ~300ms delay a
// loaded runner (Firefox in a container, six fixtures at once) sometimes let
// the action finish before that snapshot and sometimes not, on either side,
// so the expected DOM depended on timing. Later steps wait for the result.
async function addToCart(prev: State, formData: FormData): Promise<State> {
  const qty = Number(formData.get("qty"));
  log("action start", prev.count, qty);
  await delay(1500, null);
  if (!Number.isInteger(qty) || qty <= 0) return { ...prev, message: "invalid quantity" };
  return { count: prev.count + qty, message: "added " + qty };
}

// useActionState with an async client action: pending flag while running,
// previous state passed in, returned state rendered.
export default function ActionUseActionState() {
  const [state, formAction, isPending] = useActionState(addToCart, { count: 0, message: "" });
  return (
    <form action={formAction}>
      <input id="qty" name="qty" defaultValue="2" />
      <button id="add" type="submit" disabled={isPending}>
        {isPending ? "adding..." : "add"}
      </button>
      <p id="cart">
        {state.count} in cart {state.message && "(" + state.message + ")"}
      </p>
    </form>
  );
}
