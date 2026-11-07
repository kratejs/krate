import { useEffect, useState } from "react";
import { log } from "../lib/log";

function Pick(props: { v: string; disabled?: boolean; onPick: () => void; extra?: string }) {
  const [n, setN] = useState(0);
  useEffect(() => {
    log("mount pick");
    return () => log("unmount pick");
  }, []);
  return (
    <button className="p" disabled={props.disabled} onClick={() => { setN(n + 1); props.onPick(); }}>
      {props.v}:{n}:{props.extra ?? "-"}
    </button>
  );
}

// A chain of ternaries holding one component type keeps one instance;
// handlers, bare boolean props and props set on one side only switch too.
export default function CondSameTypeChain() {
  const [step, setStep] = useState(0);
  return (
    <div>
      <button id="next" onClick={() => setStep((s) => (s + 1) % 4)}>step {step}</button>
      <section>
        {step === 0 ? (
          <Pick v="zero" onPick={() => log("pick zero")} />
        ) : step === 1 ? (
          <Pick v="one" disabled onPick={() => log("pick one")} extra="x" />
        ) : (
          <Pick v="many" onPick={() => log("pick many")} />
        )}
      </section>
    </div>
  );
}
