import { lazy, Suspense, useState } from "react";

const LazyPanel = lazy(() => import("../lib/LazyPanel"));

// A lazy component that is only rendered after a click loads on demand.
//
// The import cannot be gated, so the click step waits for the panel to be
// visible (then in the steps file) rather than for a fixed time.
export default function SuspenseLazyOnClick() {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <button id="open" onClick={() => setOpen(true)}>
        show settings
      </button>
      {open && (
        <Suspense fallback={<p id="loading">loading settings</p>}>
          <LazyPanel title="Settings" />
        </Suspense>
      )}
    </div>
  );
}
