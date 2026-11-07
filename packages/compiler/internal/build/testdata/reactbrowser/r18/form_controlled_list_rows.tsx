import { useState } from "react";
import { log } from "../lib/log";

export default function FormControlledListRows() {
  const [rows, setRows] = useState([
    { id: 1, v: "b", on: false },
    { id: 2, v: "a", on: true },
  ]);
  const patch = (id: number, p: object) => setRows((rs) => rs.map((r) => (r.id === id ? { ...r, ...p } : r)));
  return (
    <div>
      <button id="rev" onClick={() => setRows((rs) => [...rs].reverse())}>rev</button>
      <button id="add" onClick={() => setRows((rs) => [{ id: rs.length + 1, v: "a", on: false }, ...rs])}>add</button>
      {rows.map((r) => (
        <label key={r.id} data-id={r.id}>
          <select value={r.v} onChange={(e) => { log("select", r.id, e.target.value); patch(r.id, { v: e.target.value }); }}>
            <option value="a">A</option>
            <option value="b">B</option>
          </select>
          <input type="checkbox" checked={r.on} onChange={(e) => { log("check", r.id, e.target.checked); patch(r.id, { on: e.target.checked }); }} />
        </label>
      ))}
    </div>
  );
}
