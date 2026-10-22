import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { log } from "../lib/log";

function Modal(props: { onClose: () => void; children?: any }) {
  useEffect(() => {
    log("modal mounted");
    return () => log("modal unmounted");
  }, []);
  return createPortal(
    <div id="backdrop" className="backdrop">
      <div role="dialog" aria-modal="true" className="modal">
        {props.children}
        <button id="close" onClick={props.onClose}>
          close
        </button>
      </div>
    </div>,
    document.body,
  );
}

// A modal mounted into body through a portal and removed again on close.
export default function PortalModalToggle() {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <button id="open" onClick={() => setOpen(true)}>
        open
      </button>
      {open && <Modal onClose={() => setOpen(false)}>Are you sure?</Modal>}
    </div>
  );
}
