import { useReducer } from "react";
import { log } from "../lib/log";

function init(start: number) {
  log("init called with", start);
  return { count: start, history: [start] };
}

function reducer(state: { count: number; history: number[] }, action: "inc" | "reset") {
  if (action === "reset") return init(5);
  const next = state.count + 1;
  return { count: next, history: [...state.history, next] };
}

// The lazy initializer (third argument) runs once, not on every render.
export default function HookUseReducerInit() {
  const [state, dispatch] = useReducer(reducer, 5, init);
  return (
    <div>
      <p id="count">{state.count}</p>
      <p id="history">{state.history.join(",")}</p>
      <button id="inc" onClick={() => dispatch("inc")}>
        inc
      </button>
      <button id="reset" onClick={() => dispatch("reset")}>
        reset
      </button>
    </div>
  );
}
