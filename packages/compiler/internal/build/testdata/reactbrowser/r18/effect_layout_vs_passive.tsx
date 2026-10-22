import { useEffect, useInsertionEffect, useLayoutEffect, useState } from "react";
import { log } from "../lib/log";

// Insertion effects run before layout effects, which run before passive
// effects. Cleanups follow the same phases.
export default function EffectLayoutVsPassive() {
  const [n, setN] = useState(0);

  useEffect(() => {
    log("passive", n);
    return () => log("passive cleanup", n);
  }, [n]);

  useLayoutEffect(() => {
    log("layout", n);
    return () => log("layout cleanup", n);
  }, [n]);

  useInsertionEffect(() => {
    log("insertion", n);
    return () => log("insertion cleanup", n);
  }, [n]);

  return (
    <button id="inc" onClick={() => setN(n + 1)}>
      n={n}
    </button>
  );
}
