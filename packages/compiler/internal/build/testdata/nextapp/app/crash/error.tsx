"use client";

export default function CrashError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <div id="crash-error">
      <p id="crash-msg">{error.message}</p>
      <button id="crash-reset" onClick={reset}>Try again</button>
    </div>
  );
}
