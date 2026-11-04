import { useState, type ReactNode } from "react";

function Toggle(props: { children: (on: boolean, flip: () => void) => ReactNode }) {
  const [on, setOn] = useState(false);
  return <div id="toggle">{props.children(on, () => setOn(!on))}</div>;
}

function Render(props: { n: number; render: (n: number) => ReactNode }) {
  return <div id="render">{props.render(props.n)}</div>;
}

export default function FnChildren() {
  const [n, setN] = useState(1);
  return (
    <div>
      <button id="inc" onClick={() => setN(n + 1)}>inc</button>
      <Toggle>{(on, flip) => <button id="flip" onClick={flip}>{on ? "ON" : "OFF"}</button>}</Toggle>
      <Render n={n} render={(k) => <span id="r">n={k}</span>} />
    </div>
  );
}
