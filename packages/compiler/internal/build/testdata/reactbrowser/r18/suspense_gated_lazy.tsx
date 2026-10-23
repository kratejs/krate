import { lazy, Suspense } from "react";

function Panel() {
  return <section id="panel">panel ready</section>;
}

// The lazy component resolves when the button is clicked, not after a timer,
// so the fallback and the reveal are deterministic. `release` is a module-level
// `let` assigned inside the promise executor and called from a handler: both
// must see the same variable.
//
// The click step settles for 900ms before its snapshot (steps file): React
// reveals content no sooner than 300ms after the fallback appeared, so a
// snapshot taken right after the click could still see React's fallback
// while Krate had already revealed, depending on how long the page load and
// the click took on a loaded machine.
let release: () => void = () => {};
const gate = new Promise<void>((resolve) => {
  release = resolve;
});
const LazyPanel = lazy(() => gate.then(() => ({ default: Panel })));

export default function GatedLazy() {
  return (
    <div>
      <button id="release" onClick={() => release()}>release</button>
      <Suspense fallback={<p id="fallback">Loading...</p>}>
        <LazyPanel />
      </Suspense>
    </div>
  );
}
