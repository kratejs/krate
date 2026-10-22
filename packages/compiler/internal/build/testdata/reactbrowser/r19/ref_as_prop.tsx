import { useRef, type Ref } from "react";
import { log } from "../lib/log";

function TextField(props: { label: string; ref?: Ref<HTMLInputElement> }) {
  return (
    <label>
      {props.label}
      <input id="field" ref={props.ref} />
    </label>
  );
}

// React 19: ref is a regular prop on function components, no forwardRef.
export default function RefAsProp() {
  const ref = useRef<HTMLInputElement>(null);
  return (
    <div>
      <TextField label="City" ref={ref} />
      <button
        id="focus"
        onClick={() => {
          ref.current?.focus();
          log("ref points at", ref.current?.id ?? "nothing");
        }}
      >
        focus
      </button>
    </div>
  );
}
