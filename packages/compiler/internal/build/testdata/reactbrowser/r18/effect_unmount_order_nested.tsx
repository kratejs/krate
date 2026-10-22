import { useEffect, useLayoutEffect, useState } from "react";
import { log } from "../lib/log";

function Leaf(props: { name: string }) {
  useLayoutEffect(() => () => log("layout cleanup", props.name), []);
  useEffect(() => () => log("passive cleanup", props.name), []);
  return <i>{props.name}</i>;
}

function Parent() {
  useLayoutEffect(() => () => log("layout cleanup parent"), []);
  useEffect(() => () => log("passive cleanup parent"), []);
  return (
    <div>
      <Leaf name="a" />
      <Leaf name="b" />
    </div>
  );
}

// Unmounting a subtree: the exact order of layout and passive cleanups
// across parent and children, as real React runs them.
export default function EffectUnmountOrderNested() {
  const [show, setShow] = useState(true);
  return (
    <div>
      {show && <Parent />}
      <button id="hide" onClick={() => setShow(false)}>
        hide
      </button>
    </div>
  );
}
