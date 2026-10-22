import { useRef, useState } from "react";
import { log } from "../lib/log";

// Refs for list items kept in a Map via callback refs; scrolling to or
// focusing an item looks it up by id.
export default function RefListRefs() {
  const [ids, setIds] = useState(["a", "b", "c"]);
  const nodes = useRef(new Map<string, HTMLButtonElement>());
  return (
    <div>
      <ul>
        {ids.map((id) => (
          <li key={id}>
            <button
              id={"item-" + id}
              ref={(node) => {
                if (node) nodes.current.set(id, node);
                else nodes.current.delete(id);
              }}
            >
              {id}
            </button>
          </li>
        ))}
      </ul>
      <button id="focus-c" onClick={() => nodes.current.get("c")?.focus()}>
        focus c
      </button>
      <button id="drop-a" onClick={() => setIds(ids.filter((x) => x !== "a"))}>
        drop a
      </button>
      <button id="count" onClick={() => log("tracked", [...nodes.current.keys()].join(","))}>
        count
      </button>
    </div>
  );
}
