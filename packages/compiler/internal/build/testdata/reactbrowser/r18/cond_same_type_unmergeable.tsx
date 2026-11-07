import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Counter(props: { label: string; step?: number; onTick?: () => void }) {
  const [n, setN] = useState(0);
  useEffect(() => {
    log("mount", props.label);
    return () => log("unmount", props.label);
  }, []);
  return <button className="c" onClick={() => setN(n + (props.step ?? 1))}>{props.label}:{n}</button>;
}

const a = { step: 2 };
const b = { step: 3 };

// Spreads of different objects cannot be merged: Krate warns (KR8360) because
// the instance restarts where React keeps it.
export default function CondSameTypeUnmergeable() {
  const [alt, setAlt] = useState(false);
  return (
    <div>
      <button id="flip" onClick={() => setAlt((v) => !v)}>flip {String(alt)}</button>
      {alt ? <Counter label="one" {...a} /> : <Counter label="two" {...b} />}
    </div>
  );
}
