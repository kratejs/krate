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

// useFormStatus in a child component reads the parent form's pending state
// and submitted data.
export default function ActionUseFormStatus() {
  const [done, setDone] = useState("");
  return (
    <form
      action={async (fd: FormData) => {
        await delay(300, null);
        setDone("subscribed " + fd.get("email"));
      }}
    >
      <input id="email" name="email" defaultValue="ada@example.com" />
      <SubmitButton />
      <p id="done">{done}</p>
    </form>
  );
}
