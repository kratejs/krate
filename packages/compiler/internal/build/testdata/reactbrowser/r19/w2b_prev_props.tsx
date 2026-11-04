import { useState } from "react";

function usePrevious<T>(v: T): T | undefined {
  const [cur, setCur] = useState(v);
  const [prev, setPrev] = useState<T | undefined>(undefined);
  if (cur !== v) {
    setPrev(cur);
    setCur(v);
  }
  return prev;
}

function Counter(props: { value: number }) {
  const [prevValue, setPrevValue] = useState(props.value);
  const [changes, setChanges] = useState(0);
  if (props.value !== prevValue) {
    setPrevValue(props.value);
    setChanges(changes + 1);
  }
  return <p id="changes">changes {changes}</p>;
}

export default function PrevProps() {
  const [n, setN] = useState(0);
  const p = usePrevious(n);
  return (
    <div>
      <button id="inc" onClick={() => setN(n + 1)}>inc</button>
      <p id="prev">{String(p)}</p>
      <Counter value={n} />
    </div>
  );
}
