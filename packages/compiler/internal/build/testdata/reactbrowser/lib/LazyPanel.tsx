import { useState } from "react";

// Code-split panel loaded with React.lazy by the Suspense fixtures.
export default function LazyPanel(props: { title: string }) {
  const [open, setOpen] = useState(true);
  return (
    <section className="lazy-panel">
      <h3>{props.title}</h3>
      {open && <p>lazy content</p>}
      <button className="lazy-toggle" onClick={() => setOpen(!open)}>
        {open ? "collapse" : "expand"}
      </button>
    </section>
  );
}
