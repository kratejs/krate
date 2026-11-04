import { useActionState } from "react";
import { signup } from "../actions/signup";

export default function Home() {
  const [state, formAction] = useActionState(signup, { error: "" });
  return (
    <main>
      <h1 id="page">signup</h1>
      <form action={formAction}>
        <input id="user" name="user" />
        <button id="go" type="submit">sign up</button>
        {state.error && <p id="error">{state.error}</p>}
      </form>
    </main>
  );
}
