import { useEffect, useState } from "react";
import { log } from "../lib/log";

// The dependency array is built in a variable before being passed in.
export default function EffectDepsVariable() {
  const [query, setQuery] = useState("a");
  const [ticks, setTicks] = useState(0);
  const deps = [query];

  useEffect(() => {
    log("search", query);
    return () => log("cancel", query);
  }, deps);

  return (
    <section>
      <p id="out">
        {query}/{ticks}
      </p>
      <button id="q" onClick={() => setQuery((q) => q + "b")}>
        change query
      </button>
      <button id="tick" onClick={() => setTicks((t) => t + 1)}>
        tick
      </button>
    </section>
  );
}
