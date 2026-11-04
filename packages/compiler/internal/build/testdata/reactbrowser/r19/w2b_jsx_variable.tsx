import { useState } from "react";

function A() { return <u id="a">A</u>; }
function B() { return <u id="b">B</u>; }

export default function JsxVariable() {
  const [on, setOn] = useState(false);
  const [tab, setTab] = useState("a");
  const badge = on ? <b>on</b> : <i>off</i>;
  let content;
  switch (tab) {
    case "a": content = <A />; break;
    default: content = <B />;
  }
  return (
    <div>
      <button id="t" onClick={() => setOn(!on)}>toggle</button>
      <button id="tab" onClick={() => setTab(tab === "a" ? "b" : "a")}>tab</button>
      <p id="badge">{badge}</p>
      <div id="content">{content}</div>
    </div>
  );
}
