import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Leaf(props: { name: string; n: number }) {
  useEffect(() => {
    log("effect", props.name, props.n);
    return () => log("cleanup", props.name, props.n);
  }, [props.n]);
  return <li>{props.name}</li>;
}

function Branch(props: { n: number }) {
  useEffect(() => {
    log("effect branch", props.n);
    return () => log("cleanup branch", props.n);
  }, [props.n]);
  return (
    <ul>
      <Leaf name="first" n={props.n} />
      <Leaf name="second" n={props.n} />
    </ul>
  );
}

// Children commit their effects before parents, siblings in order; cleanups
// on unmount follow React's order.
export default function EffectOrderParentChild() {
  const [n, setN] = useState(0);
  const [show, setShow] = useState(true);
  useEffect(() => {
    log("effect root", n);
    return () => log("cleanup root", n);
  }, [n]);
  return (
    <div>
      {show && <Branch n={n} />}
      <button id="bump" onClick={() => setN(n + 1)}>
        bump
      </button>
      <button id="hide" onClick={() => setShow(false)}>
        hide
      </button>
    </div>
  );
}
