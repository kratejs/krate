import { useEffect, useReducer, useRef, useState } from "react";
import { log } from "../lib/log";

type S = { n: number; log: string[] };
function reducer(s: S, a: { t: string; v?: number }): S {
  switch (a.t) {
    case "inc": return { ...s, n: s.n + 1, log: [...s.log, "inc" + (s.n + 1)] };
    case "set": return { ...s, n: a.v ?? 0, log: [...s.log, "set" + a.v] };
    default: return s;
  }
}

export default function HuntReducerEffectLoop() {
  const [s, dispatch] = useReducer(reducer, { n: 0, log: [] });
  const [target, setTarget] = useState(3);
  const renders = useRef(0);
  renders.current++;
  useEffect(() => {
    log("effect n", s.n, "target", target);
    if (s.n < target) dispatch({ t: "inc" });
  }, [s.n, target]);
  return (
    <div>
      <button id="t5" onClick={() => setTarget(5)}>target 5</button>
      <button id="reset" onClick={() => { dispatch({ t: "set", v: 0 }); dispatch({ t: "set", v: 1 }); }}>reset</button>
      <p id="out">{s.n} [{s.log.join(",")}]</p>
    </div>
  );
}
