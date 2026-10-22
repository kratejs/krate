import { useCallback, useState } from "react";
import { log } from "../lib/log";

// React 19: a ref callback may return a cleanup function, which runs on
// detach instead of calling the callback with null.
export default function RefCallbackCleanup() {
  const [show, setShow] = useState(true);
  const observe = useCallback((node: HTMLDivElement) => {
    log("attach", node.id);
    const onScroll = () => {};
    node.addEventListener("scroll", onScroll);
    return () => {
      node.removeEventListener("scroll", onScroll);
      log("cleanup", node.id);
    };
  }, []);
  return (
    <div>
      {show && (
        <div id="feed" ref={observe}>
          feed
        </div>
      )}
      <button id="toggle" onClick={() => setShow(!show)}>
        toggle
      </button>
    </div>
  );
}
