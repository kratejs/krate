import { useState } from "react";
import { log } from "../lib/log";

// Controlled checkboxes: "select all" derives from children, and a locked
// checkbox refuses to toggle.
export default function FormControlledCheckbox() {
  const [items, setItems] = useState({ email: true, sms: false, push: false });
  const all = items.email && items.sms && items.push;
  return (
    <div>
      <label>
        <input
          id="all"
          type="checkbox"
          checked={all}
          onChange={(e) => setItems({ email: e.target.checked, sms: e.target.checked, push: e.target.checked })}
        />
        all
      </label>
      {(Object.keys(items) as (keyof typeof items)[]).map((k) => (
        <label key={k}>
          <input id={k} type="checkbox" checked={items[k]} onChange={() => setItems({ ...items, [k]: !items[k] })} />
          {k}
        </label>
      ))}
      <label>
        <input id="locked" type="checkbox" checked={true} onChange={() => log("locked toggle refused")} />
        locked
      </label>
    </div>
  );
}
