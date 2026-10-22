import { useState } from "react";
import { log } from "../lib/log";

// onMouseEnter/onMouseLeave do not bubble: moving between children of the
// card does not re-fire the card's handlers.
export default function EventMouseEnterLeave() {
  const [hover, setHover] = useState("");
  return (
    <div>
      <div
        id="card"
        style={{ padding: 20, display: "flex", gap: 10 }}
        onMouseEnter={() => {
          log("card enter");
          setHover("card");
        }}
        onMouseLeave={() => {
          log("card leave");
          setHover("");
        }}
      >
        <button id="a" onMouseEnter={() => log("a enter")} onMouseLeave={() => log("a leave")}>
          a
        </button>
        <button id="b" onMouseEnter={() => log("b enter")}>
          b
        </button>
      </div>
      <p id="far" style={{ marginTop: 100 }}>
        hover={hover}
      </p>
    </div>
  );
}
