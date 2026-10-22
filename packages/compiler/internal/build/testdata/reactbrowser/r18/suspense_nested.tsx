import { lazy, Suspense } from "react";
import { delay } from "../lib/log";

function Sidebar() {
  return <aside id="sidebar">sidebar ready</aside>;
}

function Comments() {
  return <section id="comments">3 comments</section>;
}

const LazySidebar = lazy(() => delay(100, { default: Sidebar }));
const LazyComments = lazy(() => delay(400, { default: Comments }));

// Nested boundaries reveal independently: the outer content appears first,
// the inner boundary keeps its own fallback until its data arrives.
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
