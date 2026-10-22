import { useEffect, useRef, useState } from "react";
import { log } from "../lib/log";

function Dialog(props: { onClose: (result: string) => void }) {
  const first = useRef<HTMLInputElement>(null);
  const [name, setName] = useState("");
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    first.current?.focus();
    log("dialog open, focus moved", document.activeElement === first.current);
    return () => {
      previous?.focus();
      log("dialog closed, focus restored to", previous?.id ?? "none");
    };
  }, []);
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="dialog-title"
      id="dialog"
      onKeyDown={(e) => {
        if (e.key === "Escape") props.onClose("cancelled");
      }}
    >
      <h2 id="dialog-title">Rename</h2>
      <input id="dialog-name" ref={first} value={name} onChange={(e) => setName(e.target.value)} />
      <button id="dialog-ok" onClick={() => props.onClose("renamed to " + name)}>
        ok
      </button>
      <button id="dialog-cancel" onClick={() => props.onClose("cancelled")}>
        cancel
      </button>
    </div>
  );
}

// Modal dialog: initial focus inside, Escape closes, focus returns to the
// trigger, result shown on the page.
export default function AppModalFocus() {
  const [open, setOpen] = useState(false);
  const [result, setResult] = useState("");
  return (
    <div>
      <button id="trigger" onClick={() => setOpen(true)}>
        rename
      </button>
      <p id="result">{result}</p>
      {open && (
        <Dialog
          onClose={(r) => {
            setResult(r);
            setOpen(false);
          }}
        />
      )}
    </div>
  );
}
