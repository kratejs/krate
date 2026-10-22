import { useState } from "react";
import { log } from "../lib/log";

function Toolbar(props: { onAction: (name: string) => void; mode: string }) {
  return (
    <div>
      <button id="act" onClick={() => props.onAction("save")}>
        save ({props.mode})
      </button>
    </div>
  );
}

// A handler passed as a prop is replaced on re-render; the child always
// calls the latest one, which sees the latest state.
export default function EventHandlerPropsChange() {
  const [mode, setMode] = useState("draft");
  const handleDraft = (name: string) => log("draft handler", name, mode);
  const handlePublish = (name: string) => log("publish handler", name, mode);
  return (
    <div>
      <Toolbar mode={mode} onAction={mode === "draft" ? handleDraft : handlePublish} />
      <button id="mode" onClick={() => setMode(mode === "draft" ? "publish" : "draft")}>
        switch mode
      </button>
    </div>
  );
}
