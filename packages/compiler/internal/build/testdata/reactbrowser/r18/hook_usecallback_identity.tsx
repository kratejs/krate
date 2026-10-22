import { memo, useCallback, useRef, useState } from "react";
import { log } from "../lib/log";

const SaveButton = memo(function SaveButton(props: { onSave: () => void; label: string }) {
  const renders = useRef(0);
  renders.current += 1;
  return (
    <button id={"save-" + props.label} onClick={props.onSave}>
      save {props.label} ({renders.current})
    </button>
  );
});

// useCallback keeps a handler's identity across renders so a memo child
// skips re-rendering; an inline handler forces the child to re-render.
export default function HookUseCallbackIdentity() {
  const [doc, setDoc] = useState("draft");
  const [clock, setClock] = useState(0);
  const stableSave = useCallback(() => log("saved", doc), [doc]);
  const inlineSave = () => log("inline saved", doc);

  return (
    <div>
      <p id="state">
        {doc} @ {clock}
      </p>
      <SaveButton onSave={stableSave} label="stable" />
      <SaveButton onSave={inlineSave} label="inline" />
      <button id="tick" onClick={() => setClock(clock + 1)}>
        tick
      </button>
      <button id="edit" onClick={() => setDoc(doc + "!")}>
        edit
      </button>
    </div>
  );
}
