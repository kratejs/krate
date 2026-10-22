import { useState } from "react";

const LIMIT = 20;

// A controlled textarea with a character limit enforced in the handler.
export default function FormControlledTextarea() {
  const [bio, setBio] = useState("Hello");
  return (
    <div>
      <textarea id="bio" value={bio} onChange={(e) => setBio(e.target.value.slice(0, LIMIT))} rows={3} />
      <p id="left">{LIMIT - bio.length} left</p>
      <button id="reset" onClick={() => setBio("Hello")}>
        reset
      </button>
    </div>
  );
}
