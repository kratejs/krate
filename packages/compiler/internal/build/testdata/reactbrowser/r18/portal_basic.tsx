import { useState } from "react";
import { createPortal } from "react-dom";
import { log } from "../lib/log";

// A portal renders into document.body, but its events bubble through the
// React tree to the component that rendered it.
export default function PortalBasic() {
  const [count, setCount] = useState(0);
  return (
    <div id="host" onClick={() => log("host saw click")}>
      <p id="count">clicks {count}</p>
      {createPortal(
        <div id="toast" className="toast">
          <button id="toast-btn" onClick={() => setCount(count + 1)}>
            toast {count}
          </button>
        </div>,
        document.body,
      )}
    </div>
  );
}
