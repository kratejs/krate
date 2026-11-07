import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Card(props: { title: string; children?: any }) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    log("mount card", props.title);
    return () => log("unmount card", props.title);
  }, []);
  return (
    <div className="card">
      <button className="t" onClick={() => setOpen(!open)}>{props.title}:{open ? "open" : "closed"}</button>
      <div className="body">{props.children}</div>
    </div>
  );
}

// Different children switch with the condition; the instance stays.
export default function CondSameTypeChildren() {
  const [alt, setAlt] = useState(false);
  return (
    <div>
      <button id="flip" onClick={() => setAlt((v) => !v)}>flip {String(alt)}</button>
      <section>
        {alt ? (
          <Card title="B"><p>second</p><i>extra</i></Card>
        ) : (
          <Card title="A"><p>first</p></Card>
        )}
      </section>
      <section>
        {alt ? <Card title="C">only when alt</Card> : <Card title="C" />}
      </section>
    </div>
  );
}
