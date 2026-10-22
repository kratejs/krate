import { useCallback, useEffect, useState } from "react";
import { log } from "../lib/log";

function useToggle(initial: boolean) {
  const [on, setOn] = useState(initial);
  const toggle = useCallback(() => setOn((v) => !v), []);
  return [on, toggle] as const;
}

function useCounter(step: number) {
  const [n, setN] = useState(0);
  useEffect(() => {
    log("counter changed", n);
  }, [n]);
  return { n, inc: () => setN(n + step), reset: () => setN(0) };
}

// Custom hooks compose state, callbacks and effects; each call site has its
// own state.
export default function HookCustomHook() {
  const [open, toggleOpen] = useToggle(false);
  const [dark, toggleDark] = useToggle(true);
  const a = useCounter(1);
  const b = useCounter(5);
  return (
    <div className={dark ? "dark" : "light"}>
      <p id="flags">
        open={String(open)} dark={String(dark)}
      </p>
      <p id="counters">
        {a.n} {b.n}
      </p>
      <button id="open" onClick={toggleOpen}>
        open
      </button>
      <button id="dark" onClick={toggleDark}>
        dark
      </button>
      <button id="a" onClick={a.inc}>
        a
      </button>
      <button id="b" onClick={b.inc}>
        b
      </button>
      <button id="reset" onClick={() => (a.reset(), b.reset())}>
        reset
      </button>
    </div>
  );
}
