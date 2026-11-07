import { createContext, memo, useContext, useEffect, useState } from "react";
import { log } from "../lib/log";

const Ctx = createContext("default");

function Consumer(props: { name: string }) {
  const v = useContext(Ctx);
  useEffect(() => {
    log("consumer effect", props.name, v);
    return () => log("consumer cleanup", props.name, v);
  }, [v]);
  return <span className="consumer">{props.name}={v}</span>;
}

const Memoed = memo(function Memoed() {
  return <Consumer name="memo" />;
});

export default function HuntContextUnmountedConsumer() {
  const [v, setV] = useState("one");
  const [showA, setShowA] = useState(true);
  return (
    <div>
      <button id="v" onClick={() => setV(v === "one" ? "two" : "one")}>v</button>
      <button id="a" onClick={() => setShowA(!showA)}>a</button>
      <button id="both" onClick={() => { setV("three"); setShowA(false); }}>both</button>
      <Ctx.Provider value={v}>
        {showA && <Consumer name="a" />}
        <Memoed />
      </Ctx.Provider>
      <Consumer name="outside" />
    </div>
  );
}
