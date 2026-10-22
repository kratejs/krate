import { useCallback, useState } from "react";
import { log } from "../lib/log";

// A stable callback ref is called with the node on mount and with null on
// unmount.
export default function RefCallbackMountUnmount() {
  const [show, setShow] = useState(true);
  const [n, setN] = useState(0);
  const measure = useCallback((node: HTMLDivElement | null) => {
    log("ref", node ? node.tagName + " " + node.textContent : "null");
  }, []);
  return (
    <div>
      {show && (
        <div id="panel" ref={measure}>
          panel
        </div>
      )}
      <p id="n">{n}</p>
      <button id="toggle" onClick={() => setShow(!show)}>
        toggle
      </button>
      <button id="rerender" onClick={() => setN(n + 1)}>
        rerender
      </button>
    </div>
  );
}
