import { useEffect, useLayoutEffect, useState } from "react";
import { log } from "../lib/log";

// After a discrete event (click), React flushes the render and its passive
// effects before the event returns, so effect logs come before a microtask
// or timeout queued by the handler. Non-discrete updates (from a timeout)
// may run passive effects later.
export default function EffectPassiveFlushDiscrete() {
  const [n, setN] = useState(0);
  useLayoutEffect(() => {
    log("layout", n);
  }, [n]);
  useEffect(() => {
    log("passive", n);
  }, [n]);
  return (
    <div>
      <p id="n">{n}</p>
      <button
        id="click"
        onClick={() => {
          setN((v) => v + 1);
          log("handler");
          queueMicrotask(() => log("microtask"));
          setTimeout(() => log("timeout"), 0);
        }}
      >
        click
      </button>
      <button
        id="later"
        onClick={() =>
          setTimeout(() => {
            setN((v) => v + 10);
            log("timeout handler");
            queueMicrotask(() => log("timeout microtask"));
          }, 0)
        }
      >
        later
      </button>
    </div>
  );
}
