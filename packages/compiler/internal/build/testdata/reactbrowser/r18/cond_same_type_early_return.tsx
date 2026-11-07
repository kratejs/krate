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

function Panel(props: { wide: boolean }) {
  if (props.wide) {
    return <Counter label="wide" />;
  }
  return <Counter label="narrow" />;
}

function Early(props: { alt: boolean }) {
  if (props.alt) return <Counter label="alt" />;
  return <Counter label="main" />;
}

export default function CondSameTypeEarlyReturn() {
  const [flag, setFlag] = useState(false);
  return (
    <div>
      <button id="flip" onClick={() => setFlag((v) => !v)}>flip {String(flag)}</button>
      <section><Panel wide={flag} /></section>
      <section><Early alt={flag} /></section>
    </div>
  );
}
