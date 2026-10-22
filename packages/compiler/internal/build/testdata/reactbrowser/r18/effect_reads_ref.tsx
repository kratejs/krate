import { useEffect, useRef, useState } from "react";
import { log } from "../lib/log";

// Refs are attached before effects run, so an effect can measure or focus
// the element and read the latest value kept in a ref.
export default function EffectReadsRef() {
  const inputRef = useRef<HTMLInputElement>(null);
  const renders = useRef(0);
  const [editing, setEditing] = useState(false);

  useEffect(() => {
    renders.current += 1;
    log("effect run", renders.current, "has input", inputRef.current !== null);
    if (editing && inputRef.current) {
      inputRef.current.focus();
      log("focused", document.activeElement === inputRef.current);
    }
  }, [editing]);

  return (
    <div>
      {editing ? <input id="name" ref={inputRef} defaultValue="draft" /> : <span id="label">draft</span>}
      <button id="edit" onClick={() => setEditing(!editing)}>
        {editing ? "done" : "edit"}
      </button>
    </div>
  );
}
