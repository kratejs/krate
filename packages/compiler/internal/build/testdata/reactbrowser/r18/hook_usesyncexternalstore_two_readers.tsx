import { useEffect, useState, useSyncExternalStore } from "react";
import { log, createStore } from "../lib/log";

const store = createStore({ a: 1, b: 1 });
(window as any).__store = store;

function A() {
  const a = useSyncExternalStore(store.subscribe, () => store.get().a);
  useEffect(() => { log("A effect", a); }, [a]);
  return <span id="a">a={a}</span>;
}
function B() {
  const b = useSyncExternalStore(store.subscribe, () => store.get().b);
  useEffect(() => { log("B effect", b); }, [b]);
  return <span id="b">b={b}</span>;
}

export default function HookUseSyncExternalStoreTwoReaders() {
  const [show, setShow] = useState(true);
  return (
    <div>
      <button id="toggle" onClick={() => setShow(!show)}>toggle</button>
      <button id="inc-a" onClick={() => store.set({ ...store.get(), a: store.get().a + 1 })}>inc a</button>
      <button id="inc-b" onClick={() => store.set({ ...store.get(), b: store.get().b + 1 })}>inc b</button>
      <button id="inc-both" onClick={() => store.set({ a: store.get().a + 1, b: store.get().b + 1 })}>inc both</button>
      <A />
      {show && <B />}
    </div>
  );
}
