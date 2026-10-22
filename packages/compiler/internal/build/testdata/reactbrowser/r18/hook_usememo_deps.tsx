import { useMemo, useState } from "react";
import { log } from "../lib/log";

const products = [
  { name: "Keyboard", price: 80 },
  { name: "Mouse", price: 25 },
  { name: "Monitor", price: 240 },
  { name: "Cable", price: 8 },
];

// useMemo recomputes only when its dependencies change; unrelated state
// updates reuse the cached value.
export default function HookUseMemoDeps() {
  const [max, setMax] = useState(100);
  const [highlight, setHighlight] = useState(false);

  const visible = useMemo(() => {
    log("filter products under", max);
    return products.filter((p) => p.price <= max);
  }, [max]);

  return (
    <div>
      <ul className={highlight ? "hl" : ""}>
        {visible.map((p) => (
          <li key={p.name}>
            {p.name} ${p.price}
          </li>
        ))}
      </ul>
      <button id="cheap" onClick={() => setMax(30)}>
        under 30
      </button>
      <button id="all" onClick={() => setMax(1000)}>
        all
      </button>
      <button id="hl" onClick={() => setHighlight(!highlight)}>
        highlight
      </button>
    </div>
  );
}
