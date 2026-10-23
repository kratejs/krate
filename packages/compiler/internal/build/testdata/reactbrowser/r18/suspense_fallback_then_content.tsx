import { lazy, Suspense } from "react";
import { createGate } from "../lib/log";

function Report(props: { quarter: string }) {
  return (
    <article id="report">
      Report for {props.quarter}: <strong>+12%</strong>
    </article>
  );
}

// The lazy component resolves when the button is clicked, not after a timer,
// so the fallback is on screen for exactly as long as the steps want it and
// the reveal does not race React's 300ms throttle.
const gate = createGate();
const SlowReport = lazy(() => gate.promise.then(() => ({ default: Report })));

// A lazy component that resolves later: the fallback is visible first and is
// replaced by the content.
export default function SuspenseFallbackThenContent() {
  return (
    <div>
      <button id="release" onClick={() => gate.open()}>
        release
      </button>
      <Suspense fallback={<p id="fallback">Crunching numbers...</p>}>
        <SlowReport quarter="Q3" />
      </Suspense>
      <footer>static footer</footer>
    </div>
  );
}
