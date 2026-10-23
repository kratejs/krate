import { useState } from "react";
import { useFormStatus } from "react-dom";
import { createGate } from "../lib/log";

function SubmitButton() {
  const { pending, data } = useFormStatus();
  return (
    <button id="submit" type="submit" disabled={pending}>
      {pending ? "saving " + String(data?.get("email")) : "subscribe"}
    </button>
  );
}

// The action stays pending until a step releases it (eval window.__release()),
// so the pending UI is compared in a stable state instead of racing a timer.
// useFormStatus in a child component reads the parent form's pending state
// and submitted data.
export default function ActionUseFormStatus() {
  const [done, setDone] = useState("");
  return (
    <form
      action={async (fd: FormData) => {
        const gate = createGate();
        (window as unknown as { __release?: () => void }).__release = gate.open;
        await gate.promise;
        setDone("subscribed " + fd.get("email"));
      }}
    >
      <input id="email" name="email" defaultValue="ada@example.com" />
      <SubmitButton />
      <p id="done">{done}</p>
    </form>
  );
}
