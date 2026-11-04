import { useState } from "react";

export default function CondZero() {
  const props = { items: [] as number[] };
  const [n, setN] = useState(0);
  const [s, setS] = useState("");
  return (
    <div>
      <button id="inc" onClick={() => setN(n + 1)}>inc</button>
      <button id="dec" onClick={() => setN(0)}>zero</button>
      <button id="str" onClick={() => setS(s === "" ? "x" : "")}>str</button>
      <p id="a">{n && <b>has</b>}</p>
      <p id="b">{n && "text"}</p>
      <p id="c">{s && <b>s</b>}</p>
      <p id="d">{n > 0 && <b>pos</b>}</p>
      <p id="e">{s || <i>anon</i>}</p>
      <p id="f">{props.items.length && <b>list</b>}</p>
    </div>
  );
}
