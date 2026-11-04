import { useState } from "react";

function Detail(props: { id: string }) {
  return <p id={"detail-" + props.id}>detail {props.id}</p>;
}

function Section(props: { id: string; open: boolean; onToggle: (id: string) => void }) {
  return (
    <div id={"sec-" + props.id}>
      <button id={"toggle-" + props.id} onClick={() => props.onToggle(props.id)}>
        toggle {props.id}
      </button>
      <span className="raw">[{props.open}]</span>
      {props.open && <Detail id={props.id} />}
    </div>
  );
}

// A boolean child renders nothing (true as well as false), also when it
// changes after hydration.
export default function BoolChildContent() {
  const [open, setOpen] = useState("");
  const toggle = (id: string) => setOpen(open === id ? "" : id);
  return (
    <main>
      <Section id="a" open={open === "a"} onToggle={toggle} />
      <Section id="b" open={open === "b"} onToggle={toggle} />
    </main>
  );
}
