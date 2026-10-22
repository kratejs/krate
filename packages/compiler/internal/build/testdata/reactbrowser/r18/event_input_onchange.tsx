import { useState } from "react";
import { log } from "../lib/log";

// React's onChange on a text input fires on every keystroke (native input
// event), not on blur like the DOM change event.
export default function EventInputOnChange() {
  const [value, setValue] = useState("");
  return (
    <div>
      <input
        id="search"
        placeholder="Search"
        value={value}
        onChange={(e) => {
          log("change", e.target.value, e.type);
          setValue(e.target.value);
        }}
        onBlur={() => log("blur")}
      />
      <p id="echo">{value.length ? "searching " + value : "type something"}</p>
      <button id="other">other</button>
    </div>
  );
}
