import { startTransition, useOptimistic, useState } from "react";
import { delay, log } from "../lib/log";

type Msg = { text: string; sending?: boolean };

// The action delay is far longer than the harness's step latency on purpose:
// a snapshot is taken right after the submit, and with a ~300ms delay a
// loaded runner (Firefox in a container, six fixtures at once) sometimes let
// the action finish before that snapshot and sometimes not, on either side,
// so the expected DOM depended on timing. Later steps wait for the result.
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
    await delay(1500, null);
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
