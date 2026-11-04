import { useState } from "react";

type S = { n: number; inc: () => void };

function create(init: (set: (p: Partial<S>) => void, get: () => S) => S) {
  let state: S;
  const listeners = new Set<() => void>();
  const set = (p: Partial<S>) => { state = { ...state, ...p }; listeners.forEach((l) => l()); };
  const get = () => state;
  state = init(set, get);
  function useStore(): S {
    const [, force] = useState(0);
    useState(() => { listeners.add(() => force((x) => x + 1)); return 0; });
    return state;
  }
  return { api: { set, get }, useStore };
}

const { api, useStore } = create((set, get) => ({ n: 0, inc: () => set({ n: get().n + 1 }) }));

export default function StoreFactory() {
  const s = useStore();
  return (
    <div>
      <button id="inc" onClick={s.inc}>inc</button>
      <p id="n">{s.n}</p>
      <button id="ext" onClick={() => api.set({ n: 10 })}>ext</button>
    </div>
  );
}
