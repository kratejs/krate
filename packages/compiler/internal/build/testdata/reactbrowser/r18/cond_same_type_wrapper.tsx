import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Counter(props: { label: string }) {
  const [n, setN] = useState(0);
  useEffect(() => {
    log("mount", props.label);
    return () => log("unmount", props.label);
  }, []);
  return <button className="c" onClick={() => setN(n + 1)}>{props.label}:{n}</button>;
}

// The same component inside a wrapper element that both branches build:
// React keeps the Counter, Krate rebuilds the wrapper and warns (KR8360).
export default function CondSameTypeWrapper() {
  const [alt, setAlt] = useState(false);
  return (
    <div>
      <button id="flip" onClick={() => setAlt((v) => !v)}>flip {String(alt)}</button>
      {alt ? <section><Counter label="one" /></section> : <section><Counter label="two" /></section>}
    </div>
  );
}
