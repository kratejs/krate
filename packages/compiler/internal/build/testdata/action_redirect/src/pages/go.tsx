import { goAbout } from "../actions/nav";

export default function Go() {
  return (
    <main>
      <h1 id="page">go</h1>
      <form action={goAbout}>
        <button id="go" type="submit">go</button>
      </form>
      <a id="home" href="/">home</a>
    </main>
  );
}
