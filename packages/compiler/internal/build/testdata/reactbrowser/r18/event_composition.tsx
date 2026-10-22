import { useState } from "react";
import { log } from "../lib/log";

// Composition events (IME input) reach onCompositionStart/Update/End with
// their data; an app can hold off validation while composing.
export default function EventComposition() {
  const [composing, setComposing] = useState(false);
  const [last, setLast] = useState("");
  return (
    <div>
      <input
        id="ime"
        onCompositionStart={(e) => {
          log("start", e.data);
          setComposing(true);
        }}
        onCompositionUpdate={(e) => log("update", e.data)}
        onCompositionEnd={(e) => {
          log("end", e.data);
          setComposing(false);
          setLast(e.data);
        }}
      />
      <p id="state">
        {composing ? "composing" : "idle"} last={last}
      </p>
    </div>
  );
}
