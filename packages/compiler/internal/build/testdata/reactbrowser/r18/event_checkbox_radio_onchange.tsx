import { useState } from "react";
import { log } from "../lib/log";

// onChange on checkboxes and radios fires on click with the new checked
// state.
export default function EventCheckboxRadioOnChange() {
  const [agree, setAgree] = useState(false);
  const [size, setSize] = useState("m");
  return (
    <form>
      <label>
        <input
          id="agree"
          type="checkbox"
          checked={agree}
          onChange={(e) => {
            log("agree change", e.target.checked);
            setAgree(e.target.checked);
          }}
        />
        agree
      </label>
      {["s", "m", "l"].map((s) => (
        <label key={s}>
          <input
            id={"size-" + s}
            type="radio"
            name="size"
            value={s}
            checked={size === s}
            onChange={(e) => {
              log("size change", e.target.value);
              setSize(e.target.value);
            }}
          />
          {s}
        </label>
      ))}
      <p id="state">
        {String(agree)} {size}
      </p>
    </form>
  );
}
