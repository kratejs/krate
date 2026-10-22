import { useEffect, useState } from "react";
import { log } from "../lib/log";

// Three functional updates in one handler add 3; three updates computed from
// the same stale value add 1. Both are batched into one render.
export default function HookUseStateFunctionalBatch() {
  const [count, setCount] = useState(0);
  useEffect(() => {
    log("committed", count);
  }, [count]);
  return (
    <div>
      <p id="count">{count}</p>
      <button
        id="plus3"
        onClick={() => {
          setCount((c) => c + 1);
          setCount((c) => c + 1);
          setCount((c) => c + 1);
          log("handler sees", count);
        }}
      >
        +3
      </button>
      <button
        id="stale"
        onClick={() => {
          setCount(count + 1);
          setCount(count + 1);
          setCount(count + 1);
        }}
      >
        +1 stale
      </button>
      <button
        id="timeout"
        onClick={() => {
          setTimeout(() => {
            setCount((c) => c + 10);
            setCount((c) => c * 2);
          }, 0);
        }}
      >
        timeout batch
      </button>
    </div>
  );
}
