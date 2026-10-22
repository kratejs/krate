import { lazy, Suspense } from "react";

const LazyPanel = lazy(() => import("../lib/LazyPanel"));

// React.lazy with a dynamic import inside Suspense: fallback, then the
// interactive component.
//
// The load snapshot is taken after the reveal on purpose (settleMs 900 in
// the steps file). Both React and Krate show the fallback first and reveal
// the panel about 300ms after it appeared (FALLBACK_THROTTLE_MS). A snapshot
// taken about 150ms after load landed inside that window, so on a loaded
// machine (WebKit/Firefox in a container, many pages at once) React itself
// was sometimes already showing the panel, and the expected DOM depended on
// timing.
export default function SuspenseLazy() {
  return (
    <main>
      <h1>Dashboard</h1>
      <Suspense fallback={<p id="loading">Loading panel...</p>}>
        <LazyPanel title="Stats" />
      </Suspense>
    </main>
  );
}
