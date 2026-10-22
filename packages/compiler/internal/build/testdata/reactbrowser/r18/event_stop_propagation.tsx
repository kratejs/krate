import { useState } from "react";
import { log } from "../lib/log";

// stopPropagation in a nested handler keeps the row's click handler from
// firing; a menu inside a clickable card is the usual case.
export default function EventStopPropagation() {
  const [selected, setSelected] = useState("");
  const [menu, setMenu] = useState("");
  return (
    <div>
      <div
        id="card"
        className="card"
        onClick={() => {
          log("card selected");
          setSelected("card");
        }}
      >
        <h3>Report</h3>
        <button
          id="menu"
          onClick={(e) => {
            e.stopPropagation();
            log("menu opened", e.isPropagationStopped());
            setMenu("open");
          }}
        >
          ...
        </button>
        <button id="plain">details</button>
      </div>
      <p id="state">
        selected={selected} menu={menu}
      </p>
    </div>
  );
}
