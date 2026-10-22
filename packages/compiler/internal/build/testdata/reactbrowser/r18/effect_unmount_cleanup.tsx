import { useEffect, useLayoutEffect, useState } from "react";
import { log } from "../lib/log";

function Ticker(props: { id: string }) {
  useLayoutEffect(() => {
    log("layout mount", props.id);
    return () => log("layout unmount", props.id);
  }, []);
  useEffect(() => {
    const timer = setInterval(() => {}, 1000);
    log("passive mount", props.id);
    return () => {
      clearInterval(timer);
      log("passive unmount", props.id);
    };
  }, []);
  return <li data-id={props.id}>ticker {props.id}</li>;
}

// Removing components from a list runs their layout and passive cleanups.
export default function EffectUnmountCleanup() {
  const [ids, setIds] = useState(["a", "b", "c"]);
  return (
    <div>
      <ul>
        {ids.map((id) => (
          <Ticker key={id} id={id} />
        ))}
      </ul>
      <button id="drop-b" onClick={() => setIds(ids.filter((x) => x !== "b"))}>
        drop b
      </button>
      <button id="clear" onClick={() => setIds([])}>
        clear
      </button>
    </div>
  );
}
