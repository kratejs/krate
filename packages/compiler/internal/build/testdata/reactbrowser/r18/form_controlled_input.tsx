import { useState } from "react";

// A controlled text input reflects state; setting state from elsewhere
// updates the field.
export default function FormControlledInput() {
  const [email, setEmail] = useState("ada@example.com");
  const valid = /^[^@\s]+@[^@\s]+\.[a-z]+$/.test(email);
  return (
    <div>
      <input id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} aria-invalid={!valid} />
      <p id="status">{valid ? "valid" : "invalid"}</p>
      <button id="clear" onClick={() => setEmail("")}>
        clear
      </button>
      <button id="preset" onClick={() => setEmail("grace@navy.mil")}>
        preset
      </button>
    </div>
  );
}
