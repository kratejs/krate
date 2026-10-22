import { useEffect, useSyncExternalStore } from "react";
import { createStore, log } from "../lib/log";

const cartStore = createStore(0);
if (typeof window !== "undefined") {
  (window as unknown as { __store: typeof cartStore }).__store = cartStore;
}

function Badge() {
  const count = useSyncExternalStore(cartStore.subscribe, cartStore.get, cartStore.get);
  useEffect(() => {
    log("badge sees", count);
  }, [count]);
  return <span id="badge">{count}</span>;
}

// Components subscribed with useSyncExternalStore re-render when the store
// changes, including changes made outside React.
export default function HookUseSyncExternalStore() {
  const count = useSyncExternalStore(cartStore.subscribe, cartStore.get, cartStore.get);
  return (
    <div>
      <Badge />
      <p id="total">total {count}</p>
      <button id="add" onClick={() => cartStore.set(cartStore.get() + 1)}>
        add to cart
      </button>
    </div>
  );
}
