export default function DeepError({ error, reset }) {
  return (
    <div id="deep-error">
      <p id="deep-msg">{error.message}</p>
      <button id="deep-reset" onClick={reset}>Try again</button>
    </div>
  );
}
