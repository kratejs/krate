import { useEffect, useState } from "react";
import { log } from "../lib/log";

// Object.is(0, -0) is false, so switching between +0 and -0 re-runs the
// effect even though 0 === -0.
export default function EffectDepsSignedZero() {
  const [offset, setOffset] = useState(0);

  useEffect(() => {
    log("offset", offset, Object.is(offset, -0) ? "negative zero" : "not negative zero");
  }, [offset]);

  return (
    <div>
      <p id="out">{1 / offset > 0 ? "+0 side" : "-0 side"}</p>
      <button id="neg" onClick={() => setOffset(-0)}>
        -0
      </button>
      <button id="pos" onClick={() => setOffset(0)}>
        +0
      </button>
      <button id="same" onClick={() => setOffset(offset)}>
        same
      </button>
    </div>
  );
}
