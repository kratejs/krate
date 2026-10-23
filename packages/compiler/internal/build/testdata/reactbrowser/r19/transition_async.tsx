import { useState, useTransition } from "react";
import { log } from "../lib/log";

// The harness releases this gate from a step, so the pending state is observed
// before the transition can finish. A fixed delay races Firefox's timer
// scheduling when many fixture pages run at once.
let releaseTransition: () => void = () => {};
const transitionGate = new Promise<void>((resolve) => {
  releaseTransition = resolve;
});
if (typeof window !== "undefined") {
  (window as unknown as { __releaseTransition?: () => void }).__releaseTransition = () =>
    releaseTransition();
}

// React 19 async transitions: isPending stays true for the whole async
// function, and the old UI remains visible until the update commits.
export default function TransitionAsync() {
  const [tab, setTab] = useState("home");
  const [isPending, startTransition] = useTransition();
  const go = (next: string) =>
    startTransition(async () => {
      log("transition start", next);
      await transitionGate;
      startTransition(() => setTab(next));
    });
  return (
    <div>
      <nav className={isPending ? "pending" : ""}>
        <button id="home" onClick={() => go("home")}>
          home
        </button>
        <button id="posts" onClick={() => go("posts")}>
          posts
        </button>
      </nav>
      <p id="pending">{isPending ? "pending" : "idle"}</p>
      <main id="tab">{tab}</main>
    </div>
  );
}
