import { useRef, useState } from "react";

// React docs: do not read ref.current during rendering. Krate renders the
// value once and does not refresh it, so the build must report KR5008.
export default function DiagRefReadInRender() {
  const clicks = useRef(0);
  const [, setTick] = useState(0);
  return (
    <div>
      <p id="live">ref at render {clicks.current}</p>
      <button
        id="bump"
        onClick={() => {
          clicks.current += 1;
          setTick((t) => t + 1);
        }}
      >
        bump
      </button>
    </div>
  );
}
