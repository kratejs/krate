import { useState, useEffect, useLayoutEffect, useInsertionEffect, useMemo } from 'react';

// Conformance page for React effect semantics: phase order (insertion, layout,
// passive), children before parents, dependency arrays compared with
// Object.is, batched updates, and cleanups when a conditional branch hides.
// Every effect appends to window.__effectLog; the browser conformance script
// asserts the exact sequence React produces.

function Child({ label }: { label: string }) {
  const log = (m: string) => {
    const w = window as any;
    (w.__effectLog = w.__effectLog || []).push(m);
  };
  useInsertionEffect(() => {
    log('child insertion');
  }, []);
  useLayoutEffect(() => {
    log('child layout');
    return () => log('child layout cleanup');
  }, []);
  useEffect(() => {
    log('child passive');
    return () => log('child passive cleanup');
  }, []);
  return <p data-testid="effects-child">child {label}</p>;
}

export default function Page() {
  const [count, setCount] = useState(0);
  const [nan, setNan] = useState(NaN);
  const [show, setShow] = useState(true);
  const doubled = useMemo(() => count * 2, [count]);
  const log = (m: string) => {
    const w = window as any;
    (w.__effectLog = w.__effectLog || []).push(m);
  };

  useLayoutEffect(() => {
    log('parent layout');
  }, []);
  useEffect(() => {
    log('parent passive ' + count);
    return () => log('parent passive cleanup');
  }, [count]);
  useEffect(() => {
    log('nan effect');
  }, [nan]);

  return (
    <div class="mx-auto max-w-2xl space-y-4 px-6 py-10">
      <h1 class="text-3xl font-bold text-slate-100">React effects</h1>
      <p data-testid="effects-doubled">{doubled}</p>
      <button
        data-testid="effects-inc"
        onClick={() => {
          setCount((c) => c + 1);
          setCount((c) => c + 1);
        }}
      >
        inc
      </button>
      <button data-testid="effects-nan" onClick={() => setNan(NaN)}>
        nan
      </button>
      <button data-testid="effects-toggle" onClick={() => setShow(!show)}>
        toggle
      </button>
      {show && <Child label="a" />}
    </div>
  );
}
