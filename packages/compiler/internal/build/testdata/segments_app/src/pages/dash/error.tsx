export default function DashError({ error, reset }) {
  return (
    <div id="outer-error">
      <p id="outer-msg">{error.message}</p>
      <p id="outer-digest">{error.digest}</p>
      <button id="outer-reset" onClick={reset}>Try again</button>
    </div>
  );
}
