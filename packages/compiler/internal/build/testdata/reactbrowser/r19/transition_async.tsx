import { useState, useTransition } from "react";
import { delay, log } from "../lib/log";

// React 19 async transitions: isPending stays true for the whole async
// function, and the old UI remains visible until the update commits.
export default function TransitionAsync() {
  const [tab, setTab] = useState("home");
  const [isPending, startTransition] = useTransition();
  const go = (next: string) =>
    startTransition(async () => {
      log("transition start", next);
      await delay(300, null);
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
