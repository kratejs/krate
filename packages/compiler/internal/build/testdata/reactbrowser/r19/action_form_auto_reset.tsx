import { useState } from "react";
import { delay, log } from "../lib/log";

// <form action={fn}> with an async client function: the action receives
// FormData, and uncontrolled fields are reset after it completes.
//
// The action delay is far longer than the harness's step latency on purpose.
// A snapshot is taken right after each submit; with an 80ms delay a loaded
// machine sometimes let the action finish (field reset, new row) before that
// snapshot and sometimes not, on either side, so the expected DOM depended on
// timing. The steps wait for the new row instead of a fixed time.
export default function ActionFormAutoReset() {
  const [messages, setMessages] = useState<string[]>([]);
  async function send(formData: FormData) {
    const text = String(formData.get("text"));
    log("send", text);
    await delay(600, null);
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
