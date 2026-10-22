import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Chat(props: { contact: string }) {
  const [draft, setDraft] = useState("");
  useEffect(() => {
    log("open chat", props.contact);
    return () => log("close chat", props.contact);
  }, []);
  return (
    <div>
      <h3 id="contact">{props.contact}</h3>
      <input id="draft" value={draft} onChange={(e) => setDraft(e.target.value)} />
    </div>
  );
}

// Changing a component's key discards its state and remounts it.
export default function KeyChangeRemounts() {
  const [contact, setContact] = useState("alice");
  return (
    <div>
      <Chat key={contact} contact={contact} />
      <button id="bob" onClick={() => setContact("bob")}>
        bob
      </button>
      <button id="alice" onClick={() => setContact("alice")}>
        alice
      </button>
    </div>
  );
}
