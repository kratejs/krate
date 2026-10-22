import { lazy, Suspense } from "react";
import { delay } from "../lib/log";

function Report(props: { quarter: string }) {
  return (
    <article id="report">
      Report for {props.quarter}: <strong>+12%</strong>
    </article>
  );
}

const SlowReport = lazy(() => delay(300, { default: Report }));

// A lazy component that resolves after a delay: the fallback is visible
// first and is replaced by the content.
export default function SuspenseFallbackThenContent() {
  return (
    <div>
      <Suspense fallback={<p id="fallback">Crunching numbers...</p>}>
        <SlowReport quarter="Q3" />
      </Suspense>
      <footer>static footer</footer>
    </div>
  );
}
