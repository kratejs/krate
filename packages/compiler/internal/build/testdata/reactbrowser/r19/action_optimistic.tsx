import { startTransition, useOptimistic, useState } from "react";
import { delay, log } from "../lib/log";

type Msg = { text: string; sending?: boolean };

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
    await delay(300, null);
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
