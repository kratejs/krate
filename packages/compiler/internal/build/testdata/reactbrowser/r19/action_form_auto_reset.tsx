import { useState } from "react";
import { delay, log } from "../lib/log";

// <form action={fn}> with an async client function: the action receives
// FormData, and uncontrolled fields are reset after it completes.
export default function ActionFormAutoReset() {
  const [messages, setMessages] = useState<string[]>([]);
  async function send(formData: FormData) {
    const text = String(formData.get("text"));
    log("send", text);
    await delay(80, null);
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
