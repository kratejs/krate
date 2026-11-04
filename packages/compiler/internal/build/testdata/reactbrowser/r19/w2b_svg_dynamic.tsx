import { useState } from "react";

export default function SvgDynamic() {
  const [pts, setPts] = useState([1, 2]);
  const [show, setShow] = useState(false);
  return (
    <div>
      <button id="add" onClick={() => setPts([...pts, pts.length + 1])}>add</button>
      <button id="show" onClick={() => setShow(!show)}>show</button>
      <svg id="svg" width="100" height="60" viewBox="0 0 100 60">
        {pts.map((p) => <circle key={p} cx={p * 10} cy="10" r="4" />)}
        {show && <g id="grp"><path d="M0 0L10 10" /></g>}
      </svg>
    </div>
  );
}
