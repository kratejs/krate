import { useState } from "react";
import { log } from "../lib/log";

export default function ListFocusEventsReorder() {
  const [rows, setRows] = useState([
    { id: 1, text: "alpha" },
    { id: 2, text: "beta" },
    { id: 3, text: "gamma" },
  ]);
  const edit = (id: number, text: string) => setRows((rs) => rs.map((r) => (r.id === id ? { ...r, text } : r)));
  return (
    <div>
      <button id="sort" onClick={() => setRows((rs) => [...rs].sort((a, b) => a.text.localeCompare(b.text)))}>sort</button>
      <button id="rev" onClick={() => setRows((rs) => [...rs].reverse())}>rev</button>
      {rows.map((r) => (
        <div key={r.id} className="row" data-id={r.id}>
          <input
            value={r.text}
            onChange={(e) => edit(r.id, e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") setRows((rs) => [...rs].reverse());
            }}
            onFocus={() => log("focus", r.id)}
            onBlur={() => log("blur", r.id)}
          />
        </div>
      ))}
    </div>
  );
}
