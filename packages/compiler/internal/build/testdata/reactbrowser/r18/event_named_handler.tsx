import { useState } from "react";
import { log } from "../lib/log";

// A handler named by its declaration (onClick={bump}) behaves like an inline
// one: it sees its own instance's state and props, the event and its
// arguments, and one declaration can serve several elements.
function Row(props: { id: string; onPick: (id: string) => void }) {
  const [n, setN] = useState(0);
  function bump(e: any) {
    setN(n + 1);
    log("bump", props.id, n, e.type, arguments.length, e.currentTarget.id);
  }
  const reset = () => {
    setN(0);
    log("reset", props.id);
  };
  return (
    <li>
      <button id={"bump-" + props.id} onClick={bump}>
        bump {n}
      </button>
      <button id={"again-" + props.id} onClick={bump}>
        again
      </button>
      <button id={"reset-" + props.id} onClick={reset}>
        reset
      </button>
      <button id={"pick-" + props.id} onClick={() => props.onPick(props.id)}>
        pick
      </button>
    </li>
  );
}

export default function EventNamedHandler() {
  const [picked, setPicked] = useState("none");
  const ids = ["a", "b", "c"];
  function pick(id: string) {
    setPicked(id);
    log("pick", id);
  }
  function onKey(e: any) {
    log("key", e.key, e.currentTarget.tagName.toLowerCase());
  }
  return (
    <div>
      <p id="picked">{picked}</p>
      <input id="field" onKeyDown={onKey} />
      <ul>
        {ids.map((id) => (
          <Row key={id} id={id} onPick={pick} />
        ))}
      </ul>
      {ids.map((id) => (
        <button key={id} id={"direct-" + id} onClick={() => pick(id)}>
          {id}
        </button>
      ))}
    </div>
  );
}
