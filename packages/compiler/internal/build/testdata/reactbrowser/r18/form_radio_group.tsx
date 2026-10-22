import { useState } from "react";

const plans = [
  { id: "basic", price: 0 },
  { id: "team", price: 12 },
  { id: "enterprise", price: 40 },
];

// A controlled radio group; changing state elsewhere moves the selection.
export default function FormRadioGroup() {
  const [plan, setPlan] = useState("team");
  const price = plans.find((p) => p.id === plan)!.price;
  return (
    <fieldset>
      <legend>Plan</legend>
      {plans.map((p) => (
        <label key={p.id}>
          <input id={p.id} type="radio" name="plan" value={p.id} checked={plan === p.id} onChange={() => setPlan(p.id)} />
          {p.id}
        </label>
      ))}
      <p id="price">${price}/mo</p>
      <button id="downgrade" type="button" onClick={() => setPlan("basic")}>
        downgrade
      </button>
    </fieldset>
  );
}
