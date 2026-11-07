import { useEffect, useState } from "react";
import { createGate, log } from "../lib/log";

// Each request is held until a step releases it, so the stale response
// arrives at a known moment instead of racing a timer.
function request(v: string): Promise<string> {
  const g = createGate();
  const w = window as unknown as { __gates?: Record<string, () => void> };
  (w.__gates = w.__gates || {})[v] = g.open;
  return g.promise.then(() => v);
}

function Loader(props: { q: string }) {
  const [res, setRes] = useState("none");
  useEffect(() => {
    let live = true;
    log("start", props.q);
    request(props.q).then((v) => {
      if (live) {
        log("apply", v);
        setRes(v);
      } else {
        log("ignored", v);
      }
    });
    return () => {
      live = false;
      log("cancel", props.q);
    };
  }, [props.q]);
  return <p id="res">{res}</p>;
}

export default function EffectAsyncRaceCancel() {
  const [q, setQ] = useState("slow");
  const [show, setShow] = useState(true);
  return (
    <div>
      <button id="fast" onClick={() => setQ("fast")}>fast</button>
      <button id="slow" onClick={() => setQ("slow")}>slow</button>
      <button id="hide" onClick={() => setShow(false)}>hide</button>
      {show && <Loader q={q} />}
    </div>
  );
}
