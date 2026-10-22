import { useState } from "react";
import { log } from "../lib/log";

// React's onFocus and onBlur bubble (they map to focusin/focusout), so a
// fieldset can track whether any of its inputs has focus.
export default function EventFocusBlurBubble() {
  const [active, setActive] = useState(false);
  return (
    <div>
      <fieldset
        id="group"
        className={active ? "active" : ""}
        onFocus={(e) => {
          log("group focus", (e.target as HTMLElement).id);
          setActive(true);
        }}
        onBlur={(e) => {
          log("group blur", (e.target as HTMLElement).id, "related", (e.relatedTarget as HTMLElement | null)?.id ?? "none");
          setActive(false);
        }}
      >
        <input id="first" />
        <input id="second" />
      </fieldset>
      <button id="outside">outside</button>
      <p id="state">{active ? "editing" : "idle"}</p>
    </div>
  );
}
