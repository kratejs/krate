import { useEffect, useState } from "react";
import { log } from "../lib/log";

// Without a dependency array the effect runs after every render.
export default function EffectDepsMissing() {
  const [a, setA] = useState(0);
  const [b, setB] = useState(0);

  useEffect(() => {
    log("render effect", a, b);
    return () => log("cleanup", a, b);
  });

  return (
    <div>
      <output id="out">
        {a}-{b}
      </output>
      <button id="a" onClick={() => setA(a + 1)}>
        a
      </button>
      <button id="b" onClick={() => setB(b + 1)}>
        b
      </button>
    </div>
  );
}
