import { useEffect, useState } from "react";
import { log } from "../lib/log";

// Setting state to the current value (Object.is) bails out: no re-render
// and no effect runs.
export default function HookUseStateBailout() {
  const [status, setStatus] = useState("idle");
  const [filters, setFilters] = useState({ q: "" });

  useEffect(() => {
    log("status effect", status);
  });

  return (
    <div>
      <p id="status">
        {status} q={filters.q}
      </p>
      <button id="idle" onClick={() => setStatus("idle")}>
        idle again
      </button>
      <button id="busy" onClick={() => setStatus("busy")}>
        busy
      </button>
      <button id="same-object" onClick={() => setFilters(filters)}>
        same object
      </button>
      <button id="new-object" onClick={() => setFilters({ ...filters })}>
        copied object
      </button>
    </div>
  );
}
