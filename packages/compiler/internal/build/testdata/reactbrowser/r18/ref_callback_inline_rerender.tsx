import { useState } from "react";
import { log } from "../lib/log";

// An inline callback ref is a new function every render, so React calls the
// old one with null and the new one with the node on each update.
export default function RefCallbackInlineRerender() {
  const [n, setN] = useState(0);
  return (
    <div>
      <span id="target" ref={(node) => log("inline ref", n, node ? "node" : "null")}>
        render {n}
      </span>
      <button id="inc" onClick={() => setN(n + 1)}>
        inc
      </button>
    </div>
  );
}
