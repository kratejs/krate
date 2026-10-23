import { lazy, Suspense } from "react";
import { createGate } from "../lib/log";

function Sidebar() {
  return <aside id="sidebar">sidebar ready</aside>;
}

function Comments() {
  return <section id="comments">3 comments</section>;
}

// Each lazy component resolves when its button is clicked, not after a timer,
// so the state between the two reveals is stable instead of racing React's
// 300ms reveal throttle and the page load.
const sidebarGate = createGate();
const commentsGate = createGate();
const LazySidebar = lazy(() => sidebarGate.promise.then(() => ({ default: Sidebar })));
const LazyComments = lazy(() => commentsGate.promise.then(() => ({ default: Comments })));

// Nested boundaries reveal independently: the outer content appears first,
// the inner boundary keeps its own fallback until its data arrives.
export default function SuspenseNested() {
  return (
    <div>
      <button id="release-sidebar" onClick={() => sidebarGate.open()}>
        release sidebar
      </button>
      <button id="release-comments" onClick={() => commentsGate.open()}>
        release comments
      </button>
      <Suspense fallback={<p id="outer-fallback">Loading page...</p>}>
        <LazySidebar />
        <Suspense fallback={<p id="inner-fallback">Loading comments...</p>}>
          <LazyComments />
        </Suspense>
      </Suspense>
    </div>
  );
}
