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
//
// The load snapshot is taken after the reveal on purpose (settleMs 900 in
// the steps file). A snapshot right after load raced the 300ms delay plus
// React's own reveal throttle: on a loaded machine (Firefox in CI) React was
// sometimes already showing the report while Krate still showed the
// fallback, so the expected DOM depended on timing.
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
