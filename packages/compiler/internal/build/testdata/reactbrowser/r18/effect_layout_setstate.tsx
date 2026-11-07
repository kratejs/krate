import { useEffect, useLayoutEffect, useState } from "react";
import { log } from "../lib/log";

export default function HuntLayoutSetState() {
  const [n, setN] = useState(0);
  const [doubled, setDoubled] = useState(0);
  useLayoutEffect(() => {
    log("layout", n);
    setDoubled(n * 2);
  }, [n]);
  useEffect(() => {
    log("effect", n, doubled);
    return () => log("cleanup", n, doubled);
  }, [n, doubled]);
  return (
    <div>
      <button id="inc" onClick={() => setN(n + 1)}>inc</button>
      <p id="out">{n}/{doubled}</p>
    </div>
  );
}
