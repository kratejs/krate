import { useState } from "react";
import { log } from "../lib/log";

export default function RefCallbackInlineListReorder() {
  const [ids, setIds] = useState(["a", "b", "c"]);
  return (
    <div>
      <button id="rev" onClick={() => setIds([...ids].reverse())}>rev</button>
      <button id="del" onClick={() => setIds(ids.filter((x) => x !== "a"))}>del</button>
      <button id="add" onClick={() => setIds([...ids, "n" + ids.length])}>add</button>
      <ul>
        {ids.map((id) => (
          <li key={id} data-id={id} ref={(el) => log("ref", id, el ? "attach" : "detach")}>
            {id}
          </li>
        ))}
      </ul>
    </div>
  );
}
