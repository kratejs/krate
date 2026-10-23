// A module-level `let` is one variable shared by every component, so a write
// in one component's handler is visible in another's.
import { useState } from "react";

let count = 0;

function Inc() {
  const [tick, setTick] = useState(0);
  return (
    <button id="inc" onClick={() => { count++; setTick((t) => t + 1); }}>
      inc {tick}
    </button>
  );
}

function Show() {
  const [text, setText] = useState("none");
  return (
    <button id="show" onClick={() => setText("count=" + count)}>
      {text}
    </button>
  );
}

export default function ModuleLet() {
  return (
    <div>
      <Inc />
      <Show />
    </div>
  );
}
