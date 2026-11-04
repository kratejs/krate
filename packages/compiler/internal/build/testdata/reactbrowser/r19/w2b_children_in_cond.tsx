import { useState, type ReactNode } from "react";
import { log } from "../lib/log";

function Disclosure(props: { title: string; children?: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button id="d" onClick={() => setOpen(!open)}>{props.title}</button>
      {open && <div className="body">{props.children}</div>}
    </>
  );
}

export default function ChildrenInCond() {
  const [n, setN] = useState(0);
  return (
    <div>
      <Disclosure title="more">
        <button id="inner" onClick={() => { setN(n + 1); log("inner", n + 1); }}>inner {n}</button>
        <span id="static">static</span>
      </Disclosure>
    </div>
  );
}
