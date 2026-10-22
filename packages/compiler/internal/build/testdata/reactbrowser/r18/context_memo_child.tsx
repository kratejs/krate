import { createContext, memo, useContext, useState } from "react";
import { log } from "../lib/log";

const Count = createContext(0);

const Display = memo(function Display() {
  const n = useContext(Count);
  log("Display render", n);
  return <p id="display">count {n}</p>;
});

const Static = memo(function Static() {
  log("Static render");
  return <p id="static">static</p>;
});

// A memo child that reads context still re-renders on context changes; a
// memo child without context does not.
export default function ContextMemoChild() {
  const [n, setN] = useState(0);
  return (
    <Count.Provider value={n}>
      <Display />
      <Static />
      <button id="inc" onClick={() => setN(n + 1)}>
        inc
      </button>
    </Count.Provider>
  );
}
