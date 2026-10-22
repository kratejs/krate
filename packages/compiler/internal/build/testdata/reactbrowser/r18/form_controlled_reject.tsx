import { useState } from "react";
import { log } from "../lib/log";

// A controlled input that rejects some changes: the DOM value snaps back to
// state when the handler does not accept the edit.
export default function FormControlledReject() {
  const [pin, setPin] = useState("");
  const [code, setCode] = useState("AB");
  return (
    <div>
      <input
        id="pin"
        inputMode="numeric"
        value={pin}
        onChange={(e) => {
          const next = e.target.value;
          if (/^\d{0,4}$/.test(next)) setPin(next);
          else log("rejected", next);
        }}
      />
      <input id="code" value={code} onChange={(e) => setCode(e.target.value.toUpperCase())} />
      <input id="frozen" value="read only by state" onChange={() => log("frozen change")} />
      <p id="state">
        {pin}|{code}
      </p>
    </div>
  );
}
