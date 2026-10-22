import { useState } from "react";

// A focused, uncontrolled input inside a keyed row keeps its typed text and
// focus when rows move around it.
export default function KeyFocusedInputReorder() {
  const [rows, setRows] = useState(["x", "y", "z"]);
  return (
    <div>
      {rows.map((r) => (
        <div key={r} className="row">
          <label>{r}</label>
          <input id={"in-" + r} placeholder={r} />
        </div>
      ))}
      <button
        id="rotate"
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => setRows([rows[rows.length - 1], ...rows.slice(0, -1)])}
      >
        rotate
      </button>
    </div>
  );
}
