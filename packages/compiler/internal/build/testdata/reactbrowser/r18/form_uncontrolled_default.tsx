import { useRef, useState } from "react";

// Uncontrolled inputs with defaultValue/defaultChecked keep user edits even
// when the component re-renders; the default only applies initially.
export default function FormUncontrolledDefault() {
  const [renders, setRenders] = useState(0);
  const [read, setRead] = useState("");
  const nameRef = useRef<HTMLInputElement>(null);
  const newsRef = useRef<HTMLInputElement>(null);
  return (
    <div>
      <input id="name" ref={nameRef} defaultValue={"guest " + renders} />
      <input id="news" ref={newsRef} type="checkbox" defaultChecked={renders % 2 === 0} />
      <select id="plan" defaultValue="pro">
        <option value="free">free</option>
        <option value="pro">pro</option>
      </select>
      <button id="rerender" onClick={() => setRenders(renders + 1)}>
        rerender {renders}
      </button>
      <button id="read" onClick={() => setRead(nameRef.current!.value + "/" + newsRef.current!.checked)}>
        read
      </button>
      <p id="read-out">{read}</p>
    </div>
  );
}
