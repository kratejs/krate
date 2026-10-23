import { useState } from "react";
import { createGate, log } from "../lib/log";

// <form action={fn}> with an async client function: the action receives
// FormData, and uncontrolled fields are reset after it completes.
//
// The action stays pending until a step releases it (eval window.__release()),
// and each release step waits for the new row and the reset field together
// (the empty field shows its placeholder), so nothing depends on a timer.
export default function ActionFormAutoReset() {
  const [messages, setMessages] = useState<string[]>([]);
  async function send(formData: FormData) {
    const text = String(formData.get("text"));
    log("send", text);
    const gate = createGate();
    (window as unknown as { __release?: () => void }).__release = gate.open;
    await gate.promise;
    setMessages((m) => [...m, text]);
  }
  return (
    <div>
      <form action={send}>
        <input id="text" name="text" defaultValue="" placeholder="message" />
        <button id="send" type="submit">
          send
        </button>
      </form>
      <ul id="messages">
        {messages.map((m, i) => (
          <li key={i}>{m}</li>
        ))}
      </ul>
    </div>
  );
}
