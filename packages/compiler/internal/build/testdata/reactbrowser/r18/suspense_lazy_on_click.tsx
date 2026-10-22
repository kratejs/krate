import { lazy, Suspense, useState } from "react";

const LazyPanel = lazy(() => import("../lib/LazyPanel"));

// A lazy component that is only rendered after a click loads on demand.
//
// The click step settles for 900ms so its snapshot is taken after the
// reveal. Both sides show the fallback on click and reveal about 300ms later
// (FALLBACK_THROTTLE_MS); the default 30ms settle put the snapshot inside
// that window, and under load React's own output depended on timing.
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
