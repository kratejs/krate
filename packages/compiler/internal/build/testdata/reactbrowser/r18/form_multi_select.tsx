import { useState } from "react";

// A controlled multi-select takes an array value.
export default function FormMultiSelect() {
  const [toppings, setToppings] = useState<string[]>(["cheese"]);
  return (
    <div>
      <select
        id="toppings"
        multiple
        value={toppings}
        onChange={(e) => setToppings([...e.target.selectedOptions].map((o) => o.value))}
      >
        <option value="cheese">Cheese</option>
        <option value="olives">Olives</option>
        <option value="basil">Basil</option>
      </select>
      <p id="picked">{toppings.join("+") || "none"}</p>
      <button id="all" onClick={() => setToppings(["cheese", "olives", "basil"])}>
        everything
      </button>
      <button id="none" onClick={() => setToppings([])}>
        nothing
      </button>
    </div>
  );
}
