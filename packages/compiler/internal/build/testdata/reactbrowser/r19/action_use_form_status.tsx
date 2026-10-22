import { useState } from "react";
import { useFormStatus } from "react-dom";
import { delay } from "../lib/log";

function SubmitButton() {
  const { pending, data } = useFormStatus();
  return (
    <button id="submit" type="submit" disabled={pending}>
      {pending ? "saving " + String(data?.get("email")) : "subscribe"}
    </button>
  );
}

// The action delay is far longer than the harness's step latency on purpose:
// a snapshot is taken right after the submit, and with a ~300ms delay a
// loaded runner (Firefox in a container, six fixtures at once) sometimes let
// the action finish before that snapshot and sometimes not, on either side,
// so the expected DOM depended on timing. Later steps wait for the result.
// useFormStatus in a child component reads the parent form's pending state
// and submitted data.
export default function ActionUseFormStatus() {
  const [done, setDone] = useState("");
  return (
    <form
      action={async (fd: FormData) => {
        await delay(1500, null);
        setDone("subscribed " + fd.get("email"));
      }}
    >
      <input id="email" name="email" defaultValue="ada@example.com" />
      <SubmitButton />
      <p id="done">{done}</p>
    </form>
  );
}
