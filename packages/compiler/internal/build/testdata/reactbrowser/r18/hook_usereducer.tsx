import { useReducer } from "react";
import { log } from "../lib/log";

type State = { items: string[]; total: number };
type Action = { type: "add"; name: string; price: number } | { type: "remove"; name: string } | { type: "clear" };

function cart(state: State, action: Action): State {
  switch (action.type) {
    case "add":
      return { items: [...state.items, action.name], total: state.total + action.price };
    case "remove": {
      const idx = state.items.indexOf(action.name);
      if (idx < 0) return state;
      return { items: state.items.filter((_, i) => i !== idx), total: state.total - 10 };
    }
    case "clear":
      return { items: [], total: 0 };
  }
}

// useReducer with several dispatches in one handler: they are batched into a
// single render and applied in order.
export default function HookUseReducer() {
  const [state, dispatch] = useReducer(cart, { items: [], total: 0 });
  return (
    <div>
      <p id="summary">
        {state.items.length} items, ${state.total}
      </p>
      <ol>
        {state.items.map((it, i) => (
          <li key={i}>{it}</li>
        ))}
      </ol>
      <button
        id="bundle"
        onClick={() => {
          dispatch({ type: "add", name: "pen", price: 10 });
          dispatch({ type: "add", name: "ink", price: 10 });
          log("dispatched bundle");
        }}
      >
        add bundle
      </button>
      <button id="remove" onClick={() => dispatch({ type: "remove", name: "pen" })}>
        remove pen
      </button>
      <button id="clear" onClick={() => dispatch({ type: "clear" })}>
        clear
      </button>
    </div>
  );
}
