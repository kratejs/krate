import { useMemo, useState } from "react";

type Row = { id: number; name: string; team: string; score: number };

const rows: Row[] = [
  { id: 1, name: "Ada", team: "red", score: 92 },
  { id: 2, name: "Bob", team: "blue", score: 75 },
  { id: 3, name: "Cy", team: "red", score: 81 },
  { id: 4, name: "Di", team: "blue", score: 99 },
  { id: 5, name: "Ed", team: "green", score: 60 },
];

type SortKey = "name" | "score";

// Data table: sort by column (toggling direction), text filter, per-row and
// select-all checkboxes (with indeterminate state) scoped to visible rows.
export default function AppDataTable() {
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: "name", dir: 1 });
  const [q, setQ] = useState("");
  const [selected, setSelected] = useState<Set<number>>(new Set());

  const visible = useMemo(() => {
    const f = rows.filter((r) => (r.name + " " + r.team).toLowerCase().includes(q.toLowerCase()));
    return [...f].sort((a, b) => (a[sort.key] < b[sort.key] ? -1 : a[sort.key] > b[sort.key] ? 1 : 0) * sort.dir);
  }, [q, sort]);

  const visibleSelected = visible.filter((r) => selected.has(r.id)).length;
  const all = visible.length > 0 && visibleSelected === visible.length;
  const some = visibleSelected > 0 && !all;

  const toggleSort = (key: SortKey) => setSort(sort.key === key ? { key, dir: sort.dir === 1 ? -1 : 1 } : { key, dir: 1 });
  const toggle = (id: number) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelected(next);
  };

  return (
    <div>
      <input id="filter" value={q} onChange={(e) => setQ(e.target.value)} placeholder="filter" />
      <table>
        <thead>
          <tr>
            <th>
              <input
                id="all"
                type="checkbox"
                checked={all}
                ref={(el) => {
                  if (el) el.indeterminate = some;
                }}
                onChange={() => {
                  const next = new Set(selected);
                  visible.forEach((r) => (all ? next.delete(r.id) : next.add(r.id)));
                  setSelected(next);
                }}
              />
            </th>
            <th>
              <button id="sort-name" onClick={() => toggleSort("name")}>
                name {sort.key === "name" ? (sort.dir === 1 ? "asc" : "desc") : ""}
              </button>
            </th>
            <th>team</th>
            <th>
              <button id="sort-score" onClick={() => toggleSort("score")}>
                score {sort.key === "score" ? (sort.dir === 1 ? "asc" : "desc") : ""}
              </button>
            </th>
          </tr>
        </thead>
        <tbody>
          {visible.map((r) => (
            <tr key={r.id} className={selected.has(r.id) ? "selected" : ""}>
              <td>
                <input type="checkbox" className="row-check" data-id={r.id} checked={selected.has(r.id)} onChange={() => toggle(r.id)} />
              </td>
              <td>{r.name}</td>
              <td>{r.team}</td>
              <td>{r.score}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p id="status">
        {selected.size} selected, {visible.length} shown{some ? ", partial" : ""}
      </p>
    </div>
  );
}
