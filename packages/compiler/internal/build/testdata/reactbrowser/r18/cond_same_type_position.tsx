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

function Other() {
  useEffect(() => {
    log("mount other");
    return () => log("unmount other");
  }, []);
  return <b>other</b>;
}

export default function CondSameTypePosition() {
  const [mode, setMode] = useState(0);
  return (
    <div>
      <button id="next" onClick={() => setMode((m) => (m + 1) % 5)}>mode {mode}</button>
      <section>
        {mode === 0 && <Counter label="one" />}
        {mode === 1 && <Counter label="two" />}
        {mode === 2 && <Other />}
        {mode === 3 ? <Counter key="k" label="keyed" /> : <Counter key="j" label="keyed2" />}
      </section>
      <section>{mode % 2 === 0 ? <Counter label="p" /> : <Counter label="q" />}</section>
    </div>
  );
}
