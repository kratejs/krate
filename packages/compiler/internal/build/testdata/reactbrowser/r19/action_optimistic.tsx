import { startTransition, useOptimistic, useState } from "react";
import { createGate, log } from "../lib/log";

type Msg = { text: string; sending?: boolean };

// The action stays pending until a step releases it (eval window.__release()),
// so the pending UI is compared in a stable state instead of racing a timer.
// useOptimistic shows a pending item immediately; it is replaced by the
// confirmed state when the action finishes.
export default function ActionOptimistic() {
  const [messages, setMessages] = useState<Msg[]>([{ text: "hi" }]);
  const [optimistic, addOptimistic] = useOptimistic(messages, (state: Msg[], text: string) => [
    ...state,
    { text, sending: true },
  ]);

  async function send(formData: FormData) {
    const text = String(formData.get("text"));
    addOptimistic(text);
    const gate = createGate();
    (window as unknown as { __release?: () => void }).__release = gate.open;
    await gate.promise;
    log("confirmed", text);
    startTransition(() => setMessages((m) => [...m, { text }]));
  }

  return (
    <div>
      <ul id="thread">
        {optimistic.map((m, i) => (
          <li key={i} className={m.sending ? "sending" : "sent"}>
            {m.text}
            {m.sending && <small> (sending)</small>}
          </li>
        ))}
      </ul>
      <form action={send}>
        <input id="text" name="text" defaultValue="hello" />
        <button id="send" type="submit">
          send
        </button>
      </form>
    </div>
  );
}
