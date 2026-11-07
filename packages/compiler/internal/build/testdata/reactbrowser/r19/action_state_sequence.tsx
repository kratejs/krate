import { startTransition, useActionState } from "react";
import { createGate, log } from "../lib/log";

// Dispatches queue: each action receives the previous action's result, and
// they run one after another.
export default function ActionStateSequence() {
  const [count, dispatch, pending] = useActionState(async (prev: number, add: number) => {
    log("start", prev, add);
    const g = createGate();
    (window as any).__g = ((window as any).__g || []).concat(g);
    await g.promise;
    log("end", prev, add);
    return prev + add;
  }, 0);
  return (
    <div>
      <button id="a1" onClick={() => startTransition(() => dispatch(1))}>+1</button>
      <button id="a10" onClick={() => startTransition(() => dispatch(10))}>+10</button>
      <p id="out">{count}</p>
      <p id="pending">{pending ? "pending" : "idle"}</p>
    </div>
  );
}
