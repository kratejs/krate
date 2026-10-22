import { useEffect, useLayoutEffect, useState } from "react";
import { log } from "../lib/log";

// Effects observe the committed DOM: by the time a layout or passive effect
// runs, the text and the number of rows reflect the new state.
export default function EffectReadsDom() {
  const [items, setItems] = useState(["apple"]);

  useLayoutEffect(() => {
    const rows = document.querySelectorAll("#list li").length;
    log("layout sees rows", rows, document.getElementById("count")!.textContent);
  }, [items]);

  useEffect(() => {
    const rows = document.querySelectorAll("#list li").length;
    log("passive sees rows", rows);
  }, [items]);

  return (
    <div>
      <p id="count">{items.length} items</p>
      <ul id="list">
        {items.map((it) => (
          <li key={it}>{it}</li>
        ))}
      </ul>
      <button id="add" onClick={() => setItems([...items, "item" + items.length])}>
        add
      </button>
    </div>
  );
}
