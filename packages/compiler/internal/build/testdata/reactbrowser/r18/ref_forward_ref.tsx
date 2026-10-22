import { forwardRef, useRef } from "react";
import { log } from "../lib/log";

const FancyInput = forwardRef<HTMLInputElement, { label: string }>(function FancyInput(props, ref) {
  return (
    <label className="fancy">
      {props.label}
      <input id="fancy" ref={ref} />
    </label>
  );
});

// forwardRef passes the parent's ref through to a DOM node in the child.
export default function RefForwardRef() {
  const ref = useRef<HTMLInputElement>(null);
  return (
    <div>
      <FancyInput label="Search" ref={ref} />
      <button
        id="focus"
        onClick={() => {
          ref.current!.focus();
          ref.current!.value = "prefilled";
          log("forwarded to", ref.current!.id);
        }}
      >
        focus
      </button>
    </div>
  );
}
