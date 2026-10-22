import { useRef, useState } from "react";
import { log } from "../lib/log";

// An object ref points at the DOM node; handlers can focus and read it.
export default function RefObjectDom() {
  const input = useRef<HTMLInputElement>(null);
  const [len, setLen] = useState(-1);
  return (
    <div>
      <input id="field" ref={input} defaultValue="abc" />
      <button id="focus" onClick={() => input.current!.focus()}>
        focus
      </button>
      <button
        id="measure"
        onClick={() => {
          log("tag", input.current!.tagName, "value", input.current!.value);
          setLen(input.current!.value.length);
        }}
      >
        measure
      </button>
      <p id="len">{len}</p>
    </div>
  );
}
