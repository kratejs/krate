import { useRef, useState } from "react";
import { log } from "../lib/log";

// Mutating ref.current does not re-render; the value shows up on the next
// render caused by state.
export default function HookUseRefMutable() {
  const clicks = useRef(0);
  const [shown, setShown] = useState(0);
  return (
    <div>
      <p id="shown">shown {shown}</p>
      <p id="live">ref at render {clicks.current}</p>
      <button
        id="silent"
        onClick={() => {
          clicks.current += 1;
          log("silent click", clicks.current);
        }}
      >
        silent
      </button>
      <button id="show" onClick={() => setShown(clicks.current)}>
        show
      </button>
    </div>
  );
}
