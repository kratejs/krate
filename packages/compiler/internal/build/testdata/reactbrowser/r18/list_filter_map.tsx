import { useState } from "react";

const people = [
  { id: 1, name: "Ada Lovelace", role: "math" },
  { id: 2, name: "Grace Hopper", role: "navy" },
  { id: 3, name: "Alan Turing", role: "math" },
  { id: 4, name: "Linus Torvalds", role: "kernel" },
];

// Filtered and mapped list with an empty state and a count.
export default function ListFilterMap() {
  const [q, setQ] = useState("");
  const [role, setRole] = useState("all");
  const shown = people.filter(
    (p) => (role === "all" || p.role === role) && p.name.toLowerCase().includes(q.toLowerCase()),
  );
  return (
    <div>
      <input id="q" value={q} onChange={(e) => setQ(e.target.value)} placeholder="filter" />
      <select id="role" value={role} onChange={(e) => setRole(e.target.value)}>
        <option value="all">all</option>
        <option value="math">math</option>
        <option value="navy">navy</option>
      </select>
      <p id="count">
        {shown.length} of {people.length}
      </p>
      {shown.length === 0 ? (
        <p id="empty">No matches</p>
      ) : (
        <ul>
          {shown.map((p) => (
            <li key={p.id}>
              {p.name} <small>{p.role}</small>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
