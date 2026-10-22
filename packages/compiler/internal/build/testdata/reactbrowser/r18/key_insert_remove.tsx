import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Item(props: { id: number; text: string }) {
  useEffect(() => {
    log("mount", props.id);
    return () => log("unmount", props.id);
  }, []);
  return <li data-id={props.id}>{props.text}</li>;
}

// Inserting at the front and removing from the middle mounts and unmounts
// only the affected keys.
export default function KeyInsertRemove() {
  const [items, setItems] = useState([
    { id: 1, text: "one" },
    { id: 2, text: "two" },
    { id: 3, text: "three" },
  ]);
  const [next, setNext] = useState(4);
  return (
    <div>
      <ul>
        {items.map((it) => (
          <Item key={it.id} id={it.id} text={it.text} />
        ))}
      </ul>
      <button
        id="prepend"
        onClick={() => {
          setItems([{ id: next, text: "new " + next }, ...items]);
          setNext(next + 1);
        }}
      >
        prepend
      </button>
      <button id="remove-2" onClick={() => setItems(items.filter((it) => it.id !== 2))}>
        remove two
      </button>
    </div>
  );
}
