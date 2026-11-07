import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Detail(props: { id: string }) {
  useEffect(() => {
    log("detail mount", props.id);
    return () => log("detail unmount", props.id);
  }, []);
  return <p className="detail">detail {props.id}</p>;
}

function Row(props: { id: string }) {
  const [open, setOpen] = useState(false);
  return (
    <li data-id={props.id}>
      <button className="t" onClick={() => setOpen(!open)}>{props.id}</button>
      {open && <Detail id={props.id} />}
    </li>
  );
}

export default function ListRowToggleMove() {
  const [ids, setIds] = useState(["a", "b", "c"]);
  return (
    <div>
      <button id="rev" onClick={() => setIds([...ids].reverse())}>rev</button>
      <button id="drop" onClick={() => setIds(ids.slice(1))}>drop</button>
      <ul>{ids.map((id) => <Row key={id} id={id} />)}</ul>
    </div>
  );
}
