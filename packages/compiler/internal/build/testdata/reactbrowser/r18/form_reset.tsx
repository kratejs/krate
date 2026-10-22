import { useState } from "react";
import { log } from "../lib/log";

// Native form reset restores uncontrolled fields to their defaults; an
// onReset handler can reset related state.
export default function FormReset() {
  const [dirty, setDirty] = useState(false);
  return (
    <form
      id="settings"
      onChange={() => setDirty(true)}
      onReset={() => {
        log("reset");
        setDirty(false);
      }}
    >
      <input id="nick" name="nick" defaultValue="ada" />
      <input id="alerts" name="alerts" type="checkbox" defaultChecked />
      <textarea id="about" name="about" defaultValue="mathematician" />
      <select id="lang" name="lang" defaultValue="en">
        <option value="en">en</option>
        <option value="fr">fr</option>
      </select>
      <button id="reset" type="reset">
        reset
      </button>
      <p id="dirty">{dirty ? "unsaved changes" : "clean"}</p>
    </form>
  );
}
