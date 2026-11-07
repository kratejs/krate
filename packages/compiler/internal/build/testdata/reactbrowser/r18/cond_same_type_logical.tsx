import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Box(props: { tag: string; children?: any }) {
  const [n, setN] = useState(0);
  useEffect(() => {
    log("mount", props.tag);
    return () => log("unmount", props.tag);
  }, []);
  return <button className="b" onClick={() => setN(n + 1)}>{props.tag}:{n}{props.children}</button>;
}

export default function CondSameTypeLogical() {
  const [on, setOn] = useState(true);
  const [alt, setAlt] = useState(false);
  return (
    <div>
      <button id="on" onClick={() => setOn((v) => !v)}>on {String(on)}</button>
      <button id="alt" onClick={() => setAlt((v) => !v)}>alt {String(alt)}</button>
      <section>{on ? <Box tag="t1" /> : <Box tag="t2" />}</section>
      <section>{alt ? <Box tag="c1">x</Box> : <Box tag="c2">y</Box>}</section>
      <section>{on && <Box tag="and" />}</section>
      <section>{on || <Box tag="or" />}</section>
      <section>{on ? <Box tag="nul" /> : null}</section>
    </div>
  );
}
