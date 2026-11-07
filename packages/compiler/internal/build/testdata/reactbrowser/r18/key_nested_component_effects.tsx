import { useEffect, useLayoutEffect, useState } from "react";
import { log } from "../lib/log";

function Leaf(props: { id: string; n: number }) {
  useEffect(() => {
    log("leaf mount", props.id);
    return () => log("leaf unmount", props.id);
  }, []);
  return <i data-leaf={props.id + props.n}>{props.id}{props.n}</i>;
}

function Row(props: { id: string }) {
  const [c, setC] = useState(0);
  useLayoutEffect(() => {
    log("row layout", props.id);
    return () => log("row layout cleanup", props.id);
  }, [props.id]);
  useEffect(() => {
    log("row effect", props.id, c);
    return () => log("row effect cleanup", props.id, c);
  }, [c]);
  return (
    <li data-id={props.id}>
      <button className="inc" onClick={() => setC(c + 1)}>{props.id}:{c}</button>
      {[1, 2].map((n) => (
        <Leaf key={props.id + n} id={props.id} n={n} />
      ))}
    </li>
  );
}

export default function KeyNestedComponentEffects() {
  const [ids, setIds] = useState(["a", "b", "c", "d"]);
  return (
    <div>
      <button id="rev" onClick={() => setIds([...ids].reverse())}>rev</button>
      <button id="del" onClick={() => setIds(ids.filter((x) => x !== "b"))}>del</button>
      <button id="ins" onClick={() => setIds(["z", ...ids])}>ins</button>
      <button id="mix" onClick={() => setIds(["d", "x", "a"])}>mix</button>
      <ul>{ids.map((id) => <Row key={id} id={id} />)}</ul>
    </div>
  );
}
