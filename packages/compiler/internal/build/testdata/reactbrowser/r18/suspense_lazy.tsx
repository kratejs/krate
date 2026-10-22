import { lazy, Suspense } from "react";

const LazyPanel = lazy(() => import("../lib/LazyPanel"));

// React.lazy with a dynamic import inside Suspense: fallback, then the
// interactive component.
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
