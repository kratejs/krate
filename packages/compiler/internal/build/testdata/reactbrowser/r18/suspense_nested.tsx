import { lazy, Suspense } from "react";
import { delay } from "../lib/log";

function Sidebar() {
  return <aside id="sidebar">sidebar ready</aside>;
}

function Comments() {
  return <section id="comments">3 comments</section>;
}

const LazySidebar = lazy(() => delay(100, { default: Sidebar }));
const LazyComments = lazy(() => delay(1200, { default: Comments }));

// Nested boundaries reveal independently: the outer content appears first,
// the inner boundary keeps its own fallback until its data arrives.
//
// The inner delay is well past React's reveal throttle on purpose. The outer
// boundary reveals about 300ms after its fallback appeared (FALLBACK_THROTTLE_MS)
// and the snapshot is taken right after; with a 400ms inner delay, a timer that
// fires 100ms late on a loaded machine lets the comments resolve before the
// outer reveal commits, and React itself then shows "3 comments" without the
// inner fallback, so the expected DOM depended on timing.
//
// For the same reason the load snapshot is taken after both boundaries have
// resolved (settleMs 2500 in the steps file, past the 1200ms inner delay plus
// the throttle). A snapshot taken right after load raced the timers: on a
// loaded machine (Firefox in CI) React had sometimes already shown the
// comments while Krate still showed the inner fallback.
export default function SuspenseNested() {
  return (
    <Suspense fallback={<p id="outer-fallback">Loading page...</p>}>
      <LazySidebar />
      <Suspense fallback={<p id="inner-fallback">Loading comments...</p>}>
        <LazyComments />
      </Suspense>
    </Suspense>
  );
}
