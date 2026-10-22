import { useEffect, useState } from "react";
import { log } from "../lib/log";

// An effect with a literal dependency re-runs only when that dependency
// changes; its cleanup runs before each re-run.
export default function EffectDepsLiteral() {
  const [count, setCount] = useState(0);
  const [other, setOther] = useState(0);

  useEffect(() => {
    log("effect count", count);
    return () => log("cleanup count", count);
  }, [count]);

  return (
    <div>
      <p id="out">
        count={count} other={other}
      </p>
      <button id="inc" onClick={() => setCount((c) => c + 1)}>
        inc
      </button>
      <button id="other" onClick={() => setOther((o) => o + 1)}>
        other
      </button>
    </div>
  );
}
