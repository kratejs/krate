export default function CrashError({ error, reset }) {
  return (
    <div id="inner-error">
      <p id="inner-msg">{error.message}</p>
      <button id="inner-reset" onClick={reset}>Try again</button>
    </div>
  );
}
