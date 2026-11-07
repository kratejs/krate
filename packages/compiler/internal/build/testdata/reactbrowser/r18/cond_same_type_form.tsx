import { useEffect, useRef, useState } from "react";
import { log } from "../lib/log";

function Form(props: { mode: string; hint?: string }) {
  const [text, setText] = useState("");
  const [n, setN] = useState(0);
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    log("mount", props.mode, ref.current ? "ref" : "noref");
    return () => log("unmount", props.mode);
  }, []);
  return (
    <form>
      <h3 className="mode">{props.mode}</h3>
      <input id="in" ref={ref} value={text} onChange={(e) => setText(e.target.value)} />
      <button type="button" className="inc" onClick={() => setN(n + 1)}>n={n}</button>
      <em className="hint">{props.hint ?? "none"}</em>
    </form>
  );
}

// Same component type in both ternary branches keeps its instance.
export default function CondSameTypeForm() {
  const [isEdit, setEdit] = useState(false);
  return (
    <div>
      <button id="flip" onClick={() => setEdit((v) => !v)}>flip {String(isEdit)}</button>
      {isEdit ? <Form mode="edit" hint="h" /> : <Form mode="new" />}
    </div>
  );
}
