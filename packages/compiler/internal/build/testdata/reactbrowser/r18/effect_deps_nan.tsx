import { useEffect, useState } from "react";
import { log } from "../lib/log";

// Dependencies compare with Object.is: NaN equals NaN, so parsing the same
// invalid input twice does not re-run the effect.
export default function EffectDepsNaN() {
  const [raw, setRaw] = useState("12");
  const [renders, setRenders] = useState(0);
  const value = Number(raw);

  useEffect(() => {
    log("value changed", String(value));
  }, [value]);

  return (
    <div>
      <p id="out">
        {String(value)} ({renders})
      </p>
      <button id="bad" onClick={() => setRaw("abc")}>
        abc
      </button>
      <button id="bad2" onClick={() => setRaw("xyz")}>
        xyz
      </button>
      <button id="rerender" onClick={() => setRenders(renders + 1)}>
        rerender
      </button>
      <button id="good" onClick={() => setRaw("7")}>
        7
      </button>
    </div>
  );
}
