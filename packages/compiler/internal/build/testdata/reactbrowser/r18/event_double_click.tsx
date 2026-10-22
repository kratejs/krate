import { useState } from "react";
import { log } from "../lib/log";

// Double click fires click twice and then onDoubleClick; the label switches
// to an input for editing.
export default function EventDoubleClick() {
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState("Untitled");
  return (
    <div>
      {editing ? (
        <input
          id="title-input"
          autoFocus
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          onBlur={() => setEditing(false)}
        />
      ) : (
        <h2
          id="title"
          onClick={() => log("click")}
          onDoubleClick={() => {
            log("dblclick");
            setEditing(true);
          }}
        >
          {title}
        </h2>
      )}
      <button id="done">done</button>
    </div>
  );
}
