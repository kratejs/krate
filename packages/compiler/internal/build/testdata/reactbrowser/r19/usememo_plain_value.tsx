import { useMemo, useState } from "react";

// A memoized value used as plain render text updates when its dependency does.
export default function UseMemoPlainValue() {
  const [n, setN] = useState(1);
  const d = useMemo(() => n * 2, [n]);
  return (
    <div>
      <p id="d">{d}</p>
      <button id="inc" onClick={() => setN(n + 1)}>
        inc
      </button>
    </div>
  );
}
