import { createContext, useContext, useState } from "react";

const Sel = createContext<string>("");

function Row(props: { id: string; onPick: (id: string) => void }) {
  const sel = useContext(Sel);
  return (
    <li id={"row-" + props.id} className={sel === props.id ? "sel" : ""} onClick={() => props.onPick(props.id)}>
      {props.id}{sel === props.id ? " *" : ""}
    </li>
  );
}

export default function ContextListRows() {
  const [sel, setSel] = useState("a");
  const [ids, setIds] = useState(["a", "b", "c"]);
  return (
    <div>
      <button id="add" onClick={() => setIds([...ids, "d"])}>add</button>
      <Sel.Provider value={sel}>
        <ul>
          {ids.map((id) => <Row key={id} id={id} onPick={setSel} />)}
        </ul>
      </Sel.Provider>
    </div>
  );
}
