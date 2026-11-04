import { useEffect, useState } from "react";

type Set = (fn: (s: any) => any) => void;

function create(init: (set: Set, get: () => any) => any) {
  let state: any;
  const listeners = new globalThis.Set<() => void>();
  const set: Set = (fn) => {
    state = { ...state, ...fn(state) };
    listeners.forEach((l) => l());
  };
  const get = () => state;
  state = init(set, get);
  function useStore<T>(selector: (s: any) => T = (s) => s): T {
    const [, force] = useState(0);
    useEffect(() => {
      const l = () => force((x) => x + 1);
      listeners.add(l);
      return () => {
        listeners.delete(l);
      };
    }, []);
    return selector(state);
  }
  return Object.assign(useStore, { getState: get, setState: set });
}

const useCounter = create((set) => ({
  count: 0,
  inc: () => set((s) => ({ count: s.count + 1 })),
}));

function Display() {
  const count = useCounter((s) => s.count);
  return <p id="count">count {count}</p>;
}

function Controls() {
  const inc = useCounter((s) => s.inc);
  return (
    <div>
      <button id="inc" onClick={inc}>inc</button>
      <button id="ext" onClick={() => useCounter.setState((s) => ({ count: s.count + 10 }))}>ext</button>
    </div>
  );
}

export default function StoreZustand() {
  return (
    <div>
      <Display />
      <Controls />
    </div>
  );
}
