import { startTransition, useOptimistic, useState } from "react";
import { createGate, log } from "../lib/log";

// A second action succeeds while the first (which fails) is still pending.
export default function OptimisticOverlappingActions() {
  const [items, setItems] = useState<string[]>(["base"]);
  const [shown, addShown] = useOptimistic(items, (s: string[], t: string) => [...s, t + "?"]);
  const [err, setErr] = useState("");
  const gates: Record<string, ReturnType<typeof createGate>> = {};
  (window as any).__gates = (window as any).__gates || {};

  async function run(fd: FormData) {
    const t = String(fd.get("t"));
    addShown(t);
    const g = createGate();
    (window as any).__gates[t] = g;
    await g.promise;
    if (t.startsWith("bad")) {
      log("failed", t);
      startTransition(() => setErr("failed " + t));
      return;
    }
    log("saved", t);
    startTransition(() => setItems((l) => [...l, t]));
  }
  return (
    <div>
      <ul id="list">{shown.map((t, i) => <li key={i}>{t}</li>)}</ul>
      <p id="err">{err}</p>
      <form action={run}>
        <input id="t" name="t" defaultValue="one" />
        <button id="go" type="submit">go</button>
      </form>
    </div>
  );
}
