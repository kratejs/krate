import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Counter() {
  const [n, setN] = useState(0);
  useEffect(() => {
    log("counter mount");
    return () => log("counter unmount");
  }, []);
  return (
    <button id="count" onClick={() => setN(n + 1)}>
      count {n}
    </button>
  );
}

// {show && <Counter />} unmounts the child: toggling it back on remounts it
// with fresh state (React does not keep hidden state outside <Activity>).
export default function EffectToggleStatefulChild() {
  const [show, setShow] = useState(true);
  return (
    <div>
      {show && <Counter />}
      <button id="toggle" onClick={() => setShow(!show)}>
        toggle
      </button>
    </div>
  );
}
