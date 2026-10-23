import { lazy, Suspense } from "react";

const LazyPanel = lazy(() => import("../lib/LazyPanel"));

// React.lazy with a dynamic import inside Suspense: fallback, then the
// interactive component.
//
// The import cannot be gated, so the load snapshot waits for the panel to be
// visible (ready in the steps file) rather than for a fixed time: both sides
// are compared once the panel has been revealed, whenever that happens.
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
