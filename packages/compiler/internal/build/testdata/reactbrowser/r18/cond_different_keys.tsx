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

// Different keys, or a key on one side only, still remount like React.
export default function CondDifferentKeys() {
  const [alt, setAlt] = useState(false);
  return (
    <div>
      <button id="flip" onClick={() => setAlt((v) => !v)}>flip {String(alt)}</button>
      <section>{alt ? <Counter key="a" label="ka" /> : <Counter key="b" label="kb" />}</section>
      <section>{alt ? <Counter key="a" label="same-key-1" /> : <Counter key="a" label="same-key-2" />}</section>
      <section>{alt ? <Counter key="a" label="one-sided-1" /> : <Counter label="one-sided-2" />}</section>
    </div>
  );
}
