import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Item(props: { id: number; onRemove: () => void }) {
  const [n, setN] = useState(0);
  useEffect(() => {
    log("mount", props.id);
    return () => log("unmount", props.id);
  }, []);
  return (
    <li data-id={props.id}>
      <button
        className="rm"
        onClick={() => {
          props.onRemove();
          setN(n + 1);
          log("handler done", props.id, n);
          setTimeout(() => setN((x) => x + 1), 0);
        }}
      >
        rm {props.id}:{n}
      </button>
    </li>
  );
}

export default function HuntUnmountInHandler() {
  const [items, setItems] = useState([1, 2, 3]);
  return (
    <ul>
      {items.map((id) => (
        <Item key={id} id={id} onRemove={() => setItems((l) => l.filter((x) => x !== id))} />
      ))}
    </ul>
  );
}
