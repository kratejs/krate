import { lazy, Suspense } from "react";

function Panel() {
  return <section id="panel">panel ready</section>;
}

// The lazy component resolves when the button is clicked, not after a timer,
// so the fallback and the reveal are deterministic. `release` is a module-level
// `let` assigned inside the promise executor and called from a handler: both
// must see the same variable.
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
