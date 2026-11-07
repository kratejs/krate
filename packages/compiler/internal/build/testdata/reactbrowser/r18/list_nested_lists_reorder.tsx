import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Item(props: { g: string; id: string }) {
  const [n, setN] = useState(0);
  useEffect(() => {
    log("item mount", props.g + props.id);
    return () => log("item unmount", props.g + props.id);
  }, []);
  return <li data-k={props.g + props.id}><button className="b" onClick={() => setN(n + 1)}>{props.g}{props.id}:{n}</button></li>;
}

export default function ListNestedListsReorder() {
  const [groups, setGroups] = useState([
    { id: "g1", items: ["1", "2", "3"] },
    { id: "g2", items: ["1", "2"] },
  ]);
  return (
    <div>
      <button id="swap-groups" onClick={() => setGroups([...groups].reverse())}>swap groups</button>
      <button id="rev-items" onClick={() => setGroups(groups.map((g) => ({ ...g, items: [...g.items].reverse() })))}>rev items</button>
      <button id="move-item" onClick={() => setGroups([{ ...groups[0], items: groups[0].items.slice(1) }, { ...groups[1], items: [...groups[1].items, "9"] }])}>move item</button>
      <button id="both" onClick={() => setGroups([...groups].reverse().map((g) => ({ ...g, items: [...g.items].reverse() })))}>both</button>
      {groups.map((g) => (
        <section key={g.id} data-g={g.id}>
          <h3>{g.id}</h3>
          <ul>{g.items.map((id) => <Item key={id} g={g.id} id={id} />)}</ul>
        </section>
      ))}
    </div>
  );
}
