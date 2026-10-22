import { useState } from "react";
import { log } from "../lib/log";

function loadDraft() {
  log("loadDraft");
  return "saved draft";
}

// A function passed to useState runs only for the initial render.
export default function HookUseStateLazyInit() {
  const [draft, setDraft] = useState(loadDraft);
  return (
    <div>
      <textarea id="draft" value={draft} onChange={(e) => setDraft(e.target.value)} />
      <p id="len">{draft.length} chars</p>
    </div>
  );
}
