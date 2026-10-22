import { useState } from "react";

// Hooks inside a loop break the Rules of Hooks; Krate must report it.
export default function DiagHookInLoop() {
  const fields = ["first", "last"];
  const values: string[] = [];
  for (const f of fields) {
    const [v] = useState(f);
    values.push(v);
  }
  return <p>{values.join(" ")}</p>;
}
