"use client";

export default function BoomError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <div id="boom-error">
      <p id="boom-msg">{error.message}</p>
      <p id="boom-digest">{error.digest}</p>
      <button id="boom-reset" onClick={reset}>Try again</button>
    </div>
  );
}
