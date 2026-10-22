import { useState } from "react";

function Row(props: { name: string }) {
  const [votes, setVotes] = useState(0);
  return (
    <li>
      {props.name}: {votes}
      <button className="vote" data-name={props.name} onClick={() => setVotes(votes + 1)}>
        +
      </button>
    </li>
  );
}

// Keyed rows keep their own state when the list is reordered.
export default function KeyReorderStateful() {
  const [names, setNames] = useState(["ann", "bob", "cid"]);
  return (
    <div>
      <ul>
        {names.map((n) => (
          <Row key={n} name={n} />
        ))}
      </ul>
      <button id="reverse" onClick={() => setNames([...names].reverse())}>
        reverse
      </button>
      <button id="sort" onClick={() => setNames([...names].sort())}>
        sort
      </button>
    </div>
  );
}
