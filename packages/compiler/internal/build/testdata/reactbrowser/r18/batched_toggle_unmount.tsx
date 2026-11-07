import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Child() {
  const [n, setN] = useState(0);
  useEffect(() => {
    log("child mount");
    return () => log("child unmount");
  }, []);
  return <button id="child" onClick={() => setN(n + 1)}>child {n}</button>;
}

export default function HuntBatchedToggle() {
  const [show, setShow] = useState(true);
  const [tick, setTick] = useState(0);
  return (
    <div>
      <button id="flip-flip" onClick={() => { setShow(false); setShow(true); }}>flip-flip</button>
      <button id="hide" onClick={() => setShow(false)}>hide</button>
      <button id="show" onClick={() => setShow(true)}>show</button>
      <button id="hide-tick" onClick={() => { setShow((s) => !s); setTick((t) => t + 1); }}>toggle+tick {tick}</button>
      {show ? <Child /> : null}
    </div>
  );
}
