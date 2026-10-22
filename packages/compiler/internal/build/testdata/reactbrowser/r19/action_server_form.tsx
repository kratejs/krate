import { useActionState } from "react";
import { signGuestbook } from "../actions/guestbook";

// useActionState with a "use server" action. Under Krate the action runs on
// the server (POST /__krate/action/<id>); React runs the same function in the
// browser.
export default function ActionServerForm() {
  const [state, formAction, isPending] = useActionState(signGuestbook, { entries: [], error: "" });
  return (
    <form action={formAction}>
      <input id="name" name="name" placeholder="your name" />
      <button id="sign" type="submit">
        {isPending ? "signing" : "sign"}
      </button>
      {state.error && <p id="error">{state.error}</p>}
      <ol id="entries">
        {state.entries.map((e, i) => (
          <li key={i}>{e}</li>
        ))}
      </ol>
    </form>
  );
}
