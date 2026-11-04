import { greet } from "../actions/greet";
export default function Form() {
  return (
    <main>
      <h1 id="page">form</h1>
      <form action={greet}>
        <input id="who" name="who" />
        <button id="go" type="submit">go</button>
      </form>
    </main>
  );
}
