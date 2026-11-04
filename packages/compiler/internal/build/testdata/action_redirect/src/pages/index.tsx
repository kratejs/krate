import { useActionState } from "react";
import { login } from "../actions/nav";

export default function Home() {
  const [state, formAction] = useActionState(login, { error: "" });
  return (
    <main>
      <h1 id="page">home</h1>
      <form action={formAction}>
        <input id="user" name="user" />
        <button id="login" type="submit">login</button>
        {state.error && <p id="error">{state.error}</p>}
      </form>
      <a id="to-go" href="/go">go page</a>
    </main>
  );
}
