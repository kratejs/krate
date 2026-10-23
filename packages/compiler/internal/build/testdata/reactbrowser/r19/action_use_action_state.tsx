import { useActionState } from "react";
import { createGate, log } from "../lib/log";

type State = { count: number; message: string };

// The action stays pending until a step releases it (eval window.__release()),
// so the pending UI is compared in a stable state instead of racing a timer.
async function addToCart(prev: State, formData: FormData): Promise<State> {
  const qty = Number(formData.get("qty"));
  log("action start", prev.count, qty);
  const gate = createGate();
  (window as unknown as { __release?: () => void }).__release = gate.open;
  await gate.promise;
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
