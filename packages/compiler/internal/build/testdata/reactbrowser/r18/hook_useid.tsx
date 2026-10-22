import { useEffect, useId, useState } from "react";
import { log } from "../lib/log";

function Field(props: { label: string }) {
  const id = useId();
  const hintId = id + "-hint";
  useEffect(() => {
    const input = document.getElementById(id) as HTMLInputElement | null;
    const label = input ? document.querySelector(`label[for="${CSS.escape(id)}"]`) : null;
    log("field", props.label, "label linked", label !== null, "hint linked", input?.getAttribute("aria-describedby") === hintId);
  }, [id, hintId, props.label]);
  return (
    <div className="field">
      <label htmlFor={id}>{props.label}</label>
      <input id={id} aria-describedby={hintId} />
      <small id={hintId}>required</small>
    </div>
  );
}

// useId gives each instance a unique, stable id usable for label/aria links.
// The id format itself is not compared semantically.
export default function HookUseId() {
  const [extra, setExtra] = useState(false);
  useEffect(() => {
    const ids = [...document.querySelectorAll(".field input")].map((el) => el.id);
    log("unique ids", new Set(ids).size === ids.length, ids.length);
  });
  return (
    <form>
      <Field label="Email" />
      <Field label="Name" />
      {extra && <Field label="Phone" />}
      <button type="button" id="more" onClick={() => setExtra(true)}>
        more
      </button>
    </form>
  );
}
