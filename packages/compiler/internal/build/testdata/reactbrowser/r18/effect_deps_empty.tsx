import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Clock(props: { label: string }) {
  useEffect(() => {
    log("mount", props.label);
    return () => log("unmount", props.label);
  }, []);
  return <span className="clock">{props.label}</span>;
}

// An empty dependency array runs once on mount and cleans up on unmount,
// even when props change in between.
export default function EffectDepsEmpty() {
  const [show, setShow] = useState(true);
  const [label, setLabel] = useState("one");
  return (
    <div>
      {show ? <Clock label={label} /> : <em>hidden</em>}
      <button id="label" onClick={() => setLabel(label === "one" ? "two" : "one")}>
        relabel
      </button>
      <button id="toggle" onClick={() => setShow(!show)}>
        toggle
      </button>
    </div>
  );
}
