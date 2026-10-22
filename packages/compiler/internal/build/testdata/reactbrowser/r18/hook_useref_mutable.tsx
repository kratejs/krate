import { useRef, useState } from "react";
import { log } from "../lib/log";

// Mutating ref.current does not re-render; the value shows up once it is
// copied into state. Reading ref.current during render is covered by
// diag_ref_read_in_render (React discourages it; Krate reports KR5008).
export default function HookUseRefMutable() {
  const clicks = useRef(0);
  const [shown, setShown] = useState(0);
  return (
    <div>
      <p id="shown">shown {shown}</p>
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
